package linker

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Options configures linker behavior.
type Options struct {
	AsyncifyImports   []string
	SemverMatching    bool
	AsyncifyTransform bool
}

// DefaultOptions returns default linker configuration.
func DefaultOptions() Options {
	return Options{
		SemverMatching: true,
	}
}

// Linker manages host function definitions and component instantiation.
// Thread-safe.
type Linker struct {
	runtime        wazero.Runtime
	root           *Namespace
	resolver       *Resolver
	bridgeRefCount map[string]int
	options        Options
	mu             sync.RWMutex
	hostModuleMu   sync.Mutex
}

// New creates a new Linker with the given wazero runtime and options.
func New(rt wazero.Runtime, opts Options) *Linker {
	return &Linker{
		runtime:        rt,
		root:           NewNamespace(),
		options:        opts,
		bridgeRefCount: make(map[string]int),
	}
}

// NewWithDefaults creates a new Linker with default options.
func NewWithDefaults(rt wazero.Runtime) *Linker {
	return New(rt, DefaultOptions())
}

// Runtime returns the wazero runtime.
func (l *Linker) Runtime() wazero.Runtime {
	return l.runtime
}

// Options returns the configuration.
func (l *Linker) Options() Options {
	return l.options
}

// Root returns the root namespace.
func (l *Linker) Root() *Namespace {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.root
}

// Resolver returns the import resolver for registering named instances.
// Lazy-initialized on first call.
func (l *Linker) Resolver() *Resolver {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.resolver == nil {
		l.resolver = NewResolver(l)
	}
	return l.resolver
}

// Namespace returns or creates a namespace by path.
// Namespace accepts paths with versions: "wasi:io/streams@0.2.0"
// Nested paths are separated by "/": "wasi:io/streams@0.2.0/error"
func (l *Linker) Namespace(path string) *Namespace {
	l.mu.Lock()
	defer l.mu.Unlock()

	segments := parseNamespacePath(path)
	current := l.root

	for _, seg := range segments {
		name := seg.name
		if seg.version != nil {
			name += "@" + seg.version.String()
		}
		current = current.Instance(name)
	}

	return current
}

// DefineFunc is a convenience method to define a function at a full path.
// DefineFunc uses path format: "wasi:random/random@0.2.0#get-random-bytes"
func (l *Linker) DefineFunc(path string, fn api.GoModuleFunc, params, results []api.ValueType) error {
	// Split into namespace path and function name
	nsPath, funcName, err := splitFuncPath(path)
	if err != nil {
		return fmt.Errorf("linker: define func %q: %w", path, err)
	}

	ns := l.Namespace(nsPath)
	ns.DefineFunc(funcName, fn, params, results)
	return nil
}

// Resolve looks up a function by full path with semver matching if enabled.
// Resolve uses path format: "wasi:io/streams@0.2.0#read"
func (l *Linker) Resolve(path string) *FuncDef {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.root.ResolveWithSemver(path, l.options.SemverMatching)
}

// splitFuncPath splits "ns/path#funcname" into namespace and function parts
func splitFuncPath(path string) (nsPath, funcName string, err error) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '#' {
			return path[:i], path[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("linker: invalid function path %q: missing '#' separator", path)
}

// HostModuleBuilder builds host modules for the wazero runtime.
type HostModuleBuilder struct {
	linker     *Linker
	namespace  *Namespace
	moduleName string
}

// NewHostModule starts building a host module with the given name.
// NewHostModule expects the full WIT interface path: "wasi:random/random@0.2.0"
func (l *Linker) NewHostModule(name string) *HostModuleBuilder {
	return &HostModuleBuilder{
		linker:     l,
		namespace:  l.Namespace(name),
		moduleName: name,
	}
}

// Func adds a function to the host module builder.
func (b *HostModuleBuilder) Func(name string, fn api.GoModuleFunc, params, results []api.ValueType) *HostModuleBuilder {
	// DefineFunc currently never errors, but handle gracefully if it does
	b.namespace.DefineFunc(name, fn, params, results)
	return b
}

// Build instantiates the host module into the wazero runtime.
func (b *HostModuleBuilder) Build(ctx context.Context) (api.Module, error) {
	builder := b.linker.runtime.NewHostModuleBuilder(b.moduleName)

	funcs := b.namespace.AllFuncs()
	for _, f := range funcs {
		builder.NewFunctionBuilder().
			WithGoModuleFunction(f.Handler, f.ParamTypes, f.ResultTypes).
			Export(f.Name)
	}

	return builder.Instantiate(ctx)
}

// getOrCreateHostModule atomically gets or creates a host module.
func (l *Linker) getOrCreateHostModule(ctx context.Context, name string, builder func() (api.Module, error)) (api.Module, bool, error) {
	l.hostModuleMu.Lock()
	defer l.hostModuleMu.Unlock()

	// Check if already exists (under lock)
	if mod := l.runtime.Module(name); mod != nil {
		return mod, false, nil
	}

	// Create the module (under lock to prevent races)
	mod, err := builder()
	return mod, mod != nil && err == nil, err
}

// getOrReplaceHostModule atomically gets, validates, or replaces a host module.
func (l *Linker) getOrReplaceHostModule(ctx context.Context, name string, validator func(api.Module) bool, builder func() (api.Module, error)) (api.Module, bool, error) {
	l.hostModuleMu.Lock()
	defer l.hostModuleMu.Unlock()

	// Check if already exists
	if mod := l.runtime.Module(name); mod != nil {
		// Validate the existing module
		if validator(mod) {
			return mod, false, nil
		}
		// Invalid - close it and recreate
		mod.Close(ctx)
	}

	// Create the module
	mod, err := builder()
	return mod, mod != nil && err == nil, err
}

// addBridgeRefs increments reference counts for bridge modules.
func (l *Linker) addBridgeRefs(names map[string]bool) {
	if len(names) == 0 {
		return
	}
	l.hostModuleMu.Lock()
	defer l.hostModuleMu.Unlock()
	for name := range names {
		l.bridgeRefCount[name]++
	}
}

// releaseBridgeRefs decrements reference counts and closes bridges that reach zero.
func (l *Linker) releaseBridgeRefs(ctx context.Context, names map[string]bool) {
	if len(names) == 0 {
		return
	}
	l.hostModuleMu.Lock()
	defer l.hostModuleMu.Unlock()

	for name := range names {
		if count, ok := l.bridgeRefCount[name]; ok {
			count--
			if count <= 0 {
				delete(l.bridgeRefCount, name)
				// Close the bridge module
				if mod := l.runtime.Module(name); mod != nil {
					mod.Close(ctx)
				}
			} else {
				l.bridgeRefCount[name] = count
			}
		}
	}
}

// Close releases resources. Does not close the wazero runtime.
func (l *Linker) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.root = NewNamespace()
	l.resolver = nil
	return nil
}
