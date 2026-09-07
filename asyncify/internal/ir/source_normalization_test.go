package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func normalizedFixture(t *testing.T, source string) (*ValuePlan, *NormalizedSource) {
	t.Helper()
	values, err := valueFixture(t, source)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := NormalizeSource(values)
	if err != nil {
		t.Fatal(err)
	}
	return values, normalized
}

func normalizedOpcodes(t *testing.T, source *NormalizedSource) []byte {
	t.Helper()
	result := make([]byte, 0, source.InstructionCount())
	for index := 0; index < source.InstructionCount(); index++ {
		instruction, _, ok := source.Instruction(index)
		if !ok {
			t.Fatalf("normalized instruction %d missing", index)
		}
		result = append(result, instruction.Opcode)
	}
	return result
}

func TestNormalizeSourceElidesDeadTypedOperationsAfterTerminator(t *testing.T) {
	_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func call $yield unreachable i64.add drop))`)
	got := normalizedOpcodes(t, normalized)
	if len(got) != 2 || got[0] != wasm.OpCall || got[1] != wasm.OpUnreachable {
		t.Fatalf("active opcodes = %x, want call/unreachable", got)
	}
	if reason, elided := normalized.ElisionAt(2); !elided || reason != ElidedAfterTerminator {
		t.Fatalf("dead i64.add elision = (%v, %t)", reason, elided)
	}
	if !normalized.NeedsTransform() || normalized.SuspensionCount() != 1 {
		t.Fatal("reachable yield lost its suspension fact")
	}
	if id, ok := normalized.SuspensionID(0); !ok || id != 1 {
		t.Fatalf("active suspension id = %d, %t; want 1, true", id, ok)
	}
}

func TestNormalizeSourcePreservesCallIdentityAcrossDeadGap(t *testing.T) {
	_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func call $yield unreachable call $yield))`)
	if normalized.InstructionCount() != 2 || normalized.SuspensionCount() != 1 {
		t.Fatal("dead call remained executable")
	}
	if id, ok := normalized.SuspensionID(0); !ok || id != 1 {
		t.Fatalf("first call identity = %d, %t", id, ok)
	}
	if reason, elided := normalized.ElisionAt(2); !elided || reason != ElidedAfterTerminator {
		t.Fatalf("dead second call elision = (%v, %t)", reason, elided)
	}
}

func TestNormalizeSourceBlockExitAndLoopReentryHaveDifferentFallthrough(t *testing.T) {
	t.Run("block-exit", func(t *testing.T) {
		_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func block br 0 call $yield end call $yield))`)
		if normalized.InstructionCount() != 2 || normalized.SuspensionCount() != 1 {
			t.Fatal("block exit did not retain outer continuation only")
		}
		if id, ok := normalized.SuspensionID(0); !ok || id != 2 {
			t.Fatalf("outer yield identity = %d, %t; want 2, true", id, ok)
		}
	})
	t.Run("loop-reentry", func(t *testing.T) {
		_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func loop br 0 call $yield end call $yield))`)
		if normalized.NeedsTransform() || normalized.SuspensionCount() != 0 {
			t.Fatal("loop reentry incorrectly fell through to yields")
		}
		if reason, elided := normalized.ElisionAt(2); !elided || reason != ElidedAfterTerminator {
			t.Fatalf("after-loop call elision = (%v, %t)", reason, elided)
		}
	})
}

func TestNormalizeSourceIfAndConditionalBranchKeepPossibleFallthrough(t *testing.T) {
	t.Run("if-alternatives", func(t *testing.T) {
		_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func i32.const 0 if unreachable call $yield else call $yield end))`)
		if !normalized.NeedsTransform() || !normalized.HasAsyncInBranch() || normalized.SuspensionCount() != 1 {
			t.Fatal("reachable else suspension was not retained")
		}
		if id, ok := normalized.SuspensionID(0); !ok || id != 2 {
			t.Fatalf("else yield identity = %d, %t; want 2, true", id, ok)
		}
	})
	t.Run("br-if", func(t *testing.T) {
		_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func block i32.const 1 br_if 0 call $yield end))`)
		if !normalized.NeedsTransform() || normalized.SuspensionCount() != 1 {
			t.Fatal("br_if fallthrough incorrectly elided its call")
		}
	})
}

func TestNormalizeSourceRootBranchesDistinguishConditionalAndTerminalPaths(t *testing.T) {
	t.Run("conditional", func(t *testing.T) {
		_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func i32.const 1 br_if 0 call $yield))`)
		if !normalized.HasFunctionBranch() || !normalized.NeedsTransform() || normalized.SuspensionCount() != 1 {
			t.Fatal("conditional function branch lost fallthrough or root target")
		}
	})
	t.Run("terminal", func(t *testing.T) {
		_, normalized := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
  (func br 0 call $yield))`)
		if !normalized.HasFunctionBranch() || normalized.NeedsTransform() || normalized.SuspensionCount() != 0 {
			t.Fatal("terminal function branch retained dead yield")
		}
	})
}

func TestNormalizeSourceRejectsActiveOpaqueControlAndDoesNotMaskValidation(t *testing.T) {
	values, normalized := normalizedFixture(t, `(module (func nop))`)
	node := normalized.instructions[0]
	operation := values.operations[node]
	operation.OpaqueControl = true
	values.operations[node] = operation
	if result, err := NormalizeSource(values); err == nil || result != nil {
		t.Fatal("accepted active opaque control")
	}

	values, _ = normalizedFixture(t, `(module (func unreachable nop))`)
	dead := values.control.instructions[1]
	operation = values.operations[dead]
	operation.OpaqueControl = true
	values.operations[dead] = operation
	if result, err := NormalizeSource(values); err != nil || result == nil || result.InstructionCount() != 1 {
		t.Fatalf("dead opaque control was not safely elided: result=%v err=%v", result, err)
	}

	if values, err := valueFixture(t, `(module (func unreachable block i32.add drop end))`); err == nil || values != nil {
		t.Fatal("normalization would have hidden invalid nested source")
	}
}

// A branch-owned result write must not be mistaken for a lexical scope exit.
// These facts decide whether lowering emits a result transfer and function
// completion, so test them separately from active instruction counts.
func TestNormalizeSourceCompletionPaths(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		root, lexical, branch bool
	}{
		{"block-target", "block br 0 end", true, false, false},
		{"block-fallthrough", "block nop end", true, true, false},
		{"loop-reentry", "loop br 0 end", false, false, false},
		{"loop-exit", "loop nop end", true, true, false},
		{"outer-target", "block br 1 end", false, false, true},
		{"return", "block return end", false, false, false},
		{"trap", "block unreachable end", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values, source := normalizedFixture(t, "(module (func "+tc.body+"))")
			block := values.control.root.(*SeqNode).Children[0].(*BlockNode)
			if source.RootMayFallthrough() != tc.root || source.LexicalExit(block, NoArm) != tc.lexical || source.HasFunctionBranch() != tc.branch {
				t.Fatalf("root=%t lexical=%t branch=%t; want %t %t %t", source.RootMayFallthrough(), source.LexicalExit(block, NoArm), source.HasFunctionBranch(), tc.root, tc.lexical, tc.branch)
			}
		})
	}
}
