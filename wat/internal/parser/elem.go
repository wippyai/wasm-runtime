package parser

import (
	"fmt"
	"strings"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func (p *Parser) parseOffsetExpr() ([]ast.Instr, error) {
	offset, err := p.parseInstrs(nil)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(token.RParen); err != nil {
		return nil, err
	}
	return append(offset, ast.Instr{Opcode: ast.OpEnd}), nil
}

func (p *Parser) parseElemExpr(instrName string) ([]ast.Instr, uint32, bool, error) {
	switch instrName {
	case "ref.func":
		idx, err := p.parseIdx(p.funcMap)
		if err != nil {
			return nil, 0, false, err
		}
		return []ast.Instr{{Opcode: ast.OpRefFunc, Imm: idx}, {Opcode: ast.OpEnd}}, idx, true, nil
	case "ref.null":
		nullType, err := p.expect(token.Ident)
		if err != nil {
			return nil, 0, false, err
		}
		var heapType = ast.RefTypeFuncref
		if nullType.Value == "extern" || nullType.Value == "externref" {
			heapType = ast.RefTypeExternref
		}
		return []ast.Instr{{Opcode: ast.OpRefNull, Imm: heapType}, {Opcode: ast.OpEnd}}, 0, false, nil
	default:
		return nil, 0, false, fmt.Errorf("unexpected element expression: %s", instrName)
	}
}

func (p *Parser) parseFuncRefs(elem *ast.Elem) error {
	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen {
			break
		}
		if t.Type == token.LParen {
			p.next()
			instrTok := p.peek()
			if instrTok != nil && instrTok.Type == token.Ident {
				if instrTok.Value == "item" {
					p.next()
					if inner := p.peek(); inner != nil && inner.Type == token.LParen {
						p.next()
						itemInstr, err := p.expect(token.Ident)
						if err != nil {
							return err
						}
						expr, idx, hasIdx, err := p.parseElemExpr(itemInstr.Value)
						if err != nil {
							return err
						}
						if hasIdx {
							elem.Init = append(elem.Init, idx)
						}
						elem.Exprs = append(elem.Exprs, expr)
						if _, err := p.expect(token.RParen); err != nil {
							return err
						}
					}
					if _, err := p.expect(token.RParen); err != nil {
						return err
					}
					continue
				}
				p.next()
				expr, idx, hasIdx, err := p.parseElemExpr(instrTok.Value)
				if err != nil {
					return err
				}
				if hasIdx {
					elem.Init = append(elem.Init, idx)
				}
				elem.Exprs = append(elem.Exprs, expr)
				if _, err := p.expect(token.RParen); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("expected function reference or expression, got %v", instrTok)
		}
		idx, err := p.parseIdx(p.funcMap)
		if err != nil {
			return err
		}
		elem.Init = append(elem.Init, idx)
	}
	return nil
}

func (p *Parser) parseRefTypeItems(elem *ast.Elem) error {
	refType := p.next()
	if refType.Value == "externref" {
		elem.RefType = ast.RefTypeExternref
	} else {
		elem.RefType = ast.RefTypeFuncref
	}

	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen || t.Type != token.LParen {
			break
		}
		p.next()
		instrTok, err := p.expect(token.Ident)
		if err != nil {
			return err
		}
		expr, idx, hasIdx, err := p.parseElemExpr(instrTok.Value)
		if err != nil {
			return err
		}
		if hasIdx {
			elem.Init = append(elem.Init, idx)
		}
		elem.Exprs = append(elem.Exprs, expr)
		if _, err := p.expect(token.RParen); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) parseElem() error {
	elem := ast.Elem{}
	elemIdx := uint32(len(p.mod.Elems))
	t := p.peek()

	// Optional element name
	if t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		saved := p.pos
		name := t.Value
		p.next()
		t2 := p.peek()
		if t2 != nil && (t2.Type == token.Ident && (t2.Value == "func" || t2.Value == "funcref" || t2.Value == "externref" || t2.Value == "declare") || t2.Type == token.RParen) {
			p.elemMap[name] = elemIdx
		} else {
			p.pos = saved
		}
		t = p.peek()
	}

	// Check for mode keywords
	if t != nil && t.Type == token.Ident {
		switch t.Value {
		case "declare":
			p.next()
			elem.Mode = ast.ElemModeDeclarative
		case "func":
			p.next()
			elem.Mode = ast.ElemModePassive
			if err := p.parseFuncRefs(&elem); err != nil {
				return err
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
			p.mod.Elems = append(p.mod.Elems, elem)
			return nil
		case "funcref", "externref":
			elem.Mode = ast.ElemModePassive
			if err := p.parseRefTypeItems(&elem); err != nil {
				return err
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
			p.mod.Elems = append(p.mod.Elems, elem)
			return nil
		}
	}

	// Check for table index before offset
	if t = p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$") && t.Value != "declare")) {
		if elem.Mode == ast.ElemModeActive {
			saved := p.pos
			idx, err := p.parseIdx(p.tableMap)
			if err == nil {
				next := p.peek()
				if next != nil && next.Type == token.LParen {
					elem.TableIdx = idx
					elem.Mode = ast.ElemModeActiveTable
				} else {
					p.pos = saved
				}
			}
		}
	}

	// Parse (table ...) or (offset ...) or bare offset expression
	if tok := p.peek(); tok != nil && tok.Type == token.LParen {
		saved := p.pos
		p.next()
		tok2 := p.peek()
		if tok2 != nil && tok2.Type == token.Ident {
			switch tok2.Value {
			case "table":
				p.next()
				idx, err := p.parseIdx(p.tableMap)
				if err != nil {
					return err
				}
				elem.TableIdx = idx
				if _, err := p.expect(token.RParen); err != nil {
					return err
				}
				if nextTok := p.peek(); nextTok != nil && nextTok.Type == token.LParen {
					p.next()
					if nextTok2 := p.peek(); nextTok2 != nil && nextTok2.Type == token.Ident && nextTok2.Value == "offset" {
						p.next()
					}
					offset, err := p.parseOffsetExpr()
					if err != nil {
						return err
					}
					elem.Offset = offset
				}
			case "offset":
				p.next()
				offset, err := p.parseOffsetExpr()
				if err != nil {
					return err
				}
				elem.Offset = offset
			default:
				p.pos = saved
				p.next()
				offset, err := p.parseOffsetExpr()
				if err != nil {
					return err
				}
				elem.Offset = offset
			}
		} else {
			p.pos = saved
		}
	}

	// Check for reftype or func keyword after offset
	t = p.peek()
	if t != nil && t.Type == token.Ident {
		switch t.Value {
		case "func":
			p.next()
			if err := p.parseFuncRefs(&elem); err != nil {
				return err
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
			p.mod.Elems = append(p.mod.Elems, elem)
			return nil
		case "funcref", "externref":
			if err := p.parseRefTypeItems(&elem); err != nil {
				return err
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
			p.mod.Elems = append(p.mod.Elems, elem)
			return nil
		}
	}

	// Default: parse function references
	if err := p.parseFuncRefs(&elem); err != nil {
		return err
	}
	if _, err := p.expect(token.RParen); err != nil {
		return err
	}
	p.mod.Elems = append(p.mod.Elems, elem)
	return nil
}

func (p *Parser) parseData() error {
	dataIdx := uint32(len(p.mod.Data))
	t := p.peek()

	if t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		p.dataMap[t.Value] = dataIdx
		p.next()
		t = p.peek()
	}

	if t != nil && t.Type == token.String {
		var data []byte
		for {
			strTok := p.peek()
			if strTok == nil || strTok.Type != token.String {
				break
			}
			p.next()
			data = append(data, DecodeStringLiteral(strTok.Value)...)
		}
		if _, err := p.expect(token.RParen); err != nil {
			return err
		}
		p.mod.Data = append(p.mod.Data, ast.DataSegment{
			Passive: true,
			Init:    data,
		})
		return nil
	}

	if _, err := p.expect(token.LParen); err != nil {
		return err
	}

	var memIdx uint32

	t = p.peek()
	if t != nil && t.Type == token.Ident && t.Value == "memory" {
		p.next()
		idx, err := p.parseIdx(p.memMap)
		if err != nil {
			return err
		}
		memIdx = idx
		if _, err := p.expect(token.RParen); err != nil {
			return err
		}
		if _, err := p.expect(token.LParen); err != nil {
			return err
		}
	}

	t = p.peek()
	if t != nil && t.Type == token.Ident && t.Value == "offset" {
		p.next()
	}

	offset, err := p.parseInstrs(nil)
	if err != nil {
		return err
	}
	if _, err := p.expect(token.RParen); err != nil {
		return err
	}
	offset = append(offset, ast.Instr{Opcode: ast.OpEnd})

	var data []byte
	for {
		t := p.peek()
		if t == nil || t.Type != token.String {
			break
		}
		p.next()
		data = append(data, DecodeStringLiteral(t.Value)...)
	}

	if _, err := p.expect(token.RParen); err != nil {
		return err
	}

	p.mod.Data = append(p.mod.Data, ast.DataSegment{
		MemIdx: memIdx,
		Offset: offset,
		Init:   data,
	})
	return nil
}
