package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// ValueID denotes an immutable source operand, independently of later storage.
// Zero is the polymorphic unknown permitted at an unreachable frame's base.
type ValueID uint64

// OperandShape describes one non-control instruction's value flow. Inputs are
// ordered bottom to top. If OpaqueInputs is true, only input count is certified;
// the graph retains actual operand types but does not claim input validation.
// Such instructions are barriers for future rewrites needing stronger effects.
type OperandShape struct {
	Inputs       []wasm.ValType
	Results      []wasm.ValType
	PopCount     int
	OpaqueInputs bool
	// OpaqueControl means the operation has control transfers not represented
	// by this plan's edges. PlanValues retains it as a source-analysis barrier;
	// production Linearize rejects it until typed transfer actions exist.
	OpaqueControl bool
	AliasInput    bool
}

type OperandResolver func(wasm.Instruction) (OperandShape, error)

type ValueKind uint8

const (
	InstructionResult ValueKind = iota
	ControlParameter
	ControlResult
)

type sourceValue struct {
	Owner    Node
	Position int
	Type     wasm.ValType
	Port     ValueKind
	// ValidationOnly identifies a typed continuation synthesized from a
	// polymorphic operand, not an executed source definition.
	ValidationOnly bool
}

type valueOperation struct {
	// controlPrefix retains the source validation stack below the active frame.
	// A runtime return exits the function; validation only truncates that frame.
	controlPrefix []ValueID
	Inputs        []ValueID
	Outputs       []ValueID
	branchTargets []LabelID
	// inputStackOperands is the number of values actually present in the
	// validation stack suffix before this operation consumed Inputs. Inputs
	// preceding that suffix are polymorphic pops at an unreachable frame base.
	// A present value may still have ValueID zero, so presence is deliberately
	// separate from identity.
	inputStackOperands  int
	Selector            ValueID
	ValidationReachable bool
	HasSelector         bool
	OpaqueInputs        bool
	OpaqueControl       bool
	control             sourceControlEffect
}

// sourceControlEffect is the validated structured execution effect of a
// source operation. It deliberately records control semantics during value
// planning so later phases never rediscover them through opcode heuristics.
type sourceControlEffect uint8

const (
	controlFallsThrough sourceControlEffect = iota
	controlTraps
	controlConditionalBranch
	controlBranch
	controlReturn
)

type EdgeKind uint8

const (
	ControlEntry EdgeKind = iota
	ControlExit
	ControlBranch
)

type valueEdge struct {
	Source        Node
	From          []ValueID
	To            []ValueID
	Target        LabelID
	Kind          EdgeKind
	Port          ValueKind
	Mode          TransferMode
	Arm           ControlArm
	TargetOrdinal int
}

type valueFrame struct {
	params      []ValueID
	results     []ValueID
	label       LabelID
	base        int
	loop        bool
	unreachable bool
}

// ValuePlan owns source operand definitions, control ports and edges, and the
// values retained at each suspension. It allocates no Wasm locals and knows
// nothing about rewind routing. Its graph is private to the source phase.
type ValuePlan struct {
	control       *Analysis
	scopes        map[LabelID]valueScope
	transfers     map[Node][]int
	literals      map[ValueID]semantics.Literal
	operations    map[Node]valueOperation
	continuations map[uint64][]ValueID
	definitions   []sourceValue
	edges         []valueEdge
	opaqueControl bool
}

type valueBuilder struct {
	plan    *ValuePlan
	resolve OperandResolver
	stack   []ValueID
	frames  []*valueFrame
}

// inputStackOperandCount records the present suffix before a known number of
// pops. It is intentionally based on stack depth, never ValueID: zero is both
// the polymorphic unknown and a valid present stack entry produced after an
// unreachable instruction.
func (b *valueBuilder) inputStackOperandCount(width int) int {
	if width <= 0 {
		return 0
	}
	f := b.frames[len(b.frames)-1]
	available := len(b.stack) - f.base
	if available <= 0 {
		return 0
	}
	if available < width {
		return available
	}
	return width
}

func (b *valueBuilder) setEntryStackOperands(label LabelID, count int) {
	scope := b.plan.scopes[label]
	scope.entryStackOperands = count
	b.plan.scopes[label] = scope
}

// PlanValues applies Wasm's control-frame stack rules to the owned source tree.
// Known input signatures are checked; opaque signatures retain dependencies
// without claiming complete validation. The caller must provide every ordinary
// operation's shape. No partial plan is published on failure.
func PlanValues(control *Analysis, resolve OperandResolver) (*ValuePlan, error) {
	if control == nil || resolve == nil {
		return nil, fmt.Errorf("asyncify: missing source value planning inputs")
	}
	plan := &ValuePlan{control: control, scopes: make(map[LabelID]valueScope), transfers: make(map[Node][]int), literals: make(map[ValueID]semantics.Literal), operations: make(map[Node]valueOperation), continuations: make(map[uint64][]ValueID)}
	b := &valueBuilder{plan: plan, resolve: resolve}
	root := b.frame(control.root, 0, nil, control.functionResults, false)
	b.setEntryStackOperands(root.label, 0)
	b.frames = append(b.frames, root)
	if err := b.visit(control.root); err != nil {
		return nil, err
	}
	if err := b.finish(control.root, root, NoArm); err != nil {
		return nil, fmt.Errorf("asyncify: source function results: %w", err)
	}
	if err := plan.verifyTransfers(); err != nil {
		return nil, err
	}
	return plan, nil
}
