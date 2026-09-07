package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// PortReadOperation identifies the source port reintroduced on the guest
// operand stack. Local is only its assigned carrier, never its source identity.
// Its one-instruction projection uses the existing local snapshot machinery.
// This binds ownership; it does not prove physical carrier initialization on
// every generated path. Lexically emitted dead result reads remain possible.
type PortReadOperation struct {
	owner     Node
	port      ValueID
	scope     LabelID
	position  int
	local     uint32
	role      ValueKind
	arm       ControlArm
	valueType wasm.ValType
}

func (r PortReadOperation) Port() ValueID      { return r.port }
func (r PortReadOperation) Local() uint32      { return r.local }
func (r PortReadOperation) Type() wasm.ValType { return r.valueType }
func (r PortReadOperation) Scope() LabelID     { return r.scope }
func (r PortReadOperation) Role() ValueKind    { return r.role }
func (r PortReadOperation) Arm() ControlArm    { return r.arm }
func (r PortReadOperation) Position() int      { return r.position }
func (r PortReadOperation) instruction() wasm.Instruction {
	return wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: r.local}}
}
func verifyPortReadShape(r PortReadOperation) error {
	if r.owner == nil || r.port == 0 || r.position < 0 || (r.role != ControlParameter && r.role != ControlResult) {
		return fmt.Errorf("asyncify: invalid source port read")
	}
	if r.valueType == 0 || r.arm > ElseArm || (r.role == ControlResult && r.arm != NoArm) {
		return fmt.Errorf("asyncify: invalid source port read type or arm")
	}
	return nil
}

func (l *linearizer) newPortRead(label LabelID, role ValueKind, arm ControlArm, position int, local uint32) (PortReadOperation, error) {
	if l.values == nil {
		return PortReadOperation{}, fmt.Errorf("asyncify: port read requires source values")
	}
	scope, ok := l.values.scopes[label]
	if !ok {
		return PortReadOperation{}, fmt.Errorf("asyncify: port read has no source scope")
	}
	ports := scope.results
	if role == ControlParameter {
		ports = scope.params
	}
	if position < 0 || position >= len(ports) {
		return PortReadOperation{}, fmt.Errorf("asyncify: port read index outside source scope")
	}
	info, ok := l.values.ValueInfo(ports[position])
	assigned, exists := l.storage.portLocals[ports[position]]
	if !ok || !exists || assigned != local {
		return PortReadOperation{}, fmt.Errorf("asyncify: port read has no matching source storage")
	}
	read := PortReadOperation{owner: scope.owner, port: ports[position], scope: label, local: local, position: position, role: role, arm: arm, valueType: info.Type}
	if err := verifyPortReadShape(read); err != nil {
		return PortReadOperation{}, err
	}
	return read, nil
}

func (l *LoweredControl) verifyPortRead(read PortReadOperation, owner Node) error {
	if l.values == nil || read.owner != owner {
		return fmt.Errorf("asyncify: port read owner mismatch")
	}
	scope, ok := l.values.scopes[read.scope]
	if !ok || scope.owner != owner {
		return fmt.Errorf("asyncify: port read source scope mismatch")
	}
	if read.role == ControlParameter {
		if scope.kind == IfScope && read.arm != ThenArm && read.arm != ElseArm {
			return fmt.Errorf("asyncify: if parameter read has no source arm")
		}
		if scope.kind != IfScope && read.arm != NoArm {
			return fmt.Errorf("asyncify: non-if parameter read has an arm")
		}
	}
	ports := scope.results
	if read.role == ControlParameter {
		ports = scope.params
	}
	if read.position < 0 || read.position >= len(ports) || ports[read.position] != read.port {
		return fmt.Errorf("asyncify: port read source identity mismatch")
	}
	info, defined := l.values.ValueInfo(read.port)
	local, assigned := l.portLocals[read.port]
	if !defined || !assigned || local != read.local || info.Type != read.valueType || info.Kind != read.role {
		return fmt.Errorf("asyncify: port read source assignment mismatch")
	}
	return nil
}
