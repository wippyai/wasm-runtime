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

	tu, parseErr := p.parseTypeUseClauses()
	if parseErr != nil {
		return 0, 0, parseErr
	}
	typeIdx, _, parseErr = p.resolveTypeUse(tu, nil)
	if parseErr != nil {
		return 0, 0, parseErr
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
