package transcoder

import (
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/errors"
	"go.bytecodealliance.org/wit"
)

func TestSchemaCacheRemembersOnlyValidTypeDefs(t *testing.T) {
	var cache schemaCache
	valid := &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}
	invalid := &wit.TypeDef{Kind: (*wit.List)(nil)}

	if err := cache.validate(valid, errors.PhaseEncode, "param", 0); err != nil {
		t.Fatalf("valid type rejected: %v", err)
	}
	if _, ok := cache.valid.Load(valid); !ok {
		t.Fatal("valid TypeDef was not cached")
	}
	for range 2 {
		err := cache.validate(invalid, errors.PhaseEncode, "param", 3)
		if err == nil {
			t.Fatal("invalid type accepted")
		}
		if !strings.Contains(err.Error(), "param[3]") {
			t.Fatalf("error %q does not name param[3]", err)
		}
	}
	if _, ok := cache.valid.Load(invalid); ok {
		t.Fatal("invalid TypeDef was cached")
	}
}

func TestCachedSchemaValidationDoesNotAllocate(t *testing.T) {
	enc := NewEncoder()
	listType := &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}
	if err := enc.schemas.validate(listType, errors.PhaseEncode, "param", 0); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		_ = enc.schemas.validate(listType, errors.PhaseEncode, "param", 0)
	})
	if allocs != 0 {
		t.Fatalf("cached validation allocated %.0f times", allocs)
	}
}
