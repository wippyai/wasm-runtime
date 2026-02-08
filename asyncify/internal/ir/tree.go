// Package ir provides an intermediate representation for WebAssembly
// control flow structures, enabling tree-based processing for asyncify.
package ir

import "github.com/wippyai/wasm-runtime/wasm"

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
	Imm         wasm.BlockImm
}

func (n *IfNode) IsControlFlow() bool     { return true }
func (n *IfNode) Results() []wasm.ValType { return n.ResultTypes }

// InstrNode represents a single instruction.
type InstrNode struct {
	Instr wasm.Instruction
}

func (n *InstrNode) IsControlFlow() bool     { return false }
func (n *InstrNode) Results() []wasm.ValType { return nil }

// Parse converts a linear instruction stream into a tree representation.
// If module is provided, type indices in block types will be resolved.
func Parse(instrs []wasm.Instruction, module ...*wasm.Module) Node {
	var m *wasm.Module
	if len(module) > 0 {
		m = module[0]
	}
	p := &parser{instrs: instrs, pos: 0, module: m}
	return p.parseSeq()
}

type parser struct {
	module *wasm.Module
	instrs []wasm.Instruction
	pos    int
}

func (p *parser) parseSeq() Node {
	var children []Node

	for p.pos < len(p.instrs) {
		instr := p.instrs[p.pos]

		switch instr.Opcode {
		case wasm.OpEnd:
			p.pos++
			return &SeqNode{Children: children}

		case wasm.OpElse:
			// Return without consuming - caller handles else
			return &SeqNode{Children: children}

		case wasm.OpBlock, wasm.OpLoop:
			children = append(children, p.parseBlock())

		case wasm.OpIf:
			children = append(children, p.parseIf())

		default:
			children = append(children, &InstrNode{Instr: instr})
			p.pos++
		}
	}

	return &SeqNode{Children: children}
}

func (p *parser) parseBlock() Node {
	instr := p.instrs[p.pos]
	imm := instr.Imm.(wasm.BlockImm)
	p.pos++

	body := p.parseSeq()

	params, results := blockTypeToParamsAndResults(imm.Type, p.module)
	return &BlockNode{
		Opcode:      instr.Opcode,
		ParamTypes:  params,
		ResultTypes: results,
		Body:        body,
		Imm:         imm,
	}
}

func (p *parser) parseIf() Node {
	instr := p.instrs[p.pos]
	imm := instr.Imm.(wasm.BlockImm)
	p.pos++

	// Parse then branch
	thenBranch := p.parseSeq()

	var elseBranch Node
	// Check if we hit else
	if p.pos < len(p.instrs) && p.instrs[p.pos].Opcode == wasm.OpElse {
		p.pos++ // consume else
		elseBranch = p.parseSeq()
	}

	params, results := blockTypeToParamsAndResults(imm.Type, p.module)
	return &IfNode{
		ParamTypes:  params,
		ResultTypes: results,
		Then:        thenBranch,
		Else:        elseBranch,
		Imm:         imm,
	}
}

// blockTypeToParamsAndResults converts a block type to param and result types.
func blockTypeToParamsAndResults(blockType int32, module *wasm.Module) (params, results []wasm.ValType) {
	switch blockType {
	case -1: // i32
		return nil, []wasm.ValType{wasm.ValI32}
	case -2: // i64
		return nil, []wasm.ValType{wasm.ValI64}
	case -3: // f32
		return nil, []wasm.ValType{wasm.ValF32}
	case -4: // f64
		return nil, []wasm.ValType{wasm.ValF64}
	case -5: // v128
		return nil, []wasm.ValType{wasm.ValV128}
	case -16: // funcref
		return nil, []wasm.ValType{wasm.ValFuncRef}
	case -17: // externref
		return nil, []wasm.ValType{wasm.ValExtern}
	case -64: // void
		return nil, nil
	default:
		// Type index - look up in module types
		if blockType >= 0 && module != nil && int(blockType) < len(module.Types) {
			ft := &module.Types[blockType]
			return ft.Params, ft.Results
		}
		return nil, nil
	}
}

// blockTypeToResults converts a block type to result types (for backward compat).
func blockTypeToResults(blockType int32, module *wasm.Module) []wasm.ValType {
	_, results := blockTypeToParamsAndResults(blockType, module)
	return results
}
