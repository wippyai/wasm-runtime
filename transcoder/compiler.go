package transcoder

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/wippyai/wasm-runtime/errors"
	"go.bytecodealliance.org/wit"
)

type Compiler struct {
	layout *LayoutCalculator
	cache  sync.Map // cacheKey -> *CompiledType
}

type cacheKey struct {
	goType reflect.Type
	witPtr uintptr
}

func NewCompiler() *Compiler {
	return &Compiler{
		layout: NewLayoutCalculator(),
	}
}

func (c *Compiler) Compile(witType wit.Type, goType reflect.Type) (*CompiledType, error) {
	if goType == nil {
		return nil, errors.New(errors.PhaseCompile, errors.KindNilPointer).
			Detail("Go type cannot be nil").
			Build()
	}

	// Dereference pointer types, except for Option which expects pointer
	if goType.Kind() == reflect.Ptr && !isOptionType(witType) {
		goType = goType.Elem()
	}

	key := cacheKey{witPtr: witTypePtr(witType), goType: goType}
	if cached, ok := c.cache.Load(key); ok {
		return cached.(*CompiledType), nil
	}

	ct, err := c.compile(witType, goType, nil)
	if err != nil {
		return nil, err
	}

	c.cache.Store(key, ct)
	return ct, nil
}

func isOptionType(t wit.Type) bool {
	if td, ok := t.(*wit.TypeDef); ok {
		_, isOption := td.Kind.(*wit.Option)
		return isOption
	}
	return false
}

func witTypePtr(t wit.Type) uintptr {
	switch v := t.(type) {
	case *wit.TypeDef:
		return reflect.ValueOf(v).Pointer()
	default:
		return reflect.TypeOf(t).Size()
	}
}

func (c *Compiler) compile(witType wit.Type, goType reflect.Type, path []string) (*CompiledType, error) {
	layout := c.layout.Calculate(witType)

	switch t := witType.(type) {
	case wit.Bool:
		return c.compilePrimitive(KindBool, goType, layout, path)
	case wit.U8:
		return c.compilePrimitive(KindU8, goType, layout, path)
	case wit.S8:
		return c.compilePrimitive(KindS8, goType, layout, path)
	case wit.U16:
		return c.compilePrimitive(KindU16, goType, layout, path)
	case wit.S16:
		return c.compilePrimitive(KindS16, goType, layout, path)
	case wit.U32:
		return c.compilePrimitive(KindU32, goType, layout, path)
	case wit.S32:
		return c.compilePrimitive(KindS32, goType, layout, path)
	case wit.U64:
		return c.compilePrimitive(KindU64, goType, layout, path)
	case wit.S64:
		return c.compilePrimitive(KindS64, goType, layout, path)
	case wit.F32:
		return c.compilePrimitive(KindF32, goType, layout, path)
	case wit.F64:
		return c.compilePrimitive(KindF64, goType, layout, path)
	case wit.Char:
		return c.compilePrimitive(KindChar, goType, layout, path)
	case wit.String:
		return c.compileString(goType, layout, path)
	case *wit.TypeDef:
		return c.compileTypeDef(t, goType, layout, path)
	default:
		return nil, errors.New(errors.PhaseCompile, errors.KindUnsupported).
			Path(path...).
			Detail("unsupported WIT type: %T", witType).
			Build()
	}
}

func (c *Compiler) compilePrimitive(kind TypeKind, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	if err := c.validatePrimitive(kind, goType, path); err != nil {
		return nil, err
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: 1,
		Kind:      kind,
	}, nil
}

func (c *Compiler) validatePrimitive(kind TypeKind, goType reflect.Type, path []string) error {
	var valid bool
	var expected string

	switch kind {
	case KindBool:
		valid = goType.Kind() == reflect.Bool
		expected = "bool"
	case KindU8:
		valid = goType.Kind() == reflect.Uint8
		expected = "uint8"
	case KindS8:
		valid = goType.Kind() == reflect.Int8
		expected = "int8"
	case KindU16:
		valid = goType.Kind() == reflect.Uint16
		expected = "uint16"
	case KindS16:
		valid = goType.Kind() == reflect.Int16
		expected = "int16"
	case KindU32:
		valid = goType.Kind() == reflect.Uint32
		expected = "uint32"
	case KindS32:
		valid = goType.Kind() == reflect.Int32
		expected = "int32"
	case KindU64:
		valid = goType.Kind() == reflect.Uint64
		expected = "uint64"
	case KindS64:
		valid = goType.Kind() == reflect.Int64
		expected = "int64"
	case KindF32:
		valid = goType.Kind() == reflect.Float32
		expected = "float32"
	case KindF64:
		valid = goType.Kind() == reflect.Float64
		expected = "float64"
	case KindChar:
		valid = goType.Kind() == reflect.Int32 || goType.Kind() == reflect.Uint32
		expected = "int32 (rune)"
	}

	if !valid {
		return errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), expected)
	}
	return nil
}

func (c *Compiler) compileString(goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	if goType.Kind() != reflect.String {
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "string")
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: 2, // ptr + len
		Kind:      KindString,
	}, nil
}

func (c *Compiler) compileTypeDef(t *wit.TypeDef, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	switch kind := t.Kind.(type) {
	case *wit.Record:
		return c.compileRecord(kind, goType, layout, path)
	case *wit.List:
		return c.compileList(kind, goType, layout, path)
	case *wit.Tuple:
		return c.compileTuple(kind, goType, layout, path)
	case *wit.Enum:
		return c.compileEnum(kind, goType, layout, path)
	case *wit.Flags:
		return c.compileFlags(kind, goType, layout, path)
	case *wit.Option:
		return c.compileOption(kind, goType, layout, path)
	case *wit.Result:
		return c.compileResult(kind, goType, layout, path)
	case *wit.Variant:
		return c.compileVariant(kind, goType, layout, path)
	case *wit.Own:
		return c.compileOwn(kind, goType, layout, path)
	case *wit.Borrow:
		return c.compileBorrow(kind, goType, layout, path)
	case wit.Type:
		return c.compile(kind, goType, path)
	default:
		return nil, errors.New(errors.PhaseCompile, errors.KindUnsupported).
			Path(path...).
			Detail("unsupported TypeDef kind: %T", kind).
			Build()
	}
}

func (c *Compiler) compileRecord(r *wit.Record, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	if goType.Kind() != reflect.Struct {
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "struct")
	}

	recordLayout := c.layout.calculateRecord(r)
	fields := make([]CompiledField, 0, len(r.Fields))
	flatCount := 0

	for _, witField := range r.Fields {
		goField, found := c.findGoField(goType, witField.Name)
		if !found {
			return nil, errors.FieldMissing(errors.PhaseCompile, path, witField.Name)
		}

		fieldPath := append(append([]string{}, path...), witField.Name)
		fieldType, err := c.compile(witField.Type, goField.Type, fieldPath)
		if err != nil {
			return nil, err
		}

		fields = append(fields, CompiledField{
			Name:      goField.Name,
			WitName:   witField.Name,
			GoOffset:  goField.Offset,
			WitOffset: recordLayout.FieldOffs[witField.Name],
			Type:      fieldType,
			IsPointer: goField.Type.Kind() == reflect.Ptr,
		})

		flatCount += fieldType.FlatCount
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: flatCount,
		Fields:    fields,
		Kind:      KindRecord,
	}, nil
}

// findGoField matches by: 1) wit:"name" tag, 2) case-insensitive, 3) kebab-to-camel.
func (c *Compiler) findGoField(goType reflect.Type, witName string) (reflect.StructField, bool) {
	for i := 0; i < goType.NumField(); i++ {
		field := goType.Field(i)
		if !field.IsExported() {
			continue
		}

		// Check wit tag first
		if tag := field.Tag.Get("wit"); tag != "" {
			if tag == witName || tag == "-" {
				if tag == "-" {
					continue
				}
				return field, true
			}
		}

		// Case-insensitive match
		if strings.EqualFold(field.Name, witName) {
			return field, true
		}

		// Convert Go name to kebab-case and compare
		kebab := toKebabCase(field.Name)
		if kebab == witName {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func toKebabCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func (c *Compiler) compileList(l *wit.List, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	if goType.Kind() != reflect.Slice {
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "slice")
	}

	elemPath := append(append([]string{}, path...), "[elem]")
	elemType, err := c.compile(l.Type, goType.Elem(), elemPath)
	if err != nil {
		return nil, err
	}

	// Cache SliceType to avoid repeated reflect.SliceOf calls during decoding
	elemType.SliceType = goType

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: 2, // ptr + len
		ElemType:  elemType,
		Kind:      KindList,
	}, nil
}

func (c *Compiler) compileTuple(t *wit.Tuple, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	if goType.Kind() != reflect.Struct && goType.Kind() != reflect.Array {
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "struct or array")
	}

	fields := make([]CompiledField, 0, len(t.Types))
	flatCount := 0
	witOffset := uint32(0)

	for i, elemWitType := range t.Types {
		var elemGoType reflect.Type
		var goOffset uintptr

		if goType.Kind() == reflect.Struct {
			if i >= goType.NumField() {
				return nil, errors.New(errors.PhaseCompile, errors.KindTypeMismatch).
					Path(path...).
					Detail("tuple has %d elements but struct has %d fields", len(t.Types), goType.NumField()).
					Build()
			}
			f := goType.Field(i)
			elemGoType = f.Type
			goOffset = f.Offset
		} else {
			elemGoType = goType.Elem()
			goOffset = uintptr(i) * elemGoType.Size()
		}

		fieldPath := append(append([]string{}, path...), "["+strconv.Itoa(i)+"]")
		elemType, err := c.compile(elemWitType, elemGoType, fieldPath)
		if err != nil {
			return nil, err
		}

		witOffset = alignTo(witOffset, elemType.WitAlign)
		fields = append(fields, CompiledField{
			GoOffset:  goOffset,
			WitOffset: witOffset,
			Type:      elemType,
		})

		witOffset += elemType.WitSize
		flatCount += elemType.FlatCount
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: flatCount,
		Fields:    fields,
		Kind:      KindTuple,
	}, nil
}

func (c *Compiler) compileEnum(e *wit.Enum, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	switch goType.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Int8, reflect.Int16, reflect.Int32:
	default:
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "integer")
	}

	// Populate Cases with names for discriminant size calculation
	cases := make([]CompiledCase, len(e.Cases))
	for i, ec := range e.Cases {
		cases[i] = CompiledCase{Name: ec.Name}
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: 1,
		Cases:     cases,
		Kind:      KindEnum,
	}, nil
}

func (c *Compiler) compileFlags(f *wit.Flags, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	if len(f.Flags) > 64 {
		return nil, errors.New(errors.PhaseCompile, errors.KindInvalidData).
			Path(path...).
			Detail("flags type exceeds maximum 64 flags, got %d", len(f.Flags)).
			Build()
	}

	switch goType.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "unsigned integer")
	}

	// Populate Cases with names for flag count sizing
	cases := make([]CompiledCase, len(f.Flags))
	for i, fl := range f.Flags {
		cases[i] = CompiledCase{Name: fl.Name}
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: 1,
		Cases:     cases,
		Kind:      KindFlags,
	}, nil
}

func (c *Compiler) compileOption(o *wit.Option, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	if goType.Kind() != reflect.Ptr {
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "pointer")
	}

	elemPath := append(append([]string{}, path...), "[some]")
	elemType, err := c.compile(o.Type, goType.Elem(), elemPath)
	if err != nil {
		return nil, err
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: 1 + elemType.FlatCount,
		ElemType:  elemType,
		Kind:      KindOption,
	}, nil
}

func (c *Compiler) compileResult(r *wit.Result, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	// Results are represented as a struct or interface
	ct := &CompiledType{
		GoType:   goType,
		GoSize:   goType.Size(),
		WitSize:  layout.Size,
		WitAlign: layout.Align,
		Kind:     KindResult,
	}

	// Compile OK type if present
	okFlatCount := 0
	if r.OK != nil {
		okPath := append(append([]string{}, path...), "[ok]")
		// For results we need to infer the Go type from context
		okType, err := c.compile(r.OK, getResultOkGoType(goType, r.OK), okPath)
		if err != nil {
			return nil, err
		}
		ct.OkType = okType
		okFlatCount = okType.FlatCount
	}

	// Compile Err type if present
	errFlatCount := 0
	if r.Err != nil {
		errPath := append(append([]string{}, path...), "[err]")
		errType, err := c.compile(r.Err, getResultErrGoType(goType, r.Err), errPath)
		if err != nil {
			return nil, err
		}
		ct.ErrType = errType
		errFlatCount = errType.FlatCount
	}

	// FlatCount = 1 (discriminant) + max(ok, err)
	maxPayload := okFlatCount
	if errFlatCount > maxPayload {
		maxPayload = errFlatCount
	}
	ct.FlatCount = 1 + maxPayload

	return ct, nil
}

func (c *Compiler) compileVariant(v *wit.Variant, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	cases := make([]CompiledCase, len(v.Cases))
	maxFlatCount := 0

	for i, vc := range v.Cases {
		cc := CompiledCase{Name: vc.Name}

		// Find the Go struct field for this case
		if goType.Kind() == reflect.Struct {
			for j := 0; j < goType.NumField(); j++ {
				f := goType.Field(j)
				if strings.EqualFold(f.Name, vc.Name) {
					cc.GoOffset = f.Offset
					break
				}
			}
		}

		if vc.Type != nil {
			casePath := append(append([]string{}, path...), vc.Name)
			caseGoType := getVariantCaseGoType(goType, vc.Name, vc.Type)
			caseType, err := c.compile(vc.Type, caseGoType, casePath)
			if err != nil {
				return nil, err
			}
			cc.Type = caseType
			if caseType.FlatCount > maxFlatCount {
				maxFlatCount = caseType.FlatCount
			}
		}
		cases[i] = cc
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   layout.Size,
		WitAlign:  layout.Align,
		FlatCount: 1 + maxFlatCount, // discriminant + max payload
		Cases:     cases,
		Kind:      KindVariant,
	}, nil
}

// Infers Go type for result/variant payloads. Dereferences pointer fields since
// variant/result cases use pointer fields for presence, but WIT type is the payload.
func getResultOkGoType(goType reflect.Type, witType wit.Type) reflect.Type {
	// Try to find an "Ok" or "Value" field in a struct
	if goType.Kind() == reflect.Struct {
		for i := 0; i < goType.NumField(); i++ {
			f := goType.Field(i)
			name := strings.ToLower(f.Name)
			if name == "ok" || name == "value" {
				// Dereference pointer - the field is *T but WIT type is T
				if f.Type.Kind() == reflect.Ptr {
					return f.Type.Elem()
				}
				return f.Type
			}
		}
	}
	// Fall back to interface{}
	return reflect.TypeOf((*any)(nil)).Elem()
}

func getResultErrGoType(goType reflect.Type, witType wit.Type) reflect.Type {
	if goType.Kind() == reflect.Struct {
		for i := 0; i < goType.NumField(); i++ {
			f := goType.Field(i)
			name := strings.ToLower(f.Name)
			if name == "err" || name == "error" {
				// Dereference pointer - the field is *T but WIT type is T
				if f.Type.Kind() == reflect.Ptr {
					return f.Type.Elem()
				}
				return f.Type
			}
		}
	}
	return reflect.TypeOf((*any)(nil)).Elem()
}

func getVariantCaseGoType(goType reflect.Type, caseName string, witType wit.Type) reflect.Type {
	if goType.Kind() == reflect.Struct {
		for i := 0; i < goType.NumField(); i++ {
			f := goType.Field(i)
			if strings.EqualFold(f.Name, caseName) {
				// Dereference pointer - the field is *T but WIT type is T
				if f.Type.Kind() == reflect.Ptr {
					return f.Type.Elem()
				}
				return f.Type
			}
		}
	}
	return reflect.TypeOf((*any)(nil)).Elem()
}

func (c *Compiler) compileOwn(o *wit.Own, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	// Own<T> is represented as a u32 handle on the stack
	// Go type should be Own[T] or uint32
	if goType.Kind() != reflect.Uint32 && goType.Kind() != reflect.Struct {
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "uint32 or Own[T]")
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   4,
		WitAlign:  4,
		FlatCount: 1,
		Kind:      KindOwn,
	}, nil
}

func (c *Compiler) compileBorrow(b *wit.Borrow, goType reflect.Type, layout LayoutInfo, path []string) (*CompiledType, error) {
	// Borrow<T> is represented as a u32 handle on the stack
	// Go type should be Borrow[T] or uint32
	if goType.Kind() != reflect.Uint32 && goType.Kind() != reflect.Struct {
		return nil, errors.TypeMismatch(errors.PhaseCompile, path, goType.String(), "uint32 or Borrow[T]")
	}

	return &CompiledType{
		GoType:    goType,
		GoSize:    goType.Size(),
		WitSize:   4,
		WitAlign:  4,
		FlatCount: 1,
		Kind:      KindBorrow,
	}, nil
}
