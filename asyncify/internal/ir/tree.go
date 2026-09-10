// Package ir provides an intermediate representation for WebAssembly
// control flow structures, enabling tree-based processing for asyncify.
package ir

import "github.com/wippyai/wasm-runtime/wasm"

// LabelID names an original control target independently of nesting depth.
// Zero is the implicit function label. Nonzero IDs are assigned while parsing.
type LabelID uint64

// Node represents a node in the instruction tree.
type Node interface {
	// IsControlFlow returns true if this node represents control flow (block, loop, if).
	IsControlFlow() bool
	// Results returns the result types produced by this node, or nil for void.
	Results() []wasm.ValType
}

// SeqNode represents a sequence of nodes (instruction list).
type SeqNode struct {
	Children []Node
}

func (n *SeqNode) IsControlFlow() bool     { return false }
func (n *SeqNode) Results() []wasm.ValType { return nil }

// BlockNode represents block or loop constructs.
type BlockNode struct {
	Body        Node
	ParamTypes  []wasm.ValType
	ResultTypes []wasm.ValType
	label       LabelID
	Imm         wasm.BlockImm
	Opcode      byte
}

func (n *BlockNode) IsControlFlow() bool     { return true }
func (n *BlockNode) Results() []wasm.ValType { return n.ResultTypes }

// IfNode represents if/else constructs.
type IfNode struct {
	Then        Node
	Else        Node
	ParamTypes  []wasm.ValType
	ResultTypes []wasm.ValType
	label       LabelID
	Imm         wasm.BlockImm
}

func (n *IfNode) IsControlFlow() bool     { return true }
func (n *IfNode) Results() []wasm.ValType { return n.ResultTypes }

// InstrNode represents a single instruction.
type InstrNode struct {
	Instr         wasm.Instruction
	branchTargets []LabelID
	callID        uint64
}

func (n *InstrNode) IsControlFlow() bool     { return false }
func (n *InstrNode) Results() []wasm.ValType { return nil }
