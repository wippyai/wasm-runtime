package transcoder

import (
	"reflect"
	"strconv"

	"github.com/wippyai/wasm-runtime/errors"
	"go.bytecodealliance.org/wit"
)

// validateWITSchema is the admission boundary for caller-supplied WIT. It
// rejects nil interface values and typed-nil TypeDef kinds before any ABI
// layout code can dereference them. It walks only the declared WIT graph at
// public API entry; recursive encoders and compilers rely on that preflight.
func validateWITSchema(t wit.Type, phase errors.Phase, path []string) error {
	v := witSchemaValidator{phase: phase, states: make(map[*wit.TypeDef]schemaVisitState)}
	return v.visitType(t, path)
}

type witSchemaValidator struct {
	states map[*wit.TypeDef]schemaVisitState
	phase  errors.Phase
}

type schemaVisitState uint8

const (
	schemaVisiting schemaVisitState = iota + 1
	schemaComplete
)

func (v *witSchemaValidator) invalid(path []string, detail string, args ...any) error {
	return errors.New(v.phase, errors.KindInvalidData).
		Path(path...).
		Detail(detail, args...).
		Build()
}

func (v *witSchemaValidator) visitType(t wit.Type, path []string) error {
	if t == nil {
		return v.invalid(path, "WIT type cannot be nil")
	}
	rv := reflect.ValueOf(t)
	if isNilSchemaValue(rv) {
		return v.invalid(path, "WIT type cannot be nil")
	}
	// Compiler cache keys contain the WIT interface. Comparable must inspect
	// the dynamic contents of embedded interfaces, not merely the outer type.
	if !rv.Comparable() {
		return errors.New(v.phase, errors.KindUnsupported).
			Path(path...).
			Detail("WIT type %T cannot be used as a compiler cache key", t).
			Build()
	}

	td, ok := t.(*wit.TypeDef)
	if !ok {
		return nil
	}
	switch v.states[td] {
	case schemaComplete:
		return nil
	case schemaVisiting:
		return v.invalid(path, "recursive WIT type definitions are unsupported")
	}
	v.states[td] = schemaVisiting
	if err := v.visitKind(td.Kind, path); err != nil {
		return err
	}
	v.states[td] = schemaComplete
	return nil
}

func (v *witSchemaValidator) visitKind(kind wit.TypeDefKind, path []string) error {
	if kind == nil || isNilSchemaValue(reflect.ValueOf(kind)) {
		return v.invalid(path, "WIT type definition kind cannot be nil")
	}

	switch k := kind.(type) {
	case *wit.Record:
		for i, field := range k.Fields {
			fieldPath := append(append([]string{}, path...), "record["+field.Name+"]")
			if field.Name == "" {
				fieldPath[len(fieldPath)-1] = "record-field[" + strconv.Itoa(i) + "]"
			}
			if err := v.visitType(field.Type, fieldPath); err != nil {
				return err
			}
		}
	case *wit.List:
		return v.visitType(k.Type, append(path, "list-element"))
	case *wit.Tuple:
		for i, typ := range k.Types {
			if err := v.visitType(typ, append(path, "tuple["+strconv.Itoa(i)+"]")); err != nil {
				return err
			}
		}
	case *wit.Flags:
		return validateFlagsCount(len(k.Flags), v.phase, path)
	case *wit.Option:
		return v.visitType(k.Type, append(path, "option"))
	case *wit.Result:
		if k.OK != nil {
			if err := v.visitType(k.OK, append(path, "result-ok")); err != nil {
				return err
			}
		}
		if k.Err != nil {
			if err := v.visitType(k.Err, append(path, "result-err")); err != nil {
				return err
			}
		}
	case *wit.Variant:
		for i, c := range k.Cases {
			if c.Type != nil {
				if err := v.visitType(c.Type, append(path, "variant["+casePathName(c.Name, i)+"]")); err != nil {
					return err
				}
			}
		}
	case *wit.TypeDef:
		return v.visitType(k, append(path, "alias"))
	}
	return nil
}

func isNilSchemaValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func casePathName(name string, index int) string {
	if name != "" {
		return name
	}
	return strconv.Itoa(index)
}
