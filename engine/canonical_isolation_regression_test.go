package engine

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"go.bytecodealliance.org/wit"
)

func createTwoCoreWithOmittedFunc2Options(t *testing.T) (*WazeroEngine, *WazeroModule) {
	t.Helper()
	// Replace func2's canon lift section (14 bytes: 080c010000060303010405050700)
	// with canon lift having 0 options (8 bytes: 0806010000060000).
	target := "080c010000060303010405050700"
	replacement := "0806010000060000"

	if !strings.Contains(twoCoreFixtureWasmHex, target) {
		t.Fatalf("target pattern %q not found in twoCoreFixtureWasmHex", target)
	}
	omittedHex := strings.Replace(twoCoreFixtureWasmHex, target, replacement, 1)

	wasmBytes, err := hex.DecodeString(omittedHex)
	if err != nil {
		t.Fatalf("hex decode wasm: %v", err)
	}

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("LoadModule with omitted canon options: %v", err)
	}

	return eng, mod
}

func TestCanonicalIsolation_TwoCoreAbsentOptionsRegression(t *testing.T) {
	ctx := context.Background()
	eng, mod := createTwoCoreWithOmittedFunc2Options(t)
	defer eng.Close(ctx)

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EntryExport: "func1"})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	// Check initial heap of core1 and core2
	h1, err := inst.CallWithLift(ctx, "get-heap1")
	if err != nil {
		t.Fatalf("get-heap1: %v", err)
	}
	if h1.(uint32) != 70000 {
		t.Fatalf("expected initial heap1 = 70000, got %v", h1)
	}

	h2, err := inst.CallWithLift(ctx, "get-heap2")
	if err != nil {
		t.Fatalf("get-heap2: %v", err)
	}
	if h2.(uint32) != 140000 {
		t.Fatalf("expected initial heap2 = 140000, got %v", h2)
	}

	// Verify func2 binding has no memory and no allocator
	b2, err := inst.getExportBinding("func2")
	if err != nil {
		t.Fatalf("getExportBinding(func2): %v", err)
	}
	if b2.memory != nil {
		t.Errorf("expected func2 binding memory = nil, got %v", b2.memory)
	}
	if b2.alloc != nil {
		t.Errorf("expected func2 binding alloc = nil, got %v", b2.alloc)
	}

	// 1. CallWithLift on func2 must fail cleanly and not touch core1 heap
	t.Run("CallWithLift_FailsCleanly", func(t *testing.T) {
		_, callErr := inst.CallWithLift(ctx, "func2", "test-lift")
		if callErr == nil {
			t.Errorf("expected CallWithLift(func2) to fail, got nil error")
		}
		h1After, _ := inst.CallWithLift(ctx, "get-heap1")
		if h1After.(uint32) != 70000 {
			t.Errorf("core1 heap was modified by func2! heap1 = %v, want 70000", h1After)
		}
	})

	// 2. CallWithTypes on func2 must fail cleanly and not touch core1 heap
	t.Run("CallWithTypes_FailsCleanly", func(t *testing.T) {
		_, callErr := inst.CallWithTypes(ctx, "func2", []wit.Type{wit.String{}}, []wit.Type{wit.String{}}, "test-types")
		if callErr == nil {
			t.Errorf("expected CallWithTypes(func2) to fail, got nil error")
		}
		h1After, _ := inst.CallWithLift(ctx, "get-heap1")
		if h1After.(uint32) != 70000 {
			t.Errorf("core1 heap was modified by func2! heap1 = %v, want 70000", h1After)
		}
	})

	// 3. CallInto on func2 must fail cleanly and not touch core1 heap
	t.Run("CallInto_FailsCleanly", func(t *testing.T) {
		var res string
		callErr := inst.CallInto(ctx, "func2", []wit.Type{wit.String{}}, []wit.Type{wit.String{}}, &res, "test-into")
		if callErr == nil {
			t.Errorf("expected CallInto(func2) to fail, got nil error")
		}
		h1After, _ := inst.CallWithLift(ctx, "get-heap1")
		if h1After.(uint32) != 70000 {
			t.Errorf("core1 heap was modified by func2! heap1 = %v, want 70000", h1After)
		}
	})

	// 4. StartCall on func2 must fail cleanly and not touch core1 heap
	t.Run("StartCall_FailsCleanly", func(t *testing.T) {
		_, callErr := inst.StartCall(ctx, "func2", "test-startcall")
		if callErr == nil {
			t.Errorf("expected StartCall(func2) to fail, got nil error")
		}
		h1After, _ := inst.CallWithLift(ctx, "get-heap1")
		if h1After.(uint32) != 70000 {
			t.Errorf("core1 heap was modified by func2! heap1 = %v, want 70000", h1After)
		}
	})

	// 5. Legitimate scalar exports must succeed on both cores
	t.Run("LegitimateScalarExports_Succeed", func(t *testing.T) {
		p1, err := inst.CallWithLift(ctx, "get-post1")
		if err != nil {
			t.Fatalf("get-post1: %v", err)
		}
		if p1.(uint32) != 0 {
			t.Fatalf("expected initial post1 = 0, got %v", p1)
		}

		p2, err := inst.CallWithLift(ctx, "get-post2")
		if err != nil {
			t.Fatalf("get-post2: %v", err)
		}
		if p2.(uint32) != 0 {
			t.Fatalf("expected initial post2 = 0, got %v", p2)
		}
	})

	// 6. Legitimate func1 on core1 (with canonical options) succeeds and uses core1
	t.Run("Func1WithCanonicalOptions_Succeeds", func(t *testing.T) {
		res1, err := inst.CallWithLift(ctx, "func1", "hello-core1")
		if err != nil {
			t.Fatalf("func1: %v", err)
		}
		if res1.(string) != "hello-core1" {
			t.Fatalf("func1 result = %q, want %q", res1, "hello-core1")
		}

		// Verify core1 post incremented and core1 heap advanced
		p1, _ := inst.CallWithLift(ctx, "get-post1")
		if p1.(uint32) != 1 {
			t.Errorf("post1 = %v, want 1", p1)
		}
		h1After, _ := inst.CallWithLift(ctx, "get-heap1")
		if h1After.(uint32) <= 70000 {
			t.Errorf("heap1 after func1 = %v, want > 70000", h1After)
		}

		// Verify core2 remains untouched
		p2, _ := inst.CallWithLift(ctx, "get-post2")
		if p2.(uint32) != 0 {
			t.Errorf("post2 = %v, want 0", p2)
		}
		h2After, _ := inst.CallWithLift(ctx, "get-heap2")
		if h2After.(uint32) != 140000 {
			t.Errorf("heap2 after func1 = %v, want 140000", h2After)
		}
	})
}

func TestCanonicalIsolation_DefaultAllocatorFreeFnSafety(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EntryExport: "func1"})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	// Neither core in twoCoreFixture exports cabi_free/deallocate/free.
	// Verify that wazInst.freeFn is nil and was not mistakenly picked from another module.
	if inst.freeFn != nil {
		t.Errorf("expected wazInst.freeFn = nil when owning module exports no free, got %v", inst.freeFn)
	}
	if inst.alloc != nil && inst.alloc.freeFn != nil {
		t.Errorf("expected wazInst.alloc.freeFn = nil, got %v", inst.alloc.freeFn)
	}
}
