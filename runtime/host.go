package runtime

import (
	"reflect"
	"strings"
	"sync"
	"unicode"

	"github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/errors"
)

// Host is the interface for struct-based host modules.
// All exported methods (except Namespace) are registered as host functions.
type Host interface {
	// Namespace returns the WIT interface name (e.g., "my:pkg/api@1.0.0").
	Namespace() string
}

type HostRegistry struct {
	funcs map[string]map[string]*HostFunc
	mu    sync.RWMutex
}

type HostFunc struct {
	Handler  any
	Receiver reflect.Value
	IsAsync  bool
}

// AsyncHost extends Host with async function declarations.
// Functions listed by AsyncFunctions() yield during execution.
type AsyncHost interface {
	Host
	AsyncFunctions() []string
}

func NewHostRegistry() *HostRegistry {
	return &HostRegistry{
		funcs: make(map[string]map[string]*HostFunc),
	}
}

// ExplicitRegistrar allows hosts to provide exact WIT function names
// when automatic PascalCase-to-kebab-case conversion doesn't apply
// (e.g., "[method]fields.append").
type ExplicitRegistrar interface {
	Register() map[string]any
}

func (r *HostRegistry) RegisterHost(h Host) error {
	ns := h.Namespace()
	if ns == "" {
		return errors.InvalidInput(errors.PhaseHost, "namespace cannot be empty")
	}

	// Collect async function names if host declares them
	asyncFuncs := make(map[string]bool)
	if ah, ok := h.(AsyncHost); ok {
		for _, name := range ah.AsyncFunctions() {
			asyncFuncs[name] = true
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.funcs[ns] == nil {
		r.funcs[ns] = make(map[string]*HostFunc)
	}

	if er, ok := h.(ExplicitRegistrar); ok {
		funcs := er.Register()
		for name, handler := range funcs {
			r.funcs[ns][name] = &HostFunc{
				Handler:  handler,
				Receiver: reflect.ValueOf(h),
				IsAsync:  asyncFuncs[name],
			}
		}
		return nil
	}

	// Handle Register() *HostRegistration pattern where HostRegistration has Functions field
	if funcs := tryExtractFunctionsViaReflection(h); funcs != nil {
		for name, handler := range funcs {
			r.funcs[ns][name] = &HostFunc{
				Handler:  handler,
				Receiver: reflect.ValueOf(h),
				IsAsync:  asyncFuncs[name],
			}
		}
		return nil
	}

	rv := reflect.ValueOf(h)
	rt := rv.Type()

	for i := 0; i < rt.NumMethod(); i++ {
		method := rt.Method(i)

		if !method.IsExported() || method.Name == "Namespace" || method.Name == "AsyncFunctions" {
			continue
		}

		witName := toKebabCase(method.Name)
		boundMethod := rv.Method(i)
		handler := boundMethod.Interface()

		r.funcs[ns][witName] = &HostFunc{
			Handler:  handler,
			Receiver: rv,
			IsAsync:  asyncFuncs[witName],
		}
	}

	return nil
}

func (r *HostRegistry) RegisterFunc(namespace, name string, fn any) error {
	if namespace == "" {
		return errors.InvalidInput(errors.PhaseHost, "namespace cannot be empty")
	}
	if name == "" {
		return errors.InvalidInput(errors.PhaseHost, "function name cannot be empty")
	}

	rv := reflect.ValueOf(fn)
	if rv.Kind() != reflect.Func {
		return errors.New(errors.PhaseHost, errors.KindTypeMismatch).
			GoType(reflect.TypeOf(fn).String()).
			Detail("handler must be a function").
			Build()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.funcs[namespace] == nil {
		r.funcs[namespace] = make(map[string]*HostFunc)
	}

	r.funcs[namespace][name] = &HostFunc{
		Handler:  fn,
		Receiver: reflect.Value{},
	}

	return nil
}

// Bind registers host functions with a WazeroModule.
// Version matching: X.Y.Z satisfies imports at X.Y.W where W <= Z.
func (r *HostRegistry) Bind(mod *engine.WazeroModule) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for namespace, funcs := range r.funcs {
		for name, hf := range funcs {
			var err error
			if hf.IsAsync {
				err = mod.RegisterHostFuncTypedAsync(namespace, name, hf.Handler)
			} else {
				err = mod.RegisterHostFuncTyped(namespace, name, hf.Handler)
			}
			if err != nil {
				if strings.Contains(err.Error(), "no canon lower found") {
					continue
				}
				return errors.Registration(errors.PhaseHost, namespace, name, err)
			}
		}
	}
	return nil
}

// RegisterFuncAsync registers a single async function in the host registry.
func (r *HostRegistry) RegisterFuncAsync(namespace, name string, fn any) error {
	if namespace == "" {
		return errors.InvalidInput(errors.PhaseHost, "namespace cannot be empty")
	}
	if name == "" {
		return errors.InvalidInput(errors.PhaseHost, "function name cannot be empty")
	}

	rv := reflect.ValueOf(fn)
	if rv.Kind() != reflect.Func {
		return errors.New(errors.PhaseHost, errors.KindTypeMismatch).
			GoType(reflect.TypeOf(fn).String()).
			Detail("handler must be a function").
			Build()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.funcs[namespace] == nil {
		r.funcs[namespace] = make(map[string]*HostFunc)
	}

	r.funcs[namespace][name] = &HostFunc{
		Handler: fn,
		IsAsync: true,
	}

	return nil
}

// toKebabCase converts PascalCase to kebab-case.
// Handles acronyms: GetHTTPURL -> get-http-url
func toKebabCase(s string) string {
	if len(s) == 0 {
		return ""
	}

	runes := []rune(s)
	var result strings.Builder

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if unicode.IsUpper(r) {
			acronymEnd := i + 1
			for acronymEnd < len(runes) && unicode.IsUpper(runes[acronymEnd]) {
				acronymEnd++
			}

			if acronymEnd > i+1 {
				// Last uppercase before lowercase starts next word, not part of acronym
				if acronymEnd < len(runes) && unicode.IsLower(runes[acronymEnd]) {
					acronymEnd--
				}
			}

			if i > 0 {
				result.WriteByte('-')
			}

			for j := i; j < acronymEnd; j++ {
				result.WriteRune(unicode.ToLower(runes[j]))
			}
			i = acronymEnd - 1 // -1 because loop will increment
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func tryExtractFunctionsViaReflection(h any) map[string]any {
	rv := reflect.ValueOf(h)
	method := rv.MethodByName("Register")
	if !method.IsValid() {
		return nil
	}

	methodType := method.Type()
	if methodType.NumIn() != 0 || methodType.NumOut() != 1 {
		return nil
	}

	results := method.Call(nil)
	if len(results) != 1 {
		return nil
	}

	result := results[0]
	if !result.IsValid() || (result.Kind() == reflect.Ptr && result.IsNil()) {
		return nil
	}

	if result.Kind() == reflect.Ptr {
		result = result.Elem()
	}

	if result.Kind() != reflect.Struct {
		return nil
	}

	functionsField := result.FieldByName("Functions")
	if !functionsField.IsValid() {
		return nil
	}

	if functionsField.Kind() != reflect.Map {
		return nil
	}

	funcs := make(map[string]any)
	iter := functionsField.MapRange()
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		if key.Kind() == reflect.String {
			funcs[key.String()] = value.Interface()
		}
	}

	if len(funcs) == 0 {
		return nil
	}

	return funcs
}
