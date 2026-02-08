package parser

import (
	"fmt"
	"strings"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func (p *Parser) parseRefNull() (ast.Instr, error) {
	t, err := p.expect(token.Ident)
	if err != nil {
		return ast.Instr{}, err
	}
	var heapType byte
	switch t.Value {
	case "func", "funcref":
		heapType = ast.RefTypeFuncref
	case "extern", "externref":
		heapType = ast.RefTypeExternref
	default:
		return ast.Instr{}, fmt.Errorf("unknown heap type: %s", t.Value)
	}
	return ast.Instr{Opcode: ast.OpRefNull, Imm: heapType}, nil
}

func (p *Parser) parseRefFunc() (ast.Instr, error) {
	idx, err := p.parseIdx(p.funcMap)
	if err != nil {
		return ast.Instr{}, err
	}
	return ast.Instr{Opcode: ast.OpRefFunc, Imm: idx}, nil
}

func (p *Parser) parseSelectTypes() ([]ast.ValType, error) {
	var types []ast.ValType
	for {
		t := p.peek()
		if t == nil || t.Type != token.LParen {
			break
		}
		saved := p.pos
		p.next()
		nt := p.peek()
		if nt == nil || nt.Type != token.Ident || nt.Value != "result" {
			p.pos = saved
			break
		}
		p.next()
		for {
			rt := p.peek()
			if rt == nil || rt.Type == token.RParen {
				p.next()
				break
			}
			vt, err := p.parseValType()
			if err != nil {
				return nil, err
			}
			types = append(types, vt)
		}
	}
	return types, nil
}

func (p *Parser) parseTableIdx() uint32 {
	t := p.peek()
	if t == nil {
		return 0
	}
	if t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$")) {
		idx, err := p.parseIdx(p.tableMap)
		if err == nil {
			return idx
		}
	}
	return 0
}

func (p *Parser) parseBrTableLabels() ([]uint32, error) {
	var labels []uint32
	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen || t.Type == token.LParen {
			break
		}
		if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
			labelName := t.Value
			p.next()
			depth, ok := p.resolveLabel(labelName)
			if !ok {
				return nil, fmt.Errorf("unknown label: %s", labelName)
			}
			labels = append(labels, depth)
		} else if t.Type == token.Number {
			idx, err := p.parseU32()
			if err != nil {
				return nil, err
			}
			labels = append(labels, idx)
		} else {
			break
		}
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("br_table requires at least one label")
	}
	return labels, nil
}

func (p *Parser) parseCallIndirectArgs() (tableIdx, typeIdx uint32, err error) {
	var inlineParams []ast.ValType
	var inlineResults []ast.ValType
	hasType := false

	// Try to parse table index
	if t := p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$"))) {
		saved := p.pos
		idx, parseErr := p.parseIdx(p.tableMap)
		if parseErr == nil {
			t2 := p.peek()
			if t2 != nil && t2.Type == token.LParen {
				tableIdx = idx
			} else {
				p.pos = saved
			}
		} else {
			p.pos = saved
		}
	}

	// Parse type, param, result
parseLoop:
	for {
		tok := p.peek()
		if tok == nil || tok.Type != token.LParen {
			break
		}
		saved := p.pos
		p.next()
		identTok := p.peek()
		if identTok == nil || identTok.Type != token.Ident {
			p.pos = saved
			break
		}
		p.next()

		switch identTok.Value {
		case "type":
			idx, parseErr := p.parseIdx(p.typeMap)
			if parseErr != nil {
				return 0, 0, parseErr
			}
			typeIdx = idx
			hasType = true
			if _, parseErr := p.expect(token.RParen); parseErr != nil {
				return 0, 0, parseErr
			}
		case "param":
			for {
				pt := p.peek()
				if pt == nil || pt.Type == token.RParen {
					break
				}
				vt, parseErr := p.parseValType()
				if parseErr != nil {
					return 0, 0, parseErr
				}
				inlineParams = append(inlineParams, vt)
			}
			if _, parseErr := p.expect(token.RParen); parseErr != nil {
				return 0, 0, parseErr
			}
		case "result":
			for {
				rt := p.peek()
				if rt == nil || rt.Type == token.RParen {
					break
				}
				vt, parseErr := p.parseValType()
				if parseErr != nil {
					return 0, 0, parseErr
				}
				inlineResults = append(inlineResults, vt)
			}
			if _, parseErr := p.expect(token.RParen); parseErr != nil {
				return 0, 0, parseErr
			}
		default:
			p.pos = saved
			break parseLoop
		}
	}
	if !hasType && (len(inlineParams) > 0 || len(inlineResults) > 0) {
		ft := ast.FuncType{Params: inlineParams, Results: inlineResults}
		for i, t := range p.mod.Types {
			if t.Equal(ft) {
				typeIdx = uint32(i)
				hasType = true
				break
			}
		}
		if !hasType {
			typeIdx = uint32(len(p.mod.Types))
			p.mod.Types = append(p.mod.Types, ft)
		}
	}
	return tableIdx, typeIdx, nil
}

func (p *Parser) parseLabel() string {
	t := p.peek()
	if t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		p.next()
		return t.Value
	}
	return ""
}
