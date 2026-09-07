package ir

import "testing"

const nestedCaptureCoverageSource = `(module
  (import "env" "yield" (func $yield))
  (func
    i32.const 1
    if
      i32.const 1
      if
        call $yield
      else
      end
    else
      call $yield
    end))`

func nestedCaptureCoverageFixture(t *testing.T) *LoweredControl {
	t.Helper()
	_, lowered, _ := captureFixture(t, nestedCaptureCoverageSource)
	if _, err := lowered.CopyActions(); err != nil {
		t.Fatal(err)
	}
	return lowered
}

func captureIndices(actions []Action) []int {
	var indices []int
	for index, action := range actions {
		if action.kind == AsyncIfEntryCapture {
			indices = append(indices, index)
		}
	}
	return indices
}

func TestCaptureCoverageRejectsMissingDuplicateAndMovedEvents(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate", "moved-after-source", "moved-after-generated", "nested-order"} {
		t.Run(scenario, func(t *testing.T) {
			lowered := nestedCaptureCoverageFixture(t)
			indices := captureIndices(lowered.actions)
			if len(indices) != 2 {
				t.Fatalf("captures=%d, want 2", len(indices))
			}
			actions := append([]Action(nil), lowered.actions...)
			switch scenario {
			case "missing":
				actions = append(actions[:indices[0]], actions[indices[0]+1:]...)
			case "duplicate":
				actions = append(actions, actions[indices[0]])
			case "moved-after-source":
				source := -1
				for index := indices[0] + 1; index < len(actions); index++ {
					if actions[index].kind == SourceInstruction {
						source = index
						break
					}
				}
				if source < 0 {
					t.Fatal("fixture has no source event after capture")
				}
				actions[indices[0]], actions[source] = actions[source], actions[indices[0]]
			case "moved-after-generated":
				first := indices[0]
				if actions[first+1].kind == SourceInstruction || actions[first+1].origin.owner != actions[first].origin.owner {
					t.Fatal("fixture lacks immediate owned routing action")
				}
				actions[first], actions[first+1] = actions[first+1], actions[first]
			case "nested-order":
				actions[indices[0]], actions[indices[1]] = actions[indices[1]], actions[indices[0]]
			}
			lowered.actions = actions
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted invalid capture/source coverage")
			}
		})
	}
}

const nestedTransferCoverageSource = `(module
  (import "env" "yield" (func $yield))
  (func (result i32)
    block (result i32)
      i32.const 1
      if (result i32)
        block (result i32)
          call $yield
          i32.const 7
        end
      else
        i32.const 8
      end
    end))`

func transferIndices(actions []Action) []int {
	var indices []int
	for index, action := range actions {
		if action.kind == ScopeResultTransfer {
			indices = append(indices, index)
		}
	}
	return indices
}

func TestActionCoverageRejectsMissingDuplicateAndMovedTransfers(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate", "moved-before-source", "nested-order"} {
		t.Run(scenario, func(t *testing.T) {
			_, lowered := lowerValueFixture(t, nestedTransferCoverageSource)
			if _, err := lowered.CopyActions(); err != nil {
				t.Fatal(err)
			}
			indices := transferIndices(lowered.actions)
			if len(indices) != 4 {
				t.Fatalf("transfers=%d, want 4", len(indices))
			}
			actions := append([]Action(nil), lowered.actions...)
			switch scenario {
			case "missing":
				actions = append(actions[:indices[0]], actions[indices[0]+1:]...)
			case "duplicate":
				actions = append(actions, actions[indices[0]])
			case "moved-before-source":
				source := -1
				for index := indices[0] - 1; index >= 0; index-- {
					if actions[index].kind == SourceInstruction {
						source = index
						break
					}
				}
				if source < 0 {
					t.Fatal("fixture has no source event before first transfer")
				}
				actions[indices[0]], actions[source] = actions[source], actions[indices[0]]
			case "nested-order":
				actions[indices[1]], actions[indices[2]] = actions[indices[2]], actions[indices[1]]
			}
			lowered.actions = actions
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted invalid scope transfer coverage")
			}
		})
	}
}

func TestActionCoverageRejectsCorruptSourceOwner(t *testing.T) {
	_, lowered := lowerValueFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func call $yield))`)
	for index := range lowered.actions {
		if lowered.actions[index].kind != SourceInstruction {
			continue
		}
		actions := append([]Action(nil), lowered.actions...)
		actions[index].origin.owner = &SeqNode{}
		lowered.actions = actions
		if _, err := lowered.CopyActions(); err == nil {
			t.Fatal("CopyActions accepted a source action with a foreign owner")
		}
		return
	}
	t.Fatal("fixture emitted no source action")
}
