package errors

import (
	"fmt"
	"strings"
)

// Phase indicates where in processing the error occurred
type Phase string

const (
	PhaseCompile  Phase = "compile"  // type registration
	PhaseEncode   Phase = "encode"   // Go to WASM
	PhaseDecode   Phase = "decode"   // WASM to Go
	PhaseValidate Phase = "validate" // data validation
	PhaseRuntime  Phase = "runtime"  // runtime operations
	PhaseLinking  Phase = "linking"  // component linking
	PhaseLoad     Phase = "load"     // module loading
	PhaseHost     Phase = "host"     // host function registration
	PhaseParse    Phase = "parse"    // WAT/WIT parsing
)

// Kind categorizes the error
type Kind string

const (
	KindTypeMismatch   Kind = "type_mismatch"
	KindOutOfBounds    Kind = "out_of_bounds"
	KindInvalidData    Kind = "invalid_data"
	KindUnsupported    Kind = "unsupported"
	KindAllocation     Kind = "allocation"
	KindFieldMissing   Kind = "field_missing"
	KindFieldUnknown   Kind = "field_unknown"
	KindInvalidUTF8    Kind = "invalid_utf8"
	KindOverflow       Kind = "overflow"
	KindNilPointer     Kind = "nil_pointer"
	KindInvalidEnum    Kind = "invalid_enum"
	KindInvalidVariant Kind = "invalid_variant"
	KindMissingImport  Kind = "missing_import"
	KindNotFound       Kind = "not_found"
	KindNotInitialized Kind = "not_initialized"
	KindInvalidInput   Kind = "invalid_input"
	KindRegistration   Kind = "registration"
	KindInstantiation  Kind = "instantiation"
)

// Error is the structured error type used throughout SDK
type Error struct {
	Value   any
	Cause   error
	Phase   Phase
	Kind    Kind
	GoType  string
	WitType string
	Detail  string
	Path    []string
}

// Error implements the error interface
func (e *Error) Error() string {
	var b strings.Builder

	b.WriteByte('[')
	b.WriteString(string(e.Phase))
	b.WriteString("] ")
	b.WriteString(string(e.Kind))

	if len(e.Path) > 0 {
		b.WriteString(" at ")
		b.WriteString(strings.Join(e.Path, "."))
	}

	if e.GoType != "" || e.WitType != "" {
		b.WriteString(": ")
		if e.GoType != "" && e.WitType != "" {
			b.WriteString("Go type ")
			b.WriteString(e.GoType)
			b.WriteString(", WIT type ")
			b.WriteString(e.WitType)
		} else if e.GoType != "" {
			b.WriteString("Go type ")
			b.WriteString(e.GoType)
		} else {
			b.WriteString("WIT type ")
			b.WriteString(e.WitType)
		}
	}

	if e.Detail != "" {
		if e.GoType != "" || e.WitType != "" {
			b.WriteString(" - ")
		} else {
			b.WriteString(": ")
		}
		b.WriteString(e.Detail)
	}

	if e.Cause != nil {
		b.WriteString(" (caused by: ")
		b.WriteString(e.Cause.Error())
		b.WriteByte(')')
	}

	return b.String()
}

// Unwrap returns the underlying error
func (e *Error) Unwrap() error {
	return e.Cause
}

// Is reports whether target matches this error
func (e *Error) Is(target error) bool {
	if t, ok := target.(*Error); ok {
		return e.Phase == t.Phase && e.Kind == t.Kind
	}
	return false
}

// Builder provides structured error construction
type Builder struct {
	err Error
}

// New creates a new error builder
func New(phase Phase, kind Kind) *Builder {
	return &Builder{
		err: Error{
			Phase: phase,
			Kind:  kind,
		},
	}
}

// Path sets the field path
func (b *Builder) Path(path ...string) *Builder {
	b.err.Path = path
	return b
}

// GoType sets the Go type name
func (b *Builder) GoType(t string) *Builder {
	b.err.GoType = t
	return b
}

// WitType sets the WIT type name
func (b *Builder) WitType(t string) *Builder {
	b.err.WitType = t
	return b
}

// Value sets the offending value
func (b *Builder) Value(v any) *Builder {
	b.err.Value = v
	return b
}

// Cause sets the underlying error
func (b *Builder) Cause(err error) *Builder {
	b.err.Cause = err
	return b
}

// Detail sets the human-readable detail message
func (b *Builder) Detail(msg string, args ...any) *Builder {
	if len(args) > 0 {
		b.err.Detail = fmt.Sprintf(msg, args...)
	} else {
		b.err.Detail = msg
	}
	return b
}

// Build returns the constructed error
func (b *Builder) Build() *Error {
	return &b.err
}

// Convenience constructors for common error patterns

// TypeMismatch creates a type mismatch error
func TypeMismatch(phase Phase, path []string, goType, witType string) *Error {
	return &Error{
		Phase:   phase,
		Kind:    KindTypeMismatch,
		Path:    path,
		GoType:  goType,
		WitType: witType,
	}
}

// InvalidUTF8 creates an invalid UTF-8 error
func InvalidUTF8(phase Phase, path []string, data []byte) *Error {
	preview := data
	if len(preview) > 32 {
		preview = preview[:32]
	}
	return &Error{
		Phase:  phase,
		Kind:   KindInvalidUTF8,
		Path:   path,
		Detail: fmt.Sprintf("invalid UTF-8 sequence: %x", preview),
	}
}

// AllocationFailed creates an allocation failure error
func AllocationFailed(phase Phase, size, align uint32) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindAllocation,
		Detail: fmt.Sprintf("failed to allocate %d bytes (align %d)", size, align),
	}
}

// FieldMissing creates a missing field error
func FieldMissing(phase Phase, path []string, fieldName string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindFieldMissing,
		Path:   path,
		Detail: fmt.Sprintf("required field %q not found", fieldName),
	}
}

// InvalidDiscriminant creates an invalid discriminant error for variants/enums
func InvalidDiscriminant(phase Phase, path []string, disc uint32, maxValid uint32) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindInvalidVariant,
		Path:   path,
		Detail: fmt.Sprintf("discriminant %d out of range (max %d)", disc, maxValid),
		Value:  disc,
	}
}

// Unsupported creates an unsupported operation error
func Unsupported(phase Phase, what string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindUnsupported,
		Detail: what,
	}
}

// OutOfBounds creates an out of bounds error
func OutOfBounds(phase Phase, path []string, index, length int) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindOutOfBounds,
		Path:   path,
		Detail: fmt.Sprintf("index %d out of bounds (length %d)", index, length),
		Value:  index,
	}
}

// NilPointer creates a nil pointer error
func NilPointer(phase Phase, path []string, goType string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindNilPointer,
		Path:   path,
		GoType: goType,
		Detail: "nil pointer",
	}
}

// Overflow creates an overflow error
func Overflow(phase Phase, path []string, value any, targetType string) *Error {
	return &Error{
		Phase:   phase,
		Kind:    KindOverflow,
		Path:    path,
		WitType: targetType,
		Detail:  fmt.Sprintf("value %v overflows %s", value, targetType),
		Value:   value,
	}
}

// FieldUnknown creates an unknown field error
func FieldUnknown(phase Phase, path []string, fieldName string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindFieldUnknown,
		Path:   path,
		Detail: fmt.Sprintf("unknown field %q", fieldName),
	}
}

// InvalidEnum creates an invalid enum value error
func InvalidEnum(phase Phase, path []string, value any, enumType string) *Error {
	return &Error{
		Phase:   phase,
		Kind:    KindInvalidEnum,
		Path:    path,
		WitType: enumType,
		Detail:  fmt.Sprintf("invalid enum value %v for %s", value, enumType),
		Value:   value,
	}
}

// InvalidData creates an invalid data error
func InvalidData(phase Phase, path []string, detail string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindInvalidData,
		Path:   path,
		Detail: detail,
	}
}

// Wrap wraps an existing error with additional context
func Wrap(phase Phase, kind Kind, cause error, detail string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   kind,
		Detail: detail,
		Cause:  cause,
	}
}

// MissingImport represents a single unresolved import
type MissingImport struct {
	Namespace string // e.g., "wasi:http/types@0.2.0"
	Function  string // e.g., "new-fields"
}

// MissingImportsError is returned when component linking fails due to missing host functions
type MissingImportsError struct {
	Imports []MissingImport
}

// NewMissingImportsError creates an error from a list of "namespace#function" strings
func NewMissingImportsError(imports []string) *MissingImportsError {
	result := &MissingImportsError{
		Imports: make([]MissingImport, 0, len(imports)),
	}
	for _, imp := range imports {
		ns, fn := parseImportKey(imp)
		result.Imports = append(result.Imports, MissingImport{
			Namespace: ns,
			Function:  fn,
		})
	}
	return result
}

func parseImportKey(key string) (namespace, function string) {
	ns, fn, found := strings.Cut(key, "#")
	if found {
		return ns, fn
	}
	return key, ""
}

// demangleRust attempts to extract readable function name from mangled Rust symbol
func demangleRust(name string) string {
	// Rust mangled names start with _ZN
	if !strings.HasPrefix(name, "_ZN") {
		return name
	}

	// Extract path segments from mangled name
	// Format: _ZN<len><name><len><name>...E
	s := name[3:] // skip "_ZN"
	var parts []string

	for len(s) > 0 && s[0] != 'E' {
		// Read length (can be multiple digits)
		lenEnd := 0
		for lenEnd < len(s) && s[lenEnd] >= '0' && s[lenEnd] <= '9' {
			lenEnd++
		}
		if lenEnd == 0 {
			break
		}

		length := 0
		for i := 0; i < lenEnd; i++ {
			length = length*10 + int(s[i]-'0')
		}
		s = s[lenEnd:]

		if length > len(s) {
			break
		}

		part := s[:length]
		s = s[length:]

		// Skip wit_import markers and hash suffixes (17 char hashes starting with 'h')
		if strings.HasPrefix(part, "wit_import") {
			continue
		}
		if len(part) == 17 && part[0] == 'h' {
			allHex := true
			for i := 1; i < 17; i++ {
				c := part[i]
				if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
					allHex = false
					break
				}
			}
			if allHex {
				continue
			}
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return name
	}

	return strings.Join(parts, "::")
}

func (e *MissingImportsError) Error() string {
	if len(e.Imports) == 0 {
		return "[linking] missing_import: no imports specified"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("missing %d host function(s):\n", len(e.Imports)))

	// Group by namespace for cleaner output
	byNS := make(map[string][]string)
	var nsOrder []string
	for _, imp := range e.Imports {
		if _, exists := byNS[imp.Namespace]; !exists {
			nsOrder = append(nsOrder, imp.Namespace)
		}
		fn := demangleRust(imp.Function)
		byNS[imp.Namespace] = append(byNS[imp.Namespace], fn)
	}

	for _, ns := range nsOrder {
		b.WriteString("\n  ")
		b.WriteString(ns)
		b.WriteString(":\n")
		for _, fn := range byNS[ns] {
			b.WriteString("    - ")
			b.WriteString(fn)
			b.WriteByte('\n')
		}
	}

	return strings.TrimSuffix(b.String(), "\n")
}

// Is reports whether target matches this error type
func (e *MissingImportsError) Is(target error) bool {
	_, ok := target.(*MissingImportsError)
	return ok
}

// Runtime package convenience constructors

// NotInitialized creates a not-initialized error for missing module/instance
func NotInitialized(phase Phase, component string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindNotInitialized,
		Detail: fmt.Sprintf("%s not initialized", component),
	}
}

// NotFound creates a not-found error
func NotFound(phase Phase, what, name string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindNotFound,
		Detail: fmt.Sprintf("%s %q not found", what, name),
	}
}

// InvalidInput creates an invalid input error
func InvalidInput(phase Phase, detail string) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindInvalidInput,
		Detail: detail,
	}
}

// Registration creates a registration error
func Registration(phase Phase, namespace, name string, cause error) *Error {
	return &Error{
		Phase:  phase,
		Kind:   KindRegistration,
		Detail: fmt.Sprintf("register %s#%s", namespace, name),
		Cause:  cause,
	}
}

// Instantiation creates an instantiation error
func Instantiation(cause error) *Error {
	return &Error{
		Phase:  PhaseRuntime,
		Kind:   KindInstantiation,
		Detail: "instantiate module",
		Cause:  cause,
	}
}

// Load creates a module loading error
func Load(detail string, cause error) *Error {
	return &Error{
		Phase:  PhaseLoad,
		Kind:   KindInvalidData,
		Detail: detail,
		Cause:  cause,
	}
}

// ParseFailed creates a parsing error
func ParseFailed(what string, cause error) *Error {
	return &Error{
		Phase:  PhaseParse,
		Kind:   KindInvalidData,
		Detail: fmt.Sprintf("parse %s", what),
		Cause:  cause,
	}
}
