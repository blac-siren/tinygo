package transform

import (
	"github.com/tinygo-org/tinygo/compileopts"
	"tinygo.org/x/go-llvm"
)

// CreateStackSizeLoads replaces internal/task.getGoroutineStackSize calls with
// loads from internal/task.stackSizes that will be updated after linking. This
// way the stack sizes are loaded from a separate section and can easily be
// modified after linking.
func CreateStackSizeLoads(mod llvm.Module, config *compileopts.Config) []string {
	functionMap := map[llvm.Value][]llvm.Value{}
	var functions []llvm.Value
	var functionNames []string
	for _, use := range getUses(mod.NamedFunction("internal/task.getGoroutineStackSize")) {
		if use.FirstUse().IsNil() {
			// Apparently this stack size isn't used.
			use.EraseFromParentAsInstruction()
			continue
		}
		ptrtoint := use.Operand(0)
		if _, ok := functionMap[ptrtoint]; !ok {
			functions = append(functions, ptrtoint)
			functionNames = append(functionNames, ptrtoint.Operand(0).Name())
		}
		functionMap[ptrtoint] = append(functionMap[ptrtoint], use)
	}

	if len(functions) == 0 {
		// Nothing to do.
		return nil
	}

	// Create the new global with stack sizes, that will be put in a new section
	// just for itself.
	stackSizesGlobalType := llvm.ArrayType(functions[0].Type(), len(functions))
	stackSizesGlobal := llvm.AddGlobal(mod, stackSizesGlobalType, "internal/task.stackSizes")
	stackSizesGlobal.SetSection(".tinygo_stacksizes")
	defaultStackSizes := make([]llvm.Value, len(functions))
	defaultStackSize := llvm.ConstInt(functions[0].Type(), config.Target.DefaultStackSize, false)
	for i := range defaultStackSizes {
		defaultStackSizes[i] = defaultStackSize
	}
	stackSizesGlobal.SetInitializer(llvm.ConstArray(functions[0].Type(), defaultStackSizes))

	// Replace the calls with loads from the new global with stack sizes.
	irbuilder := mod.Context().NewBuilder()
	defer irbuilder.Dispose()
	for i, function := range functions {
		for _, use := range functionMap[function] {
			ptr := llvm.ConstGEP(stackSizesGlobal, []llvm.Value{
				llvm.ConstInt(mod.Context().Int32Type(), 0, false),
				llvm.ConstInt(mod.Context().Int32Type(), uint64(i), false),
			})
			irbuilder.SetInsertPointBefore(use)
			stacksize := irbuilder.CreateLoad(ptr, "stacksize")
			use.ReplaceAllUsesWith(stacksize)
			use.EraseFromParentAsInstruction()
		}
	}

	return functionNames
}
