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
	"github.com/wippyai/wasm-runtime/resource"
	"github.com/wippyai/wasm-runtime/transcoder"
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
	runtime      wazero.Runtime
	hostMods     map[string]struct{}
	wasiInitMu   sync.Mutex
	hostModsMu   sync.Mutex
	wasiInitDone atomic.Bool
}

// Config holds configuration for engine creation
type Config struct {
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

	runtimeCfg := wazero.NewRuntimeConfig().WithCoreFeatures(coreFeatures)

	if cfg != nil && cfg.MemoryLimitPages > 0 {
		runtimeCfg = runtimeCfg.WithMemoryLimitPages(cfg.MemoryLimitPages)
	}
	if cfg != nil && cfg.CloseOnContextDone {
		runtimeCfg = runtimeCfg.WithCloseOnContextDone(true)
	}

	runtime := wazero.NewRuntimeWithConfig(ctx, runtimeCfg)
	return &WazeroEngine{runtime: runtime}, nil
}

// CompileConfig holds configuration for pre-compilation
type CompileConfig struct {
	// EnableAsyncify enables automatic asyncify transformation for components.
	EnableAsyncify bool
}

// InstanceConfig holds configuration for module instantiation
type InstanceConfig struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	Env    map[string]string
	Name   string
	// EntryExport selects the canonical executable whose Asyncify state owns this call.
	EntryExport     string
	DecodeOptions   transcoder.DecodeOptions
	AsyncifyImports []string
	Args            []string
	Mounts          []Mount
	EnableAsyncify  bool
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
				fsCfg = withReadOnlyFSMount(fsCfg, mnt.FS, mnt.Guest)
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

func (e *WazeroEngine) LoadModule(ctx context.Context, wasmBytes []byte) (*WazeroModule, error) {
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

			return &WazeroModule{
				engine:        e,
				runtime:       e.runtime,
				compiler:      compiler,
				encoder:       transcoder.NewEncoderWithCompiler(compiler),
				decoder:       transcoder.NewDecoderWithCompiler(compiler),
				hostFuncs:     make(map[string]HostFunc),
				canonRegistry: canonRegistry,
				typeResolver:  typeResolver,
				validated:     validated,
			}, nil
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

	return &WazeroModule{
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
	}, nil
}

func (e *WazeroEngine) Close(ctx context.Context) error {
	return e.runtime.Close(ctx)
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
	engine        *WazeroEngine
	runtime       wazero.Runtime
	compiled      wazero.CompiledModule
	canonRegistry *component.CanonRegistry
	encoder       *transcoder.Encoder
	decoder       *transcoder.Decoder
	hostFuncs     map[string]HostFunc
	compiler      *transcoder.Compiler
	typeResolver  *component.TypeResolver
	validated     *component.ValidatedComponent
	cachedPre     *linker.InstancePre
	linker        *linker.Linker
	rawBytes      []byte
	hostFuncsMu   sync.RWMutex
	cachedPreMu   sync.RWMutex
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
// Uses intersection logic: a function is async only if both the host registration and
// the component canon lower agree. For core modules (no canon registry), trusts the host flag.
func (m *WazeroModule) AsyncifyImports() []string {
	m.hostFuncsMu.RLock()
	defer m.hostFuncsMu.RUnlock()

	imports := make([]string, 0, len(m.hostFuncs))
	for _, hf := range m.hostFuncs {
		if !hf.IsAsync {
			continue
		}
		if m.canonRegistry != nil {
			lowerDef := m.findLowerDef(hf.Namespace, hf.Name)
			if lowerDef == nil {
				continue
			}
		}
		imports = append(imports, hf.Namespace+"#"+hf.Name)
	}
	return imports
}

// findLowerDef looks up a canon.lower definition for a namespace and function name.
// Uses semver matching: host version X.Y.Z can satisfy component import X.Y.W where W <= Z.
// Tries exact match first, then semver-compatible matches.
func (m *WazeroModule) findLowerDef(namespace, name string) *component.LowerDef {
	// Try function name variations
	nameVariants := []string{name}
	if witName := kebabToWitName(name); witName != name {
		nameVariants = append(nameVariants, witName)
	}

	// Try exact namespace match first
	for _, n := range nameVariants {
		importName := namespace + "#" + n
		if lowerDef := m.canonRegistry.FindLower(importName); lowerDef != nil {
			return lowerDef
		}
		// Try function name only
		if lowerDef := m.canonRegistry.FindLower(n); lowerDef != nil {
			return lowerDef
		}
	}

	// Parse host namespace for semver matching
	hostBase, hostVersion, hasHostVersion := parseNamespaceVersion(namespace)
	if !hasHostVersion {
		return nil
	}

	// Search all lowers for semver-compatible match
	for _, lowerDef := range m.canonRegistry.AllLowers() {
		// Parse the lower's name to extract namespace and function
		lowerNs, lowerFunc := splitLowerName(lowerDef.Name)
		if lowerNs == "" {
			continue
		}

		// Check if function name matches any variant
		funcMatches := false
		for _, n := range nameVariants {
			if lowerFunc == n {
				funcMatches = true
				break
			}
		}
		if !funcMatches {
			continue
		}

		// Parse component's required namespace version
		compBase, compVersion, hasCompVersion := parseNamespaceVersion(lowerNs)
		if !hasCompVersion {
			continue
		}

		// Check if base paths match and host version is compatible
		if hostBase == compBase && hostVersion.Compatible(compVersion) {
			return lowerDef
		}
	}

	return nil
}

// parseNamespaceVersion splits "wasi:io/streams@0.2.8" into base path and version
func parseNamespaceVersion(namespace string) (basePath string, version linker.Version, hasVersion bool) {
	idx := -1
	for i := len(namespace) - 1; i >= 0; i-- {
		if namespace[i] == '@' {
			idx = i
			break
		}
	}
	if idx == -1 {
		return namespace, linker.Version{}, false
	}
	basePath = namespace[:idx]
	version, hasVersion = linker.ParseVersion(namespace[idx+1:])
	return basePath, version, hasVersion
}

// splitLowerName splits "wasi:io/streams@0.2.0#read" into namespace and function
func splitLowerName(name string) (namespace, funcName string) {
	idx := -1
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '#' {
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", name
	}
	return name[:idx], name[idx+1:]
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
func (m *WazeroModule) ensureLinker(cfg linkerConfig) {
	if m.linker != nil {
		return
	}

	// Auto-derive async imports from host registration + canon registry intersection
	if len(cfg.AsyncifyImports) == 0 {
		cfg.AsyncifyImports = m.AsyncifyImports()
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

	// For single-module components or core modules, apply asyncify if needed
	if m.validated == nil {
		asyncImports := m.AsyncifyImports()
		if len(asyncImports) == 0 && !cfg.EnableAsyncify {
			return nil
		}
		if m.rawBytes != nil && !asyncify.IsAsyncified(m.rawBytes) && len(asyncImports) > 0 {
			transformed, err := asyncify.Transform(m.rawBytes, asyncify.Config{
				AsyncImports: asyncImports,
			})
			if err != nil {
				return fmt.Errorf("asyncify transform: %w", err)
			}
			oldCompiled := m.compiled
			compiled, err := m.runtime.CompileModule(ctx, transformed)
			if err != nil {
				return fmt.Errorf("recompile after asyncify: %w", err)
			}
			m.compiled = compiled
			if oldCompiled != nil {
				oldCompiled.Close(ctx)
			}
		}
		return nil
	}

	// Multi-module component: create linker and cache InstancePre
	m.cachedPreMu.Lock()
	defer m.cachedPreMu.Unlock()

	if m.cachedPre != nil {
		return nil
	}

	m.ensureLinker(linkerConfig{
		AsyncifyTransform: cfg.EnableAsyncify,
	})

	// Compile the component
	pre, err := m.linker.Instantiate(ctx, m.validated)
	if err != nil {
		return fmt.Errorf("compile component: %w", err)
	}

	m.cachedPre = pre
	return nil
}

func (m *WazeroModule) Instantiate(ctx context.Context) (*WazeroInstance, error) {
	return m.InstantiateWithConfig(ctx, nil)
}

// InstantiateWithConfig creates an instance with custom configuration
func (m *WazeroModule) InstantiateWithConfig(ctx context.Context, cfg *InstanceConfig) (*WazeroInstance, error) {
	// If this is a multi-module component, use per-instantiation linker
	if m.validated != nil {
		return m.instantiateMultiModuleWithConfig(ctx, cfg)
	}

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
	if _, ok := m.compiled.ExportedFunctions()["_initialize"]; ok {
		modConfig = modConfig.WithStartFunctions("_initialize")
	}

	// Instantiate the module
	instance, err := m.runtime.InstantiateModule(ctx, m.compiled, modConfig)
	if err != nil {
		return nil, fmt.Errorf("instantiate failed: %w", err)
	}

	wazInst := &WazeroInstance{
		module:    m,
		instance:  instance,
		encoder:   m.encoder,
		decoder:   m.decoderForConfig(cfg),
		compiler:  m.compiler,
		funcCache: make(map[string]api.Function),
		liftCache: make(map[string]*cachedLift),
		stackBuf:  make([]uint64, 16), // pre-allocate stack buffer
		resources: resource.NewTable(),
	}

	// Cache memory
	if mem := instance.Memory(); hasMemory(mem) {
		wazInst.memory = &WazeroMemory{mem: mem}
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

	// Mirror multi-module behavior: asyncify enable is best-effort.
	// Callers may request asyncify on modules that have no async transform.
	if cfg != nil && cfg.EnableAsyncify {
		if err := wazInst.EnableAsyncify(AsyncifyConfig{}); err != nil {
			debugf("asyncify not available for module: %v", err)
		}
	}

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
	m.ensureLinker(linkerConfig{
		AsyncifyTransform: enableAsyncify,
		AsyncifyImports:   asyncifyImports,
	})

	pre := m.cachedPre
	if pre == nil {
		var err error
		m.cachedPre, err = m.linker.Instantiate(ctx, m.validated)
		if err != nil {
			m.cachedPreMu.Unlock()
			return nil, fmt.Errorf("compile component: %w", err)
		}
		pre = m.cachedPre
	}
	m.cachedPreMu.Unlock()

	// Create new instance from pre-compiled template
	inst, err := pre.NewInstance(ctx)
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
	module := inst.GetModule(lastMod.InstanceIndex)
	if module == nil {
		inst.Close(ctx)
		return nil, fmt.Errorf("final module not found at index %d", lastMod.InstanceIndex)
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
		module = inst.GetModule(owner.InstanceIdx)
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
	// Create WazeroInstance wrapper
	wazInst := &WazeroInstance{
		module:     m,
		instance:   module,
		encoder:    m.encoder,
		decoder:    m.decoderForConfig(cfg),
		compiler:   m.compiler,
		funcCache:  make(map[string]api.Function),
		liftCache:  make(map[string]*cachedLift),
		stackBuf:   make([]uint64, 16),
		linkerInst: inst,
		resources:  resource.NewTable(),
	}

	// Cache memory
	mem := inst.Memory()
	if entryCanon != nil {
		mem = entryCanon.Memory
	}
	if hasMemory(mem) {
		wazInst.memory = &WazeroMemory{mem: mem}
	}

	// Cache allocator
	var isSimpleAlloc bool
	wazInst.allocFn = inst.Allocator()
	if entryCanon != nil {
		wazInst.allocFn = entryCanon.Realloc
	}
	if wazInst.allocFn != nil {
		paramCount := len(wazInst.allocFn.Definition().ParamTypes())
		isSimpleAlloc = paramCount < 4
	}

	// Cache free function
	wazInst.freeFn = inst.Free()

	// Create reusable allocator
	wazInst.alloc = &wazeroAllocator{
		allocFn:       wazInst.allocFn,
		freeFn:        wazInst.freeFn,
		stackBuf:      wazInst.stackBuf,
		isSimpleAlloc: isSimpleAlloc,
	}

	// Enable asyncify if requested and module supports it
	if enableAsyncify {
		if err := wazInst.EnableAsyncify(AsyncifyConfig{}); err != nil {
			debugf("asyncify not available for component: %v", err)
		}
	}

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
type WazeroInstance struct {
	freeFn     api.Function
	allocFn    api.Function
	instance   api.Module
	module     *WazeroModule
	encoder    *transcoder.Encoder
	compiler   *transcoder.Compiler
	funcCache  map[string]api.Function
	liftCache  map[string]*cachedLift
	resources  *resource.UnifiedTable
	decoder    *transcoder.Decoder
	memory     *WazeroMemory
	alloc      *wazeroAllocator
	linkerInst *linker.Instance
	asyncify   *Asyncify
	scheduler  *Scheduler
	stackBuf   []uint64
	cacheMu    sync.RWMutex
}

// cachedLift stores pre-computed lift info for fast repeated calls
type cachedLift struct {
	fn      api.Function
	params  []wit.Type
	results []wit.Type
}

// getExportedFunction returns an exported function, using linker for multi-module components
func (i *WazeroInstance) getExportedFunction(name string) api.Function {
	if i.linkerInst != nil {
		return i.linkerInst.ExportedFunction(name)
	}
	return i.instance.ExportedFunction(name)
}

// GetExportedFunction returns an exported function by name (public wrapper).
func (i *WazeroInstance) GetExportedFunction(name string) api.Function {
	return i.getExportedFunction(name)
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

// prepareCallContext injects linker instance into context if available.
// This is needed for host handlers to resolve the correct instance when
// called from synthetic shim modules that don't have instanceID suffix.
func (i *WazeroInstance) prepareCallContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, resourcesContextKey{}, i.resources)
	if i.linkerInst != nil {
		return linker.WithInstance(ctx, i.linkerInst)
	}
	return ctx
}

// EnableAsyncify initializes asyncify support for this instance.
// Call EnableAsyncify after instantiation but before calling async functions.
// The module must have been compiled with asyncify (wasm-opt --asyncify).
func (i *WazeroInstance) EnableAsyncify(config AsyncifyConfig) error {
	a := NewAsyncify()
	if config.StackSize > 0 {
		a.SetStackSize(config.StackSize)
	}
	if config.DataAddr > 0 {
		a.SetDataAddr(config.DataAddr)
	}

	if err := a.Init(i.instance); err != nil {
		return err
	}

	i.asyncify = a
	i.scheduler = NewScheduler(a)
	return nil
}

// Asyncify returns the asyncify runtime if enabled.
func (i *WazeroInstance) Asyncify() *Asyncify {
	return i.asyncify
}

// Scheduler returns the async scheduler if enabled.
func (i *WazeroInstance) Scheduler() *Scheduler {
	return i.scheduler
}

// RunAsync executes a function with asyncify event loop support.
// It returns after the function completes, processing any async operations.
func (i *WazeroInstance) RunAsync(ctx context.Context, name string, args ...uint64) ([]uint64, error) {
	fn := i.getExportedFunction(name)
	if fn == nil {
		return nil, fmt.Errorf("function %q not found", name)
	}

	ctx = i.prepareCallContext(ctx)

	if i.asyncify == nil || i.scheduler == nil {
		return fn.Call(ctx, args...)
	}

	ctx = WithAsyncify(ctx, i.asyncify)
	ctx = WithScheduler(ctx, i.scheduler)
	return i.scheduler.Run(ctx, fn, args...)
}

// CallWithLift calls a function using cached lift information from canon registry.
// It is faster than Call for repeated invocations as it caches lookup results.
func (i *WazeroInstance) CallWithLift(ctx context.Context, funcName string, params ...any) (any, error) {
	ctx = i.prepareCallContext(ctx)

	// Check cache first (read lock)
	i.cacheMu.RLock()
	cached, ok := i.liftCache[funcName]
	i.cacheMu.RUnlock()

	if !ok {
		// Lookup and cache (write lock)
		if i.module.canonRegistry == nil {
			return nil, fmt.Errorf("no canon registry")
		}
		lift := i.module.canonRegistry.FindLift(funcName)
		if lift == nil {
			return nil, fmt.Errorf("export %q not found in component", funcName)
		}
		fn := i.getExportedFunction(funcName)
		if fn == nil {
			return nil, fmt.Errorf("function %s not found", funcName)
		}
		cached = &cachedLift{
			fn:      fn,
			params:  lift.Params,
			results: lift.Results,
		}
		i.cacheMu.Lock()
		i.liftCache[funcName] = cached
		i.cacheMu.Unlock()
	}

	// Try fast path for primitive types
	if result, ok, err := i.tryFastCall(ctx, cached.fn, cached.params, cached.results, params); ok {
		return result, err
	}

	// Fallback to general path
	return i.callGeneral(ctx, cached.fn, cached.params, cached.results, params)
}

// CallWithTypes calls a WASM function with explicit WIT type information
func (i *WazeroInstance) CallWithTypes(ctx context.Context, funcName string, paramTypes []wit.Type, resultTypes []wit.Type, params ...any) (any, error) {
	ctx = i.prepareCallContext(ctx)

	// Get cached or lookup function (read lock)
	i.cacheMu.RLock()
	fn, ok := i.funcCache[funcName]
	i.cacheMu.RUnlock()

	if !ok {
		fn = i.getExportedFunction(funcName)
		if fn == nil {
			return nil, fmt.Errorf("function %s not found", funcName)
		}
		i.cacheMu.Lock()
		i.funcCache[funcName] = fn
		i.cacheMu.Unlock()
	}

	// Try fast path for primitive types
	if result, ok, err := i.tryFastCall(ctx, fn, paramTypes, resultTypes, params); ok {
		return result, err
	}

	// Try compiled fast path for structs/lists
	if result, ok, err := i.tryCallCompiled(ctx, fn, paramTypes, resultTypes, params); ok {
		return result, err
	}

	// Fallback to general path
	return i.callGeneral(ctx, fn, paramTypes, resultTypes, params)
}

// CallInto decodes results directly into caller's memory without intermediate allocation.
// result must be a pointer to the target type (e.g., *string, *uint32, *MyStruct).
// For void returns, pass nil.
// For strings, the result points directly into WASM memory and is only valid
// while the instance is alive.
func (i *WazeroInstance) CallInto(ctx context.Context, funcName string, paramTypes []wit.Type, resultTypes []wit.Type, result any, params ...any) error {
	ctx = i.prepareCallContext(ctx)

	// Get cached or lookup function (read lock)
	i.cacheMu.RLock()
	fn, ok := i.funcCache[funcName]
	i.cacheMu.RUnlock()

	if !ok {
		fn = i.getExportedFunction(funcName)
		if fn == nil {
			return fmt.Errorf("function %s not found", funcName)
		}
		i.cacheMu.Lock()
		i.funcCache[funcName] = fn
		i.cacheMu.Unlock()
	}

	// Try fast path for string -> string
	if handled, err := i.tryCallStringInto(ctx, fn, paramTypes, resultTypes, result, params); handled {
		return err
	}

	// Try fast path for primitives
	if handled, err := i.tryCallPrimitiveInto(ctx, fn, paramTypes, resultTypes, result, params); handled {
		return err
	}

	// Try fast path for compiled types (structs, typed slices) using stack-based operations
	if handled, err := i.tryCallCompiledInto(ctx, fn, paramTypes, resultTypes, result, params); handled {
		return err
	}

	// General path
	return i.callGeneralInto(ctx, fn, paramTypes, resultTypes, result, params)
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
	a.stackMutex.Lock()
	defer a.stackMutex.Unlock()
	a.currentCtx = ctx
}

func (a *wazeroAllocator) Alloc(size, align uint32) (uint32, error) {
	if a.allocFn == nil {
		return 0, fmt.Errorf("no allocator available")
	}

	a.stackMutex.Lock()
	defer a.stackMutex.Unlock()

	ctx := a.currentCtx
	if ctx == nil {
		ctx = context.Background()
	}

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
	if a.freeFn != nil && ptr != 0 {
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
	var firstErr error
	if i.resources != nil {
		if err := i.resources.Close(); err != nil {
			firstErr = err
		}
		i.resources = nil
	}
	// Close linker instance if present (for multi-module components)
	if i.linkerInst != nil {
		if err := i.linkerInst.Close(ctx); err != nil {
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
	// Clear references to help GC
	i.funcCache = nil
	i.liftCache = nil
	i.memory = nil
	i.allocFn = nil
	i.freeFn = nil
	i.alloc = nil
	i.stackBuf = nil
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
	data, ok := m.mem.Read(offset, length)
	if !ok {
		return nil, fmt.Errorf("read out of bounds: offset=%d, length=%d", offset, length)
	}
	return data, nil
}

func (m *WazeroMemory) Write(offset uint32, data []byte) error {
	ok := m.mem.Write(offset, data)
	if !ok {
		return fmt.Errorf("write out of bounds: offset=%d, length=%d", offset, len(data))
	}
	return nil
}

func (m *WazeroMemory) ReadU8(offset uint32) (uint8, error) {
	data, err := m.Read(offset, 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (m *WazeroMemory) ReadU16(offset uint32) (uint16, error) {
	data, err := m.Read(offset, 2)
	if err != nil {
		return 0, err
	}
	return uint16(data[0]) | uint16(data[1])<<8, nil
}

func (m *WazeroMemory) ReadU32(offset uint32) (uint32, error) {
	val, ok := m.mem.ReadUint32Le(offset)
	if !ok {
		return 0, fmt.Errorf("read out of bounds")
	}
	return val, nil
}

func (m *WazeroMemory) ReadU64(offset uint32) (uint64, error) {
	val, ok := m.mem.ReadUint64Le(offset)
	if !ok {
		return 0, fmt.Errorf("read out of bounds")
	}
	return val, nil
}

func (m *WazeroMemory) WriteU8(offset uint32, value uint8) error {
	return m.Write(offset, []byte{value})
}

func (m *WazeroMemory) WriteU16(offset uint32, value uint16) error {
	return m.Write(offset, []byte{byte(value), byte(value >> 8)})
}

func (m *WazeroMemory) WriteU32(offset uint32, value uint32) error {
	ok := m.mem.WriteUint32Le(offset, value)
	if !ok {
		return fmt.Errorf("write out of bounds")
	}
	return nil
}

func (m *WazeroMemory) WriteU64(offset uint32, value uint64) error {
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
	instance         *WazeroInstance
	fn               api.Function
	paramTypes       []wit.Type
	resultTypes      []wit.Type
	postReturn       api.Function
	memory           *WazeroMemory
	postReturnCalled bool
	lifted           bool
	liftedResult     any
	liftErr          error
}

// StartCall prepares a call session by lowering params. Does not execute yet.
// Call Step to advance execution.
func (i *WazeroInstance) StartCall(ctx context.Context, funcName string, params ...any) (*CallSession, error) {
	if i.module.canonRegistry == nil {
		return nil, fmt.Errorf("no canon registry")
	}

	lift := i.module.canonRegistry.FindLift(funcName)
	if lift == nil {
		return nil, fmt.Errorf("export %q not found in component", funcName)
	}

	fn := i.getExportedFunction(funcName)
	if fn == nil {
		return nil, fmt.Errorf("function %s not found", funcName)
	}

	ctx = i.prepareCallContext(ctx)

	// Lower params into wasm args
	i.alloc.setContext(ctx)
	allocList := transcoder.NewAllocationList()

	flatParams, err := i.encoder.EncodeParams(lift.Params, params, i.memory, i.alloc, allocList)
	if err != nil {
		allocList.FreeAndRelease(i.alloc)
		return nil, fmt.Errorf("encode params: %w", err)
	}

	// Prepare scheduler
	if i.scheduler == nil {
		return nil, fmt.Errorf("asyncify not enabled on this instance")
	}

	copy(i.stackBuf, flatParams)
	args := make([]uint64, len(flatParams))
	copy(args, flatParams)

	if err := i.scheduler.Execute(ctx, fn, args...); err != nil {
		allocList.FreeAndRelease(i.alloc)
		return nil, err
	}

	var canon *linker.CanonExport
	var postReturn api.Function
	if i.linkerInst != nil {
		if exp, ok := i.linkerInst.GetExport(funcName); ok && exp.Canon != nil {
			canon = exp.Canon
			postReturn = exp.Canon.PostReturn
		}
	}
	if postReturn == nil && i.instance != nil {
		postReturn = i.instance.ExportedFunction("cabi_post_" + funcName)
	}

	mem := i.memory
	if canon != nil && canon.Memory != nil {
		if mem == nil || mem.mem != canon.Memory {
			mem = &WazeroMemory{mem: canon.Memory}
		}
	}

	return &CallSession{
		instance:    i,
		fn:          fn,
		paramTypes:  lift.Params,
		resultTypes: lift.Results,
		postReturn:  postReturn,
		memory:      mem,
	}, nil
}

// Step advances execution. Pass nil for the first call, or a YieldResult to resume.
func (cs *CallSession) Step(ctx context.Context, yr *YieldResult) (StepResult, error) {
	ctx = cs.instance.prepareCallContext(ctx)
	ctx = WithAsyncify(ctx, cs.instance.asyncify)
	ctx = WithScheduler(ctx, cs.instance.scheduler)
	return cs.instance.scheduler.Step(ctx, yr)
}

// LiftResult converts raw wasm results to typed Go values after StepDone.
func (cs *CallSession) LiftResult(ctx context.Context, rawResults []uint64) (any, error) {
	if cs == nil || cs.instance == nil {
		return nil, fmt.Errorf("call session is nil")
	}

	if cs.liftErr != nil {
		return nil, cs.liftErr
	}
	if cs.lifted {
		return cs.liftedResult, nil
	}

	mem := cs.memory
	if mem == nil {
		mem = cs.instance.memory
	}

	var goResults []any
	if len(cs.resultTypes) > 0 {
		// Canonical results exceeding one flat value are returned indirectly.
		// The core result is the address of the result area, not its discriminant.
		if usesRetptr(cs.resultTypes) {
			if len(rawResults) != 1 {
				cs.liftErr = fmt.Errorf("indirect result requires one return pointer")
				return nil, cs.liftErr
			}
			if mem == nil {
				cs.liftErr = fmt.Errorf("decode results: memory not available")
				return nil, cs.liftErr
			}
			basePtr := uint32(rawResults[0])
			if len(cs.resultTypes) == 1 {
				val, err := cs.instance.decoder.LoadValue(cs.resultTypes[0], basePtr, mem)
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
					val, err := cs.instance.decoder.LoadValue(rt, basePtr+offset, mem)
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
			goResults, err = cs.instance.decoder.DecodeResults(cs.resultTypes, rawResults, mem)
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
	if len(goResults) == 1 {
		cs.liftedResult = goResults[0]
	} else if len(goResults) > 1 {
		cs.liftedResult = goResults
	} else {
		cs.liftedResult = nil
	}
	return cs.liftedResult, nil
}
