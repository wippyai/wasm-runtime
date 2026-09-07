package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestTransferCoverageRejectsMissingAndSubstitutedOccurrences(t *testing.T) {
	for _, scenario := range []string{"missing-root-exit", "missing-if-arm", "missing-table-alternative", "repeated-ordinal", "swapped-arm-value", "changed-root-value"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := `(module (func (result i32) i32.const 1 if (result i32) i32.const 7 else i32.const 9 end))`
			if scenario == "missing-table-alternative" || scenario == "repeated-ordinal" {
				fixture = `(module (func (result i32) block (result i32) i32.const 7 i32.const 0 br_table 0 0 0 end))`
			}
			plan, err := valueFixture(t, fixture)
			if err != nil {
				t.Fatal(err)
			}
			remove := -1
			changed := false
			for index := range plan.edges {
				edge := &plan.edges[index]
				switch scenario {
				case "missing-root-exit":
					if edge.Kind == ControlExit && edge.Target == 0 {
						remove = index
						changed = true
					}
				case "missing-if-arm":
					if edge.Kind == ControlExit && edge.Arm == ThenArm {
						remove = index
						changed = true
					}
				case "missing-table-alternative":
					if edge.Kind == ControlBranch && edge.TargetOrdinal == 1 {
						remove = index
						changed = true
					}
				case "repeated-ordinal":
					if edge.Kind == ControlBranch && edge.TargetOrdinal == 0 {
						edge.TargetOrdinal = 1
						changed = true
					}
				case "swapped-arm-value":
					if edge.Kind == ControlExit && edge.Arm == ThenArm {
						for _, other := range plan.edges {
							if other.Arm == ElseArm {
								edge.From = append([]ValueID(nil), other.From...)
								changed = true
								break
							}
						}
					}
				case "changed-root-value":
					if edge.Kind == ControlExit && edge.Target == 0 {
						for id, definition := range plan.definitions {
							if definition.Port == InstructionResult {
								edge.From[0] = ValueID(id + 1)
								changed = true
								break
							}
						}
					}
				}
			}
			if !changed {
				t.Fatal("fixture did not exercise corruption")
			}
			if remove >= 0 {
				// Delete both derived representations. Coverage must be checked against
				// retained source facts, not merely consistency of these two containers.
				plan.edges = append(plan.edges[:remove], plan.edges[remove+1:]...)
				plan.transfers = make(map[Node][]int)
				for index, edge := range plan.edges {
					plan.transfers[edge.Source] = append(plan.transfers[edge.Source], index)
				}
			}
			if err := plan.verifyTransfers(); err == nil {
				t.Fatal("accepted incomplete or substituted source transfer")
			}
		})
	}
}

func TestMixedWidthControlPortsAndUnreachableArms(t *testing.T) {
	for _, fixture := range []string{
		`(module (func (result i32 i64) i32.const 7 i64.const 9 i32.const 1 if (param i32 i64) (result i32 i64) else end))`,
		`(module (func (result i32 i64) i32.const 1 if (result i32 i64) unreachable else i32.const 7 i64.const 9 end))`,
		`(module (func (result i32) block (result i32) unreachable br 0 end))`,
	} {
		plan, lowered := lowerValueFixture(t, fixture)
		if err := plan.verifyTransfers(); err != nil {
			t.Fatal(err)
		}
		root, _ := plan.Scope(0)
		if _, exists := lowered.PortStorage(root.ResultValue(0)); exists {
			t.Fatal("function without root branch unexpectedly materialized root")
		}
		for label, scope := range plan.scopes {
			if label == 0 {
				continue
			}
			for _, port := range append(append([]ValueID(nil), scope.params...), scope.results...) {
				local, exists := lowered.PortStorage(port)
				if !exists {
					t.Fatal("mixed-width source port lost carrier")
				}
				info, _ := plan.ValueInfo(port)
				if info.Type != wasm.ValI32 && info.Type != wasm.ValI64 {
					t.Fatalf("unexpected fixture type at local %d", local)
				}
			}
		}
	}
}
