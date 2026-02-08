package wasm

import "fmt"

// Validate checks the module for structural validity.
func (m *Module) Validate() error {
	if err := m.validateTypeIndices(); err != nil {
		return err
	}
	if err := m.validateFunctionIndices(); err != nil {
		return err
	}
	if err := m.validateTableIndices(); err != nil {
		return err
	}
	if err := m.validateMemoryIndices(); err != nil {
		return err
	}
	if err := m.validateGlobalIndices(); err != nil {
		return err
	}
	if err := m.validateTagIndices(); err != nil {
		return err
	}
	if err := m.validateExports(); err != nil {
		return err
	}
	if err := m.validateStart(); err != nil {
		return err
	}
	if err := m.validateDataCount(); err != nil {
		return err
	}
	if err := m.validateCodeCount(); err != nil {
		return err
	}
	if err := m.validateMemoryLimits(); err != nil {
		return err
	}
	return nil
}

// ParseModuleValidate parses a WebAssembly binary and validates it.
// This is a convenience function combining ParseModule and Validate.
func ParseModuleValidate(data []byte) (*Module, error) {
	m, err := ParseModule(data)
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Module) validateTypeIndices() error {
	numTypes := uint32(m.NumTypes())
	if numTypes == 0 {
		// No types defined, but check if anything references types
		if len(m.Funcs) > 0 {
			return fmt.Errorf("function references type but no types defined")
		}
		return nil
	}

	// Check function type indices
	for i, typeIdx := range m.Funcs {
		if typeIdx >= numTypes {
			return fmt.Errorf("function %d references invalid type index %d (max %d)", i, typeIdx, numTypes-1)
		}
	}

	// Check import type indices
	for i, imp := range m.Imports {
		if imp.Desc.Kind == KindFunc {
			if imp.Desc.TypeIdx >= numTypes {
				return fmt.Errorf("import %d (%s.%s) references invalid type index %d", i, imp.Module, imp.Name, imp.Desc.TypeIdx)
			}
		}
		if imp.Desc.Kind == KindTag && imp.Desc.Tag != nil {
			if imp.Desc.Tag.TypeIdx >= numTypes {
				return fmt.Errorf("import %d (%s.%s) tag references invalid type index %d", i, imp.Module, imp.Name, imp.Desc.Tag.TypeIdx)
			}
		}
	}

	// Check tag type indices
	for i, tag := range m.Tags {
		if tag.TypeIdx >= numTypes {
			return fmt.Errorf("tag %d references invalid type index %d", i, tag.TypeIdx)
		}
	}

	return nil
}

func (m *Module) validateFunctionIndices() error {
	numFuncs := uint32(m.NumImportedFuncs() + len(m.Funcs))

	// Check start function
	if m.Start != nil && *m.Start >= numFuncs {
		return fmt.Errorf("start function index %d exceeds function count %d", *m.Start, numFuncs)
	}

	// Check element function indices
	for i, elem := range m.Elements {
		for j, funcIdx := range elem.FuncIdxs {
			if funcIdx >= numFuncs {
				return fmt.Errorf("element %d, entry %d references invalid function index %d", i, j, funcIdx)
			}
		}
	}

	// Check export function indices
	for i, exp := range m.Exports {
		if exp.Kind == KindFunc && exp.Idx >= numFuncs {
			return fmt.Errorf("export %d (%s) references invalid function index %d", i, exp.Name, exp.Idx)
		}
	}

	return nil
}

func (m *Module) validateTableIndices() error {
	numTables := uint32(m.NumImportedTables() + len(m.Tables))

	// Check element table indices (only for active segments)
	for i, elem := range m.Elements {
		// Passive (flags & 1) and declarative (flags == 3, 7) segments don't reference tables
		isPassive := elem.Flags&0x01 != 0
		if !isPassive && elem.TableIdx >= numTables {
			return fmt.Errorf("element %d references invalid table index %d", i, elem.TableIdx)
		}
	}

	// Check export table indices
	for i, exp := range m.Exports {
		if exp.Kind == KindTable && exp.Idx >= numTables {
			return fmt.Errorf("export %d (%s) references invalid table index %d", i, exp.Name, exp.Idx)
		}
	}

	return nil
}

func (m *Module) validateMemoryIndices() error {
	numMemories := uint32(m.NumImportedMemories() + len(m.Memories))

	// Check data segment memory indices (only for active segments)
	for i, data := range m.Data {
		// Passive segments (flags == 1) don't reference memory
		if data.Flags != 1 && data.MemIdx >= numMemories {
			return fmt.Errorf("data segment %d references invalid memory index %d", i, data.MemIdx)
		}
	}

	// Check export memory indices
	for i, exp := range m.Exports {
		if exp.Kind == KindMemory && exp.Idx >= numMemories {
			return fmt.Errorf("export %d (%s) references invalid memory index %d", i, exp.Name, exp.Idx)
		}
	}

	return nil
}

func (m *Module) validateGlobalIndices() error {
	numGlobals := uint32(m.NumImportedGlobals() + len(m.Globals))

	// Check export global indices
	for i, exp := range m.Exports {
		if exp.Kind == KindGlobal && exp.Idx >= numGlobals {
			return fmt.Errorf("export %d (%s) references invalid global index %d", i, exp.Name, exp.Idx)
		}
	}

	return nil
}

func (m *Module) validateTagIndices() error {
	numTags := uint32(m.NumImportedTags() + len(m.Tags))

	// Check export tag indices
	for i, exp := range m.Exports {
		if exp.Kind == KindTag && exp.Idx >= numTags {
			return fmt.Errorf("export %d (%s) references invalid tag index %d", i, exp.Name, exp.Idx)
		}
	}

	return nil
}

func (m *Module) validateExports() error {
	seen := make(map[string]bool)
	for i, exp := range m.Exports {
		if seen[exp.Name] {
			return fmt.Errorf("duplicate export name %q at index %d", exp.Name, i)
		}
		seen[exp.Name] = true
	}
	return nil
}

func (m *Module) validateStart() error {
	if m.Start == nil {
		return nil
	}

	funcType := m.GetFuncType(*m.Start)
	if funcType == nil {
		return fmt.Errorf("start function %d has no type", *m.Start)
	}

	if len(funcType.Params) != 0 || len(funcType.Results) != 0 {
		return fmt.Errorf("start function must have signature [] -> [], got [%d params] -> [%d results]",
			len(funcType.Params), len(funcType.Results))
	}

	return nil
}

func (m *Module) validateDataCount() error {
	if m.DataCount != nil && *m.DataCount != uint32(len(m.Data)) {
		return fmt.Errorf("data count section declares %d segments, but data section has %d",
			*m.DataCount, len(m.Data))
	}
	return nil
}

func (m *Module) validateCodeCount() error {
	// Code section must have same count as function section when both exist
	if len(m.Code) > 0 && len(m.Code) != len(m.Funcs) {
		return fmt.Errorf("code section has %d entries but function section has %d",
			len(m.Code), len(m.Funcs))
	}
	return nil
}

func (m *Module) validateMemoryLimits() error {
	// Validate imported memories
	for i, imp := range m.Imports {
		if imp.Desc.Kind == KindMemory && imp.Desc.Memory != nil {
			if err := validateMemoryType(imp.Desc.Memory, i, true); err != nil {
				return err
			}
		}
	}
	// Validate declared memories
	for i := range m.Memories {
		if err := validateMemoryType(&m.Memories[i], i, false); err != nil {
			return err
		}
	}
	return nil
}

func validateMemoryType(mem *MemoryType, idx int, isImport bool) error {
	var maxPages uint64
	if mem.Limits.Memory64 {
		maxPages = MemoryMaxPages64
	} else {
		maxPages = MemoryMaxPages32
	}

	prefix := "memory"
	if isImport {
		prefix = "imported memory"
	}

	// Shared memory requires maximum limit
	if mem.Limits.Shared && mem.Limits.Max == nil {
		return fmt.Errorf("%s %d: shared memory must have maximum limit", prefix, idx)
	}

	if mem.Limits.Min > maxPages {
		return fmt.Errorf("%s %d: min pages %d exceeds maximum %d",
			prefix, idx, mem.Limits.Min, maxPages)
	}
	if mem.Limits.Max != nil && *mem.Limits.Max > maxPages {
		return fmt.Errorf("%s %d: max pages %d exceeds maximum %d",
			prefix, idx, *mem.Limits.Max, maxPages)
	}
	return nil
}
