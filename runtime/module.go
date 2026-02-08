package runtime

import (
	"context"
	"regexp"
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

func (m *Module) Instantiate(ctx context.Context) (*Instance, error) {
	wazeroInstance, err := m.wazeroModule.InstantiateWithConfig(ctx, &engine.InstanceConfig{})
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
	wazeroInstance, err := m.wazeroModule.InstantiateWithConfig(ctx, &engine.InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		return nil, errors.Instantiation(err)
	}

	return &Instance{
		module:         m,
		wazeroInstance: wazeroInstance,
	}, nil
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

// parseWitFunctions extracts function signatures from WIT text.
// Pattern: [export] name: func(params) -> result;
func parseWitFunctions(witText string) (map[string]*funcSignature, error) {
	funcs := make(map[string]*funcSignature)

	funcPattern := regexp.MustCompile(`(?:export\s+)?([a-zA-Z_][a-zA-Z0-9_-]*)\s*:\s*func\s*\(([^)]*)\)(?:\s*->\s*([^;]+))?`)

	matches := funcPattern.FindAllStringSubmatch(witText, -1)
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
				t, err := parseWitType(typStr)
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
						t, err := parseWitType(strings.TrimSpace(part))
						if err != nil {
							return nil, errors.Wrap(errors.PhaseParse, errors.KindInvalidData, err, "parse result type "+part)
						}
						sig.results = append(sig.results, t)
					}
				}
			} else {
				t, err := parseWitType(resultStr)
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

// splitParams splits parameter list, handling nested parens.
func splitParams(s string) []string {
	var result []string
	var current strings.Builder
	depth := 0

	for _, ch := range s {
		switch ch {
		case '(':
			depth++
			current.WriteRune(ch)
		case ')':
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

func parseWitType(s string) (wit.Type, error) {
	s = strings.TrimSpace(s)
	return wit.ParseType(s)
}
