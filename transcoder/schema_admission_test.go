package transcoder

import (
	"reflect"
	"testing"

	"go.bytecodealliance.org/wit"
)

func typedNilKinds() []struct {
	kind wit.TypeDefKind
	name string
} {
	return []struct {
		kind wit.TypeDefKind
		name string
	}{
		{kind: (*wit.Record)(nil), name: "record"},
		{kind: (*wit.List)(nil), name: "list"},
		{kind: (*wit.Tuple)(nil), name: "tuple"},
		{kind: (*wit.Enum)(nil), name: "enum"},
		{kind: (*wit.Flags)(nil), name: "flags"},
		{kind: (*wit.Option)(nil), name: "option"},
		{kind: (*wit.Result)(nil), name: "result"},
		{kind: (*wit.Variant)(nil), name: "variant"},
		{kind: (*wit.Own)(nil), name: "own"},
		{kind: (*wit.Borrow)(nil), name: "borrow"},
		{kind: (*wit.TypeDef)(nil), name: "alias"},
		{kind: (*wit.Resource)(nil), name: "resource"},
		{kind: (*wit.Future)(nil), name: "future"},
		{kind: (*wit.Stream)(nil), name: "stream"},
	}
}

func TestTypeDefKindAdmissionRejectsTypedNils(t *testing.T) {
	for _, tc := range typedNilKinds() {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Compile panicked: %v", r)
				}
			}()
			if _, err := NewCompiler().Compile(&wit.TypeDef{Kind: tc.kind}, reflect.TypeOf(struct{}{})); err == nil {
				t.Fatal("Compile accepted a typed-nil TypeDef kind")
			}
		})
	}
}

func TestTypeDefKindAdmissionRejectsNestedMalformedKinds(t *testing.T) {
	malformed := func() wit.Type {
		return &wit.TypeDef{Kind: (*wit.Record)(nil)}
	}
	tests := []struct {
		typ  wit.Type
		name string
	}{
		{typ: &wit.TypeDef{Kind: &wit.Record{Fields: []wit.Field{{Name: "x", Type: malformed()}}}}, name: "record-field"},
		{typ: &wit.TypeDef{Kind: &wit.Option{Type: malformed()}}, name: "option-element"},
		{typ: &wit.TypeDef{Kind: &wit.Result{OK: malformed()}}, name: "result-ok"},
		{typ: &wit.TypeDef{Kind: &wit.Result{Err: malformed()}}, name: "result-err"},
		{typ: &wit.TypeDef{Kind: &wit.Variant{Cases: []wit.Case{{Name: "bad", Type: malformed()}}}}, name: "variant-payload"},
		{typ: &wit.TypeDef{Kind: &wit.TypeDef{Kind: (*wit.Record)(nil)}}, name: "alias-target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewCompiler().Compile(tc.typ, reflect.TypeOf(struct{}{})); err == nil {
				t.Fatal("Compile accepted a nested malformed TypeDef kind")
			}
		})
	}
}

func TestTypeDefKindAdmissionRejectsCyclesAndAcceptsSharedDAG(t *testing.T) {
	selfAlias := &wit.TypeDef{}
	selfAlias.Kind = selfAlias

	left := &wit.TypeDef{}
	right := &wit.TypeDef{}
	left.Kind = &wit.Record{Fields: []wit.Field{{Name: "right", Type: right}}}
	right.Kind = &wit.Record{Fields: []wit.Field{{Name: "left", Type: left}}}

	for name, typ := range map[string]wit.Type{
		"self-alias":    selfAlias,
		"cyclic-record": left,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCompiler().Compile(typ, reflect.TypeOf(struct{}{})); err == nil {
				t.Fatal("Compile accepted recursive WIT definitions")
			}
		})
	}

	leaf := &wit.TypeDef{Kind: wit.U32{}}
	dag := &wit.TypeDef{Kind: &wit.Record{Fields: []wit.Field{
		{Name: "left", Type: leaf},
		{Name: "right", Type: leaf},
	}}}
	if _, err := NewCompiler().Compile(dag, reflect.TypeOf(struct {
		Left  uint32
		Right uint32
	}{})); err != nil {
		t.Fatalf("Compile rejected acyclic shared WIT graph: %v", err)
	}
}

func TestDynamicTypeDefKindAdmissionPrecedesGuestAccess(t *testing.T) {
	typ := &wit.TypeDef{Kind: &wit.Result{OK: &wit.TypeDef{Kind: (*wit.Record)(nil)}}}
	mem := newMockMemory(64)
	for i := range mem.data {
		mem.data[i] = 0xA5
	}
	enc := NewEncoder()
	dec := NewDecoder()

	if _, err := enc.EncodeParams([]wit.Type{typ}, []any{map[string]any{"ok": map[string]any{}}}, mem, nil, nil); err == nil {
		t.Fatal("EncodeParams accepted malformed schema")
	}
	if err := enc.StoreToMemory(typ, map[string]any{"ok": map[string]any{}}, 0, mem, nil, nil); err == nil {
		t.Fatal("StoreToMemory accepted malformed schema")
	}
	if _, err := dec.DecodeResults([]wit.Type{typ}, []uint64{0}, mem); err == nil {
		t.Fatal("DecodeResults accepted malformed schema")
	}
	if _, err := dec.LoadValue(typ, 0, mem); err == nil {
		t.Fatal("LoadValue accepted malformed schema")
	}
	for i, b := range mem.data {
		if b != 0xA5 {
			t.Fatalf("malformed schema wrote guest memory at byte %d", i)
		}
	}
}
