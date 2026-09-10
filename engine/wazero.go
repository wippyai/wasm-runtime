package engine

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"go.bytecodealliance.org/wit"
	"go.uber.org/zap"

	wasmruntime "github.com/wippyai/wasm-runtime"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/linker"
	"github.com/wippyai/wasm-runtime/memory/budget"
	"github.com/wippyai/wasm-runtime/resource"
	"github.com/wippyai/wasm-runtime/transcoder"
	"github.com/wippyai/wasm-runtime/wasm"
)

type resourcesContextKey struct{}

// ResourcesFromContext returns the host resource table owned by the instance
// executing the current call.
func ResourcesFromContext(ctx context.Context) *resource.UnifiedTable {
	resources, _ := ctx.Value(resourcesContextKey{}).(*resource.UnifiedTable)
	return resources
}

// WazeroEngine implements Engine using wazero runtime
type WazeroEngine struct {
	startups      *executionLifetime
	instances     map[*WazeroInstance]struct{}
	closeAttempt  chan struct{}
	closeErr      error
	runtime       wazero.Runtime
	hostMods      map[string]struct{}
	modules       []*WazeroModule
	closeMu       sync.RWMutex
	wasiInitMu    sync.Mutex
	hostModsMu    sync.Mutex
	modulesMu     sync.Mutex
	wasiInitDone  atomic.Bool
	closeComplete bool
	closed        bool
}

// Config holds configuration for engine creation
type Config struct {
	// CompilationCache optionally shares compiled machine code across runtimes.
	// Guest memories, host bindings and resources remain runtime-local. The caller
	// owns the cache and must close it only after all using runtimes have closed.
	CompilationCache wazero.CompilationCache

	// MemoryLimitPages sets the maximum memory per instance in pages (64KB each).
	// 0 means default (65536 pages = 4GB).
	// 256 = 16MB, 1024 = 64MB, 4096 = 256MB
	MemoryLimitPages uint32

	// CloseOnContextDone instruments guest execution to terminate on context
	// cancellation, including loops that never call a host import. Termination
	// closes the instance; it is not a resumable scheduling quantum.
	CloseOnContextDone bool

	// EnableThreads enables the WebAssembly threads proposal (experimental).
	// This allows atomic operations and shared memory within WASM modules.
	// Note: Thread operations are guest-only and not exposed to host functions.
	EnableThreads bool

	// UseInterpreter forces execution using the wazero interpreter engine instead
	// of the compiler engine.
	UseInterpreter bool
}

// NewWazeroEngine creates a new wazero-based engine
func NewWazeroEngine(ctx context.Context) (*WazeroEngine, error) {
	return NewWazeroEngineWithConfig(ctx, nil)
}

// NewWazeroEngineWithConfig creates a new engine with custom configuration
func NewWazeroEngineWithConfig(ctx context.Context, cfg *Config) (*WazeroEngine, error) {
	coreFeatures := api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling

	if cfg != nil && cfg.EnableThreads {
		coreFeatures |= experimental.CoreFeaturesThreads
	}

	var runtimeCfg wazero.RuntimeConfig
	if cfg != nil && cfg.UseInterpreter {
		runtimeCfg = wazero.NewRuntimeConfigInterpreter().WithCoreFeatures(coreFeatures)
	} else {
		runtimeCfg = wazero.NewRuntimeConfig().WithCoreFeatures(coreFeatures)
	}
	if cfg != nil && cfg.CompilationCache != nil {
		runtimeCfg = runtimeCfg.WithCompilationCache(cfg.CompilationCache)
	}

	if cfg != nil && cfg.MemoryLimitPages > 0 {
		runtimeCfg = runtimeCfg.WithMemoryLimitPages(cfg.MemoryLimitPages)
	}
	if cfg != nil && cfg.CloseOnContextDone {
		runtimeCfg = runtimeCfg.WithCloseOnContextDone(true)
	}

	runtime := wazero.NewRuntimeWithConfig(ctx, runtimeCfg)
	return &WazeroEngine{runtime: runtime, startups: newExecutionLifetime(), instances: make(map[*WazeroInstance]struct{})}, nil
}

// CompileConfig holds configuration for pre-compilation
type CompileConfig struct {
	// EnableAsyncify enables automatic asyncify transformation for components.
	EnableAsyncify bool
}

// InstanceConfig holds configuration for module instantiation
type InstanceConfig struct {
	// MemoryBudget optionally shares a logical linear-memory budget across
	// instances. Nil disables admission. It excludes host buffers/backing capacity
	// and is not an RSS limit. Internal candidate; W1 configuration is not wired.
	MemoryBudget *budget.Budget
	// OnCoreModuleClosed observes closure of owned cores after backend
	// instantiation. A failed Wasm start section can precede hook installation
	// and produce no notification; this callback is not a cleanup owner. It is called
	// after the instance execution domain stops and must not block or panic.
	// Engine core instantiation owns wazero's experimental close notifier slot;
	// opaque experimental.WithCloseNotifier context hooks are not composed.
	OnCoreModuleClosed func(context.Context, uint32)
	Stdout             io.Writer
	Stderr             io.Writer
	Stdin              io.Reader
	Env                map[string]string
	Name               string
	// EntryExport selects the canonical executable whose Asyncify state owns this call.
	EntryExport     string
	DecodeOptions   transcoder.DecodeOptions
	AsyncifyImports []string
	Args            []string
	Mounts          []Mount
	EnableAsyncify  bool
	// AsyncifyStackBytes requests an instance-owned Asyncify data header and
	// stack reservation for each transformed core. Zero selects the bounded
	// DefaultAsyncifyStackBytes reservation; allocation failures reject startup.
	AsyncifyStackBytes uint32
}

// Mount preopens a filesystem into the guest at Guest. FS is mounted when set;
// otherwise Host (a host directory path) is used.
type Mount struct {
	Guest    string
	FS       fs.FS
	Host     string
	ReadOnly bool
}

// applyWASIConfig threads preview1 args/env/stdio/preopens from cfg into the wazero
// module config so a core wasi_snapshot_preview1 guest can see them.
func applyWASIConfig(mc wazero.ModuleConfig, cfg *InstanceConfig) wazero.ModuleConfig {
	// wazero defaults to a fake clock and a zero random source, so a guest reads
	// 1970 and identical "random" bytes on every run. A language runtime needs
	// neither: TLS rejects every certificate as "not yet valid" against a 1970
	// clock, and a predictable random source is a real weakness once the guest can
	// reach the network. Give it the host's time and the host's entropy.
	mc = mc.WithSysWalltime().WithSysNanotime().WithRandSource(rand.Reader)
	if len(cfg.Args) > 0 {
		mc = mc.WithArgs(cfg.Args...)
	}
	for k, v := range cfg.Env {
		mc = mc.WithEnv(k, v)
	}
	if cfg.Stdout != nil {
		mc = mc.WithStdout(cfg.Stdout)
	}
	if cfg.Stderr != nil {
		mc = mc.WithStderr(cfg.Stderr)
	}
	if cfg.Stdin != nil {
		mc = mc.WithStdin(cfg.Stdin)
	}
	if len(cfg.Mounts) > 0 {
		fsCfg := wazero.NewFSConfig()
		for _, mnt := range cfg.Mounts {
			switch {
			case mnt.FS != nil:
				fsCfg = withCapabilityFSMount(fsCfg, mnt.FS, mnt.Guest, mnt.ReadOnly)
			case mnt.ReadOnly:
				fsCfg = fsCfg.WithReadOnlyDirMount(mnt.Host, mnt.Guest)
			default:
				fsCfg = fsCfg.WithDirMount(mnt.Host, mnt.Guest)
			}
		}
		mc = mc.WithFSConfig(fsCfg)
	}
	return mc
}

func (e *WazeroEngine) registerModule(m *WazeroModule) error {
	e.modulesMu.Lock()
	defer e.modulesMu.Unlock()
	if e.closed {
		return fmt.Errorf("engine is closed")
	}
	e.modules = append(e.modules, m)
	return nil
}

func (e *WazeroEngine) removeModule(target *WazeroModule) {
	e.modulesMu.Lock()
	defer e.modulesMu.Unlock()
	if e.closed {
		return
	}
	for i, m := range e.modules {
		if m == target {
			copy(e.modules[i:], e.modules[i+1:])
			e.modules[len(e.modules)-1] = nil
			e.modules = e.modules[:len(e.modules)-1]
			break
		}
	}
}

// IsClosed reports whether this engine has been closed.
func (e *WazeroEngine) IsClosed() bool {
	if e == nil {
		return true
	}
	e.modulesMu.Lock()
	defer e.modulesMu.Unlock()
	return e.closed
}

func (e *WazeroEngine) LoadModule(ctx context.Context, wasmBytes []byte) (*WazeroModule, error) {
	e.closeMu.RLock()
	defer e.closeMu.RUnlock()

	e.modulesMu.Lock()
	if e.closed {
		e.modulesMu.Unlock()
		return nil, fmt.Errorf("engine is closed")
	}
	e.modulesMu.Unlock()

	var canonRegistry *component.CanonRegistry
	var typeResolver *component.TypeResolver

	// Check if it's a component
	if component.IsComponent(wasmBytes) {
		// Decode and validate to get properly resolved types
		validated, err := component.DecodeAndValidate(wasmBytes)
		if err != nil {
			return nil, fmt.Errorf("decode component: %w", err)
		}
		comp := validated.Raw

		// Use the cumulative type index space built during decoding
		typeResolver = component.NewTypeResolverWithInstances(comp.TypeIndexSpace, comp.InstanceTypes)
		canonRegistry, err = component.NewCanonRegistry(comp, typeResolver)
		if err != nil {
			return nil, fmt.Errorf("build canon registry: %w", err)
		}

		// Check if this is a multi-module component
		if len(comp.CoreModules) > 1 || len(comp.CoreInstances) > 0 {
			// Store validated component for per-instantiation linker creation
			// Create shared compiler for layout caching
			compiler := transcoder.NewCompiler()

			mod := &WazeroModule{
				engine:        e,
				runtime:       e.runtime,
				compiler:      compiler,
				encoder:       transcoder.NewEncoderWithCompiler(compiler),
				decoder:       transcoder.NewDecoderWithCompiler(compiler),
				hostFuncs:     make(map[string]HostFunc),
				canonRegistry: canonRegistry,
				typeResolver:  typeResolver,
				validated:     validated,
			}
			if err := e.registerModule(mod); err != nil {
				return nil, err
			}
			return mod, nil
		}

		// Single module component - use first core module
		if len(comp.CoreModules) == 0 {
			return nil, fmt.Errorf("component has no core modules")
		}
		wasmBytes = comp.CoreModules[0]
	}

	compiled, err := e.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile failed: %w", err)
	}

	// Create shared compiler for layout caching across encoder/decoder
	compiler := transcoder.NewCompiler()

	mod := &WazeroModule{
		engine:        e,
		runtime:       e.runtime,
		compiled:      compiled,
		compiler:      compiler,
		encoder:       transcoder.NewEncoderWithCompiler(compiler),
		decoder:       transcoder.NewDecoderWithCompiler(compiler),
		hostFuncs:     make(map[string]HostFunc),
		canonRegistry: canonRegistry,
		typeResolver:  typeResolver,
		rawBytes:      wasmBytes,
	}

	e.modulesMu.Lock()
	if e.closed {
		e.modulesMu.Unlock()
		_ = compiled.Close(ctx)
		return nil, fmt.Errorf("engine is closed")
	}
	e.modules = append(e.modules, mod)
	e.modulesMu.Unlock()

	return mod, nil
}

// InitWASI instantiates the WASI singleton for this engine's runtime.
// Safe for concurrent calls from multiple modules sharing the same engine.
func (e *WazeroEngine) InitWASI(ctx context.Context) error {
	if e.wasiInitDone.Load() {
		return nil
	}

	e.wasiInitMu.Lock()
	defer e.wasiInitMu.Unlock()

	if e.wasiInitDone.Load() {
		return nil
	}

	if e.runtime.Module("wasi_snapshot_preview1") != nil {
		e.wasiInitDone.Store(true)
		return nil
	}

	_, err := InstantiateWASIWithAdapter(ctx, e.runtime)
	if err != nil {
		// If another path initialized WASI concurrently in the same runtime,
		// treat it as success and mark done.
		if e.runtime.Module("wasi_snapshot_preview1") == nil {
			return fmt.Errorf("instantiate WASI: %w", err)
		}
	}

	e.wasiInitDone.Store(true)
	return nil
}

// WazeroModule is a compiled WASM module
type WazeroModule struct {
	engine              *WazeroEngine
	runtime             wazero.Runtime
	compiled            wazero.CompiledModule
	canonRegistry       *component.CanonRegistry
	encoder             *transcoder.Encoder
	decoder             *transcoder.Decoder
	hostFuncs           map[string]HostFunc
	compiler            *transcoder.Compiler
	typeResolver        *component.TypeResolver
	validated           *component.ValidatedComponent
	cachedPre           *linker.InstancePre
	linker              *linker.Linker
	rawBytes            []byte
	transformed         bool
	asyncifyAddedMemory bool
	hostFuncsMu         sync.RWMutex
	cachedPreMu         sync.RWMutex

	closeMu   sync.Mutex
	compileMu sync.Mutex
	closed    bool
}

// Close releases compiled module resources owned by this module and unregisters
// it from the parent engine.
func (m *WazeroModule) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if m.engine != nil {
		m.engine.removeModule(m)
	}
	return m.close(ctx)
}

func (m *WazeroModule) close(ctx context.Context) error {
	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		return nil
	}
	m.closed = true
	cm := m.compiled
	m.compiled = nil
	m.closeMu.Unlock()

	m.cachedPreMu.Lock()
	pre := m.cachedPre
	m.cachedPre = nil
	m.cachedPreMu.Unlock()

	var firstErr error
	if cm != nil {
		if err := cm.Close(ctx); err != nil {
			firstErr = err
		}
	}
	if pre != nil {
		if err := pre.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// IsClosed reports whether this module has been closed.
func (m *WazeroModule) IsClosed() bool {
	if m == nil {
		return true
	}
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	return m.closed
}

// IsTransformed reports whether this module was transformed by our embedded
// asyncify transformer in this load.
func (m *WazeroModule) IsTransformed() bool {
	if m == nil {
		return false
	}
	return m.transformed
}

type HostFunc struct {
	Handler   any
	Wrapper   *LowerWrapper
	Raw       api.GoModuleFunc
	Namespace string
	Name      string
	ParamVT   []api.ValueType
	ResultVT  []api.ValueType
	Typed     bool
	IsAsync   bool
}

// RegisterHostFuncTyped registers a typed Go function that will be auto-wrapped
// using the Canon ABI. The handler signature is validated against the WIT import.
// It uses semver matching: a host at version X.Y.Z can satisfy imports for X.Y.W where W <= Z.
func (m *WazeroModule) RegisterHostFuncTyped(namespace, name string, handler any) error {
	if m.canonRegistry == nil {
		return fmt.Errorf("typed host functions require a component with canon imports")
	}

	// Find the lower definition using semver-aware matching
	lowerDef := m.findLowerDef(namespace, name)
	if lowerDef == nil {
		// Resource drops are represented in core space, not canon lowers.
		// Bind them via an explicit raw wrapper so linker can resolve
		// `[resource-drop]*` imports without trap stubs.
		if isResourceDropImport(name) {
			raw, err := buildResourceDropFunc(handler)
			if err != nil {
				return fmt.Errorf("create resource-drop wrapper: %w", err)
			}

			m.hostFuncsMu.Lock()
			defer m.hostFuncsMu.Unlock()

			key := namespace + "::" + name
			m.hostFuncs[key] = HostFunc{
				Namespace: namespace,
				Name:      name,
				Handler:   handler,
				Raw:       raw,
				ParamVT:   []api.ValueType{api.ValueTypeI32},
				ResultVT:  nil,
			}
			return nil
		}
		return fmt.Errorf("no canon lower found for import %q#%s", namespace, name)
	}

	// Create wrapper with validation
	wrapper, err := NewLowerWrapper(lowerDef, handler)
	if err != nil {
		return fmt.Errorf("create wrapper: %w", err)
	}

	if err := wrapper.ValidateHandler(); err != nil {
		return fmt.Errorf("handler validation: %w", err)
	}

	m.hostFuncsMu.Lock()
	defer m.hostFuncsMu.Unlock()

	key := namespace + "::" + name
	m.hostFuncs[key] = HostFunc{
		Namespace: namespace,
		Name:      name,
		Handler:   handler,
		Typed:     true,
		Wrapper:   wrapper,
	}
	return nil
}

// RegisterHostFuncRaw registers a host function with an explicit core signature.
//
// Typed registration lowers through the Canon ABI and therefore needs a component
// with canon imports; a core module has none, so its imports can only be satisfied
// by a function whose parameter and result types are stated outright. Marking the
// function async makes calls to it yield, which is what lets a core module block on
// host I/O.
func (m *WazeroModule) RegisterHostFuncRaw(
	namespace, name string,
	params, results []api.ValueType,
	fn api.GoModuleFunc,
	async bool,
) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("raw host function needs a namespace and a name")
	}
	if fn == nil {
		return fmt.Errorf("raw host function %q#%s has no handler", namespace, name)
	}

	m.hostFuncsMu.Lock()
	defer m.hostFuncsMu.Unlock()

	m.hostFuncs[namespace+"::"+name] = HostFunc{
		Namespace: namespace,
		Name:      name,
		Raw:       fn,
		ParamVT:   params,
		ResultVT:  results,
		IsAsync:   async,
	}
	return nil
}

// RegisterHostFuncTypedAsync registers a typed Go function as an async host function.
// Same as RegisterHostFuncTyped but marks the function as async (yields during execution).
func (m *WazeroModule) RegisterHostFuncTypedAsync(namespace, name string, handler any) error {
	if m.canonRegistry == nil {
		return fmt.Errorf("typed host functions require a component with canon imports")
	}

	lowerDef := m.findLowerDef(namespace, name)
	if lowerDef == nil {
		if isResourceDropImport(name) {
			return fmt.Errorf("resource-drop imports cannot be async: %q#%s", namespace, name)
		}
		return fmt.Errorf("no canon lower found for import %q#%s", namespace, name)
	}

	wrapper, err := NewLowerWrapper(lowerDef, handler)
	if err != nil {
		return fmt.Errorf("create wrapper: %w", err)
	}

	if err := wrapper.ValidateHandler(); err != nil {
		return fmt.Errorf("handler validation: %w", err)
	}

	m.hostFuncsMu.Lock()
	defer m.hostFuncsMu.Unlock()

	key := namespace + "::" + name
	m.hostFuncs[key] = HostFunc{
		Namespace: namespace,
		Name:      name,
		Handler:   handler,
		Typed:     true,
		Wrapper:   wrapper,
		IsAsync:   true,
	}
	return nil
}

// AsyncifyImports returns the list of import names that require asyncify transformation.
//
// Deprecated: wrapper for public API compatibility. Production paths call deriveAsyncifyImports().
func (m *WazeroModule) AsyncifyImports() []string {
	imports, err := m.deriveAsyncifyImports()
	if err != nil {
		Logger().Warn("AsyncifyImports: failed to derive imports", zap.Error(err))
	}
	return imports
}

// initHostModules initializes WASI and other host modules via the engine singleton.
func (m *WazeroModule) initHostModules(ctx context.Context) error {
	if err := m.engine.InitWASI(ctx); err != nil {
		return err
	}
	return m.initRawHostModules(ctx)
}

// initRawHostModules instantiates a wazero host module per namespace holding raw
// host functions.
//
// The linker materializes namespaces only for multi-module components; a single
// core module is instantiated straight against the wazero runtime, so an import of
// a raw host function would resolve to nothing and instantiation would fail with
// "module[...] not instantiated". Host modules live in the runtime's namespace and
// are shared by every instance, so each name is instantiated once.
func (m *WazeroModule) initRawHostModules(ctx context.Context) error {
	// This runs on every instantiation, so the common case of no raw host functions
	// must not allocate: scan first, group only when there is something to group.
	m.hostFuncsMu.RLock()
	hasRaw := false
	for _, hf := range m.hostFuncs {
		if hf.Raw != nil {
			hasRaw = true
			break
		}
	}
	if !hasRaw {
		m.hostFuncsMu.RUnlock()
		return nil
	}

	byNamespace := make(map[string][]HostFunc)
	for _, hf := range m.hostFuncs {
		if hf.Raw != nil {
			byNamespace[hf.Namespace] = append(byNamespace[hf.Namespace], hf)
		}
	}
	m.hostFuncsMu.RUnlock()

	m.engine.hostModsMu.Lock()
	defer m.engine.hostModsMu.Unlock()
	if m.engine.hostMods == nil {
		m.engine.hostMods = make(map[string]struct{})
	}

	for namespace, funcs := range byNamespace {
		if _, done := m.engine.hostMods[namespace]; done {
			continue
		}

		builder := m.engine.runtime.NewHostModuleBuilder(namespace)
		for _, hf := range funcs {
			builder.NewFunctionBuilder().
				WithGoModuleFunction(hf.Raw, hf.ParamVT, hf.ResultVT).
				Export(hf.Name)
		}
		if _, err := builder.Instantiate(ctx); err != nil {
			return fmt.Errorf("instantiate raw host module %q: %w", namespace, err)
		}
		m.engine.hostMods[namespace] = struct{}{}
	}

	return nil
}

// linkerConfig holds configuration for ensureLinker
type linkerConfig struct {
	AsyncifyImports   []string
	AsyncifyTransform bool
}

// ensureLinker creates the linker if needed and registers all host functions.
// Must be called with m.cachedPreMu held.
func (m *WazeroModule) ensureLinker(cfg linkerConfig) error {
	if m.linker != nil {
		return nil
	}

	// Auto-derive async imports from host registration + canon registry intersection
	if len(cfg.AsyncifyImports) == 0 {
		derived, err := m.deriveAsyncifyImports()
		if err != nil {
			return fmt.Errorf("derive asyncify imports: %w", err)
		}
		cfg.AsyncifyImports = derived
	}
	if len(cfg.AsyncifyImports) > 0 {
		cfg.AsyncifyTransform = true
	}

	opts := linker.Options{
		SemverMatching:    true,
		AsyncifyTransform: cfg.AsyncifyTransform,
		AsyncifyImports:   cfg.AsyncifyImports,
	}
	m.linker = linker.New(m.runtime, opts)

	// Register host functions with the linker's namespace
	m.hostFuncsMu.RLock()
	defer m.hostFuncsMu.RUnlock()

	for _, hf := range m.hostFuncs {
		// Resource drops are raw host funcs with explicit core signature.
		if hf.Raw != nil {
			ns := m.linker.Namespace(hf.Namespace)
			ns.DefineFunc(hf.Name, hf.Raw, hf.ParamVT, hf.ResultVT)
			continue
		}

		// Canon-lowered host functions.
		if !hf.Typed || hf.Wrapper == nil {
			continue // Skip invalid registrations
		}

		// Use the canonical WIT name from the LowerDef metadata
		witName := hf.Wrapper.Name()
		paramTypes := hf.Wrapper.FlatParamTypes()
		resultTypes := hf.Wrapper.FlatResultTypes()
		fn := m.buildTypedHostFunc(hf.Wrapper)

		ns := m.linker.Namespace(hf.Namespace)
		ns.DefineFunc(witName, fn, paramTypes, resultTypes)
		// Also register under the original name if different (for compatibility)
		if witName != hf.Name {
			ns.DefineFunc(hf.Name, fn, paramTypes, resultTypes)
		}
	}

	return nil
}

func isResourceDropImport(name string) bool {
	return len(name) > 15 && name[:15] == "[resource-drop]"
}

func buildResourceDropFunc(handler any) (api.GoModuleFunc, error) {
	switch h := handler.(type) {
	case func(context.Context, uint32):
		return func(ctx context.Context, _ api.Module, stack []uint64) {
			if len(stack) == 0 {
				return
			}
			h(ctx, uint32(stack[0]))
		}, nil
	case func(uint32):
		return func(_ context.Context, _ api.Module, stack []uint64) {
			if len(stack) == 0 {
				return
			}
			h(uint32(stack[0]))
		}, nil
	}

	rv := reflect.ValueOf(handler)
	if rv.Kind() != reflect.Func {
		return nil, fmt.Errorf("handler must be function, got %T", handler)
	}
	rt := rv.Type()

	argc := rt.NumIn()
	if argc != 1 && argc != 2 {
		return nil, fmt.Errorf("expected func(uint32) or func(context.Context,uint32), got %s", rt.String())
	}

	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	pos := 0
	withCtx := false
	if argc == 2 {
		if !rt.In(0).Implements(ctxType) {
			return nil, fmt.Errorf("first arg must implement context.Context, got %s", rt.In(0))
		}
		withCtx = true
		pos = 1
	}

	selfType := rt.In(pos)
	if selfType.Kind() != reflect.Uint32 {
		return nil, fmt.Errorf("self arg must be uint32, got %s", selfType)
	}
	if rt.NumOut() != 0 {
		return nil, fmt.Errorf("resource-drop handler must not return values, got %d", rt.NumOut())
	}

	return func(ctx context.Context, _ api.Module, stack []uint64) {
		if len(stack) == 0 {
			return
		}
		args := make([]reflect.Value, 0, argc)
		if withCtx {
			args = append(args, reflect.ValueOf(ctx))
		}
		args = append(args, reflect.ValueOf(uint32(stack[0])))
		rv.Call(args)
	}, nil
}

// Compile pre-compiles the module and validates imports.
// Call Compile at registration time to fail fast on missing imports (strict mode).
// It returns error if compilation fails or imports cannot be satisfied.
// After Compile, Instantiate will reuse the cached InstancePre.
func (m *WazeroModule) Compile(ctx context.Context, cfg *CompileConfig) error {
	if cfg == nil {
		cfg = &CompileConfig{}
	}

	if m.engine != nil {
		m.engine.closeMu.RLock()
		defer m.engine.closeMu.RUnlock()
	}

	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		return fmt.Errorf("module is closed")
	}
	m.closeMu.Unlock()

	// For single-module components or core modules, apply asyncify if needed
	if m.validated == nil {
		asyncImports, err := m.deriveAsyncifyImports()
		if err != nil {
			return fmt.Errorf("derive asyncify imports: %w", err)
		}
		if len(asyncImports) == 0 && !cfg.EnableAsyncify {
			return nil
		}

		m.compileMu.Lock()
		defer m.compileMu.Unlock()

		m.closeMu.Lock()
		if m.closed {
			m.closeMu.Unlock()
			return fmt.Errorf("module is closed")
		}
		if m.transformed {
			m.closeMu.Unlock()
			return nil
		}
		rawBytes := m.rawBytes
		m.closeMu.Unlock()

		if rawBytes != nil && !asyncify.IsAsyncified(rawBytes) && len(asyncImports) > 0 {
			originalMetadata, err := wasm.ParseModuleMetadata(rawBytes)
			if err != nil {
				return fmt.Errorf("parse asyncify source metadata: %w", err)
			}
			transformed, err := asyncify.Transform(rawBytes, asyncify.Config{
				AsyncImports:  asyncImports,
				ExportGlobals: true,
			})
			if err != nil {
				return fmt.Errorf("asyncify transform: %w", err)
			}
			metadata, err := wasm.ParseModuleMetadata(transformed)
			if err != nil {
				return fmt.Errorf("parse asyncify result metadata: %w", err)
			}
			addedMemory := len(originalMetadata.Memories) == 0 && originalMetadata.NumImportedMemories() == 0 && len(metadata.Memories) == 1 && metadata.NumImportedMemories() == 0
			compiled, err := m.runtime.CompileModule(ctx, transformed)
			if err != nil {
				return fmt.Errorf("recompile after asyncify: %w", err)
			}

			m.closeMu.Lock()
			if m.closed {
				m.closeMu.Unlock()
				_ = compiled.Close(ctx)
				return fmt.Errorf("module is closed")
			}
			oldCompiled := m.compiled
			m.compiled = compiled
			m.transformed = true
			m.asyncifyAddedMemory = addedMemory
			m.rawBytes = transformed
			m.closeMu.Unlock()

			if oldCompiled != nil {
				_ = oldCompiled.Close(ctx)
			}
		}
		return nil
	}

	// Multi-module component: create linker and cache InstancePre
	m.cachedPreMu.Lock()
	defer m.cachedPreMu.Unlock()

	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		return fmt.Errorf("module is closed")
	}
	m.closeMu.Unlock()

	if m.cachedPre != nil {
		return nil
	}

	if err := m.ensureLinker(linkerConfig{
		AsyncifyTransform: cfg.EnableAsyncify,
	}); err != nil {
		return fmt.Errorf("ensure linker: %w", err)
	}

	// Compile the component
	pre, err := m.linker.Instantiate(ctx, m.validated)
	if err != nil {
		return fmt.Errorf("compile component: %w", err)
	}

	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		_ = pre.Close(ctx)
		return fmt.Errorf("module is closed")
	}
	m.cachedPre = pre
	m.closeMu.Unlock()
	return nil
}

func (m *WazeroModule) Instantiate(ctx context.Context) (*WazeroInstance, error) {
	return m.InstantiateWithConfig(ctx, nil)
}

// InstantiateWithConfig creates an instance with custom configuration
func (m *WazeroModule) InstantiateWithConfig(ctx context.Context, cfg *InstanceConfig) (*WazeroInstance, error) {
	if m.engine != nil {
		startupCtx, finish, err := m.engine.enterInstantiation(ctx)
		if err != nil {
			return nil, err
		}
		defer finish()
		ctx = startupCtx
	}
	// If this is a multi-module component, use per-instantiation linker
	if m.validated != nil {
		return m.instantiateMultiModuleWithConfig(ctx, cfg)
	}

	m.closeMu.Lock()
	if m.closed || m.compiled == nil {
		m.closeMu.Unlock()
		return nil, fmt.Errorf("module is closed")
	}
	compiled := m.compiled
	m.closeMu.Unlock()

	// Initialize host modules
	if err := m.initHostModules(ctx); err != nil {
		return nil, err
	}

	// Build module config
	modConfig := wazero.NewModuleConfig()
	if cfg != nil && cfg.Name != "" {
		modConfig = modConfig.WithName(cfg.Name)
	} else {
		modConfig = modConfig.WithName("") // anonymous for parallel instantiation
	}
	if cfg != nil {
		modConfig = applyWASIConfig(modConfig, cfg)
	}
	// WASI reactors export _initialize (not _start); wazero's default only invokes
	// _start, so a reactor's libc/global-ctor init (which populates environ from the
	// host env) would never run. Invoke _initialize when the module exports it.
	if _, ok := compiled.ExportedFunctions()["_initialize"]; ok {
		modConfig = modConfig.WithStartFunctions("_initialize")
	}

	// Startup has an independent instance owner before any guest initializer.
	lifetime := newExecutionLifetime()
	startupCtx, finishStartup, err := lifetime.enter(ctx)
	if err != nil {
		lifetime.stop()
		return nil, err
	}
	transferred := false
	var admission *linker.MemoryAdmission
	defer func() {
		finishStartup()
		if !transferred {
			lifetime.stop()
			if admission != nil {
				admission.ReleaseAfterModulesClosed()
			}
		}
	}()
	admission, err = m.admitInstanceMemory(cfg, lifetime, nil)
	if err != nil {
		return nil, fmt.Errorf("memory admission: %w", err)
	}
	if admission != nil {
		startupCtx = admission.WithAllocator(startupCtx)
	}
	startupCtx = lifetime.withCoreCloseNotifier(startupCtx, cfg, admission)
	instance, err := m.runtime.InstantiateModule(startupCtx, compiled, modConfig)
	if err != nil {
		return nil, fmt.Errorf("instantiate failed: %w", err)
	}

	wazInst := &WazeroInstance{
		lifetime:       lifetime,
		admission:      admission,
		module:         m,
		instance:       instance,
		encoder:        m.encoder,
		decoder:        m.decoderForConfig(cfg),
		compiler:       m.compiler,
		stackBuf:       make([]uint64, 16), // pre-allocate stack buffer
		resources:      resource.NewTable(),
		transformed:    m.transformed,
		exportBindings: make(map[string]*exportBinding),
		allocatorCache: make(map[api.Function]*wazeroAllocator),
		memoryCache:    make(map[api.Memory]*WazeroMemory),
		asyncifyCache:  make(map[api.Module]*asyncifyCoreState),
	}

	// Cache memory
	if mem := instance.Memory(); hasMemory(mem) {
		wazInst.memory = &WazeroMemory{mem: mem}
		wazInst.memoryCache[mem] = wazInst.memory
	}

	// Cache allocator - try standard cabi_realloc first, then fallbacks.
	// Use the map key (export name) for ExportedFunction lookup, not
	// FunctionDefinition.Name() which returns empty for anonymous modules
	// instantiated with WithName("").
	allExports := instance.ExportedFunctionDefinitions()
	allocFnDef := allExports[CabiRealloc]
	allocName := CabiRealloc
	if allocFnDef == nil {
		allocFnDef = allExports[legacyRealloc]
		allocName = legacyRealloc
	}
	if allocFnDef == nil {
		allocFnDef = allExports[legacyAlloc]
		allocName = legacyAlloc
	}
	if allocFnDef == nil {
		allocFnDef = allExports[simpleAlloc]
		allocName = simpleAlloc
	}

	var isSimpleAlloc bool
	if allocFnDef != nil {
		wazInst.allocFn = instance.ExportedFunction(allocName)
		paramCount := len(allocFnDef.ParamTypes())
		isSimpleAlloc = paramCount < 4
	}

	// Cache free function
	if freeFn := instance.ExportedFunction(CabiFree); freeFn != nil {
		wazInst.freeFn = freeFn
	} else if freeFn := instance.ExportedFunction(legacyDealloc); freeFn != nil {
		wazInst.freeFn = freeFn
	} else if freeFn := instance.ExportedFunction(simpleFree); freeFn != nil {
		wazInst.freeFn = freeFn
	}

	// Create reusable allocator
	wazInst.alloc = &wazeroAllocator{
		allocFn:       wazInst.allocFn,
		freeFn:        wazInst.freeFn,
		stackBuf:      wazInst.stackBuf,
		isSimpleAlloc: isSimpleAlloc,
	}
	if wazInst.allocFn != nil {
		wazInst.allocatorCache[wazInst.allocFn] = wazInst.alloc
	}

	// An uninstrumented core remains synchronous; an instrumented core must
	// establish storage ownership before it can be published.
	if cfg != nil && cfg.EnableAsyncify {
		if err := wazInst.initializeAsyncify(startupCtx, cfg.AsyncifyStackBytes); err != nil {
			finishStartup()
			_ = wazInst.Close(context.WithoutCancel(ctx))
			return nil, fmt.Errorf("initialize owned asyncify stack: %w", err)
		}
	}

	if err := lifetime.startupResult(startupCtx); err != nil {
		finishStartup()
		_ = wazInst.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("instance startup stopped: %w", err)
	}
	if m.engine != nil {
		if err := m.engine.registerInstance(wazInst); err != nil {
			finishStartup()
			_ = wazInst.Close(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	transferred = true
	return wazInst, nil
}

// instantiateMultiModuleWithConfig handles multi-module instantiation with configuration.
func (m *WazeroModule) instantiateMultiModuleWithConfig(ctx context.Context, cfg *InstanceConfig) (*WazeroInstance, error) {
	enableAsyncify := cfg != nil && cfg.EnableAsyncify
	var asyncifyImports []string
	if cfg != nil {
		asyncifyImports = cfg.AsyncifyImports
	}

	// Get or create the cached InstancePre (single lock acquisition)
	m.cachedPreMu.Lock()
	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		m.cachedPreMu.Unlock()
		return nil, fmt.Errorf("module is closed")
	}
	m.closeMu.Unlock()

	if err := m.ensureLinker(linkerConfig{
		AsyncifyTransform: enableAsyncify,
		AsyncifyImports:   asyncifyImports,
	}); err != nil {
		m.cachedPreMu.Unlock()
		return nil, fmt.Errorf("ensure linker: %w", err)
	}

	pre := m.cachedPre
	if pre == nil {
		var err error
		pre, err = m.linker.Instantiate(ctx, m.validated)
		if err != nil {
			m.cachedPreMu.Unlock()
			return nil, fmt.Errorf("compile component: %w", err)
		}
		m.closeMu.Lock()
		if m.closed {
			m.closeMu.Unlock()
			m.cachedPreMu.Unlock()
			_ = pre.Close(ctx)
			return nil, fmt.Errorf("module is closed")
		}
		m.cachedPre = pre
		m.closeMu.Unlock()
	}
	m.cachedPreMu.Unlock()

	// Shared host setup uses the original context; only owned cores retain the
	// new instance's close notifier and startup execution scope.
	lifetime := newExecutionLifetime()
	startupCtx, finishStartup, err := lifetime.enter(ctx)
	if err != nil {
		lifetime.stop()
		return nil, err
	}
	transferred := false
	var admission *linker.MemoryAdmission
	defer func() {
		finishStartup()
		if !transferred {
			lifetime.stop()
			if admission != nil {
				admission.ReleaseAfterModulesClosed()
			}
		}
	}()
	admission, err = m.admitInstanceMemory(cfg, lifetime, pre)
	if err != nil {
		return nil, fmt.Errorf("memory admission: %w", err)
	}
	if admission != nil {
		startupCtx = admission.WithAllocator(startupCtx)
	}
	startupCtx = lifetime.withCoreCloseNotifier(startupCtx, cfg, admission)
	inst, err := pre.NewInstanceWithCoreContext(ctx, startupCtx)
	if err != nil {
		return nil, fmt.Errorf("instantiate component: %w", err)
	}

	// Get the final module for the WazeroInstance
	graph := inst.Graph()
	if graph == nil {
		inst.Close(ctx)
		return nil, fmt.Errorf("no instance graph")
	}

	mods := graph.ModuleInstantiations()
	if len(mods) == 0 {
		inst.Close(ctx)
		return nil, fmt.Errorf("no module instantiations")
	}

	lastMod := mods[len(mods)-1]
	selectedIdx := lastMod.InstanceIndex
	module := inst.GetModule(selectedIdx)
	if module == nil {
		inst.Close(ctx)
		return nil, fmt.Errorf("final module not found at index %d", selectedIdx)
	}

	// An adapter or fixup may be instantiated last. Select the executable
	// through the entry's canonical core-function index, never allocator names.
	var entryCanon *linker.CanonExport
	if cfg != nil && cfg.EntryExport != "" {
		lift := m.canonRegistry.FindLift(cfg.EntryExport)
		if lift == nil || int(lift.CoreFuncIdx) >= len(m.validated.Raw.CoreFuncIndexSpace) {
			inst.Close(ctx)
			return nil, fmt.Errorf("entry export %q has no canonical executable", cfg.EntryExport)
		}
		owner := m.validated.Raw.CoreFuncIndexSpace[lift.CoreFuncIdx]
		if owner.Kind != component.CoreFuncAliasExport {
			inst.Close(ctx)
			return nil, fmt.Errorf("entry export %q has unsupported executable ownership", cfg.EntryExport)
		}
		selectedIdx = owner.InstanceIdx
		module = inst.GetModule(selectedIdx)
		if module == nil {
			inst.Close(ctx)
			return nil, fmt.Errorf("entry export %q executable unavailable", cfg.EntryExport)
		}
		exp, ok := inst.GetExport(cfg.EntryExport)
		if !ok || exp.Canon == nil {
			inst.Close(ctx)
			return nil, fmt.Errorf("entry export %q canonical binding unavailable", cfg.EntryExport)
		}
		entryCanon = exp.Canon
	}
	// Inspect canonical exports to find canonical memory/allocator bindings.
	// If all canonical exports that declare memory share the same memory, select it
	// as the unambiguous shared canonical binding for default reporting (e.g. MemorySize).
	// If multiple distinct canonical memories exist across exports and no EntryExport was
	// specified, default reporting memory is left unset (nil) because no single memory can
	// represent multiple distinct cores, while each warm call binds to its export's exact memory.
	var defaultCanonMem api.Memory
	var defaultCanonRealloc api.Function
	var defaultCanonReallocMod api.Module
	var multipleCanonMemories bool
	var multipleCanonReallocs bool

	if m.canonRegistry != nil {
		for _, lift := range m.canonRegistry.AllLifts() {
			if exp, ok := inst.GetExport(lift.Name); ok && exp.Canon != nil {
				if exp.Canon.Memory != nil {
					if defaultCanonMem == nil {
						defaultCanonMem = exp.Canon.Memory
					} else if defaultCanonMem != exp.Canon.Memory {
						multipleCanonMemories = true
					}
				}
				if exp.Canon.Realloc != nil {
					if defaultCanonRealloc == nil {
						defaultCanonRealloc = exp.Canon.Realloc
						defaultCanonReallocMod = exp.Canon.ReallocMod
					} else if defaultCanonRealloc != exp.Canon.Realloc {
						multipleCanonReallocs = true
					}
				}
			}
		}
	}

	var mem api.Memory
	var allocFn api.Function
	var allocMod api.Module
	if entryCanon != nil {
		mem = entryCanon.Memory
		allocFn = entryCanon.Realloc
		allocMod = entryCanon.ReallocMod
	} else if multipleCanonMemories {
		// Explicitly defined behavior for multiple memories without EntryExport:
		// Do not arbitrarily pick one core's memory. Memory is left nil for instance-level reporting;
		// individual export calls bind to their respective core's canon memory.
		mem = nil
		allocFn = nil
		allocMod = nil
	} else if defaultCanonMem != nil {
		// Unambiguous shared canonical binding: all canonical exports with memory share this memory.
		mem = defaultCanonMem
		if !multipleCanonReallocs {
			allocFn = defaultCanonRealloc
			allocMod = defaultCanonReallocMod
		}
	} else {
		// Fallback for modules with no canonical memory declarations.
		// Find a module that exports memory and an allocator bound to the same memory,
		// preventing foreign memory mismatches.
		for _, mod := range inst.Modules() {
			if mod == nil {
				continue
			}
			m := mod.Memory()
			if !hasMemory(m) {
				continue
			}
			if mem == nil {
				mem = m
			}
			if mod.Memory() == mem && allocFn == nil {
				for _, name := range []string{CabiRealloc, legacyRealloc, "alloc", "malloc"} {
					if rf := mod.ExportedFunction(name); rf != nil {
						allocFn = rf
						allocMod = localFunctionOwner(mod, name)
						break
					}
				}
			}
			if mem != nil && allocFn != nil {
				break
			}
		}
	}

	// Create WazeroInstance wrapper
	wazInst := &WazeroInstance{
		lifetime:             lifetime,
		admission:            admission,
		module:               m,
		instance:             module,
		encoder:              m.encoder,
		decoder:              m.decoderForConfig(cfg),
		compiler:             m.compiler,
		stackBuf:             make([]uint64, 16),
		linkerInst:           inst,
		resources:            resource.NewTable(),
		transformed:          inst.IsInstanceTransformed(selectedIdx),
		exportBindings:       make(map[string]*exportBinding),
		allocatorCache:       make(map[api.Function]*wazeroAllocator),
		memoryCache:          make(map[api.Memory]*WazeroMemory),
		asyncifyCache:        make(map[api.Module]*asyncifyCoreState),
		asyncifyReservations: make(map[api.Module]asyncifyStackReservation),
		asyncifyEnabled:      enableAsyncify,
	}

	// Cache memory
	if hasMemory(mem) {
		wazInst.memory = &WazeroMemory{mem: mem}
		wazInst.memoryCache[mem] = wazInst.memory
	}

	// Cache allocator
	var isSimpleAlloc bool
	wazInst.allocFn = allocFn
	if wazInst.allocFn != nil {
		paramCount := len(wazInst.allocFn.Definition().ParamTypes())
		isSimpleAlloc = paramCount < 4
	}

	// Use only the allocator module established while resolving its canonical
	// option. Never infer a bound instance from shared function definitions.
	freeFn := localFreeFunction(allocMod)

	wazInst.freeFn = freeFn

	// Create reusable allocator
	if wazInst.allocFn != nil {
		wazInst.alloc = &wazeroAllocator{
			allocFn:       wazInst.allocFn,
			freeFn:        wazInst.freeFn,
			stackBuf:      wazInst.stackBuf,
			isSimpleAlloc: isSimpleAlloc,
		}
		wazInst.allocatorCache[wazInst.allocFn] = wazInst.alloc
	}

	if enableAsyncify {
		if err := wazInst.initializeAsyncify(startupCtx, cfg.AsyncifyStackBytes); err != nil {
			finishStartup()
			_ = wazInst.Close(context.WithoutCancel(ctx))
			return nil, fmt.Errorf("initialize owned asyncify stack: %w", err)
		}
	}

	if err := lifetime.startupResult(startupCtx); err != nil {
		finishStartup()
		_ = wazInst.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("instance startup stopped: %w", err)
	}
	if m.engine != nil {
		if err := m.engine.registerInstance(wazInst); err != nil {
			finishStartup()
			_ = wazInst.Close(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	transferred = true
	return wazInst, nil
}

func (m *WazeroModule) decoderForConfig(cfg *InstanceConfig) *transcoder.Decoder {
	if cfg == nil || cfg.DecodeOptions == (transcoder.DecodeOptions{}) {
		return m.decoder
	}
	return m.decoder.WithOptions(cfg.DecodeOptions)
}

// WazeroInstance is a running WASM instance.
// It is NOT safe for concurrent use from multiple goroutines.
// Each goroutine should have its own Instance, or access must be synchronized externally.
// Close is an exception: concurrent callers share one teardown owner and may
// cancel their wait without reclaiming resources still owned by that teardown.
type WazeroInstance struct {
	closeErr             error
	freeFn               api.Function
	allocFn              api.Function
	instance             api.Module
	memory               *WazeroMemory
	asyncify             *Asyncify
	admission            *linker.MemoryAdmission
	closeAttempt         chan struct{}
	lifetime             *executionLifetime
	linkerInst           *linker.Instance
	scheduler            *Scheduler
	compiler             *transcoder.Compiler
	resources            *resource.UnifiedTable
	decoder              *transcoder.Decoder
	asyncifyReservations map[api.Module]asyncifyStackReservation
	alloc                *wazeroAllocator
	module               *WazeroModule
	exportBindings       map[string]*exportBinding
	encoder              *transcoder.Encoder
	activeSession        *CallSession
	// asyncifyPoison records a failed async execution whose complete core-state
	// recovery cannot be proved. Asyncify can have parked frames in a child core
	// reached through a component bridge, while the public CallSession only owns
	// the root scheduler. Do not make a later call look safe by merely dropping
	// that root session; callers must close the instance before reusing it.
	asyncifyPoison          error
	asyncifyCache           map[api.Module]*asyncifyCoreState
	memoryCache             map[api.Memory]*WazeroMemory
	allocatorCache          map[api.Function]*wazeroAllocator
	stackBuf                []uint64
	bindingMu               sync.RWMutex
	asyncifyConfig          AsyncifyConfig
	closeMu                 sync.Mutex
	asyncifyOwnedStackBytes uint32
	closed                  bool
	transformed             bool
	asyncifyEnabled         bool
}

// IsTransformed reports whether this instance was proven by trusted linker/engine
// metadata to have been transformed by our embedded transformer in this load.
func (i *WazeroInstance) IsTransformed() bool {
	if i == nil {
		return false
	}
	return i.transformed
}

// getExportedFunction returns an exported function, using linker for multi-module components
func (i *WazeroInstance) getExportedFunction(name string) api.Function {
	if i.linkerInst != nil {
		return i.linkerInst.ExportedFunction(name)
	}
	if i.instance == nil {
		return nil
	}
	return i.instance.ExportedFunction(name)
}

// GetExportedFunction returns a lifetime-bound raw core function. The handle
// keeps its metadata after Close, but cannot execute after shutdown begins.
// As with wazero functions, callers must not invoke one handle concurrently.
func (i *WazeroInstance) GetExportedFunction(name string) api.Function {
	i.closeMu.Lock()
	defer i.closeMu.Unlock()
	if i.closed || i.closeAttempt != nil {
		return nil
	}
	fn := i.getExportedFunction(name)
	if fn == nil {
		return nil
	}
	return &instanceFunction{Function: fn, owner: i}
}

// HasMemory reports whether the instance has linear memory.
func (i *WazeroInstance) HasMemory() bool {
	return i != nil && i.memory != nil && hasMemory(i.memory.mem)
}

// MemorySize returns the current linear memory size in bytes, or 0 if no memory.
func (i *WazeroInstance) MemorySize() uint32 {
	if !i.HasMemory() {
		return 0
	}
	return i.memory.Size()
}

// resourceCallContext carries the resource table for synchronous call paths.
// It embeds the linker context by value so synthetic shims retain their owned
// instance identity through arbitrary standard context wrappers.
type resourceCallContext struct {
	linker.InstanceContext
	resources *resource.UnifiedTable
}

func (c *resourceCallContext) Value(key any) any {
	switch key {
	case resourcesContextKey{}:
		return c.resources
	default:
		return c.InstanceContext.Value(key)
	}
}

// prepareCallContext supplies the resource table and linker instance for host
// handlers called from synthetic shim modules without an instance ID suffix.
func (i *WazeroInstance) prepareCallContext(ctx context.Context) context.Context {
	if ctx == nil {
		panic("cannot create context from nil parent")
	}
	return &resourceCallContext{
		InstanceContext: linker.NewInstanceContext(ctx, i.linkerInst),
		resources:       i.resources,
	}
}

// EnableAsyncify configures owned suspension storage when DataAddr is zero.
// A positive DataAddr is an advanced contract: the caller must reserve the full
// header and stack in each affected core memory before calling this method.
func (i *WazeroInstance) EnableAsyncify(config AsyncifyConfig) error {
	if config.ownedStackBytes != 0 {
		return fmt.Errorf("asyncify: owned stack configuration is internal")
	}
	if config.DataAddr == 0 {
		return i.enableOwnedAsyncify(context.Background(), config.StackSize)
	}
	return i.enableAsyncify(context.Background(), config)
}

func (i *WazeroInstance) enableAsyncify(ctx context.Context, config AsyncifyConfig) error {
	if ctx == nil {
		return fmt.Errorf("asyncify: nil execution context")
	}
	// Reconfiguration writes guest memory and inspects owned core modules. It
	// must finish before shutdown may reclaim them, including during startup.
	_, finish, err := i.enterExecution(ctx)
	if err != nil {
		return err
	}
	defer finish()

	i.bindingMu.Lock()
	defer i.bindingMu.Unlock()

	ownedStackBytes := config.ownedStackBytes
	if ownedStackBytes == 0 && len(i.asyncifyReservations) != 0 {
		return fmt.Errorf("asyncify: instance has owned stack reservations; reconfigure through the same instance-owned stack policy")
	}

	if i.activeSession != nil && !i.activeSession.lifted {
		activeName := ""
		if i.activeSession.binding != nil {
			activeName = i.activeSession.binding.name
		}
		if activeName != "" {
			return fmt.Errorf("cannot enable asyncify: instance has an active suspended session %q; resume or complete active call before reconfiguring", activeName)
		}
		return fmt.Errorf("cannot enable asyncify: instance has an active suspended session; resume or complete active call before reconfiguring")
	}

	a := NewAsyncify()
	if config.StackSize > 0 {
		a.SetStackSize(config.StackSize)
	}
	if config.DataAddr > 0 {
		a.SetDataAddr(config.DataAddr)
	}
	// For a component, trusted controls belong to the exact core selected
	// below. A transformed entry core must not bless a sibling merely because
	// that sibling exports Asyncify-shaped functions.
	a.trusted = i.transformed

	initMod := i.instance
	if (initMod == nil || initMod.ExportedFunction("asyncify_get_state") == nil) && i.linkerInst != nil {
		for _, mod := range i.linkerInst.Modules() {
			if mod != nil && mod.ExportedFunction("asyncify_get_state") != nil {
				initMod = mod
				break
			}
		}
	}
	if initMod == nil {
		if i.instance != nil {
			initMod = i.instance
		} else {
			return fmt.Errorf("instance is closed or uninitialized")
		}
	}
	if i.linkerInst != nil {
		a.trusted = i.linkerInst.IsModuleTransformed(initMod)
	}
	if ownedStackBytes != 0 {
		if err := i.prepareOwnedAsyncifyReservationsLocked(ctx, initMod, ownedStackBytes); err != nil {
			return err
		}
		reservation, err := i.ownedAsyncifyReservationLocked(initMod, ownedStackBytes)
		if err != nil {
			return err
		}
		a.SetStackSize(ownedStackBytes)
		a.SetDataAddr(reservation.dataAddr)
	}

	header, err := a.prepareInit(initMod)
	if err != nil {
		return err
	}
	headers := []asyncifyHeader{header}
	generation := &asyncifyGeneration{}
	a.generation = generation
	a.lifetime = i.lifetime

	newSched := NewScheduler(a)
	newCache := make(map[api.Module]*asyncifyCoreState)
	if initMod != nil {
		newCache[initMod] = &asyncifyCoreState{asyncify: a, scheduler: newSched}
	}

	resolveAsyncForMod := func(mod api.Module, modIdx int) (*Asyncify, *Scheduler, error) {
		if mod == nil || mod == initMod {
			return a, newSched, nil
		}
		if entry, ok := newCache[mod]; ok {
			return entry.asyncify, entry.scheduler, nil
		}
		if mod.ExportedFunction("asyncify_get_state") == nil {
			return nil, nil, nil
		}
		modA := NewAsyncify()
		if config.StackSize > 0 {
			modA.SetStackSize(config.StackSize)
		}
		if config.DataAddr > 0 {
			modA.SetDataAddr(config.DataAddr)
		}
		if ownedStackBytes != 0 {
			reservation, err := i.ownedAsyncifyReservationLocked(mod, ownedStackBytes)
			if err != nil {
				return nil, nil, err
			}
			modA.SetStackSize(ownedStackBytes)
			modA.SetDataAddr(reservation.dataAddr)
		}
		if i.linkerInst != nil {
			modA.trusted = i.linkerInst.IsModuleTransformed(mod)
		} else {
			modA.trusted = i.transformed
		}
		header, err := modA.prepareInit(mod)
		if err != nil {
			return nil, nil, err
		}
		headers = append(headers, header)
		modA.generation = generation
		modA.lifetime = i.lifetime
		modSched := NewScheduler(modA)
		newCache[mod] = &asyncifyCoreState{asyncify: modA, scheduler: modSched}
		return modA, modSched, nil
	}

	// An owned reservation is a per-core contract. Initialize every current
	// Asyncify core while all ownership checks and headers are transactional,
	// rather than deferring an unproven core until its first export call.
	if ownedStackBytes != 0 {
		for _, mod := range i.asyncifyStackModulesLocked(initMod) {
			if _, _, err := resolveAsyncForMod(mod, -1); err != nil {
				return fmt.Errorf("configure owned asyncify core %q: %w", asyncifyCoreName(mod), err)
			}
		}
	}

	// Rebind any existing cached export bindings consistently so they do not stay stale.
	newBindings := make(map[string]*exportBinding, len(i.exportBindings))
	for name, oldBinding := range i.exportBindings {
		var exportAsync *Asyncify
		var exportSched *Scheduler
		if i.linkerInst != nil {
			var err error
			exportAsync, exportSched, err = resolveAsyncForMod(oldBinding.coreMod, oldBinding.coreModIdx)
			if err != nil {
				return fmt.Errorf("rebind asyncify for export %q: %w", name, err)
			}
		} else {
			exportAsync = a
			exportSched = newSched
		}

		newBindings[name] = &exportBinding{
			name:        oldBinding.name,
			fn:          oldBinding.fn,
			postReturn:  oldBinding.postReturn,
			coreMod:     oldBinding.coreMod,
			coreModIdx:  oldBinding.coreModIdx,
			memory:      oldBinding.memory,
			alloc:       oldBinding.alloc,
			paramTypes:  oldBinding.paramTypes,
			resultTypes: oldBinding.resultTypes,
			asyncify:    exportAsync,
			scheduler:   exportSched,
		}
	}

	// All fallible preparation is complete. Commit guest headers and revoke the
	// old configuration before publishing the replacement; caller serialization
	// excludes guest execution throughout this transaction.
	for _, header := range headers {
		header.commit()
	}
	if i.asyncify != nil && i.asyncify.generation != nil {
		i.asyncify.generation.revoked.Store(true)
	}
	// Publish state only after init and rebinding succeed
	i.asyncifyConfig = config
	if ownedStackBytes != 0 {
		i.asyncifyOwnedStackBytes = ownedStackBytes
	}
	i.asyncifyEnabled = true
	i.asyncify = a
	i.scheduler = newSched
	i.asyncifyCache = newCache
	i.exportBindings = newBindings
	if i.linkerInst != nil {
		controllers := make(map[api.Module]asyncify.RuntimeController, len(newCache))
		for mod, state := range newCache {
			if mod != nil && state != nil && state.asyncify != nil {
				controllers[mod] = state.asyncify
			}
		}
		i.linkerInst.SetAsyncifyControllers(controllers)
	}
	return nil
}

// Asyncify returns the asyncify runtime if enabled.
func (i *WazeroInstance) Asyncify() *Asyncify {
	i.bindingMu.Lock()
	defer i.bindingMu.Unlock()
	return i.asyncify
}

// Scheduler returns the async scheduler if enabled.
func (i *WazeroInstance) Scheduler() *Scheduler {
	i.bindingMu.Lock()
	defer i.bindingMu.Unlock()
	return i.scheduler
}

// RunAsync executes a function with asyncify event loop support.
// It returns after the function completes, processing any async operations.
func (i *WazeroInstance) RunAsync(ctx context.Context, name string, args ...uint64) ([]uint64, error) {
	ctx, finish, err := i.enterExecution(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	binding, err := i.getExportBinding(name)
	if err != nil {
		return nil, err
	}

	if err := i.checkSuspended(binding); err != nil {
		return nil, err
	}

	ctx = i.prepareCallContext(ctx)

	sched := binding.scheduler
	async := binding.asyncify
	if sched == nil || async == nil {
		return binding.fn.Call(ctx, args...)
	}

	ctx = WithAsyncify(ctx, async)
	ctx = WithScheduler(ctx, sched)
	results, err := sched.Run(ctx, binding.fn, args...)
	if err != nil {
		i.poisonAsyncify(err)
	}
	return results, err
}

// CallWithLift calls a function using cached lift information from canon registry.
// It is faster than Call for repeated invocations as it caches lookup results.
func (i *WazeroInstance) CallWithLift(ctx context.Context, funcName string, params ...any) (any, error) {
	ctx, finish, err := i.enterExecution(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	binding, err := i.getExportBinding(funcName)
	if err != nil {
		return nil, err
	}
	if err := i.checkSuspended(binding); err != nil {
		return nil, err
	}
	ctx = i.prepareCallContext(ctx)

	// Try fast path for primitive types
	if result, ok, err := i.tryFastCall(ctx, binding, binding.paramTypes, binding.resultTypes, params); ok {
		return result, err
	}

	// Fallback to general path
	return i.callGeneral(ctx, binding, binding.paramTypes, binding.resultTypes, params)
}

// CallWithTypes calls a WASM function with explicit WIT type information
func (i *WazeroInstance) CallWithTypes(ctx context.Context, funcName string, paramTypes []wit.Type, resultTypes []wit.Type, params ...any) (any, error) {
	ctx, finish, err := i.enterExecution(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	binding, err := i.getExportBinding(funcName)
	if err != nil {
		return nil, err
	}
	if err := i.checkSuspended(binding); err != nil {
		return nil, err
	}
	ctx = i.prepareCallContext(ctx)

	// Try fast path for primitive types
	if result, ok, err := i.tryFastCall(ctx, binding, paramTypes, resultTypes, params); ok {
		return result, err
	}

	// Try compiled fast path for structs/lists
	if result, ok, err := i.tryCallCompiled(ctx, binding, paramTypes, resultTypes, params); ok {
		return result, err
	}

	// Fallback to general path
	return i.callGeneral(ctx, binding, paramTypes, resultTypes, params)
}

// CallInto decodes results directly into caller's memory without intermediate allocation.
// result must be a pointer to the target type (e.g., *string, *uint32, *MyStruct).
// For void returns, pass nil.
// For strings, the result points directly into WASM memory and is only valid
// while the instance is alive.
func (i *WazeroInstance) CallInto(ctx context.Context, funcName string, paramTypes []wit.Type, resultTypes []wit.Type, result any, params ...any) error {
	ctx, finish, err := i.enterExecution(ctx)
	if err != nil {
		return err
	}
	defer finish()
	binding, err := i.getExportBinding(funcName)
	if err != nil {
		return err
	}
	if err := i.checkSuspended(binding); err != nil {
		return err
	}
	ctx = i.prepareCallContext(ctx)

	// Try fast path for string -> string
	if handled, err := i.tryCallStringInto(ctx, binding, paramTypes, resultTypes, result, params); handled {
		return err
	}

	// Try fast path for primitives
	if handled, err := i.tryCallPrimitiveInto(ctx, binding, paramTypes, resultTypes, result, params); handled {
		return err
	}

	// Try fast path for compiled types (structs, typed slices) using stack-based operations
	if handled, err := i.tryCallCompiledInto(ctx, binding, paramTypes, resultTypes, result, params); handled {
		return err
	}

	// General path
	return i.callGeneralInto(ctx, binding, paramTypes, resultTypes, result, params)
}

// wazeroAllocator implements wasmruntime.Allocator using wazero functions
type wazeroAllocator struct {
	allocFn       api.Function
	freeFn        api.Function
	currentCtx    context.Context
	stackBuf      []uint64
	stackMutex    sync.Mutex
	isSimpleAlloc bool
}

func (a *wazeroAllocator) setContext(ctx context.Context) {
	if a == nil {
		return
	}
	a.stackMutex.Lock()
	defer a.stackMutex.Unlock()
	a.currentCtx = ctx
}

func (a *wazeroAllocator) Alloc(size, align uint32) (uint32, error) {
	if a == nil {
		return 0, fmt.Errorf("no allocator available")
	}
	a.stackMutex.Lock()
	ctx := a.currentCtx
	a.stackMutex.Unlock()
	return a.AllocContext(ctx, size, align)
}

// AllocContext performs allocation under the supplied execution context. It is
// used for instance-owned reservations so an untrusted guest allocator sees the
// caller's cancellation and linker/resource identity instead of Background.
func (a *wazeroAllocator) AllocContext(ctx context.Context, size, align uint32) (uint32, error) {
	if a == nil || a.allocFn == nil {
		return 0, fmt.Errorf("no allocator available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.stackMutex.Lock()
	defer a.stackMutex.Unlock()

	if a.isSimpleAlloc {
		a.stackBuf[0] = uint64(size)
		err := a.allocFn.CallWithStack(ctx, a.stackBuf[:1])
		if err != nil {
			return 0, err
		}
		ptr := uint32(a.stackBuf[0])
		if ptr == 0 && size > 0 {
			return 0, fmt.Errorf("allocator returned null pointer (size=%d)", size)
		}
		return ptr, nil
	}
	a.stackBuf[0] = 0
	a.stackBuf[1] = 0
	a.stackBuf[2] = uint64(align)
	a.stackBuf[3] = uint64(size)
	err := a.allocFn.CallWithStack(ctx, a.stackBuf[:4])
	if err != nil {
		return 0, err
	}
	ptr := uint32(a.stackBuf[0])
	if ptr == 0 && size > 0 {
		return 0, fmt.Errorf("cabi_realloc returned null pointer (size=%d, align=%d)", size, align)
	}
	return ptr, nil
}

func (a *wazeroAllocator) Free(ptr, size, align uint32) {
	if a != nil && a.freeFn != nil && ptr != 0 {
		a.stackMutex.Lock()
		defer a.stackMutex.Unlock()

		ctx := a.currentCtx
		if ctx == nil {
			ctx = context.Background()
		}

		a.stackBuf[0] = uint64(ptr)
		a.stackBuf[1] = uint64(size)
		a.stackBuf[2] = uint64(align)
		if err := a.freeFn.CallWithStack(ctx, a.stackBuf[:3]); err != nil {
			Logger().Warn("Free: failed to call cabi_realloc for deallocation",
				zap.Uint32("ptr", ptr),
				zap.Uint32("size", size),
				zap.Error(err))
		}
	}
}

func (i *WazeroInstance) Close(ctx context.Context) error {
	if i.admission != nil {
		i.admission.Stop()
	}
	if i.lifetime != nil {
		i.lifetime.stop()
		if i.lifetime.heldBy(ctx) {
			return ErrCloseFromExecution
		}
		if err := i.lifetime.wait(ctx); err != nil {
			return err
		}
	}
	owner, err := i.beginClose(ctx)
	if !owner {
		return err
	}
	var firstErr error
	defer func() { i.finishClose(firstErr) }()
	// All guarded execution has returned; a suspended session may still own a
	// pending host-operation context. Close it before releasing host resources.
	i.bindingMu.Lock()
	if i.activeSession != nil {
		i.activeSession.closeExecution()
	}
	i.bindingMu.Unlock()

	if i.resources != nil {
		if err := i.resources.Close(); err != nil {
			firstErr = err
		}
		i.resources = nil
	}
	// Close linker instance if present (for multi-module components)
	if i.linkerInst != nil {
		if err := i.linkerInst.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		i.linkerInst = nil
	}
	if i.instance != nil {
		if err := i.instance.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		i.instance = nil
	}
	if i.admission != nil {
		i.admission.ReleaseAfterModulesClosed()
	}
	// Clear references to help GC
	i.memory = nil
	i.allocFn = nil
	i.freeFn = nil
	i.alloc = nil
	i.stackBuf = nil
	i.bindingMu.Lock()
	i.exportBindings = nil
	i.allocatorCache = nil
	i.memoryCache = nil
	i.asyncifyCache = nil
	i.activeSession = nil
	i.bindingMu.Unlock()
	if i.module != nil && i.module.engine != nil {
		i.module.engine.removeInstance(i)
	}
	return firstErr
}

// WazeroMemory wraps wazero memory to implement wasmruntime.Memory
type WazeroMemory struct {
	mem api.Memory
}

func hasMemory(mem api.Memory) bool {
	if mem == nil {
		return false
	}
	v := reflect.ValueOf(mem)
	return v.Kind() != reflect.Pointer || !v.IsNil()
}

func (m *WazeroMemory) Read(offset uint32, length uint32) ([]byte, error) {
	if m == nil || m.mem == nil {
		return nil, fmt.Errorf("memory not available")
	}
	data, ok := m.mem.Read(offset, length)
	if !ok {
		return nil, fmt.Errorf("read out of bounds: offset=%d, length=%d", offset, length)
	}
	return data, nil
}

func (m *WazeroMemory) Write(offset uint32, data []byte) error {
	if m == nil || m.mem == nil {
		return fmt.Errorf("memory not available")
	}
	ok := m.mem.Write(offset, data)
	if !ok {
		return fmt.Errorf("write out of bounds: offset=%d, length=%d", offset, len(data))
	}
	return nil
}

func (m *WazeroMemory) ReadU8(offset uint32) (uint8, error) {
	if m == nil || m.mem == nil {
		return 0, fmt.Errorf("memory not available")
	}
	data, err := m.Read(offset, 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (m *WazeroMemory) ReadU16(offset uint32) (uint16, error) {
	if m == nil || m.mem == nil {
		return 0, fmt.Errorf("memory not available")
	}
	data, err := m.Read(offset, 2)
	if err != nil {
		return 0, err
	}
	return uint16(data[0]) | uint16(data[1])<<8, nil
}

func (m *WazeroMemory) ReadU32(offset uint32) (uint32, error) {
	if m == nil || m.mem == nil {
		return 0, fmt.Errorf("memory not available")
	}
	val, ok := m.mem.ReadUint32Le(offset)
	if !ok {
		return 0, fmt.Errorf("read out of bounds")
	}
	return val, nil
}

func (m *WazeroMemory) ReadU64(offset uint32) (uint64, error) {
	if m == nil || m.mem == nil {
		return 0, fmt.Errorf("memory not available")
	}
	val, ok := m.mem.ReadUint64Le(offset)
	if !ok {
		return 0, fmt.Errorf("read out of bounds")
	}
	return val, nil
}

func (m *WazeroMemory) WriteU8(offset uint32, value uint8) error {
	if m == nil || m.mem == nil {
		return fmt.Errorf("memory not available")
	}
	if !m.mem.WriteByte(offset, value) {
		return fmt.Errorf("write out of bounds: offset=%d, length=1", offset)
	}
	return nil
}

func (m *WazeroMemory) WriteU16(offset uint32, value uint16) error {
	if m == nil || m.mem == nil {
		return fmt.Errorf("memory not available")
	}
	if !m.mem.WriteUint16Le(offset, value) {
		return fmt.Errorf("write out of bounds: offset=%d, length=2", offset)
	}
	return nil
}

func (m *WazeroMemory) WriteU32(offset uint32, value uint32) error {
	if m == nil || m.mem == nil {
		return fmt.Errorf("memory not available")
	}
	ok := m.mem.WriteUint32Le(offset, value)
	if !ok {
		return fmt.Errorf("write out of bounds")
	}
	return nil
}

func (m *WazeroMemory) WriteU64(offset uint32, value uint64) error {
	if m == nil || m.mem == nil {
		return fmt.Errorf("memory not available")
	}
	ok := m.mem.WriteUint64Le(offset, value)
	if !ok {
		return fmt.Errorf("write out of bounds")
	}
	return nil
}

// buildTypedHostFunc creates a GoModuleFunc from a LowerWrapper
func (m *WazeroModule) buildTypedHostFunc(wrapper *LowerWrapper) api.GoModuleFunc {
	return wrapper.BuildRawFunc()
}

func (m *WazeroMemory) Size() uint32 {
	if m == nil || !hasMemory(m.mem) {
		return 0
	}
	return m.mem.Size()
}

// Compile-time check that WazeroMemory implements wasmruntime.Memory and MemorySizer
var _ wasmruntime.Memory = (*WazeroMemory)(nil)
var _ wasmruntime.MemorySizer = (*WazeroMemory)(nil)

// Compile-time check that wazeroAllocator implements wasmruntime.Allocator
var _ wasmruntime.Allocator = (*wazeroAllocator)(nil)

// FindLift returns the lift definition for an exported function
func (m *WazeroModule) FindLift(name string) *component.LiftDef {
	if m.canonRegistry == nil {
		return nil
	}
	return m.canonRegistry.FindLift(name)
}

// ExportNames returns the names of all exported functions
func (m *WazeroModule) ExportNames() []string {
	if m.canonRegistry == nil {
		return nil
	}
	lifts := m.canonRegistry.AllLifts()
	names := make([]string, 0, len(lifts))
	for _, lift := range lifts {
		names = append(names, lift.Name)
	}
	return names
}

// CallSession represents an in-progress async function call.
// Use StartCall to create, Step to advance, and LiftResult to extract results.
type CallSession struct {
	stepContextCleanup func()
	execution          *executionCall
	fn                 api.Function
	liftErr            error
	liftedResult       any
	postReturn         api.Function
	alloc              *wazeroAllocator
	instance           *WazeroInstance
	memory             *WazeroMemory
	binding            *exportBinding
	asyncify           *Asyncify
	scheduler          *Scheduler
	paramTypes         []wit.Type
	resultTypes        []wit.Type
	postReturnCalled   bool
	lifted             bool
	done               bool
}

// StartCall prepares a call session by lowering params. Does not execute yet.
// Call Step to advance execution.
func (i *WazeroInstance) StartCall(ctx context.Context, funcName string, params ...any) (*CallSession, error) {
	execution, ctx, leave, err := i.beginSessionExecution(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	transferred := false
	defer func() {
		if execution != nil && !transferred {
			execution.close()
		}
	}()
	binding, err := i.getExportBinding(funcName)
	if err != nil {
		return nil, err
	}

	if err := i.checkSuspended(binding); err != nil {
		return nil, err
	}

	sched := binding.scheduler
	async := binding.asyncify
	if sched == nil {
		return nil, fmt.Errorf("asyncify not enabled on this instance")
	}

	ctx = i.prepareCallContext(ctx)

	mem := binding.memory
	alloc := binding.alloc

	paramTypes := binding.paramTypes
	if len(paramTypes) == 0 && i.module.canonRegistry != nil {
		if lift := i.module.canonRegistry.FindLift(funcName); lift != nil {
			paramTypes = lift.Params
		}
	}

	if paramsRequireAlloc(paramTypes) && (alloc == nil || alloc.allocFn == nil) {
		return nil, fmt.Errorf("canonical allocator not available for export %q", funcName)
	}
	if paramsRequireMemory(paramTypes) && (mem == nil || mem.mem == nil) {
		return nil, fmt.Errorf("canonical memory not available for export %q", funcName)
	}

	// Lower params into wasm args
	if alloc != nil {
		alloc.setContext(ctx)
	}
	allocList := transcoder.NewAllocationList()
	defer allocList.Release()

	var memInterface wasmruntime.Memory
	if mem != nil && mem.mem != nil {
		memInterface = mem
	}
	var allocInterface wasmruntime.Allocator
	if alloc != nil && alloc.allocFn != nil {
		allocInterface = alloc
	}

	flatParams, err := i.encoder.EncodeParams(paramTypes, params, memInterface, allocInterface, allocList)
	if err != nil {
		if allocInterface != nil {
			allocList.Free(allocInterface)
		}
		return nil, fmt.Errorf("encode params: %w", err)
	}

	args := make([]uint64, len(flatParams))
	copy(args, flatParams)

	callCtx := &engineCallContext{
		resourceCallContext: resourceCallContext{
			InstanceContext: linker.NewInstanceContext(ctx, i.linkerInst),
			resources:       i.resources,
		},
		asyncify:  async,
		scheduler: sched,
	}

	if err := sched.Execute(callCtx, binding.fn, args...); err != nil {
		if alloc != nil {
			allocList.Free(alloc)
		}
		return nil, err
	}

	session := &CallSession{
		execution: execution, instance: i,
		binding:     binding,
		memory:      mem,
		alloc:       alloc,
		fn:          binding.fn,
		paramTypes:  binding.paramTypes,
		resultTypes: binding.resultTypes,
		postReturn:  binding.postReturn,
		asyncify:    async,
		scheduler:   sched,
	}
	i.setSuspendedSession(session)
	transferred = true
	return session, nil
}

// engineCallContext carries the instance resource table, asyncify, and scheduler
// for one CallSession.Step. Unknown keys, deadlines, and cancellation come from
// the embedded parent context.
type engineCallContext struct {
	resourceCallContext
	asyncify  *Asyncify
	scheduler *Scheduler
}

func (c *engineCallContext) AsyncifyRuntimeControllerState() asyncify.RuntimeControllerContext {
	if c.asyncify == nil {
		return asyncify.RuntimeControllerContext{}
	}
	return asyncify.RuntimeControllerContext{Controller: c.asyncify}
}

func (c *engineCallContext) Value(key any) any {
	switch key {
	case asyncify.RuntimeControllerContextKey{}:
		return c
	case ctxKeyAsyncify{}:
		return c.asyncify
	case ctxKeyScheduler{}:
		return c.scheduler
	default:
		return c.resourceCallContext.Value(key)
	}
}

func withEngineCallContext(ctx context.Context, i *WazeroInstance) context.Context {
	if ctx == nil {
		panic("cannot create context from nil parent")
	}
	return &engineCallContext{
		resourceCallContext: resourceCallContext{
			InstanceContext: linker.NewInstanceContext(ctx, i.linkerInst),
			resources:       i.resources,
		},
		asyncify:  i.asyncify,
		scheduler: i.scheduler,
	}
}

func withSessionCallContext(ctx context.Context, cs *CallSession) context.Context {
	callCtx := newSessionEngineCallContext(ctx, cs)
	return &callCtx
}

// newSessionEngineCallContext snapshots the session-owned host state for one
// guest entry. Step embeds this value in stepExecutionContext so its active
// lease and engine context share one allocation.
func newSessionEngineCallContext(ctx context.Context, cs *CallSession) engineCallContext {
	if ctx == nil {
		panic("cannot create context from nil parent")
	}
	sched := cs.scheduler
	if sched == nil {
		sched = cs.instance.scheduler
	}
	async := cs.asyncify
	if async == nil {
		async = cs.instance.asyncify
	}
	return engineCallContext{
		resourceCallContext: resourceCallContext{
			InstanceContext: linker.NewInstanceContext(ctx, cs.instance.linkerInst),
			resources:       cs.instance.resources,
		},
		asyncify:  async,
		scheduler: sched,
	}
}

// stepExecutionContext is the immutable context handed to guest code during a
// CallSession.Step. For tracked sessions it owns the active execution lease by
// value. Its embedded engine context and its lease both point at base, never at
// this outer context, so retained host contexts cannot form a cycle or alter
// cancellation ownership after the step returns.
type stepExecutionContext struct {
	engineCallContext
	lease executionLease
}

func newStepExecutionContext(base context.Context, cs *CallSession) *stepExecutionContext {
	engine := newSessionEngineCallContext(base, cs)
	return &stepExecutionContext{
		engineCallContext: engine,
		lease: executionLease{
			Context: base,
		},
	}
}

func (c *stepExecutionContext) Value(key any) any {
	if _, ok := key.(executionLeaseKey); ok {
		return &c.lease
	}
	return c.engineCallContext.Value(key)
}

// Step advances execution. Pass nil for the first call, or a YieldResult to resume.
func (cs *CallSession) Step(ctx context.Context, yr *YieldResult) (StepResult, error) {
	if cs == nil || cs.instance == nil || cs.done {
		err := fmt.Errorf("call session is nil or execution has already completed")
		return StepResult{Error: err, ErrorKind: KindInvalid}, err
	}
	ctx, lease, err := cs.enterStepExecution(ctx)
	if err != nil {
		cs.instance.poisonAsyncify(err)
		cs.instance.clearSuspendedSession(cs)
		cs.done = true
		if cs.execution != nil {
			cs.closeExecution()
		}
		return StepResult{Error: err, ErrorKind: ClassifyError(err)}, err
	}
	defer lease.finish()
	cs.instance.bindingMu.RLock()
	active := cs.instance.activeSession
	cs.instance.bindingMu.RUnlock()
	if active != nil && active != cs {
		err := fmt.Errorf("call session does not own the active execution")
		return StepResult{Error: err, ErrorKind: KindInvalid}, err
	}
	sched := cs.scheduler
	if sched == nil {
		sched = cs.instance.scheduler
	}
	res, err := sched.Step(ctx, yr)
	if err != nil {
		cs.instance.poisonAsyncify(err)
		cs.instance.clearSuspendedSession(cs)
		cs.done = true
		if cs.execution != nil {
			cs.closeExecution()
		}
		return res, err
	}
	switch res.Status {
	case StepContinue:
		cs.instance.setSuspendedSession(cs)
	case StepDone:
		// Keep ownership until lifting and canonical post-return finish.
		cs.done = true
	}
	return res, nil
}

// LiftResult converts raw wasm results to typed Go values after StepDone.
func (cs *CallSession) LiftResult(ctx context.Context, rawResults []uint64) (any, error) {
	if cs == nil || cs.instance == nil {
		return nil, fmt.Errorf("call session is nil")
	}

	cs.instance.bindingMu.RLock()
	active := cs.instance.activeSession
	cs.instance.bindingMu.RUnlock()
	if active == cs && !cs.done {
		return nil, fmt.Errorf("call execution has not completed; step it before lifting results")
	}

	if cs.liftErr != nil {
		return nil, cs.liftErr
	}
	if cs.lifted {
		return cs.liftedResult, nil
	}

	ctx, lease, err := cs.enterSessionExecution(ctx)
	if err != nil {
		cs.liftErr = fmt.Errorf("lift/post-return entry: %w", err)
		if cs.execution != nil {
			cs.closeExecution()
		}
		return nil, cs.liftErr
	}
	defer lease.finish()
	if cs.execution != nil {
		defer cs.closeExecution()
	}
	mem := cs.memory

	var goResults []any
	if len(cs.resultTypes) > 0 {
		if resultsRequireMemory(cs.resultTypes) && (mem == nil || mem.mem == nil) {
			cs.liftErr = fmt.Errorf("decode results: memory not available")
			return nil, cs.liftErr
		}

		var memInterface wasmruntime.Memory
		if mem != nil && mem.mem != nil {
			memInterface = mem
		}

		// Canonical results exceeding one flat value are returned indirectly.
		// The core result is the address of the result area, not its discriminant.
		if usesRetptr(cs.resultTypes) {
			if len(rawResults) != 1 {
				cs.liftErr = fmt.Errorf("indirect result requires one return pointer")
				return nil, cs.liftErr
			}
			if mem == nil || mem.mem == nil {
				cs.liftErr = fmt.Errorf("decode results: memory not available")
				return nil, cs.liftErr
			}
			basePtr := uint32(rawResults[0])
			if len(cs.resultTypes) == 1 {
				val, err := cs.instance.decoder.LoadValue(cs.resultTypes[0], basePtr, memInterface)
				if err != nil {
					cs.liftErr = fmt.Errorf("decode results: %w", err)
					return nil, cs.liftErr
				}
				goResults = []any{val}
			} else {
				offset := uint32(0)
				lc := transcoder.NewLayoutCalculator()
				goResults = make([]any, len(cs.resultTypes))
				for idx, rt := range cs.resultTypes {
					layout := lc.Calculate(rt)
					if layout.Align > 0 {
						offset = (offset + layout.Align - 1) &^ (layout.Align - 1)
					}
					val, err := cs.instance.decoder.LoadValue(rt, basePtr+offset, memInterface)
					if err != nil {
						cs.liftErr = fmt.Errorf("decode results: %w", err)
						return nil, cs.liftErr
					}
					goResults[idx] = val
					offset += layout.Size
				}
			}
		} else {
			expectedFlat := flatResultCount(cs.resultTypes)
			if len(rawResults) < expectedFlat {
				cs.liftErr = fmt.Errorf("decode results: expected %d raw results, got %d", expectedFlat, len(rawResults))
				return nil, cs.liftErr
			}
			var err error
			goResults, err = cs.instance.decoder.DecodeResults(cs.resultTypes, rawResults, memInterface)
			if err != nil {
				cs.liftErr = fmt.Errorf("decode results: %w", err)
				return nil, cs.liftErr
			}
		}
	}

	// Post-return cleanup must run exactly once after successful lifting.
	if cs.postReturn != nil && !cs.postReturnCalled {
		cs.postReturnCalled = true
		callCtx := ctx
		if callCtx == nil {
			callCtx = context.Background()
		}
		callCtx = cs.instance.prepareCallContext(callCtx)
		if _, err := cs.postReturn.Call(callCtx, rawResults...); err != nil {
			cs.liftErr = fmt.Errorf("post-return: %w", err)
			return nil, cs.liftErr
		}
	}

	cs.lifted = true
	cs.done = true
	cs.instance.clearSuspendedSession(cs)
	if len(goResults) == 1 {
		cs.liftedResult = goResults[0]
	} else if len(goResults) > 1 {
		cs.liftedResult = goResults
	} else {
		cs.liftedResult = nil
	}
	return cs.liftedResult, nil
}
