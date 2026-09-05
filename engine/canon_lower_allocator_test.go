package engine

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
	"go.bytecodealliance.org/wit"
)

// Preview1 adapters do not export cabi_realloc on calls whose results require
// only the caller-provided return area, such as descriptor.get-type.
func TestLowerWrapperFixedResultWithoutAllocator(t *testing.T) {
	ctx := context.Background()
	binary, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	mod, err := eng.LoadModule(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	if inst.instance.ExportedFunction(CabiRealloc) != nil {
		t.Fatal("fixture unexpectedly has an allocator")
	}
	resultType := &wit.TypeDef{Kind: &wit.Result{OK: wit.U32{}, Err: wit.U32{}}}
	for _, failed := range []bool{false, true} {
		called := false
		wrapper, err := NewLowerWrapper(&component.LowerDef{
			Name:    "test:fs/types#fixed-result",
			Params:  []wit.Type{wit.U32{}},
			Results: []wit.Type{resultType},
		}, func(handle uint32) (uint32, *uint32) {
			called = true
			if handle != 7 {
				t.Errorf("handle = %d, want 7", handle)
			}
			if failed {
				code := uint32(8)
				return 0, &code
			}
			return 3, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := wrapper.ValidateHandler(); err != nil {
			t.Fatal(err)
		}
		// Poison the return area so skipping either the handler or result lowering fails.
		inst.instance.Memory().Write(32, []byte{255, 255, 255, 255, 255, 255, 255, 255})
		wrapper.BuildRawFunc()(ctx, inst.instance, []uint64{7, 32})
		if !called {
			t.Fatal("handler was skipped without cabi_realloc")
		}
		tag, _ := inst.instance.Memory().ReadByte(32)
		value, _ := inst.instance.Memory().ReadUint32Le(36)
		wantTag, wantValue := byte(0), uint32(3)
		if failed {
			wantTag, wantValue = 1, 8
		}
		if tag != wantTag || value != wantValue {
			t.Fatalf("result = (%d,%d), want (%d,%d)", tag, value, wantTag, wantValue)
		}
	}
}
