package arena

import "fmt"

// State represents the validation state for a component, instance type, or component type.
// State maintains all index spaces that grow as sections are processed.
type State struct {
	imports     map[string]EntityType
	exports     map[string]EntityType
	coreFuncs   []TypeID
	coreModules []TypeID
	types       []AnyTypeID
	funcs       []TypeID
	values      []ValueEntry
	instances   []TypeID
	components  []TypeID
	kind        Kind
}

// Kind identifies what this state represents
type Kind byte

const (
	KindComponent Kind = iota
	KindInstanceType
	KindComponentType
)

// ValueEntry tracks a value and whether it's been used
type ValueEntry struct {
	Type ValType
	Used bool
}

// NewState creates a new component state
func NewState(kind Kind) *State {
	return &State{
		kind:    kind,
		imports: make(map[string]EntityType),
		exports: make(map[string]EntityType),
	}
}

// AddType adds a type to the type index space
func (cs *State) AddType(id AnyTypeID) uint32 {
	idx := uint32(len(cs.types))
	cs.types = append(cs.types, id)
	return idx
}

// GetType retrieves a type by index
func (cs *State) GetType(idx uint32) (AnyTypeID, error) {
	if int(idx) >= len(cs.types) {
		return AnyTypeID{}, fmt.Errorf("type index %d out of range", idx)
	}
	return cs.types[idx], nil
}

// AddFunc adds a function type to the func index space
func (cs *State) AddFunc(typeID TypeID) uint32 {
	idx := uint32(len(cs.funcs))
	cs.funcs = append(cs.funcs, typeID)
	return idx
}

// GetFunc retrieves a function type by index
func (cs *State) GetFunc(idx uint32) (TypeID, error) {
	if int(idx) >= len(cs.funcs) {
		return 0, fmt.Errorf("func index %d out of range", idx)
	}
	return cs.funcs[idx], nil
}

// AddValue adds a value to the value index space
func (cs *State) AddValue(ty ValType) uint32 {
	idx := uint32(len(cs.values))
	cs.values = append(cs.values, ValueEntry{Type: ty, Used: false})
	return idx
}

// GetValue retrieves a value by index
func (cs *State) GetValue(idx uint32) (ValType, error) {
	if int(idx) >= len(cs.values) {
		return ValType{}, fmt.Errorf("value index %d out of range", idx)
	}
	return cs.values[idx].Type, nil
}

// MarkValueUsed marks a value as used
func (cs *State) MarkValueUsed(idx uint32) {
	if int(idx) < len(cs.values) {
		cs.values[idx].Used = true
	}
}

// AddInstance adds an instance type to the instance index space
func (cs *State) AddInstance(typeID TypeID) uint32 {
	idx := uint32(len(cs.instances))
	cs.instances = append(cs.instances, typeID)
	return idx
}

// GetInstance retrieves an instance type by index
func (cs *State) GetInstance(idx uint32) (TypeID, error) {
	if int(idx) >= len(cs.instances) {
		return 0, fmt.Errorf("instance index %d out of range", idx)
	}
	return cs.instances[idx], nil
}

// AddComponent adds a component type to the component index space
func (cs *State) AddComponent(typeID TypeID) uint32 {
	idx := uint32(len(cs.components))
	cs.components = append(cs.components, typeID)
	return idx
}

// GetComponent retrieves a component type by index
func (cs *State) GetComponent(idx uint32) (TypeID, error) {
	if int(idx) >= len(cs.components) {
		return 0, fmt.Errorf("component index %d out of range", idx)
	}
	return cs.components[idx], nil
}

// AddCoreModule adds a core module to the core module index space
func (cs *State) AddCoreModule(typeID TypeID) uint32 {
	idx := uint32(len(cs.coreModules))
	cs.coreModules = append(cs.coreModules, typeID)
	return idx
}

// GetCoreModule retrieves a core module type by index
func (cs *State) GetCoreModule(idx uint32) (TypeID, error) {
	if int(idx) >= len(cs.coreModules) {
		return 0, fmt.Errorf("core module index %d out of range", idx)
	}
	return cs.coreModules[idx], nil
}

// AddCoreFunc adds a core function to the core func index space
func (cs *State) AddCoreFunc(typeID TypeID) uint32 {
	idx := uint32(len(cs.coreFuncs))
	cs.coreFuncs = append(cs.coreFuncs, typeID)
	return idx
}

// GetCoreFunc retrieves a core function type by index
func (cs *State) GetCoreFunc(idx uint32) (TypeID, error) {
	if int(idx) >= len(cs.coreFuncs) {
		return 0, fmt.Errorf("core func index %d out of range", idx)
	}
	return cs.coreFuncs[idx], nil
}

// AddImport adds an import to the component
func (cs *State) AddImport(name string, entity EntityType) error {
	if _, exists := cs.imports[name]; exists {
		return fmt.Errorf("import %q already exists", name)
	}
	cs.imports[name] = entity
	return nil
}

// AddExport adds an export to the component
func (cs *State) AddExport(name string, entity EntityType) error {
	if _, exists := cs.exports[name]; exists {
		return fmt.Errorf("export %q already exists", name)
	}
	cs.exports[name] = entity
	return nil
}

// TypeCount returns the number of types in the type index space
func (cs *State) TypeCount() int {
	return len(cs.types)
}

// FuncCount returns the number of functions in the func index space
func (cs *State) FuncCount() int {
	return len(cs.funcs)
}

// InstanceCount returns the number of instances in the instance index space
func (cs *State) InstanceCount() int {
	return len(cs.instances)
}

// Instances returns the instance index space
func (cs *State) Instances() []TypeID {
	return cs.instances
}

// Exports returns the exports map
func (cs *State) Exports() map[string]EntityType {
	return cs.exports
}

// CheckAllValuesUsed returns an error if any value was not used
func (cs *State) CheckAllValuesUsed() error {
	for idx, val := range cs.values {
		if !val.Used {
			return fmt.Errorf("value index %d was not used", idx)
		}
	}
	return nil
}
