package runtime

import (
	"context"

	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/errors"
)

type Runtime struct {
	engine *engine.WazeroEngine
	hosts  *HostRegistry
}

func New(ctx context.Context) (*Runtime, error) {
	eng, err := engine.NewWazeroEngine(ctx)
	if err != nil {
		return nil, errors.Load("create engine", err)
	}

	return &Runtime{
		engine: eng,
		hosts:  NewHostRegistry(),
	}, nil
}

// Close releases all runtime resources.
// All instances must be closed before calling this.
func (r *Runtime) Close(ctx context.Context) error {
	return r.engine.Close(ctx)
}

// RegisterHost registers all exported methods of h as host functions.
// Must be called BEFORE loading modules that import these functions.
// Method names are converted from PascalCase to kebab-case (GetValue -> get-value).
func (r *Runtime) RegisterHost(h Host) error {
	return r.hosts.RegisterHost(h)
}

func (r *Runtime) RegisterFunc(namespace, name string, fn any) error {
	return r.hosts.RegisterFunc(namespace, name, fn)
}

func (r *Runtime) Hosts() *HostRegistry {
	return r.hosts
}

// LoadComponent loads a WebAssembly Component Model binary.
// Types are extracted from component metadata automatically.
func (r *Runtime) LoadComponent(ctx context.Context, wasm []byte) (*Module, error) {
	if !component.IsComponent(wasm) {
		return nil, errors.InvalidInput(errors.PhaseLoad, "not a valid component binary")
	}

	wazeroModule, err := r.engine.LoadModule(ctx, wasm)
	if err != nil {
		return nil, errors.Load("load module", err)
	}

	if err := r.hosts.Bind(wazeroModule); err != nil {
		return nil, errors.Load("bind hosts", err)
	}

	return &Module{
		runtime:      r,
		wazeroModule: wazeroModule,
		isComponent:  true,
	}, nil
}

// LoadWASM loads a core WebAssembly module (not Component Model).
// witText provides function signatures for type-safe calls since core
// modules lack type metadata.
func (r *Runtime) LoadWASM(ctx context.Context, wasm []byte, witText string) (*Module, error) {
	if component.IsComponent(wasm) {
		return nil, errors.InvalidInput(errors.PhaseLoad, "use LoadComponent for component binaries")
	}

	wazeroModule, err := r.engine.LoadModule(ctx, wasm)
	if err != nil {
		return nil, errors.Load("load module", err)
	}

	if err := r.hosts.Bind(wazeroModule); err != nil {
		return nil, errors.Load("bind hosts", err)
	}

	return &Module{
		runtime:      r,
		wazeroModule: wazeroModule,
		isComponent:  false,
		witText:      witText,
	}, nil
}
