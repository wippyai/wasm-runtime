package engine

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
	"go.bytecodealliance.org/wit"
)

func TestStringFastPathAllocatorInstanceIsolation(t *testing.T) {
	for _, withContext := range []bool{false, true} {
		name := "without-context"
		if withContext {
			name = "with-context"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			rt := wazero.NewRuntime(ctx)
			defer rt.Close(ctx)
			data, err := wat.Compile(`(module
    (memory (export "memory") 1)
    (global $next (mut i32) (i32.const 1024))
    (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
      global.get $next
      global.get $next local.get 3 i32.add global.set $next))`)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := rt.CompileModule(ctx, data)
			if err != nil {
				t.Fatal(err)
			}
			instantiate := func() api.Module {
				m, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			a, b := instantiate(), instantiate()
			var handler any = func(a, b string) string { return a + b }
			if withContext {
				handler = func(_ context.Context, a, b string) string { return a + b }
			}
			wrapper, err := NewLowerWrapper(&component.LowerDef{
				Params: []wit.Type{wit.String{}, wit.String{}}, Results: []wit.Type{wit.String{}},
			}, handler)
			if err != nil {
				t.Fatal(err)
			}
			fn := wrapper.tryBuildFastFunc()
			if fn == nil {
				t.Fatal("missing fast path")
			}
			call := func(m api.Module, value string) uint32 {
				t.Helper()
				if !m.Memory().WriteString(32, value) {
					t.Fatal("write input")
				}
				fn(ctx, m, []uint64{32, uint64(len(value)), 32, uint64(len(value)), 64})
				ptr, ok := m.Memory().ReadUint32Le(64)
				if !ok {
					t.Fatal("read pointer")
				}
				n, ok := m.Memory().ReadUint32Le(68)
				if !ok {
					t.Fatal("read length")
				}
				got, ok := m.Memory().Read(ptr, n)
				if !ok || string(got) != value+value {
					t.Fatalf("got %q, want %q", got, value+value)
				}
				return ptr
			}
			if got := call(a, "alpha"); got != 1024 {
				t.Fatalf("A first allocation = %d", got)
			}
			// Both live guests must advance their own heap, even before either closes.
			if got := call(b, "bravo"); got != 1024 {
				t.Fatalf("B first allocation = %d, want 1024", got)
			}
			if err := a.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if got := call(b, "charlie"); got != 1034 {
				t.Fatalf("B next allocation = %d, want 1034", got)
			}
			c := instantiate()
			if got := call(c, "delta"); got != 1024 {
				t.Fatalf("C first allocation = %d", got)
			}
			var workers sync.WaitGroup
			for i, m := range []api.Module{b, c} {
				workers.Add(1)
				go func() {
					defer workers.Done()
					value := []string{"guest-B", "guest-C"}[i]
					for range 100 {
						call(m, value)
					}
				}()
			}
			workers.Wait()
		})
	}
}

func TestStringFastPathAllocationFailureTraps(t *testing.T) {
	for _, withContext := range []bool{false, true} {
		for _, tc := range []struct {
			name, allocator, result string
			retptr                  uint32
			wantTrap                bool
		}{
			{"allocator-trap", "unreachable", "result", 64, true},
			{"allocator-null", "i32.const 0", "result", 64, true},
			{"allocator-out-of-bounds", "i32.const 65535", "result", 64, true},
			{"record-out-of-bounds", "i32.const 1024", "result", 65532, true},
			{"record-wraparound", "i32.const 1024", "result", 0xfffffffc, true},
			{"empty-result", "unreachable", "", 64, false},
			{"valid-result", "i32.const 1024", "result", 64, false},
		} {
			name := tc.name + "/without-context"
			if withContext {
				name = tc.name + "/with-context"
			}
			t.Run(name, func(t *testing.T) {
				ctx := t.Context()
				rt := wazero.NewRuntime(ctx)
				defer rt.Close(ctx)
				var handler any = func(a, b string) string { return tc.result }
				if withContext {
					handler = func(_ context.Context, a, b string) string { return tc.result }
				}
				w, err := NewLowerWrapper(&component.LowerDef{Name: "concat", Params: []wit.Type{wit.String{}, wit.String{}}, Results: []wit.Type{wit.String{}}}, handler)
				if err != nil {
					t.Fatal(err)
				}
				fn := w.tryBuildFastFunc()
				if fn == nil {
					t.Fatal("missing fast path")
				}
				_, err = rt.NewHostModuleBuilder("host").NewFunctionBuilder().WithGoModuleFunction(fn, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).Export("concat").Instantiate(ctx)
				if err != nil {
					t.Fatal(err)
				}
				data, err := wat.Compile(`(module
      (import "host" "concat" (func $concat (param i32 i32 i32 i32 i32)))
      (memory (export "memory") 1)
      (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) ` + tc.allocator + `)
      (func (export "run") (param i32)
        i32.const 32 i32.const 0 i32.const 32 i32.const 0 local.get 0 call $concat))`)
				if err != nil {
					t.Fatal(err)
				}
				m, err := rt.Instantiate(ctx, data)
				if err != nil {
					t.Fatal(err)
				}
				sentinel := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0xab, 0xcd}
				if !m.Memory().Write(64, sentinel) {
					t.Fatal("sentinel write")
				}
				_, err = m.ExportedFunction("run").Call(ctx, uint64(tc.retptr))
				if tc.wantTrap {
					if err == nil {
						t.Fatal("expected guest call to trap")
					}
					got, ok := m.Memory().Read(64, 8)
					if !ok || !bytes.Equal(got, sentinel) {
						t.Fatalf("failure published result record: %x", got)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					ptr, _ := m.Memory().ReadUint32Le(64)
					n, _ := m.Memory().ReadUint32Le(68)
					got, ok := m.Memory().Read(ptr, n)
					if !ok || string(got) != tc.result {
						t.Fatalf("result=%q want %q", got, tc.result)
					}
				}
			})
		}
	}
}
