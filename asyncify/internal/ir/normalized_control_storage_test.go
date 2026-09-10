package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func normalizedStorage(t *testing.T, values *ValuePlan, source *NormalizedSource) *controlStorage {
	t.Helper()
	next := uint32(30)
	storage, err := planControlStorage(values.control, func(wasm.ValType) uint32 {
		local := next
		next++
		return local
	}, true, source)
	if err != nil {
		t.Fatal(err)
	}
	return storage
}

func TestNormalizedControlStorageUsesActiveFunctionBranch(t *testing.T) {
	values, source := normalizedFixture(t, `(module (func (result i32)
  unreachable
  i32.const 7
  br 0))`)
	if source.HasFunctionBranch() {
		t.Fatal("dead root branch remained active after normalization")
	}
	storage := normalizedStorage(t, values, source)
	if storage.root != values.control.root {
		t.Fatal("dead root branch materialized a function carrier wrapper")
	}
	branch := values.control.instructions[2]
	if source.Active(branch) || storage.nodes[branch].hasSelector {
		t.Fatal("inactive root branch acquired control storage")
	}
}

func TestNormalizedControlStorageSkipsInactiveScopePorts(t *testing.T) {
	values, source := normalizedFixture(t, `(module (func (result i32)
  i32.const 7
  br 0
  block (result i32)
    i32.const 9
  end))`)
	storage := normalizedStorage(t, values, source)
	if err := storage.bindSourcePorts(values); err != nil {
		t.Fatal(err)
	}
	var inactive LabelID
	for label, scope := range values.scopes {
		if scope.kind == BlockScope {
			inactive = label
			if source.Active(scope.owner) {
				t.Fatal("fixture block was not normalized away")
			}
			if _, allocated := storage.nodes[scope.owner]; allocated {
				t.Fatal("inactive source scope acquired carrier storage")
			}
			for _, port := range append(append([]ValueID(nil), scope.params...), scope.results...) {
				if _, assigned := storage.portLocals[port]; assigned {
					t.Fatal("inactive source port acquired storage")
				}
			}
		}
	}
	if inactive == 0 {
		t.Fatal("fixture block scope missing")
	}
	root, ok := values.Scope(0)
	if !ok {
		t.Fatal("source root scope missing")
	}
	if _, assigned := storage.portLocals[root.ResultValue(0)]; !assigned {
		t.Fatal("active function branch lost root result port")
	}
}

func TestNormalizedControlStorageUsesNormalizedSuspensionFacts(t *testing.T) {
	values, source := normalizedFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    i32.const 1
    if
      unreachable
      call $yield
    else
      nop
    end))`)
	if source.SuspensionCount() != 0 {
		t.Fatal("dead call remained a normalized suspension")
	}
	storage := normalizedStorage(t, values, source)
	ifNode := values.control.root.(*SeqNode).Children[1].(*IfNode)
	carriers, exists := storage.nodes[ifNode]
	if !exists || carriers.hasSelector {
		t.Fatal("dead nested suspension allocated an if selector carrier")
	}
}

func TestNormalizedSavedCarriersSkipInactiveCallsWithOriginalIDs(t *testing.T) {
	values, source := normalizedFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    i32.const 1
    if
      unreachable
      call $yield
    else
      call $yield
    end))`)
	if source.SuspensionCount() != 1 {
		t.Fatal("expected one active suspension")
	}
	id, ok := source.SuspensionID(0)
	if !ok || id != 2 {
		t.Fatalf("normalized call identity=%d, want preserved ID 2", id)
	}
	storage := normalizedStorage(t, values, source)
	lowered := &LoweredControl{suspensions: []SuspensionSite{{SourceCallID: id}}}
	if err := storage.bindSavedCarriers(values.control, lowered, true); err != nil {
		t.Fatal(err)
	}
	if len(lowered.suspensions[0].ControlLocals) != 1 {
		t.Fatalf("active call did not receive exactly its enclosing selector carrier: %v", lowered.suspensions[0].ControlLocals)
	}
}

func TestNormalizedSavedCarriersKeepSyntheticFunctionRootActive(t *testing.T) {
	values, source := normalizedFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (result i32)
    call $yield
    i32.const 7
    br 0))`)
	if !source.HasFunctionBranch() || source.SuspensionCount() != 1 {
		t.Fatal("fixture did not retain active function branch and call")
	}
	storage := normalizedStorage(t, values, source)
	if storage.root == values.control.root {
		t.Fatal("active function branch did not materialize its carrier wrapper")
	}
	id, _ := source.SuspensionID(0)
	lowered := &LoweredControl{suspensions: []SuspensionSite{{SourceCallID: id}}}
	if err := storage.bindSavedCarriers(values.control, lowered, true); err != nil {
		t.Fatal(err)
	}
	if len(lowered.suspensions[0].ControlLocals) != 1 {
		t.Fatalf("synthetic function-root carrier not saved at active call: %v", lowered.suspensions[0].ControlLocals)
	}
}
