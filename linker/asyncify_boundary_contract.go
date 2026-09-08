package linker

import "github.com/wippyai/wasm-runtime/wasm"

// validateAsyncifyBoundaryContract bounds the component execution topology to
// edges that carry a per-core continuation controller. Function imports are
// wrapped by the linker, but a call_indirect through an imported guest table
// can enter another core without crossing any wrapper.
//
// Table-only initializer modules remain supported: with no defined functions,
// they cannot execute an indirect call or install a reference to their own
// executable code. Their element segments refer only to imported functions,
// which the linker supplies through its function boundaries. This covers the
// canonical adapter fixup pattern by structure, independent of names.
//
// Executable cores may own private tables. Executable cores importing tables
// require an additional function-entry continuation protocol and are currently
// rejected, even when a particular program would use its imported table only
// synchronously. This is an execution-profile limit, not invalid WebAssembly.
func validateAsyncifyBoundaryContract(modules [][]byte) error {
	for index, data := range modules {
		module, err := wasm.ParseModuleMetadata(data)
		if err != nil {
			return instError("asyncify_profile", index, "", "parse source module metadata", err)
		}
		if len(module.Funcs) != 0 && module.NumImportedTables() != 0 {
			return instError("asyncify_profile", index, "", "executable core importing a table requires an unsupported cross-core continuation boundary", nil)
		}
		// A reference returned by another core can be installed into a private
		// table and invoked without a bridge too. Close every reference-valued
		// import channel, not just the shared-table channel. Numeric handles
		// used by the canonical ABI do not transport executable references.
		for _, imp := range module.Imports {
			switch imp.Desc.Kind {
			case wasm.KindFunc:
				signature := module.GetFuncTypeByTypeIndex(imp.Desc.TypeIdx)
				if signature == nil {
					return instError("asyncify_profile", index, imp.Name, "unresolved core function import type", nil)
				}
				if !numericBoundaryValues(signature.Params, signature.ExtParams) || !numericBoundaryValues(signature.Results, signature.ExtResults) {
					return instError("asyncify_profile", index, imp.Name, "reference-valued core function import requires an unsupported cross-core continuation boundary", nil)
				}
			case wasm.KindGlobal:
				if imp.Desc.Global == nil || !numericBoundaryValue(imp.Desc.Global.ValType) || (imp.Desc.Global.ExtType != nil && imp.Desc.Global.ExtType.Kind == wasm.ExtValKindRef) {
					return instError("asyncify_profile", index, imp.Name, "reference-valued core global import requires an unsupported cross-core continuation boundary", nil)
				}
			}
		}
	}
	return nil
}

func numericBoundaryValues(values []wasm.ValType, extended []wasm.ExtValType) bool {
	for _, value := range values {
		if !numericBoundaryValue(value) {
			return false
		}
	}
	for _, value := range extended {
		if value.Kind != wasm.ExtValKindSimple || !numericBoundaryValue(value.ValType) {
			return false
		}
	}
	return true
}

func numericBoundaryValue(value wasm.ValType) bool {
	switch value {
	case wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64, wasm.ValV128:
		return true
	default:
		return false
	}
}
