package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestStackOutputProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, source  string
		action, input int
		kind          StackOutputKind
	}{
		{"arithmetic", `(module (func (result i32) i32.const 1 i32.const 2 i32.add))`, 2, -1, MaterializedOutput},
		{"tee", `(module (func (result i32) (local i32) i32.const 1 local.tee 0))`, 1, 0, ForwardedOutput},
		{"dead-concrete", `(module (func (result i32) unreachable i32.const 1))`, 1, -1, MaterializedOutput},
		{"validation-tee", `(module (func (result i32) (local i32) unreachable local.tee 0))`, 1, -1, ValidationOutput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, lowered := lowerValueFixture(t, tc.source)
			contract, err := lowered.StackContract(tc.action)
			if err != nil {
				t.Fatal(err)
			}
			output, ok := contract.Output(0)
			if !ok || output.Kind() != tc.kind || output.Type() != wasm.ValI32 {
				t.Fatalf("unexpected output: %+v exists=%t", output, ok)
			}
			input, forwarded := output.InputIndex()
			if forwarded != (tc.input >= 0) || (forwarded && input != tc.input) {
				t.Fatalf("input=%d forwarded=%t", input, forwarded)
			}
			if _, ok := contract.Output(-1); ok {
				t.Fatal("negative output accepted")
			}
			if _, ok := contract.Output(contract.OutputCount()); ok {
				t.Fatal("out-of-range output accepted")
			}
		})
	}
}

func TestBranchAndPortOutputProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		kind         StackOutputKind
	}{
		{"live", `(module (func (result i32) block (result i32) i32.const 11 i32.const 0 br_if 0 end))`, ForwardedOutput},
		{"polymorphic", `(module (func (result i32) block (result i32) unreachable br_if 0 end))`, ValidationOutput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, lowered := lowerValueFixture(t, tc.source)
			seenBranch, seenPort := false, false
			for i, action := range lowered.actions {
				if action.kind != SourceBranch && action.kind != SourcePortRead {
					continue
				}
				contract, err := lowered.StackContract(i)
				if err != nil {
					t.Fatal(err)
				}
				output, ok := contract.Output(0)
				if !ok || output.Type() != wasm.ValI32 {
					t.Fatal("missing typed output")
				}
				if action.kind == SourceBranch {
					seenBranch = true
					if output.Kind() != tc.kind {
						t.Fatalf("branch kind=%v", output.Kind())
					}
					if tc.kind == ForwardedOutput {
						input, ok := output.InputIndex()
						if !ok || contract.InputValue(input) != contract.OutputValue(0) {
							t.Fatal("forwarded input identity lost")
						}
					}
				} else {
					seenPort = true
					if output.Kind() != MaterializedOutput {
						t.Fatal("port read not materialized")
					}
				}
			}
			if !seenBranch || !seenPort {
				t.Fatal("fixture lost branch or port read")
			}
		})
	}
}
