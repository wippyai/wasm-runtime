package bridge

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	internalwasm "github.com/wippyai/wasm-runtime/linker/internal/wasm"
)

// SynthBuilder creates synthetic WASM bridge modules.
// Synthetic bridges are needed when wazero host modules can't satisfy requirements:
// - Memory exports (host modules can't export memory)
// - Table exports (host modules can't export tables)
// - Global exports (host modules can't export globals)
type SynthBuilder struct {
	runtime wazero.Runtime
}

// NewSynthBuilder creates a new synthetic bridge builder.
// NewSynthBuilder returns error if rt is nil.
func NewSynthBuilder(rt wazero.Runtime) (*SynthBuilder, error) {
	if rt == nil {
		return nil, ErrNilRuntime
	}
	return &SynthBuilder{runtime: rt}, nil
}

// SynthSpec describes a synthetic bridge module to create.
type SynthSpec struct {
	Name         string        // module name
	HostModName  string        // name of host module with functions
	Funcs        []Export      // functions to re-export from host module
	Memory       *MemoryInfo   // memory to import/export (optional)
	Table        *TableInfo    // table to import/export (optional)
	Globals      []GlobalInfo  // globals to import/export
	LocalGlobals []LocalGlobal // local globals to define
	TableSize    uint32        // minimum table size
}

// LocalGlobal describes a local global to define in the synthetic module.
type LocalGlobal struct {
	ExportName string
	ValType    api.ValueType
	Mutable    bool
	InitValue  int64
}

// Build creates a synthetic WASM module from the spec.
// Build returns nil if the spec is nil or produces an empty module.
func (s *SynthBuilder) Build(ctx context.Context, spec *SynthSpec) (api.Module, error) {
	if spec == nil {
		return nil, nil
	}
	builder := internalwasm.NewSynthModuleBuilder(spec.HostModName)

	// Add functions (will be imported from host module, re-exported)
	for _, fn := range spec.Funcs {
		builder.AddFunc(fn.Name, fn.ParamTypes, fn.ResultTypes)
	}

	// Add table import/export
	if spec.Table != nil && spec.Table.SourceModule != nil {
		sourceModName := spec.Table.SourceModule.Name()
		builder.SetTableImport(sourceModName, spec.Table.SourceExport, spec.Table.ExportAs)
	}

	// Set table size
	if spec.TableSize > 0 {
		builder.SetTableSize(spec.TableSize)
	}

	// Add memory import/export
	if spec.Memory != nil && spec.Memory.SourceModule != nil && spec.Memory.SourceExport != "" {
		sourceModName := spec.Memory.SourceModule.Name()
		builder.SetMemoryImport(sourceModName, spec.Memory.SourceExport, spec.Memory.ExportAs)
	}

	// Add global imports
	for _, g := range spec.Globals {
		if g.SourceModule == nil {
			continue
		}
		builder.AddGlobalImport(g.SourceModule.Name(), g.SourceExport, g.Name, g.ValType, g.Mutable)
	}

	// Add local globals
	for _, g := range spec.LocalGlobals {
		builder.AddLocalGlobal(g.ExportName, g.ValType, g.Mutable, g.InitValue)
	}

	synthWasm := builder.Build()
	if synthWasm == nil {
		return nil, nil
	}

	compiled, err := s.runtime.CompileModule(ctx, synthWasm)
	if err != nil {
		return nil, err
	}

	modConfig := wazero.NewModuleConfig().WithName(spec.Name)
	mod, err := s.runtime.InstantiateModule(ctx, compiled, modConfig)
	if err != nil {
		compiled.Close(ctx)
		return nil, err
	}

	return mod, nil
}

// BuildHostModule creates the companion host module for a synthetic bridge.
// BuildHostModule is used because the synthetic module imports functions from this host module.
func (s *SynthBuilder) BuildHostModule(ctx context.Context, name string, exports []Export) (api.Module, error) {
	if len(exports) == 0 {
		return nil, nil
	}

	hostBuilder := s.runtime.NewHostModuleBuilder(name)
	for _, exp := range exports {
		hostBuilder.NewFunctionBuilder().
			WithGoModuleFunction(exp.Fn, exp.ParamTypes, exp.ResultTypes).
			Export(exp.Name)
	}

	return hostBuilder.Instantiate(ctx)
}
