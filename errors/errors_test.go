package errors

import (
	"errors"
	"testing"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		contains []string
	}{
		{
			name: "full error",
			err: &Error{
				Phase:   PhaseEncode,
				Kind:    KindTypeMismatch,
				Path:    []string{"user", "address", "zip"},
				GoType:  "string",
				WitType: "u32",
				Detail:  "cannot convert",
			},
			contains: []string{"[encode]", "type_mismatch", "user.address.zip", "string", "u32", "cannot convert"},
		},
		{
			name: "minimal error",
			err: &Error{
				Phase: PhaseDecode,
				Kind:  KindOutOfBounds,
			},
			contains: []string{"[decode]", "out_of_bounds"},
		},
		{
			name: "error with cause",
			err: &Error{
				Phase:  PhaseRuntime,
				Kind:   KindAllocation,
				Detail: "memory full",
				Cause:  errors.New("underlying error"),
			},
			contains: []string{"[runtime]", "allocation", "memory full", "caused by", "underlying error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, s := range tt.contains {
				if !containsSubstring(msg, s) {
					t.Errorf("error message %q does not contain %q", msg, s)
				}
			}
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &Error{
		Phase: PhaseEncode,
		Kind:  KindInvalidData,
		Cause: cause,
	}

	if !errors.Is(err.Unwrap(), cause) {
		t.Error("Unwrap did not return cause")
	}

	// Test with errors.Unwrap
	if !errors.Is(errors.Unwrap(err), cause) {
		t.Error("errors.Unwrap did not return cause")
	}
}

func TestError_Is(t *testing.T) {
	err := &Error{
		Phase: PhaseEncode,
		Kind:  KindTypeMismatch,
		Path:  []string{"foo"},
	}

	// Same phase and kind
	if !err.Is(&Error{Phase: PhaseEncode, Kind: KindTypeMismatch}) {
		t.Error("Is should match same phase and kind")
	}

	// Different phase
	if err.Is(&Error{Phase: PhaseDecode, Kind: KindTypeMismatch}) {
		t.Error("Is should not match different phase")
	}

	// Different kind
	if err.Is(&Error{Phase: PhaseEncode, Kind: KindOutOfBounds}) {
		t.Error("Is should not match different kind")
	}

	// Test with errors.Is
	target := &Error{Phase: PhaseEncode, Kind: KindTypeMismatch}
	if !errors.Is(err, target) {
		t.Error("errors.Is should match")
	}
}

func TestBuilder(t *testing.T) {
	cause := errors.New("root")
	err := New(PhaseEncode, KindTypeMismatch).
		Path("user", "name").
		GoType("string").
		WitType("u32").
		Value(42).
		Cause(cause).
		Detail("expected %s, got %s", "string", "int").
		Build()

	if err.Phase != PhaseEncode {
		t.Errorf("Phase = %v, want %v", err.Phase, PhaseEncode)
	}
	if err.Kind != KindTypeMismatch {
		t.Errorf("Kind = %v, want %v", err.Kind, KindTypeMismatch)
	}
	if len(err.Path) != 2 || err.Path[0] != "user" || err.Path[1] != "name" {
		t.Errorf("Path = %v, want [user name]", err.Path)
	}
	if err.GoType != "string" {
		t.Errorf("GoType = %v, want 'string'", err.GoType)
	}
	if err.WitType != "u32" {
		t.Errorf("WitType = %v, want 'u32'", err.WitType)
	}
	if err.Value != 42 {
		t.Errorf("Value = %v, want 42", err.Value)
	}
	if !errors.Is(err.Cause, cause) {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
	if err.Detail != "expected string, got int" {
		t.Errorf("Detail = %v, want 'expected string, got int'", err.Detail)
	}
}

func TestConvenienceConstructors(t *testing.T) {
	t.Run("TypeMismatch", func(t *testing.T) {
		err := TypeMismatch(PhaseEncode, []string{"field"}, "int", "string")
		if err.Kind != KindTypeMismatch {
			t.Errorf("Kind = %v, want %v", err.Kind, KindTypeMismatch)
		}
		if err.GoType != "int" || err.WitType != "string" {
			t.Errorf("GoType=%v WitType=%v", err.GoType, err.WitType)
		}
	})

	t.Run("InvalidUTF8", func(t *testing.T) {
		data := []byte{0xff, 0xfe}
		err := InvalidUTF8(PhaseDecode, []string{"str"}, data)
		if err.Kind != KindInvalidUTF8 {
			t.Errorf("Kind = %v, want %v", err.Kind, KindInvalidUTF8)
		}
	})

	t.Run("AllocationFailed", func(t *testing.T) {
		err := AllocationFailed(PhaseEncode, 1024, 8)
		if err.Kind != KindAllocation {
			t.Errorf("Kind = %v, want %v", err.Kind, KindAllocation)
		}
		if !containsSubstring(err.Detail, "1024") {
			t.Errorf("Detail = %v, should contain size", err.Detail)
		}
	})

	t.Run("FieldMissing", func(t *testing.T) {
		err := FieldMissing(PhaseDecode, []string{"record"}, "name")
		if err.Kind != KindFieldMissing {
			t.Errorf("Kind = %v, want %v", err.Kind, KindFieldMissing)
		}
	})

	t.Run("InvalidDiscriminant", func(t *testing.T) {
		err := InvalidDiscriminant(PhaseDecode, []string{"variant"}, 5, 3)
		if err.Kind != KindInvalidVariant {
			t.Errorf("Kind = %v, want %v", err.Kind, KindInvalidVariant)
		}
	})

	t.Run("Unsupported", func(t *testing.T) {
		err := Unsupported(PhaseCompile, "resource types")
		if err.Kind != KindUnsupported {
			t.Errorf("Kind = %v, want %v", err.Kind, KindUnsupported)
		}
	})

	t.Run("OutOfBounds", func(t *testing.T) {
		err := OutOfBounds(PhaseDecode, []string{"list"}, 10, 5)
		if err.Kind != KindOutOfBounds {
			t.Errorf("Kind = %v, want %v", err.Kind, KindOutOfBounds)
		}
		if err.Value != 10 {
			t.Errorf("Value = %v, want 10", err.Value)
		}
	})

	t.Run("NilPointer", func(t *testing.T) {
		err := NilPointer(PhaseEncode, []string{"ptr"}, "*User")
		if err.Kind != KindNilPointer {
			t.Errorf("Kind = %v, want %v", err.Kind, KindNilPointer)
		}
		if err.GoType != "*User" {
			t.Errorf("GoType = %v, want '*User'", err.GoType)
		}
	})

	t.Run("Overflow", func(t *testing.T) {
		err := Overflow(PhaseEncode, []string{"val"}, 300, "u8")
		if err.Kind != KindOverflow {
			t.Errorf("Kind = %v, want %v", err.Kind, KindOverflow)
		}
		if err.Value != 300 {
			t.Errorf("Value = %v, want 300", err.Value)
		}
	})

	t.Run("FieldUnknown", func(t *testing.T) {
		err := FieldUnknown(PhaseDecode, []string{"record"}, "extra")
		if err.Kind != KindFieldUnknown {
			t.Errorf("Kind = %v, want %v", err.Kind, KindFieldUnknown)
		}
	})

	t.Run("InvalidEnum", func(t *testing.T) {
		err := InvalidEnum(PhaseDecode, []string{"status"}, "invalid", "Status")
		if err.Kind != KindInvalidEnum {
			t.Errorf("Kind = %v, want %v", err.Kind, KindInvalidEnum)
		}
	})
}

func TestMissingImportsError(t *testing.T) {
	t.Run("single import", func(t *testing.T) {
		err := NewMissingImportsError([]string{"wasi:http/types@0.2.0#new-fields"})
		if len(err.Imports) != 1 {
			t.Errorf("expected 1 import, got %d", len(err.Imports))
		}
		if err.Imports[0].Namespace != "wasi:http/types@0.2.0" {
			t.Errorf("namespace = %q, want wasi:http/types@0.2.0", err.Imports[0].Namespace)
		}
		if err.Imports[0].Function != "new-fields" {
			t.Errorf("function = %q, want new-fields", err.Imports[0].Function)
		}
	})

	t.Run("multiple imports same namespace", func(t *testing.T) {
		err := NewMissingImportsError([]string{
			"wasi:http/types@0.2.0#new-fields",
			"wasi:http/types@0.2.0#set-response-body",
		})
		if len(err.Imports) != 2 {
			t.Errorf("expected 2 imports, got %d", len(err.Imports))
		}

		msg := err.Error()
		if !containsSubstring(msg, "missing") {
			t.Errorf("error should contain 'missing'")
		}
		if !containsSubstring(msg, "2") {
			t.Errorf("error should contain count")
		}
		if !containsSubstring(msg, "wasi:http/types@0.2.0") {
			t.Errorf("error should contain namespace")
		}
		if !containsSubstring(msg, "new-fields") {
			t.Errorf("error should contain function name")
		}
	})

	t.Run("multiple namespaces grouped", func(t *testing.T) {
		err := NewMissingImportsError([]string{
			"wasi:http/types@0.2.0#new-fields",
			"wasi:io/streams@0.2.0#read",
			"wasi:http/types@0.2.0#finish",
		})
		msg := err.Error()
		// Verify grouping by namespace
		if !containsSubstring(msg, "wasi:http/types@0.2.0:") {
			t.Errorf("error should group by namespace")
		}
		if !containsSubstring(msg, "wasi:io/streams@0.2.0:") {
			t.Errorf("error should contain second namespace")
		}
	})

	t.Run("empty imports", func(t *testing.T) {
		err := NewMissingImportsError([]string{})
		msg := err.Error()
		if !containsSubstring(msg, "no imports specified") {
			t.Errorf("empty error should have specific message, got: %s", msg)
		}
	})

	t.Run("errors.Is", func(t *testing.T) {
		err := NewMissingImportsError([]string{"ns#fn"})
		if !errors.Is(err, &MissingImportsError{}) {
			t.Error("errors.Is should match MissingImportsError")
		}
	})
}

func TestDemangleRust(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "new-fields",
			expected: "new-fields",
		},
		{
			input:    "_ZN10hello_http8bindings4wasi4http5types6Fields3new11wit_import017ha931456e169eb010E",
			expected: "hello_http::bindings::wasi::http::types::Fields::new",
		},
		{
			input:    "_ZN4core3ptr8write_fn17ha1b2c3d4e5f67890E",
			expected: "core::ptr::write_fn",
		},
	}

	for _, tt := range tests {
		name := tt.input
		if len(name) > 30 {
			name = name[:30]
		}
		t.Run(name, func(t *testing.T) {
			result := demangleRust(tt.input)
			if result != tt.expected {
				t.Errorf("demangleRust(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && containsSubstringHelper(s, substr)))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
