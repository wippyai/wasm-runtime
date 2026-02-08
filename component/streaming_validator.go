package component

import (
	"fmt"
	"io"

	"github.com/wippyai/wasm-runtime/component/internal/arena"
)

// StreamingValidator validates component sections in streaming order
type StreamingValidator struct {
	types      *arena.TypeArena
	components []*arena.State
	state      ValidatorState
}

// ValidatorState tracks the current parsing state
type ValidatorState byte

const (
	StateUnparsed ValidatorState = iota
	StateModule
	StateComponent
	StateEnd
)

// ValidatedComponent contains a component with fully resolved types
type ValidatedComponent struct {
	arena *arena.TypeArena // internal
	state *arena.State     // internal
	Raw   *Component       // Raw parsed component data
}

// TypeCount returns the number of types
func (v *ValidatedComponent) TypeCount() int {
	if v.state == nil {
		return 0
	}
	return v.state.TypeCount()
}

// FuncCount returns the number of functions
func (v *ValidatedComponent) FuncCount() int {
	if v.state == nil {
		return 0
	}
	return v.state.FuncCount()
}

// InstanceCount returns the number of instances
func (v *ValidatedComponent) InstanceCount() int {
	if v.state == nil {
		return 0
	}
	return v.state.InstanceCount()
}

// NewStreamingValidator creates a new streaming validator
func NewStreamingValidator() *StreamingValidator {
	return &StreamingValidator{
		state:      StateUnparsed,
		types:      arena.NewTypeArena(),
		components: make([]*arena.State, 0),
	}
}

// ProcessSection processes a component section in streaming order
func (v *StreamingValidator) ProcessSection(sectionID byte, data []byte) error {
	switch sectionID {
	case 7: // Type section
		return v.processTypeSection(data)
	case 10: // Import section
		return v.processImportSection(data)
	case 6: // Alias section
		return v.processAliasSection(data)
	case 8: // Canon section
		return v.processCanonSection(data)
	case 11: // Export section
		return v.processExportSection(data)
	default:
		return nil
	}
}

// Version processes the component version header
func (v *StreamingValidator) Version(version uint16) error {
	// Component binary version is 0x0d (13) stored in first byte, rest is layer version 0x00 0x01 0x00
	// We only check the first byte for now
	if version == 0x0d || version == 0x000d {
		v.state = StateComponent
		v.components = append(v.components, arena.NewState(arena.KindComponent))
		return nil
	}
	return fmt.Errorf("unsupported version: %#x", version)
}

// End finalizes validation and returns the validated component
func (v *StreamingValidator) End() (*ValidatedComponent, error) {
	if v.state != StateComponent {
		return nil, fmt.Errorf("unexpected state at end: %v", v.state)
	}

	if len(v.components) != 1 {
		return nil, fmt.Errorf("expected 1 component, got %d", len(v.components))
	}

	// Check all values were used
	if err := v.components[0].CheckAllValuesUsed(); err != nil {
		return nil, err
	}

	v.state = StateEnd

	return &ValidatedComponent{
		arena: v.types,
		state: v.components[0],
	}, nil
}

// current returns the current component state
func (v *StreamingValidator) current() (*arena.State, error) {
	if len(v.components) == 0 {
		return nil, fmt.Errorf("no component state")
	}
	return v.components[len(v.components)-1], nil
}

// processTypeSection processes a type section
func (v *StreamingValidator) processTypeSection(data []byte) error {
	current, err := v.current()
	if err != nil {
		return err
	}

	parsed, err := ParseTypeSection(data)
	if err != nil {
		return fmt.Errorf("parse type section: %w", err)
	}

	for _, ty := range parsed.Types {
		if err := v.addType(current, ty); err != nil {
			return err
		}
	}

	return nil
}

// addType adds a component type to the current state
func (v *StreamingValidator) addType(current *arena.State, ty Type) error {
	switch t := ty.(type) {
	case *FuncType:
		return v.addFuncType(current, t)
	case *InstanceType:
		return v.addInstanceType(current, t)
	case TypeIndexRef:
		// Type reference - just add the referenced type
		anyType, err := current.GetType(t.Index)
		if err != nil {
			return err
		}
		current.AddType(anyType)
		return nil
	case PrimValType, RecordType, VariantType, ListType, TupleType, EnumType, FlagsType, OptionType, ResultType:
		return v.addDefinedType(current, ty)
	case OwnType:
		// Own type references a resource (or alias to resource)
		anyType, err := current.GetType(t.TypeIndex)
		if err != nil {
			return fmt.Errorf("own type: %w", err)
		}
		// Store the type ID - the referenced type might be a resource or an alias chain
		// Resolution to actual resource happens during type resolution/flattening
		defID := v.types.AllocDefined(arena.DefinedType{Kind: arena.DefinedKindOwn, Data: anyType.ID})
		current.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: defID})
		return nil
	case BorrowType:
		// Borrow type references a resource (or alias to resource)
		anyType, err := current.GetType(t.TypeIndex)
		if err != nil {
			return fmt.Errorf("borrow type: %w", err)
		}
		// Store the type ID - the referenced type might be a resource or an alias chain
		defID := v.types.AllocDefined(arena.DefinedType{Kind: arena.DefinedKindBorrow, Data: anyType.ID})
		current.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: defID})
		return nil
	default:
		return fmt.Errorf("unsupported component type: %T", ty)
	}
}

// addFuncType adds a function type
func (v *StreamingValidator) addFuncType(current *arena.State, ft *FuncType) error {
	params := make([]arena.ParamData, len(ft.Params))
	for i, p := range ft.Params {
		valType, err := v.resolveValType(current, p.Type)
		if err != nil {
			return fmt.Errorf("function param %d: %w", i, err)
		}
		params[i] = arena.ParamData{Name: p.Name, Type: valType}
	}

	var result *arena.ValType
	if ft.Result != nil {
		vt, err := v.resolveValType(current, *ft.Result)
		if err != nil {
			return fmt.Errorf("function result: %w", err)
		}
		result = &vt
	}

	funcID := v.types.AllocFunc(arena.FuncTypeData{
		Params: params,
		Result: result,
	})

	current.AddType(arena.AnyTypeID{Kind: arena.TypeKindFunc, ID: funcID})
	return nil
}

// addInstanceType adds an instance type
func (v *StreamingValidator) addInstanceType(current *arena.State, it *InstanceType) error {
	// Push new scope for instance type's internal type space
	instanceState := arena.NewState(arena.KindInstanceType)
	v.components = append(v.components, instanceState)

	// Process instance declarations
	for i, decl := range it.Decls {
		if err := v.processInstanceDecl(instanceState, decl); err != nil {
			v.components = v.components[:len(v.components)-1]
			return fmt.Errorf("instance decl %d: %w", i, err)
		}
	}

	// Pop scope
	v.components = v.components[:len(v.components)-1]

	// Create instance type
	instID := v.types.AllocInstance(arena.InstanceTypeData{
		Exports: instanceState.Exports(),
	})

	current.AddType(arena.AnyTypeID{Kind: arena.TypeKindInstance, ID: instID})
	return nil
}

// processInstanceDecl processes an instance type declaration
func (v *StreamingValidator) processInstanceDecl(instanceState *arena.State, decl InstanceDecl) error {
	switch d := decl.DeclType.(type) {
	case InstanceDeclType:
		err := v.addType(instanceState, d.Type)
		if err != nil {
			return fmt.Errorf("component type %T: %w", d.Type, err)
		}
		return nil

	case InstanceDeclAlias:
		// Parse the alias from raw data
		parsed, err := parseSingleAlias(d.Alias.Kind, d.Alias.Data)
		if err != nil {
			return fmt.Errorf("parse alias: %w", err)
		}
		return v.processAlias(instanceState, parsed)

	case InstanceDeclExport:
		name := d.Export.Name
		if name == "" {
			name = decl.Name
		}

		entity, err := v.resolveExportEntity(instanceState, &d.Export.externDesc)
		if err != nil {
			return fmt.Errorf("export %q: %w", name, err)
		}

		if err := instanceState.AddExport(name, entity); err != nil {
			return err
		}

		// Type exports also add to the type index space
		if entity.Kind == arena.EntityKindType {
			instanceState.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: entity.ID})
		}

		return nil

	default:
		return fmt.Errorf("unsupported instance decl: %T", d)
	}
}

// resolveExportEntity resolves an export descriptor to an entity type
func (v *StreamingValidator) resolveExportEntity(state *arena.State, desc *externDesc) (arena.EntityType, error) {
	switch desc.Kind {
	case 0x03: // Type export
		// Check if this is a SubResource bound (creates a fresh resource)
		if desc.HasBound && desc.BoundKind == 0x01 {
			// Create a fresh resource type
			resourceID := v.types.AllocResource()
			return arena.EntityType{Kind: arena.EntityKindType, ID: resourceID}, nil
		}

		// Otherwise it's an Eq bound, look up the type
		anyType, err := state.GetType(desc.TypeIndex)
		if err != nil {
			return arena.EntityType{}, err
		}
		return arena.EntityType{Kind: arena.EntityKindType, ID: anyType.ID}, nil

	case 0x01: // Func export
		anyType, err := state.GetType(desc.TypeIndex)
		if err != nil {
			return arena.EntityType{}, err
		}
		if anyType.Kind != arena.TypeKindFunc {
			return arena.EntityType{}, fmt.Errorf("func export type index %d is not a function type", desc.TypeIndex)
		}
		return arena.EntityType{Kind: arena.EntityKindFunc, ID: anyType.ID}, nil

	case 0x02: // Instance export
		anyType, err := state.GetType(desc.TypeIndex)
		if err != nil {
			return arena.EntityType{}, err
		}
		if anyType.Kind != arena.TypeKindInstance {
			return arena.EntityType{}, fmt.Errorf("instance export type index %d is not an instance type", desc.TypeIndex)
		}
		return arena.EntityType{Kind: arena.EntityKindInstance, ID: anyType.ID}, nil

	default:
		return arena.EntityType{}, fmt.Errorf("unsupported export kind 0x%02x", desc.Kind)
	}
}

// addDefinedType adds a defined type
func (v *StreamingValidator) addDefinedType(current *arena.State, ty Type) error {
	var defType arena.DefinedType

	switch t := ty.(type) {
	case PrimValType:
		defType = arena.DefinedType{Kind: arena.DefinedKindPrimitive, Data: arena.PrimType(t.Type)}

	case RecordType:
		fields := make([]arena.FieldData, len(t.Fields))
		for i, f := range t.Fields {
			vt, err := v.resolveValType(current, f.Type)
			if err != nil {
				return fmt.Errorf("record field %q: %w", f.Name, err)
			}
			fields[i] = arena.FieldData{Name: f.Name, Type: vt}
		}
		defType = arena.DefinedType{Kind: arena.DefinedKindRecord, Data: arena.RecordData{Fields: fields}}

	case VariantType:
		cases := make([]arena.CaseData, len(t.Cases))
		for i, c := range t.Cases {
			var vt *arena.ValType
			if c.Type != nil {
				resolved, err := v.resolveValType(current, *c.Type)
				if err != nil {
					return fmt.Errorf("variant case %q: %w", c.Name, err)
				}
				vt = &resolved
			}
			cases[i] = arena.CaseData{Name: c.Name, Type: vt, Refines: c.Refines}
		}
		defType = arena.DefinedType{Kind: arena.DefinedKindVariant, Data: arena.VariantData{Cases: cases}}

	case ListType:
		elemType, err := v.resolveValType(current, t.ElemType)
		if err != nil {
			return fmt.Errorf("list element: %w", err)
		}
		defType = arena.DefinedType{Kind: arena.DefinedKindList, Data: elemType}

	case TupleType:
		types := make([]arena.ValType, len(t.Types))
		for i, elemType := range t.Types {
			vt, err := v.resolveValType(current, elemType)
			if err != nil {
				return fmt.Errorf("tuple element %d: %w", i, err)
			}
			types[i] = vt
		}
		defType = arena.DefinedType{Kind: arena.DefinedKindTuple, Data: arena.TupleData{Types: types}}

	case EnumType:
		defType = arena.DefinedType{Kind: arena.DefinedKindEnum, Data: t.Cases}

	case FlagsType:
		defType = arena.DefinedType{Kind: arena.DefinedKindFlags, Data: t.Names}

	case OptionType:
		elemType, err := v.resolveValType(current, t.Type)
		if err != nil {
			return fmt.Errorf("option type: %w", err)
		}
		defType = arena.DefinedType{Kind: arena.DefinedKindOption, Data: elemType}

	case ResultType:
		var okType, errType *arena.ValType
		if t.OK != nil {
			vt, err := v.resolveValType(current, *t.OK)
			if err != nil {
				return fmt.Errorf("result ok: %w", err)
			}
			okType = &vt
		}
		if t.Err != nil {
			vt, err := v.resolveValType(current, *t.Err)
			if err != nil {
				return fmt.Errorf("result err: %w", err)
			}
			errType = &vt
		}
		defType = arena.DefinedType{Kind: arena.DefinedKindResult, Data: arena.ResultData{OK: okType, Err: errType}}

	default:
		return fmt.Errorf("unsupported defined type: %T", ty)
	}

	defID := v.types.AllocDefined(defType)
	current.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: defID})
	return nil
}

// resolveValType resolves a component value type
func (v *StreamingValidator) resolveValType(current *arena.State, cvt ValType) (arena.ValType, error) {
	switch t := cvt.(type) {
	case PrimValType:
		return arena.ValType{Primitive: arena.PrimType(t.Type)}, nil

	case TypeIndexRef:
		anyType, err := current.GetType(t.Index)
		if err != nil {
			return arena.ValType{}, err
		}
		if anyType.Kind != arena.TypeKindDefined {
			return arena.ValType{}, fmt.Errorf("type index %d is not a defined type", t.Index)
		}
		return arena.ValType{TypeID: anyType.ID}, nil

	case OwnType:
		anyType, err := current.GetType(t.TypeIndex)
		if err != nil {
			return arena.ValType{}, err
		}
		if anyType.Kind != arena.TypeKindResource {
			return arena.ValType{}, fmt.Errorf("own type index %d is not a resource type", t.TypeIndex)
		}
		defID := v.types.AllocDefined(arena.DefinedType{Kind: arena.DefinedKindOwn, Data: anyType.ID})
		return arena.ValType{TypeID: defID}, nil

	case BorrowType:
		anyType, err := current.GetType(t.TypeIndex)
		if err != nil {
			return arena.ValType{}, err
		}
		if anyType.Kind != arena.TypeKindResource {
			return arena.ValType{}, fmt.Errorf("borrow type index %d is not a resource type", t.TypeIndex)
		}
		defID := v.types.AllocDefined(arena.DefinedType{Kind: arena.DefinedKindBorrow, Data: anyType.ID})
		return arena.ValType{TypeID: defID}, nil

	default:
		return arena.ValType{}, fmt.Errorf("unsupported value type: %T", cvt)
	}
}

// processImportSection processes an import section
func (v *StreamingValidator) processImportSection(data []byte) error {
	current, err := v.current()
	if err != nil {
		return err
	}

	imports, err := decodeImports(data)
	if err != nil {
		return err
	}

	for _, imp := range imports {
		anyType, err := current.GetType(imp.TypeIndex)
		if err != nil {
			return fmt.Errorf("import %q: %w", imp.Name, err)
		}

		switch imp.ExternKind {
		case ExternCoreModule:
			// Core module imports add to the core module index space
			current.AddCoreModule(anyType.ID)

		case ExternType:
			// Type imports add to the type index space
			current.AddType(anyType)

		case ExternFunc:
			// Function imports add to the function index space
			if anyType.Kind != arena.TypeKindFunc {
				return fmt.Errorf("import %q type index %d is not a function type", imp.Name, imp.TypeIndex)
			}
			current.AddFunc(anyType.ID)

		case ExternInstance:
			// Instance imports add to the instance index space
			if anyType.Kind != arena.TypeKindInstance {
				return fmt.Errorf("import %q type index %d is not an instance type", imp.Name, imp.TypeIndex)
			}
			current.AddInstance(anyType.ID)

		case ExternComponent:
			// Component imports add to the component index space
			if anyType.Kind != arena.TypeKindComponent {
				return fmt.Errorf("import %q type index %d is not a component type", imp.Name, imp.TypeIndex)
			}
			current.AddComponent(anyType.ID)

		case ExternValue:
			// Value imports add to the value index space
			// Value type is stored differently, need to handle this

		default:
			return fmt.Errorf("unknown import kind: 0x%02x", imp.ExternKind)
		}

		// Map extern kind to entity kind and add to imports
		entityKind := externToEntityKind(imp.ExternKind)
		if err := current.AddImport(imp.Name, arena.EntityType{Kind: entityKind, ID: anyType.ID}); err != nil {
			return fmt.Errorf("import %q: %w", imp.Name, err)
		}
	}

	return nil
}

// processAliasSection processes an alias section
func (v *StreamingValidator) processAliasSection(data []byte) error {
	current, err := v.current()
	if err != nil {
		return err
	}

	aliases, err := parseAliasSection(data)
	if err != nil {
		return err
	}

	for _, alias := range aliases {
		if err := v.processAlias(current, alias); err != nil {
			return err
		}
	}

	return nil
}

// processAlias processes a single alias
func (v *StreamingValidator) processAlias(current *arena.State, alias *ParsedAlias) error {
	// Sort 0x00 = core aliases (core modules, funcs, memories, etc.) - skip for component validation
	if alias.Sort == 0x00 {
		return nil
	}

	// Type aliases (sort=0x03)
	if alias.Sort == 0x03 {
		switch alias.TargetKind {
		case 0x00: // Instance export alias
			return v.aliasInstanceExportType(current, alias.Instance, alias.Name)
		case 0x02: // Outer alias
			return v.aliasOuterType(alias.OuterCount, alias.OuterIndex)
		default:
			return fmt.Errorf("unsupported type alias target kind 0x%02x", alias.TargetKind)
		}
	}

	// Function aliases (sort=0x01)
	if alias.Sort == 0x01 && alias.TargetKind == 0x00 {
		return v.aliasInstanceExportFunc(current, alias.Instance, alias.Name)
	}

	return nil
}

// aliasInstanceExportType aliases a type from an instance export
func (v *StreamingValidator) aliasInstanceExportType(current *arena.State, instanceIdx uint32, name string) error {
	instTypeID, err := current.GetInstance(instanceIdx)
	if err != nil {
		return fmt.Errorf("instance index %d out of range", instanceIdx)
	}

	instType := v.types.GetInstance(instTypeID)
	if instType == nil {
		return fmt.Errorf("instance type %d not found", instTypeID)
	}

	entity, ok := instType.Exports[name]
	if !ok {
		return fmt.Errorf("instance has no export %q", name)
	}

	if entity.Kind != arena.EntityKindType {
		return fmt.Errorf("export %q is not a type", name)
	}

	current.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: entity.ID})
	return nil
}

// aliasOuterType aliases a type from an outer scope
func (v *StreamingValidator) aliasOuterType(count uint32, index uint32) error {
	if int(count) >= len(v.components) {
		return fmt.Errorf("outer count %d out of range", count)
	}

	outerComponent := v.components[len(v.components)-1-int(count)]
	anyType, err := outerComponent.GetType(index)
	if err != nil {
		return fmt.Errorf("outer type index %d: %w", index, err)
	}

	current, err := v.current()
	if err != nil {
		return fmt.Errorf("get current component: %w", err)
	}
	current.AddType(anyType)
	return nil
}

// aliasInstanceExportFunc aliases a function from an instance export
func (v *StreamingValidator) aliasInstanceExportFunc(current *arena.State, instanceIdx uint32, name string) error {
	instTypeID, err := current.GetInstance(instanceIdx)
	if err != nil {
		return fmt.Errorf("instance index %d out of range", instanceIdx)
	}

	instType := v.types.GetInstance(instTypeID)
	if instType == nil {
		return fmt.Errorf("instance type %d not found", instTypeID)
	}

	entity, ok := instType.Exports[name]
	if !ok {
		return fmt.Errorf("instance has no export %q", name)
	}

	if entity.Kind != arena.EntityKindFunc {
		return fmt.Errorf("export %q is not a function", name)
	}

	current.AddFunc(entity.ID)
	return nil
}

// processCanonSection processes a canon section
func (v *StreamingValidator) processCanonSection(data []byte) error {
	current, err := v.current()
	if err != nil {
		return err
	}

	parsed, err := ParseCanonSection(data)
	if err != nil {
		return err
	}

	switch parsed.Kind {
	case CanonLower:
		// Lower creates a core function from a component function
		current.AddCoreFunc(0)

	case CanonLift:
		// Lift creates a component function from a core function
		funcTypeID, err := current.GetType(parsed.TypeIndex)
		if err != nil {
			return fmt.Errorf("canon lift has %d types available: %w", current.TypeCount(), err)
		}
		if funcTypeID.Kind != arena.TypeKindFunc {
			return fmt.Errorf("canon lift type index %d is not a function type", parsed.TypeIndex)
		}
		current.AddFunc(funcTypeID.ID)

	case CanonResourceNew:
		// resource.new creates a core function
		current.AddCoreFunc(0)

	case CanonResourceDrop:
		// resource.drop creates a core function
		current.AddCoreFunc(0)

	case CanonResourceRep:
		// resource.rep creates a core function
		current.AddCoreFunc(0)

	case CanonResourceDropAsync:
		// resource.drop-async creates a core function
		current.AddCoreFunc(0)

	case CanonTaskCancel:
		// task.cancel creates a core function
		current.AddCoreFunc(0)

	case CanonSubtaskCancel:
		// subtask.cancel creates a core function
		current.AddCoreFunc(0)

	default:
		return fmt.Errorf("unsupported canon operation: 0x%02x", parsed.Kind)
	}

	return nil
}

// processExportSection processes an export section
func (v *StreamingValidator) processExportSection(data []byte) error {
	current, err := v.current()
	if err != nil {
		return err
	}

	exports, err := decodeExports(data)
	if err != nil {
		return err
	}

	for _, exp := range exports {
		// Function exports (Sort=0x01) add to component func index space
		if exp.Sort == 0x01 {
			current.AddFunc(0)
		}
	}

	return nil
}

// parseSingleAlias parses a single alias from its kind byte and data
func parseSingleAlias(kind byte, data []byte) (*ParsedAlias, error) {
	r := getReader(data)
	defer putReader(r)

	// The data includes the sort byte at the beginning, but we already have it
	// in the kind parameter, so skip it
	sortInData, err := readByte(r)
	if err != nil {
		return nil, fmt.Errorf("read sort from data: %w", err)
	}
	if sortInData != kind {
		return nil, fmt.Errorf("sort mismatch: expected 0x%02x, got 0x%02x", kind, sortInData)
	}

	sort := kind

	// For core sort (0x00), read the additional core:sort byte
	var coreSort byte
	if sort == 0x00 {
		coreSort, err = readByte(r)
		if err != nil {
			return nil, fmt.Errorf("read core:sort: %w", err)
		}
	}

	targetKind, err := readByte(r)
	if err != nil {
		return nil, fmt.Errorf("read target kind: %w", err)
	}

	alias := &ParsedAlias{
		Sort:       sort,
		CoreSort:   coreSort,
		TargetKind: targetKind,
	}

	switch targetKind {
	case 0x00: // instance export
		instIdx, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read instance idx: %w", err)
		}
		nameLen, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read name length: %w", err)
		}
		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return nil, fmt.Errorf("read name: %w", err)
		}
		alias.Instance = instIdx
		alias.Name = string(nameBytes)

	case 0x02: // outer
		count, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read outer count: %w", err)
		}
		index, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read outer index: %w", err)
		}
		alias.OuterCount = count
		alias.OuterIndex = index

	default:
		return nil, fmt.Errorf("unsupported alias target kind: 0x%02x", targetKind)
	}

	return alias, nil
}

// externToEntityKind maps external kind to entity kind
func externToEntityKind(ext byte) arena.EntityKind {
	switch ext {
	case ExternCoreModule:
		return arena.EntityKindModule
	case ExternFunc:
		return arena.EntityKindFunc
	case ExternValue:
		return arena.EntityKindValue
	case ExternType:
		return arena.EntityKindType
	case ExternComponent:
		return arena.EntityKindComponent
	case ExternInstance:
		return arena.EntityKindInstance
	default:
		return arena.EntityKindValue
	}
}
