package engine

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/wasm"
)

// HelperBuilder constructs asyncify helper functions.
//
// These are the runtime support functions that the host calls to control
// the asyncify state machine: start/stop unwind/rewind, get state.
type HelperBuilder struct {
	globals     GlobalIndices
	pointerSize int // 4 for wasm32, 8 for wasm64
}

// NewHelperBuilder creates a builder for the given global indices (wasm32).
func NewHelperBuilder(globals GlobalIndices) *HelperBuilder {
	return &HelperBuilder{globals: globals, pointerSize: 4}
}

// NewHelperBuilder64 creates a builder for wasm64 modules.
func NewHelperBuilder64(globals GlobalIndices) *HelperBuilder {
	return &HelperBuilder{globals: globals, pointerSize: 8}
}

// BuildGetState creates the asyncify_get_state function.
//
// Simply returns the current state global value.
func (h *HelperBuilder) BuildGetState() []byte {
	em := codegen.NewEmitter()
	em.GlobalGet(h.globals.StateGlobal).End()
	return em.Bytes()
}

// BuildStartUnwind creates the asyncify_start_unwind function.
//
// Sets state to unwinding (1) and stores the data pointer.
// Validates that state is Normal before transitioning.
// Validates that stack_ptr <= stack_end, traps if overflow.
// Takes one i32/i64 parameter: the data buffer address.
func (h *HelperBuilder) BuildStartUnwind() []byte {
	em := codegen.NewEmitter()

	// Validate: must be in Normal state (trap if not)
	em.GlobalGet(h.globals.StateGlobal).
		I32Const(StateNormal).
		I32Ne().
		If(codegen.BlockVoid).Unreachable().End()

	em.I32Const(StateUnwinding).GlobalSet(h.globals.StateGlobal)
	em.LocalGet(0).GlobalSet(h.globals.DataGlobal)

	// Validate: stack_ptr <= stack_end (unsigned comparison)
	// Data layout: [stack_ptr, stack_end, ...]
	h.emitStackValidation(em)

	em.End()
	return em.Bytes()
}

// BuildStopUnwind creates the asyncify_stop_unwind function.
//
// Sets state back to normal (0) after unwinding completes.
// Validates that state is Unwinding before transitioning.
// Validates stack_ptr <= stack_end to detect corruption during unwind.
func (h *HelperBuilder) BuildStopUnwind() []byte {
	em := codegen.NewEmitter()

	// Validate: must be in Unwinding state (trap if not)
	em.GlobalGet(h.globals.StateGlobal).
		I32Const(StateUnwinding).
		I32Ne().
		If(codegen.BlockVoid).Unreachable().End()

	em.I32Const(StateNormal).GlobalSet(h.globals.StateGlobal)

	// Post-validation: detect if stack was corrupted during unwind
	h.emitStackValidation(em)

	em.End()
	return em.Bytes()
}

// BuildStartRewind creates the asyncify_start_rewind function.
//
// Sets state to rewinding (2) and stores the data pointer.
// Validates that state is Normal before transitioning.
// Validates that stack_ptr <= stack_end, traps if overflow.
// Takes one i32/i64 parameter: the data buffer address.
func (h *HelperBuilder) BuildStartRewind() []byte {
	em := codegen.NewEmitter()

	// Validate: must be in Normal state (trap if not)
	em.GlobalGet(h.globals.StateGlobal).
		I32Const(StateNormal).
		I32Ne().
		If(codegen.BlockVoid).Unreachable().End()

	em.I32Const(StateRewinding).GlobalSet(h.globals.StateGlobal)
	em.LocalGet(0).GlobalSet(h.globals.DataGlobal)

	// Validate: stack_ptr <= stack_end (unsigned comparison)
	h.emitStackValidation(em)

	em.End()
	return em.Bytes()
}

// BuildStopRewind creates the asyncify_stop_rewind function.
//
// Sets state back to normal (0) after rewinding completes.
// Validates that state is Rewinding before transitioning.
// Validates stack_ptr <= stack_end to detect corruption during rewind.
func (h *HelperBuilder) BuildStopRewind() []byte {
	em := codegen.NewEmitter()

	// Validate: must be in Rewinding state (trap if not)
	em.GlobalGet(h.globals.StateGlobal).
		I32Const(StateRewinding).
		I32Ne().
		If(codegen.BlockVoid).Unreachable().End()

	em.I32Const(StateNormal).GlobalSet(h.globals.StateGlobal)

	// Post-validation: detect if stack was corrupted during rewind
	h.emitStackValidation(em)

	em.End()
	return em.Bytes()
}

// emitStackValidation emits code to check stack_ptr <= stack_end and trap if exceeded.
// Data layout: [stack_ptr at offset 0, stack_end at offset ptrSize]
func (h *HelperBuilder) emitStackValidation(em *codegen.Emitter) {
	if h.pointerSize == 8 {
		// wasm64: i64 pointers
		em.GlobalGet(h.globals.DataGlobal).I64Load(3, 0) // stack_ptr
		em.GlobalGet(h.globals.DataGlobal).I64Load(3, 8) // stack_end
		em.EmitRawOpcode(wasm.OpI64GtU)                  // stack_ptr > stack_end
	} else {
		// wasm32: i32 pointers
		em.GlobalGet(h.globals.DataGlobal).I32Load(2, 0) // stack_ptr
		em.GlobalGet(h.globals.DataGlobal).I32Load(2, 4) // stack_end
		em.I32GtU()                                      // stack_ptr > stack_end
	}
	em.If(codegen.BlockVoid).Unreachable().End()
}

// ExportManager handles adding asyncify exports to a module.
type ExportManager struct {
	module  *wasm.Module
	globals GlobalIndices
	wasm64  bool
}

// NewExportManager creates an export manager for the given module.
func NewExportManager(m *wasm.Module, globals GlobalIndices) *ExportManager {
	return &ExportManager{module: m, globals: globals, wasm64: false}
}

// NewExportManager64 creates an export manager for wasm64 modules.
func NewExportManager64(m *wasm.Module, globals GlobalIndices) *ExportManager {
	return &ExportManager{module: m, globals: globals, wasm64: true}
}

// AddAsyncifyExports adds all asyncify helper function exports.
func (em *ExportManager) AddAsyncifyExports() {
	var builder *HelperBuilder
	if em.wasm64 {
		builder = NewHelperBuilder64(em.globals)
	} else {
		builder = NewHelperBuilder(em.globals)
	}

	// asyncify_get_state: () -> i32
	getStateType := em.ensureFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}})
	getStateIdx := em.addFunction(getStateType, builder.BuildGetState())
	em.addExport("asyncify_get_state", getStateIdx)

	// Pointer type for data parameter
	ptrType := wasm.ValI32
	if em.wasm64 {
		ptrType = wasm.ValI64
	}

	// asyncify_start_unwind: (ptr) -> ()
	ptrVoidType := em.ensureFuncType(wasm.FuncType{Params: []wasm.ValType{ptrType}})
	startUnwindIdx := em.addFunction(ptrVoidType, builder.BuildStartUnwind())
	em.addExport("asyncify_start_unwind", startUnwindIdx)

	// asyncify_stop_unwind: () -> ()
	voidVoidType := em.ensureFuncType(wasm.FuncType{})
	stopUnwindIdx := em.addFunction(voidVoidType, builder.BuildStopUnwind())
	em.addExport("asyncify_stop_unwind", stopUnwindIdx)

	// asyncify_start_rewind: (ptr) -> ()
	startRewindIdx := em.addFunction(ptrVoidType, builder.BuildStartRewind())
	em.addExport("asyncify_start_rewind", startRewindIdx)

	// asyncify_stop_rewind: () -> ()
	stopRewindIdx := em.addFunction(voidVoidType, builder.BuildStopRewind())
	em.addExport("asyncify_stop_rewind", stopRewindIdx)
}

func (em *ExportManager) ensureFuncType(ft wasm.FuncType) uint32 {
	for i, t := range em.module.Types {
		if funcTypesEqual(t, ft) {
			return uint32(i)
		}
	}
	em.module.Types = append(em.module.Types, ft)
	return uint32(len(em.module.Types) - 1)
}

func (em *ExportManager) addFunction(typeIdx uint32, code []byte) uint32 {
	em.module.Funcs = append(em.module.Funcs, typeIdx)
	em.module.Code = append(em.module.Code, wasm.FuncBody{Code: code})
	return uint32(em.module.NumImportedFuncs() + len(em.module.Code) - 1)
}

func (em *ExportManager) addExport(name string, funcIdx uint32) {
	em.module.Exports = append(em.module.Exports, wasm.Export{
		Name: name,
		Kind: 0, // function
		Idx:  funcIdx,
	})
}

func funcTypesEqual(a, b wasm.FuncType) bool {
	if len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] {
			return false
		}
	}
	for i := range a.Results {
		if a.Results[i] != b.Results[i] {
			return false
		}
	}
	return true
}
