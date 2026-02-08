package component

import (
	"fmt"
	"strings"

	"go.bytecodealliance.org/wit"
)

// CanonRegistry maps export/import names to their canonical ABI definitions
type CanonRegistry struct {
	Lifts    map[string]*LiftDef
	Lowers   map[string]*LowerDef
	resolver *TypeResolver

	// liftByFuncIdx maps component function index to lift definition
	liftByFuncIdx map[uint32]*LiftDef
}

// LiftDef describes a canon lift (core wasm func -> component export)
type LiftDef struct {
	Name        string
	Params      []wit.Type
	Results     []wit.Type
	ParamNames  []string
	CoreFuncIdx uint32
	TypeIdx     uint32
	MemoryIdx   uint32
	ReallocIdx  int32
}

// LowerDef describes a canon lower (component import -> core wasm func)
type LowerDef struct {
	Name       string
	Params     []wit.Type
	Results    []wit.Type
	ParamNames []string
	FuncIdx    uint32
	MemoryIdx  uint32
	ReallocIdx int32
	IsAsync    bool
}

// NewCanonRegistry builds a registry by processing canons in section order.
// Section order matters: func aliases, canon lifts, and func exports all
// contribute to the component function index space and must be processed
// in their binary appearance order.
func NewCanonRegistry(comp *Component, resolver *TypeResolver) (*CanonRegistry, error) {
	reg := &CanonRegistry{
		Lifts:         make(map[string]*LiftDef),
		Lowers:        make(map[string]*LowerDef),
		resolver:      resolver,
		liftByFuncIdx: make(map[uint32]*LiftDef),
	}

	compFuncIdx := uint32(0)
	liftFuncIndices := make(map[int]uint32) // maps canon index to func index

	for _, section := range comp.SectionOrder {
		switch section.Kind {
		case SectionAlias:
			// Process aliases in this section
			for i := section.StartIndex; i < section.StartIndex+section.Count; i++ {
				if i < len(comp.Aliases) && comp.Aliases[i].Parsed != nil {
					alias := comp.Aliases[i].Parsed
					// Func aliases (Sort=0x01) add to component func space
					if alias.Sort == 0x01 {
						compFuncIdx++
					}
				}
			}
		case SectionCanon:
			// Process canons in this section
			for i := section.StartIndex; i < section.StartIndex+section.Count; i++ {
				if i < len(comp.Canons) && comp.Canons[i].Parsed != nil {
					canon := comp.Canons[i].Parsed
					// Canon lift adds to component func space
					if canon.Kind == CanonLift {
						liftFuncIndices[i] = compFuncIdx
						compFuncIdx++
					}
				}
			}
		case SectionExport:
			// Process exports in this section
			// Func exports (Sort=0x01) add to component func space
			for i := section.StartIndex; i < section.StartIndex+section.Count; i++ {
				if i < len(comp.Exports) {
					exp := comp.Exports[i]
					if exp.Sort == 0x01 { // Func export
						compFuncIdx++
					}
				}
			}
		}
	}

	// Now process canons with correct func index mapping
	for i, canon := range comp.Canons {
		if canon.Parsed == nil {
			continue
		}

		switch canon.Parsed.Kind {
		case CanonLift:
			funcIdx := liftFuncIndices[i]
			if err := reg.processLiftWithFuncIdx(comp, canon.Parsed, funcIdx); err != nil {
				return nil, err
			}
		case CanonLower:
			if err := reg.processLower(comp, canon.Parsed); err != nil {
				return nil, err
			}
		}
	}

	return reg, nil
}

func (r *CanonRegistry) processLiftWithFuncIdx(comp *Component, canon *CanonDef, compFuncIdx uint32) error {
	ft, err := r.resolver.ResolveFuncType(canon.TypeIndex)
	if err != nil {
		return fmt.Errorf("lift: %w", err)
	}

	params := make([]wit.Type, len(ft.Params))
	paramNames := make([]string, len(ft.Params))
	for i, p := range ft.Params {
		params[i], err = r.resolver.Resolve(p.Type)
		if err != nil {
			return fmt.Errorf("lift param %q: %w", p.Name, err)
		}
		paramNames[i] = p.Name
	}

	var results []wit.Type
	if ft.Result != nil {
		result, err := r.resolver.Resolve(*ft.Result)
		if err != nil {
			return fmt.Errorf("lift result: %w", err)
		}
		results = []wit.Type{result}
	}

	// Find export name - first try from core func export name (for instance exports)
	// then fall back to component function export
	name := ""
	if int(canon.FuncIndex) < len(comp.CoreFuncIndexSpace) {
		entry := comp.CoreFuncIndexSpace[canon.FuncIndex]
		if entry.Kind == CoreFuncAliasExport && entry.ExportName != "" && strings.Contains(entry.ExportName, "#") {
			name = entry.ExportName
		}
	}
	if name == "" {
		name = r.findExportNameByFuncIdx(comp, compFuncIdx)
	}

	lift := &LiftDef{
		Name:        name,
		CoreFuncIdx: canon.FuncIndex,
		TypeIdx:     canon.TypeIndex,
		Params:      params,
		Results:     results,
		ParamNames:  paramNames,
		MemoryIdx:   canon.GetMemoryIndex(),
		ReallocIdx:  canon.GetReallocIndex(),
	}

	r.Lifts[name] = lift
	r.liftByFuncIdx[compFuncIdx] = lift
	return nil
}

func (r *CanonRegistry) processLower(comp *Component, canon *CanonDef) error {
	name := r.findImportName(comp, canon.FuncIndex)
	if name == "" {
		name = fmt.Sprintf("lower_%d", canon.FuncIndex)
	}

	isAsync := false
	for _, opt := range canon.Options {
		if opt.Kind == CanonOptAsync {
			isAsync = true
			break
		}
	}

	lower := &LowerDef{
		Name:       name,
		FuncIdx:    canon.FuncIndex,
		MemoryIdx:  canon.GetMemoryIndex(),
		ReallocIdx: canon.GetReallocIndex(),
		IsAsync:    isAsync,
	}

	// Resolve function type from the component function index space
	if int(canon.FuncIndex) < len(comp.FuncIndexSpace) {
		funcEntry := comp.FuncIndexSpace[canon.FuncIndex]

		// Get the instance type
		if int(funcEntry.InstanceIdx) < len(comp.InstanceTypes) {
			typeIdx := comp.InstanceTypes[funcEntry.InstanceIdx]

			// Look up the instance type
			if int(typeIdx) < len(comp.TypeIndexSpace) {
				if instType, ok := comp.TypeIndexSpace[typeIdx].(*InstanceType); ok {
					// Find the function export in the instance type
					funcType, internalTypes := r.findFuncInInstanceType(instType, funcEntry.ExportName)
					if funcType != nil {
						// Resolve the function type using instance-internal type context
						params, result, err := r.resolver.ResolveFuncWithInternalTypes(funcType, internalTypes)
						if err == nil {
							// Method function types in component binary already include self param
							lower.Params = params
							lower.ParamNames = make([]string, len(funcType.Params))
							for i, p := range funcType.Params {
								lower.ParamNames[i] = p.Name
							}
							if result != nil {
								lower.Results = []wit.Type{result}
							}
						}
					}
					// For resource methods without full type info, apply defaults
					if (strings.Contains(name, "[method]") || strings.Contains(name, "[static]")) && len(lower.Results) == 0 {
						// Common patterns for resource methods
						if strings.Contains(name, ".ready") || strings.Contains(name, ".is-") ||
							strings.Contains(name, ".has-") || strings.Contains(name, ".check") {
							lower.Results = []wit.Type{wit.Bool{}}
						}
					}
				}
			}
		}
	}

	r.Lowers[name] = lower
	return nil
}

// findFuncInInstanceType finds a function type and builds the internal type
// index space for resolution. Returns nil if not found.
func (r *CanonRegistry) findFuncInInstanceType(instType *InstanceType, exportName string) (*FuncType, map[uint32]Type) {
	internalTypes := make(map[uint32]Type)
	typeIdx := uint32(0)

	// Build internal type index space: type decls (0x01), type aliases (0x03),
	// and type exports with bounds (0x04 + kind 0x03) all contribute indices
	for _, decl := range instType.Decls {
		switch d := decl.DeclType.(type) {
		case InstanceDeclType:
			internalTypes[typeIdx] = d.Type
			typeIdx++

		case InstanceDeclAlias:
			// Only type aliases (sort=0x03) add to the type index space
			if d.Alias.Kind == SortType {
				parsed, err := parseSingleAlias(d.Alias.Kind, d.Alias.Data)
				if err == nil && parsed.TargetKind == 0x02 && r.resolver != nil {
					if int(parsed.OuterIndex) < len(r.resolver.types) {
						internalTypes[typeIdx] = r.resolver.types[parsed.OuterIndex]
					} else {
						internalTypes[typeIdx] = PrimValType{Type: PrimU32}
					}
				} else {
					internalTypes[typeIdx] = PrimValType{Type: PrimU32}
				}
				typeIdx++
			}

		case InstanceDeclExport:
			// Type exports (Kind=0x03) add to type index space
			if d.Export.externDesc.Kind == 0x03 {
				boundIdx := d.Export.externDesc.TypeIndex
				if boundType, found := internalTypes[boundIdx]; found {
					internalTypes[typeIdx] = boundType
				} else {
					internalTypes[typeIdx] = PrimValType{Type: PrimU32}
				}
				typeIdx++
			}
		}
	}

	for _, decl := range instType.Decls {
		if d, ok := decl.DeclType.(InstanceDeclExport); ok {
			if decl.Name == exportName {
				if d.Export.externDesc.Kind == 0x01 { // func
					idx := d.Export.externDesc.TypeIndex
					if t, ok := internalTypes[idx]; ok && t != nil {
						if ft, ok := t.(*FuncType); ok {
							return ft, internalTypes
						}
					}
				}
			}
		}
	}

	return nil, nil
}

func (r *CanonRegistry) findExportNameByFuncIdx(comp *Component, compFuncIdx uint32) string {
	for _, exp := range comp.Exports {
		if exp.Sort == 0x01 && exp.SortIndex == compFuncIdx {
			return exp.Name
		}
	}
	return fmt.Sprintf("func_%d", compFuncIdx)
}

func (r *CanonRegistry) findImportName(comp *Component, funcIdx uint32) string {
	if int(funcIdx) < len(comp.FuncIndexSpace) {
		funcEntry := comp.FuncIndexSpace[funcIdx]
		if int(funcEntry.InstanceIdx) < len(comp.Imports) {
			instanceCount := uint32(0)
			for _, imp := range comp.Imports {
				if imp.ExternKind == ExternInstance {
					if instanceCount == funcEntry.InstanceIdx {
						// Build qualified name: namespace#function
						return imp.Name + "#" + funcEntry.ExportName
					}
					instanceCount++
				}
			}
		}
	}
	return ""
}

// FindLift returns the lift definition for an export name, or nil
func (r *CanonRegistry) FindLift(name string) *LiftDef {
	return r.Lifts[name]
}

// FindLower returns the lower definition for an import name, or nil
func (r *CanonRegistry) FindLower(name string) *LowerDef {
	return r.Lowers[name]
}

// AllLifts returns all lift definitions in arbitrary order
func (r *CanonRegistry) AllLifts() []*LiftDef {
	result := make([]*LiftDef, 0, len(r.Lifts))
	for _, lift := range r.Lifts {
		result = append(result, lift)
	}
	return result
}

// AllLowers returns all lower definitions in arbitrary order
func (r *CanonRegistry) AllLowers() []*LowerDef {
	result := make([]*LowerDef, 0, len(r.Lowers))
	for _, lower := range r.Lowers {
		result = append(result, lower)
	}
	return result
}
