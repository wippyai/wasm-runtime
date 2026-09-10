package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestContinuationStorageRejectsPhysicalDivergence(t *testing.T) {
	for _, mutation := range []string{"none", "slot", "identity", "type", "missing", "unbound"} {
		t.Run(mutation, func(t *testing.T) {
			plan, err := newContinuationStorage([]CallSite{{ActionIndex: 4}})
			if err != nil {
				t.Fatal(err)
			}
			inputs := []stackEntry{semantics.StoredOperand(3, wasm.ValI32).WithBinding(11)}
			if err := plan.capture(4, inputs); err != nil {
				t.Fatal(err)
			}
			if err := plan.seal(); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "slot":
				inputs[0] = semantics.StoredOperand(8, wasm.ValI32).WithBinding(11)
			case "identity":
				inputs[0] = inputs[0].WithBinding(12)
			case "type":
				inputs[0] = semantics.StoredOperand(3, wasm.ValI64).WithBinding(11)
			case "missing":
				inputs = nil
			case "unbound":
				inputs[0] = semantics.StoredOperand(3, wasm.ValI32)
			}
			err = plan.verify(4, len(inputs), func(index int) (stackEntry, bool) { return inputs[index], true })
			if (err == nil) != (mutation == "none") {
				t.Fatalf("unexpected verification: %v", err)
			}
			locals, err := plan.savedLocals()
			if err != nil || len(locals) != 1 || !locals[3] {
				t.Fatalf("capture was externally changed: %v, %v", locals, err)
			}
			delete(locals, 3)
			again, err := plan.savedLocals()
			if err != nil || !again[3] {
				t.Fatal("saved-local result aliases frozen state")
			}
		})
	}
}

func TestContinuationStorageCaptureProtocol(t *testing.T) {
	if _, err := newContinuationStorage([]CallSite{{ActionIndex: 2}, {ActionIndex: 2}}); err == nil {
		t.Fatal("duplicate action accepted")
	}
	plan, err := newContinuationStorage([]CallSite{{ActionIndex: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.savedLocals(); err == nil {
		t.Fatal("unsealed frame selection accepted")
	}
	if err := plan.seal(); err == nil {
		t.Fatal("missing capture accepted")
	}
	if err := plan.capture(3, nil); err == nil {
		t.Fatal("unknown continuation accepted")
	}
	if err := plan.capture(2, []stackEntry{semantics.StoredOperand(0, wasm.ValI32)}); err == nil {
		t.Fatal("unbound capture accepted")
	}
	if err := plan.capture(2, nil); err != nil {
		t.Fatal(err)
	}
	if err := plan.capture(2, nil); err == nil {
		t.Fatal("duplicate empty capture accepted")
	}
	if err := plan.seal(); err != nil {
		t.Fatal(err)
	}
	if err := plan.capture(2, nil); err == nil {
		t.Fatal("sealed capture mutated")
	}
	if err := plan.verify(2, 0, func(int) (stackEntry, bool) { t.Fatal("empty continuation read operand"); return stackEntry{}, false }); err != nil {
		t.Fatal(err)
	}
}

func TestContinuationStorageOwnsAllFrameRequirements(t *testing.T) {
	sites := []CallSite{{ActionIndex: 2, LiveLocals: []uint32{1}, ControlLocals: []uint32{3}}}
	plan, err := newContinuationStorage(sites)
	if err != nil {
		t.Fatal(err)
	}
	sites[0].LiveLocals[0], sites[0].ControlLocals[0] = 8, 9
	if err := plan.capture(2, []stackEntry{semantics.StoredOperand(5, wasm.ValI32).WithBinding(11)}); err != nil {
		t.Fatal(err)
	}
	if err := plan.seal(); err != nil {
		t.Fatal(err)
	}
	saved, err := plan.savedLocals()
	if err != nil || len(saved) != 3 || !saved[1] || !saved[3] || !saved[5] {
		t.Fatalf("lost or mutable frame requirements: %v, %v", saved, err)
	}
}
