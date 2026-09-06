package parser

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

// typeUse is a WAT type use: an optional type index plus optional inline
// param/result declarations. When both are present the inline signature must
// be syntactically equal to the referenced function type.
type typeUse struct {
	typeIdx    *uint32
	params     []ast.ValType
	results    []ast.ValType
	paramNames []string
	hasInline  bool
}

func (p *Parser) parseTypeUseClauses() (typeUse, error) {
	var tu typeUse
	for {
		tok := p.peek()
		if tok == nil || tok.Type != token.LParen {
			return tu, nil
		}
		saved := p.pos
		p.next()
		identTok := p.peek()
		if identTok == nil || identTok.Type != token.Ident {
			p.pos = saved
			return tu, nil
		}
		switch identTok.Value {
		case "type":
			p.next()
			if err := p.parseTypeUseType(&tu); err != nil {
				return tu, err
			}
		case "param":
			p.next()
			if err := p.parseTypeUseParams(&tu); err != nil {
				return tu, err
			}
		case "result":
			p.next()
			if err := p.parseTypeUseResults(&tu); err != nil {
				return tu, err
			}
		default:
			p.pos = saved
			return tu, nil
		}
	}
}

func (p *Parser) parseTypeUseType(tu *typeUse) error {
	if tu.typeIdx != nil {
		return fmt.Errorf("multiple type uses")
	}
	idx, err := p.parseIdx(p.typeMap)
	if err != nil {
		return err
	}
	if _, err := p.expect(token.RParen); err != nil {
		return err
	}
	tu.typeIdx = new(uint32)
	*tu.typeIdx = idx
	return nil
}

func (p *Parser) parseTypeUseParams(tu *typeUse) error {
	tu.hasInline = true
	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen {
			p.next()
			return nil
		}
		if t.Type == token.Ident && len(t.Value) > 0 && t.Value[0] == '$' {
			name := t.Value
			p.next()
			vt, err := p.parseValType()
			if err != nil {
				return err
			}
			tu.params = append(tu.params, vt)
			tu.paramNames = append(tu.paramNames, name)
			continue
		}
		vt, err := p.parseValType()
		if err != nil {
			return err
		}
		tu.params = append(tu.params, vt)
		tu.paramNames = append(tu.paramNames, "")
	}
}

func (p *Parser) parseTypeUseResults(tu *typeUse) error {
	tu.hasInline = true
	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen {
			p.next()
			return nil
		}
		vt, err := p.parseValType()
		if err != nil {
			return err
		}
		tu.results = append(tu.results, vt)
	}
}

func (p *Parser) resolveTypeUse(tu typeUse, localMap map[string]uint32) (uint32, ast.FuncType, error) {
	bindNames := func() error {
		seen := make(map[string]struct{})
		for i, name := range tu.paramNames {
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				return fmt.Errorf("duplicate local identifier: %s", name)
			}
			seen[name] = struct{}{}
			if localMap != nil {
				localMap[name] = uint32(i)
			}
		}
		return nil
	}

	inline := ast.FuncType{Params: tu.params, Results: tu.results}
	if tu.typeIdx != nil {
		idx := *tu.typeIdx
		if uint64(idx) >= uint64(len(p.mod.Types)) {
			return 0, ast.FuncType{}, fmt.Errorf("type index %d out of range", idx)
		}
		ft := p.mod.Types[idx]
		if tu.hasInline && !ft.Equal(inline) {
			return 0, ast.FuncType{}, fmt.Errorf("inline signature does not match type %d", idx)
		}
		if tu.hasInline {
			if err := bindNames(); err != nil {
				return 0, ast.FuncType{}, err
			}
		}
		return idx, ft, nil
	}
	idx := p.findOrAddType(inline)
	if err := bindNames(); err != nil {
		return 0, ast.FuncType{}, err
	}
	return idx, inline, nil
}
