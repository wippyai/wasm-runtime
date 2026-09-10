package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

const portReadFixture = `(module
 (import "env" "yield" (func $yield))
 (func (result i32)
  i32.const 10 i32.const 20
  block (param i32 i32) (result i32 i32) call $yield end
  i32.add))`

func TestPortReadBindsSourcePortAndSnapshotProjection(t *testing.T) {
	values, lowered := lowerValueFixture(t, portReadFixture)
	actions, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	params, results := 0, 0
	for _, action := range actions {
		read, ok := action.ReadPort()
		if !ok {
			continue
		}
		scope, ok := values.Scope(read.Scope())
		if !ok {
			t.Fatal("missing scope")
		}
		var expected ValueID
		if read.Role() == ControlParameter {
			expected = scope.ParameterValue(read.Position())
			params++
		} else {
			expected = scope.ResultValue(read.Position())
			results++
		}
		local, assigned := lowered.PortStorage(expected)
		if read.Port() != expected || !assigned || read.Local() != local || read.Type() != wasm.ValI32 {
			t.Fatal("read lost source assignment")
		}
		instruction, primitive := action.Primitive()
		domain, hasDomain := action.Domain()
		if !primitive || !hasDomain || domain != semantics.GuestExecution || instruction.Opcode != wasm.OpLocalGet || instruction.Imm.(wasm.LocalImm).LocalIdx != local {
			t.Fatal("read lost snapshot projection")
		}
	}
	if params != 2 || results != 2 {
		t.Fatalf("param reads=%d result reads=%d", params, results)
	}
}

func TestPortReadRejectsRetargetingAndReordering(t *testing.T) {
	for _, mutation := range []string{"port", "cell", "role", "arm", "position", "primitive", "domain", "drop", "duplicate", "swap"} {
		t.Run(mutation, func(t *testing.T) {
			_, lowered := lowerValueFixture(t, portReadFixture)
			index := -1
			for i, a := range lowered.actions {
				if a.kind == SourcePortRead {
					index = i
					break
				}
			}
			if index < 0 {
				t.Fatal("missing port read")
			}
			read := &lowered.actions[index].portRead
			switch mutation {
			case "port":
				read.port++
			case "cell":
				read.local++
			case "role":
				read.role = ControlResult
			case "arm":
				read.arm = ThenArm
			case "position":
				read.position++
			case "primitive":
				lowered.actions[index].instruction = wasm.Instruction{Opcode: wasm.OpNop}
			case "domain":
				lowered.actions[index].domain = semantics.RewindRouting
			case "drop":
				lowered.actions = append(lowered.actions[:index], lowered.actions[index+1:]...)
			case "duplicate":
				lowered.actions = append(lowered.actions, lowered.actions[index])
			case "swap":
				lowered.actions[index], lowered.actions[index+1] = lowered.actions[index+1], lowered.actions[index]
			}
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("accepted corrupted port read")
			}
		})
	}
}
