package ir

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestSourceBindingsAreTotalAndCanonical(t *testing.T) {
	for _, scenario := range []string{"missing-zero-param-if", "missing-instruction", "foreign-operation", "missing-scope", "foreign-scope"} {
		t.Run(scenario, func(t *testing.T) {
			plan, err := valueFixture(t, `(module (func (result i32) i32.const 1 if (result i32) i32.const 7 else i32.const 9 end))`)
			if err != nil {
				t.Fatal(err)
			}
			scope := scopeOfKind(t, plan, IfScope)
			owner := plan.scopes[scope.Label()].owner
			switch scenario {
			case "missing-zero-param-if":
				delete(plan.operations, owner)
			case "missing-instruction":
				delete(plan.operations, plan.control.instructions[0])
			case "foreign-operation":
				plan.operations[&InstrNode{}] = valueOperation{}
			case "missing-scope":
				delete(plan.scopes, scope.Label())
			case "foreign-scope":
				plan.scopes[99] = plan.scopes[scope.Label()]
			}
			if err := plan.verifyTransfers(); err == nil {
				t.Fatal("accepted incomplete or foreign source binding")
			}
		})
	}
}

// Wasm validation starts each nested control frame with its own operand-stack
// boundary. Propagating the outer frame's polymorphic state into the child would
// accept invalid instructions there. These flags cannot prove executability.
func TestNestedUnreachableUsesWasmValidationFrames(t *testing.T) {
	for _, fixture := range []struct {
		source string
		valid  bool
	}{
		{`(module (func unreachable if nop else nop end))`, true},
		{`(module (func unreachable block i32.add drop end))`, false},
	} {
		raw, err := wat.Compile(fixture.source)
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		runtime := wazero.NewRuntime(ctx)
		compiled, compileErr := runtime.CompileModule(ctx, raw)
		if compileErr == nil {
			compiled.Close(ctx)
		}
		runtime.Close(ctx)
		if (compileErr == nil) != fixture.valid {
			t.Fatalf("independent Wasm validation: valid=%t err=%v", fixture.valid, compileErr)
		}
		plan, planErr := valueFixture(t, fixture.source)
		if (planErr == nil) != fixture.valid {
			t.Fatalf("source validation disagrees: valid=%t err=%v", fixture.valid, planErr)
		}
		if fixture.valid {
			scope := scopeOfKind(t, plan, IfScope)
			facts := plan.scopes[scope.Label()]
			if facts.entryValidationReachable || !facts.exits[0].validationReachable || !facts.exits[1].validationReachable {
				t.Fatal("nested frame validation facts changed")
			}
		}
	}
}
