package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

const selectorPlainIfFixture = `(module (func (result i32)
  i32.const 7
  i32.const 1
  if (param i32) (result i32)
    i32.const 1 i32.add
  else
    i32.const 2 i32.add
  end))`

const selectorAsyncIfFixture = `(module
  (import "env" "yield" (func $yield))
  (func (result i32)
    i32.const 7
    i32.const 1
    if (param i32) (result i32)
      call $yield
      i32.const 1 i32.add
    else
      i32.const 2 i32.add
    end))`

func selectorScope(t *testing.T, values *ValuePlan) ControlScope {
	t.Helper()
	var label LabelID
	found := false
	for candidate, scope := range values.scopes {
		if scope.kind != IfScope {
			continue
		}
		if found {
			t.Fatal("fixture has more than one if scope")
		}
		label, found = candidate, true
	}
	if !found {
		t.Fatal("fixture has no if scope")
	}
	scope, ok := values.Scope(label)
	if !ok {
		t.Fatal("if scope is not published")
	}
	return scope
}

func actionIndex(t *testing.T, lowered *LoweredControl, kind ActionKind, arm ControlArm) int {
	t.Helper()
	for index, action := range lowered.actions {
		if action.kind == kind && (!isSelectorAction(kind) || action.selector.arm == arm) {
			return index
		}
	}
	t.Fatalf("missing selector action kind=%d arm=%d", kind, arm)
	return -1
}

func assertSelectorAction(t *testing.T, action Action, kind ActionKind, scope ControlScope, local uint32, arm ControlArm) {
	t.Helper()
	operation, ok := action.Selector()
	if !ok {
		t.Fatal("selector action did not expose selector payload")
	}
	value, hasSelector := scope.Selector()
	if !hasSelector || operation.Scope() != scope.Label() || operation.Value() != value || operation.Local() != local || operation.Arm() != arm {
		t.Fatal("selector action lost its source identity or carrier assignment")
	}
	if action.origin.owner != operation.owner || action.origin.source != nil {
		t.Fatal("selector action has incorrect source provenance")
	}
	domain, hasDomain := action.Domain()
	instruction, primitive := action.Primitive()
	expectedDomain := semantics.GuestExecution
	expectedOpcode := wasm.OpLocalGet
	if kind == SourceSelectorStore {
		expectedOpcode = wasm.OpLocalSet
	}
	if kind == SourceSelectorRoute {
		expectedDomain = semantics.RewindRouting
	}
	if !hasDomain || !primitive || domain != expectedDomain || instruction.Opcode != expectedOpcode || instruction.Synthetic != (kind == SourceSelectorRoute) {
		t.Fatal("selector action lost its canonical execution projection")
	}
	immediate, ok := instruction.Imm.(wasm.LocalImm)
	if !ok || immediate.LocalIdx != local {
		t.Fatal("selector action projection uses the wrong carrier")
	}
}

func TestSelectorActionsBindPlainIfStoreEntryAndLoad(t *testing.T) {
	values, lowered := lowerValueFixture(t, selectorPlainIfFixture)
	scope := selectorScope(t, values)
	local, assigned := lowered.selectorLocals[scope.Label()]
	if !assigned {
		t.Fatal("if selector has no assigned carrier")
	}
	storeIndex := actionIndex(t, lowered, SourceSelectorStore, NoArm)
	entryIndex := actionIndex(t, lowered, ScopeEntryTransfer, NoArm)
	loadIndex := actionIndex(t, lowered, SourceSelectorLoad, NoArm)
	if storeIndex >= entryIndex || entryIndex >= loadIndex {
		t.Fatal("plain if did not store selector before parameter entry and reload it after")
	}
	assertSelectorAction(t, lowered.actions[storeIndex], SourceSelectorStore, scope, local, NoArm)
	assertSelectorAction(t, lowered.actions[loadIndex], SourceSelectorLoad, scope, local, NoArm)
	if _, ok := lowered.actions[entryIndex].Selector(); ok {
		t.Fatal("scope entry transfer impersonated a selector action")
	}
}

func TestSelectorActionsRouteAsyncIfArms(t *testing.T) {
	values, lowered := lowerValueFixture(t, selectorAsyncIfFixture)
	scope := selectorScope(t, values)
	local, assigned := lowered.selectorLocals[scope.Label()]
	if !assigned {
		t.Fatal("async if selector has no assigned carrier")
	}
	capture := actionIndex(t, lowered, AsyncIfEntryCapture, NoArm)
	thenRoute := actionIndex(t, lowered, SourceSelectorRoute, ThenArm)
	elseRoute := actionIndex(t, lowered, SourceSelectorRoute, ElseArm)
	if capture >= thenRoute || thenRoute >= elseRoute {
		t.Fatal("async if did not capture then route and else route in source order")
	}
	assertSelectorAction(t, lowered.actions[thenRoute], SourceSelectorRoute, scope, local, ThenArm)
	assertSelectorAction(t, lowered.actions[elseRoute], SourceSelectorRoute, scope, local, ElseArm)
	for _, action := range lowered.actions {
		if action.kind == SourceSelectorStore || action.kind == SourceSelectorLoad {
			t.Fatal("async if used a guest selector access instead of entry capture and routing")
		}
	}
}

func TestSelectorActionsRejectRetargetingAndCoverageChanges(t *testing.T) {
	for _, mutation := range []string{"value", "scope", "cell", "arm", "omit", "duplicate", "reorder"} {
		t.Run(mutation, func(t *testing.T) {
			values, lowered := lowerValueFixture(t, selectorPlainIfFixture)
			scope := selectorScope(t, values)
			storeIndex := actionIndex(t, lowered, SourceSelectorStore, NoArm)
			loadIndex := actionIndex(t, lowered, SourceSelectorLoad, NoArm)
			switch mutation {
			case "value":
				lowered.actions[storeIndex].selector.value = scope.EntryValue(0)
			case "scope":
				lowered.actions[storeIndex].selector.scope++
			case "cell":
				lowered.actions[storeIndex].selector.local++
			case "arm":
				lowered.actions[storeIndex].selector.arm = ThenArm
			case "omit":
				lowered.actions = append(lowered.actions[:storeIndex], lowered.actions[storeIndex+1:]...)
			case "duplicate":
				lowered.actions = append(lowered.actions, lowered.actions[storeIndex])
			case "reorder":
				lowered.actions[storeIndex], lowered.actions[loadIndex] = lowered.actions[loadIndex], lowered.actions[storeIndex]
			}
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("accepted corrupted selector action contract")
			}
		})
	}
}
