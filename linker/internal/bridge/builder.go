package bridge

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Builder creates and manages bridge modules for component instantiation.
// Thread-safe for concurrent use.
type Builder struct {
	runtime     wazero.Runtime
	synth       *SynthBuilder
	createdMods map[string]bool
	virtualMods map[string]bool
	mu          sync.Mutex
}

// ErrNilRuntime is returned when a nil runtime is passed to constructors.
var ErrNilRuntime = fmt.Errorf("bridge: runtime is nil")

// NewBuilder creates a new bridge builder.
// NewBuilder returns error if rt is nil.
func NewBuilder(rt wazero.Runtime) (*Builder, error) {
	if rt == nil {
		return nil, ErrNilRuntime
	}
	synth, err := NewSynthBuilder(rt)
	if err != nil {
		return nil, err
	}
	return &Builder{
		runtime:     rt,
		synth:       synth,
		createdMods: make(map[string]bool),
		virtualMods: make(map[string]bool),
	}, nil
}

// Source describes where to get exports for a bridge module.
type Source interface {
	isSource()
}

// ModuleSource wraps a real wazero module as a bridge source.
type ModuleSource struct {
	Module      api.Module
	ModuleIndex int // index in component's CoreModules (for parsing globals)
}

func (ModuleSource) isSource() {}

// VirtualSource wraps collected exports as a bridge source.
type VirtualSource struct {
	Exports []Export     // function exports
	Memory  *MemoryInfo  // memory export (optional)
	Table   *TableInfo   // table export (optional)
	Globals []GlobalInfo // global exports
}

func (VirtualSource) isSource() {}

// CreateSpec contains all information needed to create a bridge module.
type CreateSpec struct {
	Source          Source
	ExpectedFuncs   map[string]ImportSig
	ExpectedTables  map[string]TableLimit
	ExpectedGlobals map[string]GlobalInfo
	Name            string
	HostBindings    []HostBinding
}

// Result describes the outcome of bridge creation.
type Result struct {
	ModuleName   string
	BridgeModule string
	Created      bool
	IsVirtual    bool
}

// CreateHostBridge creates a pure host module bridge (functions only).
// CreateHostBridge is for sources with only functions, no memory/table/globals.
func (b *Builder) CreateHostBridge(ctx context.Context, name string, exports []Export, expectedTypes map[string]ImportSig) (*Result, error) {
	// Copy exports to avoid mutating input slice
	if len(expectedTypes) > 0 && len(exports) > 0 {
		exportsCopy := make([]Export, len(exports))
		copy(exportsCopy, exports)
		exports = exportsCopy
		for i := range exports {
			if sig, ok := expectedTypes[exports[i].Name]; ok {
				exports[i].ParamTypes = sig.Params
				exports[i].ResultTypes = sig.Results
			}
		}
	}

	if len(exports) == 0 {
		return &Result{Created: false}, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if already exists
	if b.runtime.Module(name) != nil {
		return &Result{Created: true, ModuleName: name}, nil
	}

	hostBuilder := b.runtime.NewHostModuleBuilder(name)
	for _, exp := range exports {
		hostBuilder.NewFunctionBuilder().
			WithGoModuleFunction(exp.Fn, exp.ParamTypes, exp.ResultTypes).
			Export(exp.Name)
	}

	_, err := hostBuilder.Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	b.createdMods[name] = true
	return &Result{
		Created:      true,
		ModuleName:   name,
		BridgeModule: name,
	}, nil
}

// CreateSynthBridge creates a synthetic WASM bridge module.
// CreateSynthBridge is for sources with memory, table, or globals that host modules can't handle.
func (b *Builder) CreateSynthBridge(ctx context.Context, spec *SynthSpec, expectedTypes map[string]ImportSig) (*Result, error) {
	if spec == nil {
		return &Result{Created: false}, nil
	}

	// Create a local copy to avoid mutating input
	localSpec := *spec
	localSpec.HostModName = localSpec.Name + "$host"

	// Copy and update funcs with expected types
	if len(expectedTypes) > 0 && len(spec.Funcs) > 0 {
		localSpec.Funcs = make([]Export, len(spec.Funcs))
		copy(localSpec.Funcs, spec.Funcs)
		for i := range localSpec.Funcs {
			if sig, ok := expectedTypes[localSpec.Funcs[i].Name]; ok {
				localSpec.Funcs[i].ParamTypes = sig.Params
				localSpec.Funcs[i].ResultTypes = sig.Results
			}
		}
	}

	spec = &localSpec

	b.mu.Lock()
	defer b.mu.Unlock()

	// Create host module for functions (if any)
	if len(spec.Funcs) > 0 {
		// Always recreate host module for virtual sources
		if existing := b.runtime.Module(spec.HostModName); existing != nil {
			_ = existing.Close(ctx)
		}

		_, err := b.synth.BuildHostModule(ctx, spec.HostModName, spec.Funcs)
		if err != nil {
			return nil, err
		}
		b.createdMods[spec.HostModName] = true
	}

	// Check if we need to recreate the synth module
	needsRecreate := false
	existing := b.runtime.Module(spec.Name)
	if existing != nil {
		// Check if existing module has all required exports
		if spec.Table != nil && spec.Table.ExportAs != "" {
			// Can't reliably check tables - recreate to be safe
			needsRecreate = true
		}
		if spec.Memory != nil && spec.Memory.ExportAs != "" {
			memDefs := existing.ExportedMemoryDefinitions()
			if _, ok := memDefs[spec.Memory.ExportAs]; !ok {
				needsRecreate = true
			}
		}
		for _, g := range spec.Globals {
			if existing.ExportedGlobal(g.Name) == nil {
				needsRecreate = true
				break
			}
		}

		if needsRecreate {
			_ = existing.Close(ctx)
		}
	}

	if existing != nil && !needsRecreate {
		return &Result{Created: true, ModuleName: spec.Name}, nil
	}

	// Build synthetic module
	mod, err := b.synth.Build(ctx, spec)
	if err != nil {
		return nil, err
	}
	if mod == nil {
		return &Result{Created: false}, nil
	}

	b.createdMods[spec.Name] = true
	return &Result{
		Created:      true,
		ModuleName:   spec.Name,
		BridgeModule: spec.Name,
	}, nil
}

// MarkVirtual marks a bridge as created from a virtual source.
// MarkVirtual is for tracking whether a bridge needs replacement when real module becomes available.
func (b *Builder) MarkVirtual(name string) {
	b.mu.Lock()
	b.virtualMods[name] = true
	b.mu.Unlock()
}

// IsVirtual checks if a bridge was created from a virtual source.
func (b *Builder) IsVirtual(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.virtualMods[name]
}

// ClearVirtual removes the virtual marker for a bridge.
func (b *Builder) ClearVirtual(name string) {
	b.mu.Lock()
	delete(b.virtualMods, name)
	b.mu.Unlock()
}

// CreatedModules returns names of all modules created by this builder.
func (b *Builder) CreatedModules() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]string, 0, len(b.createdMods))
	for name := range b.createdMods {
		result = append(result, name)
	}
	return result
}
