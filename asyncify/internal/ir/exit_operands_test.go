package ir

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestExitOperandsPreserveValidationStackPresence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		count      int
		unknown    bool
	}{
		{"reachable", "i32.const 1 i32.const 2", 2, false},
		{"absent", "unreachable", 0, true},
		{"absent_after_nop", "unreachable nop", 0, true},
		{"partial", "unreachable i32.const 2", 1, false},
		{"present_unknown", "unreachable select", 1, true},
		{"repeated_select", "unreachable select select", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `(module (func block (result i32 i32) ` + tc.body + ` end drop drop))`
			plan, err := valueFixture(t, source)
			if err != nil {
				t.Fatal(err)
			}
			scope := scopeOfKind(t, plan, BlockScope)
			exit, ok := scope.Exit(0)
			if !ok || scope.ExitCount() != 1 || exit.Arm() != NoArm || exit.ValueCount() != 2 || exit.StackOperandCount() != tc.count {
				t.Fatalf("incorrect exit operands: %+v", plan.scopes[scope.label].exits)
			}
			if (exit.Value(1) == 0) != tc.unknown {
				t.Fatal("unknown identity disagrees with fixture")
			}
			if _, ok := scope.Exit(-1); ok {
				t.Fatal("negative exit index accepted")
			}
			if _, ok := scope.Exit(1); ok {
				t.Fatal("out-of-bounds exit accepted")
			}
			validateExitLowering(t, source, plan, tc.count)
		})
	}
}

// Wazero validates both source and raw-lowered bodies. Counting transfer
// operands exercises the complete validation-plan consumer of exit facts
// without pretending a present polymorphic value has a concrete local store.
// Runtime lowering instead consumes the normalized executable view.
func validateExitLowering(t *testing.T, source string, plan *ValuePlan, present int) {
	t.Helper()
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)
	raw, err := wat.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.CompileModule(ctx, raw); err != nil {
		t.Fatalf("source invalid: %v", err)
	}
	module, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	var next uint32
	lowered, err := Linearize(plan, &LinearizeConfig{AllocLocal: func(vt wasm.ValType) uint32 {
		index := next
		next++
		module.Code[0].Locals = append(module.Code[0].Locals, wasm.LocalEntry{Count: 1, ValType: vt})
		return index
	}})
	if err != nil {
		t.Fatal(err)
	}
	instructions, err := lowered.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	actual := 0
	for _, action := range lowered.actions {
		transfer, ok := action.Transfer()
		if !ok {
			continue
		}
		for index := 0; index < transfer.OperandCount(); index++ {
			operand, ok := transfer.Operand(index)
			if !ok {
				t.Fatal("transfer operand disappeared")
			}
			if operand.Present() {
				actual++
			}
		}
	}
	if actual != present {
		t.Fatalf("emitted %d present transfer operands, want %d", actual, present)
	}
	module.Code[0].Code = wasm.EncodeInstructions(append(instructions, wasm.Instruction{Opcode: wasm.OpEnd}))
	if _, err := runtime.CompileModule(ctx, module.Encode()); err != nil {
		t.Fatalf("lowered invalid: %v", err)
	}
}

func TestExitOperandsRejectCorruptAbsentPrefix(t *testing.T) {
	plan, err := valueFixture(t, `(module (func block (result i32 i32) unreachable i32.const 2 end drop drop))`)
	if err != nil {
		t.Fatal(err)
	}
	scope := scopeOfKind(t, plan, BlockScope)
	facts := plan.scopes[scope.label]
	facts.exits[0].stackOperands = 0
	if err := plan.verifyTransfers(); err == nil {
		t.Fatal("accepted concrete source value in absent prefix")
	}
}

func TestExitOperandsLoweringAcrossScopes(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		stores       int
	}{
		{"mixed_partial", `(module (func block (result i32 i64) unreachable i64.const 2 end drop drop))`, 1},
		{"nested_frame", `(module (func block (result i32) unreachable block (result i32) i32.const 7 end end drop))`, 2},
		{"if_arms", `(module (func i32.const 1 if (result i32 i32) unreachable select else unreachable i32.const 2 end drop drop))`, 2},
		// The branch's value is handled by its existing branch edge. The
		// materialized root completion exit is absent, so its scope transfer
		// deliberately has no present operand to overwrite that port.
		{"root_branch", `(module (func (result i32) i32.const 7 br 0 nop))`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := valueFixture(t, tc.source)
			if err != nil {
				t.Fatal(err)
			}
			validateExitLowering(t, tc.source, plan, tc.stores)
		})
	}
}
