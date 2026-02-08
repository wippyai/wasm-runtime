package engine

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/component"
)

// TestNewLowerWrapper tests wrapper creation
func TestNewLowerWrapper(t *testing.T) {
	tests := []struct {
		handler any
		def     *component.LowerDef
		name    string
		wantErr bool
	}{
		{
			name:    "valid u32 handler",
			def:     &component.LowerDef{Name: "test", Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler: func(x uint32) uint32 { return x },
			wantErr: false,
		},
		{
			name:    "valid ctx handler",
			def:     &component.LowerDef{Name: "test", Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler: func(ctx context.Context, x uint32) uint32 { return x },
			wantErr: false,
		},
		{
			name:    "not a function",
			def:     &component.LowerDef{Name: "test"},
			handler: "not a function",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewLowerWrapper(tc.def, tc.handler)
			if (err != nil) != tc.wantErr {
				t.Errorf("NewLowerWrapper() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && w == nil {
				t.Error("expected non-nil wrapper")
			}
		})
	}
}

// TestLowerWrapper_FastPaths tests direct fast paths
func TestLowerWrapper_FastPaths(t *testing.T) {
	tests := []struct {
		handler    any
		def        *component.LowerDef
		name       string
		stackInput []uint64
		wantStack  []uint64
		hasFast    bool
	}{
		{
			name:       "u32,u32 -> u32 with ctx",
			def:        &component.LowerDef{Params: []wit.Type{wit.U32{}, wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler:    func(ctx context.Context, a, b uint32) uint32 { return a + b },
			hasFast:    true,
			stackInput: []uint64{3, 5},
			wantStack:  []uint64{8, 5},
		},
		{
			name:       "u32 -> u32 with ctx",
			def:        &component.LowerDef{Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler:    func(ctx context.Context, a uint32) uint32 { return a * 2 },
			hasFast:    true,
			stackInput: []uint64{21},
			wantStack:  []uint64{42},
		},
		{
			name:       "u32,u32 -> u32 no ctx",
			def:        &component.LowerDef{Params: []wit.Type{wit.U32{}, wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler:    func(a, b uint32) uint32 { return a * b },
			hasFast:    true,
			stackInput: []uint64{6, 7},
			wantStack:  []uint64{42, 7},
		},
		{
			name:       "ctx -> u32",
			def:        &component.LowerDef{Params: []wit.Type{}, Results: []wit.Type{wit.U32{}}},
			handler:    func(ctx context.Context) uint32 { return 99 },
			hasFast:    true,
			stackInput: []uint64{0},
			wantStack:  []uint64{99},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewLowerWrapper(tc.def, tc.handler)
			if err != nil {
				t.Fatalf("NewLowerWrapper failed: %v", err)
			}

			fn := w.tryBuildFastFunc()
			if tc.hasFast && fn == nil {
				t.Fatal("expected fast path but got nil")
			}
			if !tc.hasFast && fn != nil {
				t.Fatal("expected no fast path but got one")
			}

			if fn != nil {
				ctx := context.Background()
				stack := make([]uint64, len(tc.stackInput))
				copy(stack, tc.stackInput)
				fn(ctx, nil, stack)

				for i, want := range tc.wantStack {
					if stack[i] != want {
						t.Errorf("stack[%d] = %d, want %d", i, stack[i], want)
					}
				}
			}
		})
	}
}

// TestLowerWrapper_BoolFastPaths tests bool return fast paths
func TestLowerWrapper_BoolFastPaths(t *testing.T) {
	tests := []struct {
		name       string
		def        *component.LowerDef
		handler    any
		stackInput []uint64
		wantStack  uint64
	}{
		{
			name:       "u32 -> bool true",
			def:        &component.LowerDef{Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.Bool{}}},
			handler:    func(ctx context.Context, a uint32) bool { return a > 0 },
			stackInput: []uint64{5},
			wantStack:  1,
		},
		{
			name:       "u32 -> bool false",
			def:        &component.LowerDef{Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.Bool{}}},
			handler:    func(ctx context.Context, a uint32) bool { return a > 0 },
			stackInput: []uint64{0},
			wantStack:  0,
		},
		{
			name:       "u32,u32 -> bool",
			def:        &component.LowerDef{Params: []wit.Type{wit.U32{}, wit.U32{}}, Results: []wit.Type{wit.Bool{}}},
			handler:    func(ctx context.Context, a, b uint32) bool { return a == b },
			stackInput: []uint64{10, 10},
			wantStack:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewLowerWrapper(tc.def, tc.handler)
			if err != nil {
				t.Fatalf("NewLowerWrapper failed: %v", err)
			}

			fn := w.tryBuildBoolFastFunc(len(tc.def.Params), len(tc.def.Results))
			if fn == nil {
				t.Fatal("expected bool fast path")
			}

			ctx := context.Background()
			stack := make([]uint64, len(tc.stackInput))
			copy(stack, tc.stackInput)
			fn(ctx, nil, stack)

			if stack[0] != tc.wantStack {
				t.Errorf("stack[0] = %d, want %d", stack[0], tc.wantStack)
			}
		})
	}
}

// TestLowerWrapper_StackBoundsCheck ensures stack bounds are validated
func TestLowerWrapper_StackBoundsCheck(t *testing.T) {
	def := &component.LowerDef{
		Params:  []wit.Type{wit.U32{}, wit.U32{}},
		Results: []wit.Type{wit.U32{}},
	}
	handler := func(ctx context.Context, a, b uint32) uint32 { return a + b }

	w, _ := NewLowerWrapper(def, handler)
	fn := w.tryBuildFastFunc()
	if fn == nil {
		t.Fatal("expected fast path")
	}

	ctx := context.Background()

	// Test with insufficient stack - should not panic
	t.Run("empty stack", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic with empty stack: %v", r)
			}
		}()
		fn(ctx, nil, []uint64{})
	})

	t.Run("partial stack", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic with partial stack: %v", r)
			}
		}()
		fn(ctx, nil, []uint64{1})
	})
}

// TestLowerWrapper_FlatSignature tests signature calculations
func TestLowerWrapper_FlatSignature(t *testing.T) {
	tests := []struct {
		name        string
		params      []wit.Type
		results     []wit.Type
		wantParams  int
		wantResults int
	}{
		{
			name:        "empty",
			params:      nil,
			results:     nil,
			wantParams:  0,
			wantResults: 0,
		},
		{
			name:        "primitives",
			params:      []wit.Type{wit.U32{}, wit.U32{}},
			results:     []wit.Type{wit.U32{}},
			wantParams:  2,
			wantResults: 1,
		},
		{
			name:        "string param",
			params:      []wit.Type{wit.String{}},
			results:     []wit.Type{wit.U32{}},
			wantParams:  2, // string = ptr + len
			wantResults: 1,
		},
		{
			name:        "string result",
			params:      []wit.Type{wit.U32{}},
			results:     []wit.Type{wit.String{}},
			wantParams:  1,
			wantResults: 2, // string = ptr + len
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := &component.LowerDef{Params: tc.params, Results: tc.results}
			w, err := NewLowerWrapper(def, func() {})
			if err != nil {
				t.Fatalf("NewLowerWrapper failed: %v", err)
			}

			params, results := w.FlatSignature()
			if params != tc.wantParams {
				t.Errorf("params = %d, want %d", params, tc.wantParams)
			}
			if results != tc.wantResults {
				t.Errorf("results = %d, want %d", results, tc.wantResults)
			}
		})
	}
}

// TestLowerWrapper_ValidateHandler tests handler validation
func TestLowerWrapper_ValidateHandler(t *testing.T) {
	tests := []struct {
		handler any
		def     *component.LowerDef
		name    string
		wantErr bool
	}{
		{
			name:    "matching signature",
			def:     &component.LowerDef{Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler: func(x uint32) uint32 { return x },
			wantErr: false,
		},
		{
			name:    "matching with ctx",
			def:     &component.LowerDef{Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler: func(ctx context.Context, x uint32) uint32 { return x },
			wantErr: false,
		},
		{
			name:    "wrong param count",
			def:     &component.LowerDef{Params: []wit.Type{wit.U32{}, wit.U32{}}, Results: []wit.Type{wit.U32{}}},
			handler: func(x uint32) uint32 { return x },
			wantErr: true,
		},
		{
			name:    "wrong result count",
			def:     &component.LowerDef{Params: []wit.Type{wit.U32{}}, Results: []wit.Type{wit.U32{}, wit.U32{}}},
			handler: func(x uint32) uint32 { return x },
			wantErr: true,
		},
		{
			name:    "nil params skips validation",
			def:     &component.LowerDef{Params: nil, Results: []wit.Type{wit.U32{}}},
			handler: func(ctx context.Context, a, b, c uint32) uint32 { return a },
			wantErr: false,
		},
		{
			name:    "nil results skips result validation",
			def:     &component.LowerDef{Params: []wit.Type{wit.U32{}}, Results: nil},
			handler: func(x uint32) (uint32, error) { return x, nil },
			wantErr: false,
		},
		{
			name:    "empty params requires 0 handler params",
			def:     &component.LowerDef{Params: []wit.Type{}, Results: []wit.Type{wit.U32{}}},
			handler: func() uint32 { return 42 },
			wantErr: false,
		},
		{
			name:    "empty params fails with handler params",
			def:     &component.LowerDef{Params: []wit.Type{}, Results: []wit.Type{wit.U32{}}},
			handler: func(x uint32) uint32 { return x },
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewLowerWrapper(tc.def, tc.handler)
			if err != nil {
				t.Fatalf("NewLowerWrapper failed: %v", err)
			}

			err = w.ValidateHandler()
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateHandler() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestGetFlatTypes tests type flattening
func TestGetFlatTypes(t *testing.T) {
	tests := []struct {
		witType  wit.Type
		name     string
		wantLen  int
		wantType api.ValueType
	}{
		{wit.Bool{}, "bool", 1, api.ValueTypeI32},
		{wit.U8{}, "u8", 1, api.ValueTypeI32},
		{wit.U16{}, "u16", 1, api.ValueTypeI32},
		{wit.U32{}, "u32", 1, api.ValueTypeI32},
		{wit.U64{}, "u64", 1, api.ValueTypeI64},
		{wit.S32{}, "s32", 1, api.ValueTypeI32},
		{wit.S64{}, "s64", 1, api.ValueTypeI64},
		{wit.F32{}, "f32", 1, api.ValueTypeF32},
		{wit.F64{}, "f64", 1, api.ValueTypeF64},
		{wit.Char{}, "char", 1, api.ValueTypeI32},
		{wit.String{}, "string", 2, api.ValueTypeI32}, // ptr + len
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			types := getFlatTypes(tc.witType)
			if len(types) != tc.wantLen {
				t.Errorf("len(getFlatTypes(%s)) = %d, want %d", tc.name, len(types), tc.wantLen)
			}
			if len(types) > 0 && types[0] != tc.wantType {
				t.Errorf("getFlatTypes(%s)[0] = %v, want %v", tc.name, types[0], tc.wantType)
			}
		})
	}
}

// TestFlatCount tests flat counting
func TestFlatCount(t *testing.T) {
	tests := []struct {
		witType wit.Type
		name    string
		want    int
	}{
		{wit.U32{}, "u32", 1},
		{wit.U64{}, "u64", 1},
		{wit.String{}, "string", 2},
		{wit.Bool{}, "bool", 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := flatCount(tc.witType)
			if got != tc.want {
				t.Errorf("flatCount(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// Note: TestResultSize and TestUsesRetptr are in abi_test.go

// BenchmarkLowerWrapper_FastPath benchmarks the fast path
func BenchmarkLowerWrapper_FastPath(b *testing.B) {
	def := &component.LowerDef{
		Params:  []wit.Type{wit.U32{}, wit.U32{}},
		Results: []wit.Type{wit.U32{}},
	}
	handler := func(ctx context.Context, a, b uint32) uint32 { return a + b }

	w, _ := NewLowerWrapper(def, handler)
	fn := w.tryBuildFastFunc()

	ctx := context.Background()
	stack := []uint64{10, 20, 0}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(ctx, nil, stack)
	}
}

// BenchmarkLowerWrapper_BoolFastPath benchmarks bool fast path
func BenchmarkLowerWrapper_BoolFastPath(b *testing.B) {
	def := &component.LowerDef{
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{wit.Bool{}},
	}
	handler := func(ctx context.Context, a uint32) bool { return a > 0 }

	w, _ := NewLowerWrapper(def, handler)
	fn := w.tryBuildBoolFastFunc(1, 1)

	ctx := context.Background()
	stack := []uint64{42}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(ctx, nil, stack)
	}
}
