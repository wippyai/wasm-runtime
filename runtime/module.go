package runtime

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/errors"
)

type Module struct {
	funcTypesErr  error
	runtime       *Runtime
	wazeroModule  *engine.WazeroModule
	funcTypes     map[string]*funcSignature
	witText       string
	funcTypesOnce sync.Once
	isComponent   bool
}

func (m *Module) IsComponent() bool {
	return m.isComponent
}

// Compile pre-compiles and validates imports. Call at registration time
// to fail fast. After Compile(), Instantiate() reuses the cached linker.
func (m *Module) Compile(ctx context.Context) error {
	return m.wazeroModule.Compile(ctx, &engine.CompileConfig{})
}

// CompileWithConfig is Compile with explicit compilation options. Set
// Synchronous for modules whose imported host functions never suspend: the
// module then runs without Asyncify instrumentation.
func (m *Module) CompileWithConfig(ctx context.Context, cfg *engine.CompileConfig) error {
	return m.wazeroModule.Compile(ctx, cfg)
}

// ReleaseSource drops the module binary kept after Compile. Call it after the
// last Compile; a later Compile that needs an Asyncify transform fails.
func (m *Module) ReleaseSource() error {
	return m.wazeroModule.ReleaseSource()
}

// Close releases the runtime backing this module.
// Call this only for modules that own an isolated runtime; modules loaded from a
// shared Runtime should be released by closing that Runtime instead.
func (m *Module) Close(ctx context.Context) error {
	if m == nil || m.runtime == nil {
		return nil
	}
	return m.runtime.Close(ctx)
}

// Release frees this module's compiled resources without closing its backing
// runtime or sibling modules. The caller must first close all instances of this
// module. Release is idempotent and is suitable for retiring a generation loaded
// from a shared Runtime. Close retains its isolated-runtime shutdown semantics.
func (m *Module) Release(ctx context.Context) error {
	if m == nil || m.wazeroModule == nil {
		return nil
	}
	return m.wazeroModule.Close(ctx)
}

func (m *Module) Instantiate(ctx context.Context) (*Instance, error) {
	return m.InstantiateWithConfig(ctx, &engine.InstanceConfig{})
}

func (m *Module) InstantiateWithConfig(ctx context.Context, cfg *engine.InstanceConfig) (*Instance, error) {
	wazeroInstance, err := m.wazeroModule.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.Instantiation(err)
	}

	return &Instance{
		module:         m,
		wazeroInstance: wazeroInstance,
	}, nil
}

// InstantiateWithAsyncify creates an instance with asyncify transformation.
// Use for components calling async host functions (e.g., WASI HTTP).
func (m *Module) InstantiateWithAsyncify(ctx context.Context) (*Instance, error) {
	return m.InstantiateWithConfig(ctx, &engine.InstanceConfig{
		EnableAsyncify: true,
	})
}

type Export struct {
	Name string
}

func (m *Module) Exports() []Export {
	names := m.wazeroModule.ExportNames()
	if names == nil {
		return nil
	}
	exports := make([]Export, len(names))
	for i, name := range names {
		exports[i] = Export{Name: name}
	}
	return exports
}

type funcSignature struct {
	params  []wit.Type
	results []wit.Type
}

// GetFunctionTypes returns WIT param and result types for a function.
// Parses witText lazily on first call.
func (m *Module) GetFunctionTypes(name string) ([]wit.Type, []wit.Type, error) {
	m.funcTypesOnce.Do(func() {
		m.funcTypes, m.funcTypesErr = parseWitFunctions(m.witText)
	})

	if m.funcTypesErr != nil {
		return nil, nil, m.funcTypesErr
	}

	sig, ok := m.funcTypes[name]
	if !ok {
		return nil, nil, errors.NotFound(errors.PhaseRuntime, "function", name)
	}

	return sig.params, sig.results, nil
}

var (
	witFuncPattern   = regexp.MustCompile(`(?:export\s+)?([a-zA-Z_][a-zA-Z0-9_-]*)\s*:\s*func\s*\(([^)]*)\)(?:\s*->\s*([^;]+))?`)
	witRecordPattern = regexp.MustCompile(`\brecord\s+([a-zA-Z_][a-zA-Z0-9_-]*)\s*\{([^}]*)\}`)
)

// parseWitFunctions extracts function signatures from WIT text.
// Pattern: [export] name: func(params) -> result;
// Signatures may reference records declared as: record name { field: type, ... }
func parseWitFunctions(witText string) (map[string]*funcSignature, error) {
	witText, err := witDeclarationText(witText)
	if err != nil {
		return nil, err
	}
	types, err := newWitTypes(witText)
	if err != nil {
		return nil, err
	}

	funcs := make(map[string]*funcSignature)

	matches := witFuncPattern.FindAllStringSubmatch(witText, -1)
	for _, match := range matches {
		name := match[1]
		paramsStr := strings.TrimSpace(match[2])
		resultStr := ""
		if len(match) > 3 {
			resultStr = strings.TrimSpace(match[3])
		}

		sig := &funcSignature{}

		if paramsStr != "" {
			paramParts := splitParams(paramsStr)
			for _, p := range paramParts {
				typStr := p
				if idx := strings.LastIndex(p, ":"); idx != -1 {
					typStr = strings.TrimSpace(p[idx+1:])
				}
				t, err := parseWitType(typStr, types)
				if err != nil {
					return nil, errors.Wrap(errors.PhaseParse, errors.KindInvalidData, err, "parse param type "+typStr)
				}
				sig.params = append(sig.params, t)
			}
		}

		if resultStr != "" && resultStr != "()" {
			if strings.HasPrefix(resultStr, "(") && strings.HasSuffix(resultStr, ")") {
				inner := strings.TrimPrefix(strings.TrimSuffix(resultStr, ")"), "(")
				if inner != "" {
					parts := splitParams(inner)
					for _, part := range parts {
						t, err := parseWitType(strings.TrimSpace(part), types)
						if err != nil {
							return nil, errors.Wrap(errors.PhaseParse, errors.KindInvalidData, err, "parse result type "+part)
						}
						sig.results = append(sig.results, t)
					}
				}
			} else {
				t, err := parseWitType(resultStr, types)
				if err != nil {
					return nil, errors.Wrap(errors.PhaseParse, errors.KindInvalidData, err, "parse result type "+resultStr)
				}
				sig.results = []wit.Type{t}
			}
		}

		funcs[name] = sig
	}

	if len(funcs) == 0 {
		return nil, errors.InvalidInput(errors.PhaseParse, "no functions found in WIT text")
	}

	return funcs, nil
}

// witDeclarationText masks comments and quoted annotation text so declaration
// matching sees only WIT code. Token boundaries and line breaks are preserved.
func witDeclarationText(text string) (string, error) {
	buf := []byte(text)
	for i := 0; i < len(text); {
		if text[i] == '"' {
			start := i
			i++
			for i < len(text) {
				ch := text[i]
				i++
				if ch == '\\' && i < len(text) {
					i++
				} else if ch == '"' {
					break
				}
			}
			for j := start; j < i; j++ {
				if buf[j] != '\n' && buf[j] != '\r' {
					buf[j] = ' '
				}
			}
			continue
		}
		if text[i] != '/' || i+1 == len(text) || (text[i+1] != '/' && text[i+1] != '*') {
			i++
			continue
		}
		start := i
		if text[i+1] == '/' {
			i += 2
			for i < len(text) && text[i] != '\n' {
				i++
			}
		} else {
			i += 2
			depth := 1
			for i < len(text) && depth > 0 {
				if i+1 < len(text) && text[i:i+2] == "/*" {
					depth++
					i += 2
				} else if i+1 < len(text) && text[i:i+2] == "*/" {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return "", errors.InvalidInput(errors.PhaseParse, "unterminated WIT block comment")
			}
		}
		for j := start; j < i; j++ {
			if buf[j] != '\n' && buf[j] != '\r' {
				buf[j] = ' '
			}
		}
	}
	return string(buf), nil
}

// witTypes holds the named types declared in a WIT text. Records resolve on
// first reference, so declarations may appear in any order.
type witTypes struct {
	bodies    map[string]string
	defs      map[string]*wit.TypeDef
	resolving map[string]bool
}

func newWitTypes(witText string) (*witTypes, error) {
	types := &witTypes{
		bodies:    make(map[string]string),
		defs:      make(map[string]*wit.TypeDef),
		resolving: make(map[string]bool),
	}
	for _, match := range witRecordPattern.FindAllStringSubmatch(witText, -1) {
		name := match[1]
		if _, dup := types.bodies[name]; dup {
			return nil, errors.InvalidInput(errors.PhaseParse, "duplicate record "+name)
		}
		types.bodies[name] = match[2]
	}
	for name := range types.bodies {
		if _, err := types.lookup(name); err != nil {
			return nil, err
		}
	}
	return types, nil
}

// lookup returns the named type, or nil when no type of that name is declared.
func (w *witTypes) lookup(name string) (wit.Type, error) {
	if w == nil {
		return nil, nil
	}
	if def, ok := w.defs[name]; ok {
		return def, nil
	}
	body, ok := w.bodies[name]
	if !ok {
		return nil, nil
	}
	if w.resolving[name] {
		return nil, errors.InvalidInput(errors.PhaseParse, "record "+name+" is recursive")
	}
	w.resolving[name] = true
	defer delete(w.resolving, name)

	record := &wit.Record{}
	seen := make(map[string]bool)
	for _, field := range splitParams(body) {
		fieldName, fieldType, ok := strings.Cut(field, ":")
		fieldName, fieldType = strings.TrimSpace(fieldName), strings.TrimSpace(fieldType)
		if !ok || fieldName == "" || fieldType == "" {
			return nil, errors.InvalidInput(errors.PhaseParse, "malformed field "+strconv.Quote(field)+" in record "+name)
		}
		if seen[fieldName] {
			return nil, errors.InvalidInput(errors.PhaseParse, "duplicate field "+fieldName+" in record "+name)
		}
		seen[fieldName] = true
		t, err := parseWitType(fieldType, w)
		if err != nil {
			return nil, errors.Wrap(errors.PhaseParse, errors.KindInvalidData, err, "parse field "+fieldName+" of record "+name)
		}
		record.Fields = append(record.Fields, wit.Field{Name: fieldName, Type: t})
	}
	if len(record.Fields) == 0 {
		return nil, errors.InvalidInput(errors.PhaseParse, "record "+name+" has no fields")
	}

	def := &wit.TypeDef{Name: &name, Kind: record}
	w.defs[name] = def
	return def, nil
}

// splitParams splits a comma-separated list, ignoring commas nested in
// parentheses or angle brackets.
func splitParams(s string) []string {
	var result []string
	var current strings.Builder
	depth := 0

	for _, ch := range s {
		switch ch {
		case '(', '<':
			depth++
			current.WriteRune(ch)
		case ')', '>':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				if str := strings.TrimSpace(current.String()); str != "" {
					result = append(result, str)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if str := strings.TrimSpace(current.String()); str != "" {
		result = append(result, str)
	}

	return result
}

// parseWitType parses a WIT value type from its textual form. wit.ParseType only
// handles primitives, so the single-element compound types (list<T>, option<T>) are
// parsed here recursively and other names resolve against the declared types.
func parseWitType(s string, types *witTypes) (wit.Type, error) {
	s = strings.TrimSpace(s)
	if inner, ok := witCompound(s, "list"); ok {
		elem, err := parseWitType(inner, types)
		if err != nil {
			return nil, err
		}
		return &wit.TypeDef{Kind: &wit.List{Type: elem}}, nil
	}
	if inner, ok := witCompound(s, "option"); ok {
		elem, err := parseWitType(inner, types)
		if err != nil {
			return nil, err
		}
		return &wit.TypeDef{Kind: &wit.Option{Type: elem}}, nil
	}
	named, err := types.lookup(s)
	if err != nil {
		return nil, err
	}
	if named != nil {
		return named, nil
	}
	return wit.ParseType(s)
}

// witCompound reports whether s is "<name><T>" and returns the inner type T.
func witCompound(s, name string) (string, bool) {
	prefix := name + "<"
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, ">") {
		return "", false
	}
	return strings.TrimSpace(s[len(prefix) : len(s)-1]), true
}
