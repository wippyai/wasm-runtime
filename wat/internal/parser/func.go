package parser

import (
	"fmt"
	"strings"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func (p *Parser) parseFunc(funcIdx *uint32) error {
	var name string
	var exports []string
	localMap := make(map[string]uint32)
	var localIdx uint32

	ft := ast.FuncType{}
	body := ast.FuncBody{}

	for {
		t := p.peek()
		if t == nil {
			return fmt.Errorf("unexpected end in func")
		}
		if t.Type == token.RParen {
			p.next()
			break
		}

		if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
			name = t.Value
			p.next()
			continue
		}

		if t.Type != token.LParen {
			instrs, err := p.parseInstrs(localMap)
			if err != nil {
				return err
			}
			body.Code = append(body.Code, instrs...)
			continue
		}

		p.next()
		t, err := p.expect(token.Ident)
		if err != nil {
			return err
		}

		switch t.Value {
		case "import":
			modName, err := p.expect(token.String)
			if err != nil {
				return err
			}
			importName, err := p.expect(token.String)
			if err != nil {
				return err
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
			for {
				t := p.peek()
				if t == nil || t.Type == token.RParen {
					p.next()
					break
				}
				if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
					if name == "" {
						name = t.Value
					}
					p.next()
					continue
				}
				if t.Type != token.LParen {
					break
				}
				p.next()
				clause, err := p.expect(token.Ident)
				if err != nil {
					return err
				}
				switch clause.Value {
				case "param":
					for {
						pt := p.peek()
						if pt == nil || pt.Type == token.RParen {
							p.next()
							break
						}
						if pt.Type == token.Ident && strings.HasPrefix(pt.Value, "$") {
							p.next()
							continue
						}
						vt, err := p.parseValType()
						if err != nil {
							return err
						}
						ft.Params = append(ft.Params, vt)
					}
				case "result":
					for {
						pt := p.peek()
						if pt == nil || pt.Type == token.RParen {
							p.next()
							break
						}
						vt, err := p.parseValType()
						if err != nil {
							return err
						}
						ft.Results = append(ft.Results, vt)
					}
				case "type":
					idx, err := p.parseIdx(p.typeMap)
					if err != nil {
						return err
					}
					if idx < uint32(len(p.mod.Types)) {
						ft = p.mod.Types[idx]
					}
					if _, err := p.expect(token.RParen); err != nil {
						return err
					}
				default:
					return fmt.Errorf("unexpected clause in import func: %s", clause.Value)
				}
			}
			typeIdx := p.findOrAddType(ft)
			imp := ast.Import{
				Module: modName.Value,
				Name:   importName.Value,
				Desc: ast.ImportDesc{
					Kind:    ast.KindFunc,
					TypeIdx: typeIdx,
					Type:    &ft,
				},
			}
			if name != "" {
				p.funcMap[name] = *funcIdx
			}
			p.mod.Imports = append(p.mod.Imports, imp)
			*funcIdx++
			return nil

		case "export":
			exp, err := p.expect(token.String)
			if err != nil {
				return err
			}
			exports = append(exports, exp.Value)
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}

		case "type":
			idx, err := p.parseIdx(p.typeMap)
			if err != nil {
				return err
			}
			if idx < uint32(len(p.mod.Types)) {
				ft = p.mod.Types[idx]
				localIdx = uint32(len(ft.Params))
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}

		case "param":
			for {
				t := p.peek()
				if t == nil || t.Type == token.RParen {
					p.next()
					break
				}
				if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
					pname := t.Value
					p.next()
					vt, err := p.parseValType()
					if err != nil {
						return err
					}
					localMap[pname] = localIdx
					localIdx++
					ft.Params = append(ft.Params, vt)
					continue
				}
				vt, err := p.parseValType()
				if err != nil {
					return err
				}
				localIdx++
				ft.Params = append(ft.Params, vt)
			}

		case "result":
			for {
				t := p.peek()
				if t == nil || t.Type == token.RParen {
					p.next()
					break
				}
				vt, err := p.parseValType()
				if err != nil {
					return err
				}
				ft.Results = append(ft.Results, vt)
			}

		case "local":
			for {
				t := p.peek()
				if t == nil || t.Type == token.RParen {
					p.next()
					break
				}
				if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
					lname := t.Value
					p.next()
					vt, err := p.parseValType()
					if err != nil {
						return err
					}
					localMap[lname] = localIdx
					localIdx++
					body.Locals = append(body.Locals, vt)
					continue
				}
				vt, err := p.parseValType()
				if err != nil {
					return err
				}
				localIdx++
				body.Locals = append(body.Locals, vt)
			}

		default:
			p.pos--
			instrs, err := p.parseInstrs(localMap)
			if err != nil {
				return err
			}
			body.Code = append(body.Code, instrs...)
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		}
	}

	body.Code = append(body.Code, ast.Instr{Opcode: ast.OpEnd})

	if name != "" {
		p.funcMap[name] = *funcIdx
	}

	typeIdx := p.findOrAddType(ft)

	p.mod.Funcs = append(p.mod.Funcs, ast.FuncEntry{TypeIdx: typeIdx})
	p.mod.Code = append(p.mod.Code, body)

	for _, exp := range exports {
		p.mod.Exports = append(p.mod.Exports, ast.Export{Name: exp, Kind: ast.KindFunc, Idx: *funcIdx})
	}

	*funcIdx++
	return nil
}

func (p *Parser) parseTable() error {
	var name string
	if t := p.peek(); t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		name = t.Value
		p.next()
	}

	var exportName string
	var importMod, importName string
parseTableClauses:
	for {
		t := p.peek()
		if t == nil || t.Type != token.LParen {
			break
		}
		saved := p.pos
		p.next()
		clause, err := p.expect(token.Ident)
		if err != nil {
			p.pos = saved
			break
		}
		switch clause.Value {
		case "export":
			exp, err := p.expect(token.String)
			if err != nil {
				return err
			}
			exportName = exp.Value
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		case "import":
			modTok, err := p.expect(token.String)
			if err != nil {
				return err
			}
			nameTok, err := p.expect(token.String)
			if err != nil {
				return err
			}
			importMod = modTok.Value
			importName = nameTok.Value
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		default:
			p.pos = saved
			break parseTableClauses
		}
	}

	// Parse table limits or inline elem
	t := p.peek()
	if t != nil && t.Type == token.Ident && (t.Value == "funcref" || t.Value == "anyfunc" || t.Value == "externref") {
		var elemType byte
		switch t.Value {
		case "funcref", "anyfunc":
			elemType = ast.RefTypeFuncref
		case "externref":
			elemType = ast.RefTypeExternref
		}
		p.next()

		if _, err := p.expect(token.LParen); err != nil {
			return err
		}
		elemTok, err := p.expect(token.Ident)
		if err != nil {
			return err
		}
		if elemTok.Value != "elem" {
			return fmt.Errorf("expected 'elem' in abbreviated table syntax, got %s", elemTok.Value)
		}

		var funcIndices []uint32
		for {
			t := p.peek()
			if t == nil || t.Type == token.RParen {
				break
			}
			idx, err := p.parseIdx(p.funcMap)
			if err != nil {
				return err
			}
			funcIndices = append(funcIndices, idx)
		}
		if _, err := p.expect(token.RParen); err != nil {
			return err
		}
		if _, err := p.expect(token.RParen); err != nil {
			return err
		}

		tableSize := uint32(len(funcIndices))
		if name != "" {
			p.tableMap[name] = uint32(len(p.mod.Tables))
		}
		p.mod.Tables = append(p.mod.Tables, ast.Table{ElemType: elemType, Limits: ast.Limits{Min: tableSize, Max: nil}})

		if exportName != "" {
			p.mod.Exports = append(p.mod.Exports, ast.Export{
				Name: exportName,
				Kind: ast.KindTable,
				Idx:  uint32(len(p.mod.Tables) - 1),
			})
		}

		p.mod.Elems = append(p.mod.Elems, ast.Elem{
			TableIdx: uint32(len(p.mod.Tables) - 1),
			Offset:   []ast.Instr{{Opcode: ast.OpI32Const, Imm: int32(0)}, {Opcode: ast.OpEnd}},
			Init:     funcIndices,
			Mode:     ast.ElemModeActive,
		})
		return nil
	}

	minVal, err := p.parseU32()
	if err != nil {
		return err
	}

	var maxVal *uint32
	if t := p.peek(); t != nil && t.Type == token.Number {
		m, err := p.parseU32()
		if err != nil {
			return err
		}
		maxVal = &m
	}

	elemTok, err := p.expect(token.Ident)
	if err != nil {
		return err
	}
	var elemType byte
	switch elemTok.Value {
	case "funcref", "anyfunc":
		elemType = ast.RefTypeFuncref
	case "externref":
		elemType = ast.RefTypeExternref
	default:
		return fmt.Errorf("expected funcref or externref, got %s", elemTok.Value)
	}

	if _, err := p.expect(token.RParen); err != nil {
		return err
	}

	if importMod != "" {
		if name != "" {
			p.tableMap[name] = uint32(len(p.mod.Tables))
		}
		tbl := ast.Table{ElemType: elemType, Limits: ast.Limits{Min: minVal, Max: maxVal}}
		p.mod.Imports = append(p.mod.Imports, ast.Import{
			Module: importMod,
			Name:   importName,
			Desc: ast.ImportDesc{
				Kind:     ast.KindTable,
				TableTyp: &tbl,
			},
		})
		return nil
	}

	if name != "" {
		p.tableMap[name] = uint32(len(p.mod.Tables))
	}
	p.mod.Tables = append(p.mod.Tables, ast.Table{ElemType: elemType, Limits: ast.Limits{Min: minVal, Max: maxVal}})

	if exportName != "" {
		p.mod.Exports = append(p.mod.Exports, ast.Export{
			Name: exportName,
			Kind: ast.KindTable,
			Idx:  uint32(len(p.mod.Tables) - 1),
		})
	}

	return nil
}

func (p *Parser) parseMemory() error {
	var name string
	if t := p.peek(); t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		name = t.Value
		p.next()
	}

	var exportName string
	var importMod, importName string
parseMemClauses:
	for {
		t := p.peek()
		if t == nil || t.Type != token.LParen {
			break
		}
		saved := p.pos
		p.next()
		clause, err := p.expect(token.Ident)
		if err != nil {
			p.pos = saved
			break
		}
		switch clause.Value {
		case "export":
			exp, err := p.expect(token.String)
			if err != nil {
				return err
			}
			exportName = exp.Value
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		case "import":
			modTok, err := p.expect(token.String)
			if err != nil {
				return err
			}
			nameTok, err := p.expect(token.String)
			if err != nil {
				return err
			}
			importMod = modTok.Value
			importName = nameTok.Value
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		case "data":
			var dataBytes []byte
			for {
				t := p.peek()
				if t == nil {
					break
				}
				if t.Type == token.String {
					p.next()
					decoded := DecodeStringLiteral(t.Value)
					dataBytes = append(dataBytes, decoded...)
				} else if t.Type == token.RParen {
					break
				} else {
					return fmt.Errorf("expected string in inline data, got %v", t.Type)
				}
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}

			pages := (uint32(len(dataBytes)) + 65535) / 65536
			if pages == 0 {
				pages = 1
			}

			if name != "" {
				p.memMap[name] = uint32(len(p.mod.Memories))
			}
			p.mod.Memories = append(p.mod.Memories, ast.Memory{Limits: ast.Limits{Min: pages, Max: nil}})

			if exportName != "" {
				p.mod.Exports = append(p.mod.Exports, ast.Export{
					Name: exportName,
					Kind: ast.KindMemory,
					Idx:  uint32(len(p.mod.Memories) - 1),
				})
			}

			p.mod.Data = append(p.mod.Data, ast.DataSegment{
				MemIdx:  uint32(len(p.mod.Memories) - 1),
				Offset:  []ast.Instr{{Opcode: ast.OpI32Const, Imm: int32(0)}, {Opcode: ast.OpEnd}},
				Init:    dataBytes,
				Passive: false,
			})
			return nil

		default:
			p.pos = saved
			break parseMemClauses
		}
	}

	// Parse memory limits
	minVal, err := p.parseU32()
	if err != nil {
		return err
	}

	var maxVal *uint32
	if t := p.peek(); t != nil && t.Type == token.Number {
		m, err := p.parseU32()
		if err != nil {
			return err
		}
		maxVal = &m
	}

	if _, err = p.expect(token.RParen); err != nil {
		return err
	}

	if importMod != "" {
		if name != "" {
			p.memMap[name] = uint32(len(p.mod.Memories))
		}
		lim := ast.Limits{Min: minVal, Max: maxVal}
		p.mod.Imports = append(p.mod.Imports, ast.Import{
			Module: importMod,
			Name:   importName,
			Desc: ast.ImportDesc{
				Kind:      ast.KindMemory,
				MemLimits: &lim,
			},
		})
		return nil
	}

	if name != "" {
		p.memMap[name] = uint32(len(p.mod.Memories))
	}

	p.mod.Memories = append(p.mod.Memories, ast.Memory{Limits: ast.Limits{Min: minVal, Max: maxVal}})

	if exportName != "" {
		p.mod.Exports = append(p.mod.Exports, ast.Export{
			Name: exportName,
			Kind: ast.KindMemory,
			Idx:  uint32(len(p.mod.Memories) - 1),
		})
	}

	return nil
}

func (p *Parser) parseGlobal(globalIdx *uint32) error {
	var name string
	if t := p.peek(); t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		name = t.Value
		p.next()
	}

	var exportName string
	var importMod, importName string
parseGlobalClauses:
	for {
		t := p.peek()
		if t == nil || t.Type != token.LParen {
			break
		}
		saved := p.pos
		p.next()
		clause, err := p.expect(token.Ident)
		if err != nil {
			p.pos = saved
			break
		}
		switch clause.Value {
		case "export":
			exp, err := p.expect(token.String)
			if err != nil {
				return err
			}
			exportName = exp.Value
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		case "import":
			modTok, err := p.expect(token.String)
			if err != nil {
				return err
			}
			nameTok, err := p.expect(token.String)
			if err != nil {
				return err
			}
			importMod = modTok.Value
			importName = nameTok.Value
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		case "mut":
			p.pos = saved
			break parseGlobalClauses
		default:
			p.pos = saved
			break parseGlobalClauses
		}
	}

	// Parse global type
	gt := ast.GlobalType{}
	if t := p.peek(); t != nil && t.Type == token.LParen {
		p.next()
		t, err := p.expect(token.Ident)
		if err != nil {
			return err
		}
		if t.Value == "mut" {
			gt.Mutable = true
			vt, err := p.parseValType()
			if err != nil {
				return err
			}
			gt.ValType = vt
			if _, err := p.expect(token.RParen); err != nil {
				return err
			}
		} else {
			p.pos -= 2
			vt, err := p.parseValType()
			if err != nil {
				return err
			}
			gt.ValType = vt
		}
	} else {
		vt, err := p.parseValType()
		if err != nil {
			return err
		}
		gt.ValType = vt
	}

	if importMod != "" {
		if _, err := p.expect(token.RParen); err != nil {
			return err
		}
		if name != "" {
			p.globalMap[name] = *globalIdx
		}
		*globalIdx++
		p.mod.Imports = append(p.mod.Imports, ast.Import{
			Module: importMod,
			Name:   importName,
			Desc: ast.ImportDesc{
				Kind:      ast.KindGlobal,
				GlobalTyp: &gt,
			},
		})
		return nil
	}

	instrs, err := p.parseInstrs(nil)
	if err != nil {
		return err
	}
	instrs = append(instrs, ast.Instr{Opcode: ast.OpEnd})

	if _, err := p.expect(token.RParen); err != nil {
		return err
	}

	if name != "" {
		p.globalMap[name] = *globalIdx
	}

	p.mod.Globals = append(p.mod.Globals, ast.Global{Type: gt, Init: instrs})

	if exportName != "" {
		p.mod.Exports = append(p.mod.Exports, ast.Export{
			Name: exportName,
			Kind: ast.KindGlobal,
			Idx:  *globalIdx,
		})
	}

	*globalIdx++
	return nil
}
