package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func (p *Parser) prescanTypes() {
	saved := p.pos
	depth := 0

	for p.pos < len(p.tokens) {
		t := &p.tokens[p.pos]
		if t.Type == token.LParen {
			depth++
			p.pos++
			if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && p.tokens[p.pos].Value == "type" {
				p.pos++
				if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
					name := p.tokens[p.pos].Value
					p.pos++
					if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.LParen {
						p.pos++
						if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && p.tokens[p.pos].Value == "func" {
							p.pos++
							ft := ast.FuncType{}
							_ = p.parseFuncSig(&ft) // Error handled by later parsing
							p.typeMap[name] = uint32(len(p.mod.Types))
							p.mod.Types = append(p.mod.Types, ft)
						}
					}
				}
				for p.pos < len(p.tokens) && depth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						depth++
					case token.RParen:
						depth--
					}
					p.pos++
				}
				continue
			}
		} else if t.Type == token.RParen {
			depth--
			if depth <= 0 {
				break
			}
		}
		p.pos++
	}

	p.pos = saved
}

// prescanNames collects all module-level names before the main parsing pass
// to support forward references in WAT
func (p *Parser) prescanNames() {
	saved := p.pos
	depth := 0
	var funcIdx, globalIdx, tableIdx, memIdx, dataIdx, elemIdx uint32

	for p.pos < len(p.tokens) {
		t := &p.tokens[p.pos]
		if t.Type == token.LParen {
			depth++
			p.pos++
			if p.pos >= len(p.tokens) {
				break
			}
			if p.tokens[p.pos].Type != token.Ident {
				continue
			}
			keyword := p.tokens[p.pos].Value
			p.pos++

			switch keyword {
			case "import":
				// Find the import descriptor to track indices and inline names
				for p.pos < len(p.tokens) {
					if p.tokens[p.pos].Type == token.LParen {
						p.pos++
						if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident {
							kind := p.tokens[p.pos].Value
							p.pos++
							// Check for inline name after kind
							if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
								name := p.tokens[p.pos].Value
								switch kind {
								case "func":
									p.funcMap[name] = funcIdx
								case "global":
									p.globalMap[name] = globalIdx
								case "table":
									p.tableMap[name] = tableIdx
								case "memory":
									p.memMap[name] = memIdx
								}
							}
							switch kind {
							case "func":
								funcIdx++
							case "global":
								globalIdx++
							case "table":
								tableIdx++
							case "memory":
								memIdx++
							}
							break
						}
					}
					p.pos++
					if p.pos >= len(p.tokens) || p.tokens[p.pos].Type == token.RParen {
						break
					}
				}
				// Skip to matching RParen
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue

			case "func":
				if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
					p.funcMap[p.tokens[p.pos].Value] = funcIdx
				}
				funcIdx++
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue

			case "global":
				if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
					p.globalMap[p.tokens[p.pos].Value] = globalIdx
				}
				globalIdx++
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue

			case "table":
				if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
					p.tableMap[p.tokens[p.pos].Value] = tableIdx
				}
				tableIdx++
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue

			case "memory":
				if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
					p.memMap[p.tokens[p.pos].Value] = memIdx
				}
				memIdx++
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue

			case "data":
				if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
					p.dataMap[p.tokens[p.pos].Value] = dataIdx
				}
				dataIdx++
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue

			case "elem":
				if p.pos < len(p.tokens) && p.tokens[p.pos].Type == token.Ident && strings.HasPrefix(p.tokens[p.pos].Value, "$") {
					p.elemMap[p.tokens[p.pos].Value] = elemIdx
				}
				elemIdx++
				// Skip to matching RParen
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue

			case "module":
				// Don't skip the module - we need to process its children
				continue

			default:
				// Skip unknown elements
				localDepth := 1
				for p.pos < len(p.tokens) && localDepth > 0 {
					switch p.tokens[p.pos].Type {
					case token.LParen:
						localDepth++
					case token.RParen:
						localDepth--
					}
					p.pos++
				}
				continue
			}
		}
		if t.Type == token.RParen {
			depth--
		}
		p.pos++
	}

	p.pos = saved
}

func (p *Parser) parseModule() (*ast.Module, error) {
	if _, err := p.expect(token.LParen); err != nil {
		return nil, err
	}
	t, err := p.expect(token.Ident)
	if err != nil {
		return nil, err
	}
	if t.Value != "module" {
		return nil, fmt.Errorf("expected 'module', got %q", t.Value)
	}

	if t := p.peek(); t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		p.next()
	}

	p.mod = &ast.Module{}
	p.prescanNames()
	p.prescanTypes()

	var funcIdx uint32
	var globalIdx uint32
	var tableIdx uint32
	var memIdx uint32

	for {
		t := p.peek()
		if t == nil {
			return nil, fmt.Errorf("unexpected end of module")
		}
		if t.Type == token.RParen {
			p.next()
			break
		}

		if _, err = p.expect(token.LParen); err != nil {
			return nil, err
		}
		t, err = p.expect(token.Ident)
		if err != nil {
			return nil, err
		}

		switch t.Value {
		case "type":
			if err := p.parseType(); err != nil {
				return nil, err
			}
		case "import":
			if err := p.parseImport(&funcIdx, &globalIdx, &tableIdx, &memIdx); err != nil {
				return nil, err
			}
		case "func":
			if err := p.parseFunc(&funcIdx); err != nil {
				return nil, err
			}
		case "table":
			if err := p.parseTable(); err != nil {
				return nil, err
			}
		case "memory":
			if err := p.parseMemory(); err != nil {
				return nil, err
			}
		case "global":
			if err := p.parseGlobal(&globalIdx); err != nil {
				return nil, err
			}
		case "export":
			if err := p.parseExport(); err != nil {
				return nil, err
			}
		case "start":
			if err := p.parseStart(); err != nil {
				return nil, err
			}
		case "elem":
			if err := p.parseElem(); err != nil {
				return nil, err
			}
		case "data":
			if err := p.parseData(); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown module field: %s", t.Value)
		}
	}

	return p.mod, nil
}

func (p *Parser) parseType() error {
	var name string
	if t := p.peek(); t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
		name = t.Value
		p.next()
	}

	if _, err := p.expect(token.LParen); err != nil {
		return err
	}
	t, err := p.expect(token.Ident)
	if err != nil {
		return err
	}
	if t.Value != "func" {
		return fmt.Errorf("expected 'func' in type definition")
	}

	ft := ast.FuncType{}
	if err := p.parseFuncSig(&ft); err != nil {
		return err
	}

	if _, err := p.expect(token.RParen); err != nil {
		return err
	}
	if _, err := p.expect(token.RParen); err != nil {
		return err
	}

	if name != "" {
		if _, exists := p.typeMap[name]; exists {
			return nil
		}
		p.typeMap[name] = uint32(len(p.mod.Types))
	}
	p.mod.Types = append(p.mod.Types, ft)
	return nil
}

func (p *Parser) parseFuncSig(ft *ast.FuncType) error {
	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen {
			break
		}
		if t.Type != token.LParen {
			break
		}
		p.next()

		t, err := p.expect(token.Ident)
		if err != nil {
			return err
		}

		switch t.Value {
		case "param":
			if err := p.parseParams(ft); err != nil {
				return err
			}
		case "result":
			if err := p.parseResults(ft); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected %q in function signature", t.Value)
		}
	}
	return nil
}

func (p *Parser) parseParams(ft *ast.FuncType) error {
	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen {
			p.next()
			return nil
		}
		if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
			p.next()
			continue
		}
		vt, err := p.parseValType()
		if err != nil {
			return err
		}
		ft.Params = append(ft.Params, vt)
	}
}

func (p *Parser) parseResults(ft *ast.FuncType) error {
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
		ft.Results = append(ft.Results, vt)
	}
}

func (p *Parser) parseImport(funcIdx, globalIdx, tableIdx, memIdx *uint32) error {
	modName, err := p.expect(token.String)
	if err != nil {
		return err
	}
	name, err := p.expect(token.String)
	if err != nil {
		return err
	}

	if _, err = p.expect(token.LParen); err != nil {
		return err
	}
	t, err := p.expect(token.Ident)
	if err != nil {
		return err
	}

	imp := ast.Import{Module: modName.Value, Name: name.Value}

	switch t.Value {
	case "func":
		var fname string
		if tok := p.peek(); tok != nil && tok.Type == token.Ident && strings.HasPrefix(tok.Value, "$") {
			fname = tok.Value
			p.next()
		}

		var ft ast.FuncType
		if tok := p.peek(); tok != nil && tok.Type == token.LParen {
			saved := p.pos
			p.next()
			t2, err := p.expect(token.Ident)
			if err != nil {
				return err
			}
			if t2.Value == "type" {
				typeIdx, err := p.parseIdx(p.typeMap)
				if err != nil {
					return err
				}
				if _, err := p.expect(token.RParen); err != nil {
					return err
				}
				if int(typeIdx) >= len(p.mod.Types) {
					return fmt.Errorf("type index %d out of range", typeIdx)
				}
				ft = p.mod.Types[typeIdx]
				imp.Desc.TypeIdx = typeIdx
			} else {
				p.pos = saved
				if err := p.parseFuncSig(&ft); err != nil {
					return err
				}
				p.mod.Types = append(p.mod.Types, ft)
				imp.Desc.TypeIdx = uint32(len(p.mod.Types) - 1)
			}
		} else {
			if err := p.parseFuncSig(&ft); err != nil {
				return err
			}
			p.mod.Types = append(p.mod.Types, ft)
			imp.Desc.TypeIdx = uint32(len(p.mod.Types) - 1)
		}

		if _, err := p.expect(token.RParen); err != nil {
			return err
		}

		imp.Desc.Kind = ast.KindFunc
		imp.Desc.Type = &ft
		if fname != "" {
			p.funcMap[fname] = *funcIdx
		}
		*funcIdx++

	case "global":
		var gname string
		if tok := p.peek(); tok != nil && tok.Type == token.Ident && strings.HasPrefix(tok.Value, "$") {
			gname = tok.Value
			p.next()
		}

		gt := ast.GlobalType{}
		if tok := p.peek(); tok != nil && tok.Type == token.LParen {
			p.next()
			mutTok, err := p.expect(token.Ident)
			if err != nil {
				return err
			}
			if mutTok.Value == "mut" {
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
				return fmt.Errorf("expected 'mut' in global type")
			}
		} else {
			vt, err := p.parseValType()
			if err != nil {
				return err
			}
			gt.ValType = vt
		}
		if _, err := p.expect(token.RParen); err != nil {
			return err
		}

		imp.Desc.Kind = ast.KindGlobal
		imp.Desc.GlobalTyp = &gt
		if gname != "" {
			p.globalMap[gname] = *globalIdx
		}
		*globalIdx++

	case "memory":
		var mname string
		if tok := p.peek(); tok != nil && tok.Type == token.Ident && strings.HasPrefix(tok.Value, "$") {
			mname = tok.Value
			p.next()
		}

		lim := ast.Limits{}
		minTok, err := p.expect(token.Number)
		if err != nil {
			return err
		}
		minVal, err := strconv.ParseUint(minTok.Value, 0, 32)
		if err != nil {
			return fmt.Errorf("invalid memory min: %s", minTok.Value)
		}
		lim.Min = uint32(minVal)

		if tok := p.peek(); tok != nil && tok.Type == token.Number {
			maxTok := p.next()
			maxVal, err := strconv.ParseUint(maxTok.Value, 0, 32)
			if err != nil {
				return fmt.Errorf("invalid memory max: %s", maxTok.Value)
			}
			maxU32 := uint32(maxVal)
			lim.Max = &maxU32
		}

		if _, err := p.expect(token.RParen); err != nil {
			return err
		}

		imp.Desc.Kind = ast.KindMemory
		imp.Desc.MemLimits = &lim
		if mname != "" {
			p.memMap[mname] = *memIdx
		}
		*memIdx++

	case "table":
		var tname string
		if tok := p.peek(); tok != nil && tok.Type == token.Ident && strings.HasPrefix(tok.Value, "$") {
			tname = tok.Value
			p.next()
		}

		lim := ast.Limits{}
		minTok, err := p.expect(token.Number)
		if err != nil {
			return err
		}
		minVal, err := strconv.ParseUint(minTok.Value, 0, 32)
		if err != nil {
			return fmt.Errorf("invalid table min: %s", minTok.Value)
		}
		lim.Min = uint32(minVal)

		if tok := p.peek(); tok != nil && tok.Type == token.Number {
			maxTok := p.next()
			maxVal, err := strconv.ParseUint(maxTok.Value, 0, 32)
			if err != nil {
				return fmt.Errorf("invalid table max: %s", maxTok.Value)
			}
			maxU32 := uint32(maxVal)
			lim.Max = &maxU32
		}

		elemTyp, err := p.expect(token.Ident)
		if err != nil {
			return err
		}
		var elemByte byte
		switch elemTyp.Value {
		case "funcref":
			elemByte = ast.RefTypeFuncref
		case "externref":
			elemByte = ast.RefTypeExternref
		default:
			return fmt.Errorf("expected funcref or externref, got %s", elemTyp.Value)
		}

		if _, err := p.expect(token.RParen); err != nil {
			return err
		}

		imp.Desc.Kind = ast.KindTable
		imp.Desc.TableTyp = &ast.Table{ElemType: elemByte, Limits: lim}
		if tname != "" {
			p.tableMap[tname] = *tableIdx
		}
		*tableIdx++

	default:
		return fmt.Errorf("unsupported import kind: %s", t.Value)
	}

	if _, err := p.expect(token.RParen); err != nil {
		return err
	}

	p.mod.Imports = append(p.mod.Imports, imp)
	return nil
}

func (p *Parser) parseStart() error {
	idx, err := p.parseIdx(p.funcMap)
	if err != nil {
		return err
	}
	p.mod.Start = &idx

	if _, err := p.expect(token.RParen); err != nil {
		return err
	}
	return nil
}

func (p *Parser) parseExport() error {
	name, err := p.expect(token.String)
	if err != nil {
		return err
	}

	if _, err = p.expect(token.LParen); err != nil {
		return err
	}

	kind, err := p.expect(token.Ident)
	if err != nil {
		return err
	}

	var kindByte byte
	var nameMap map[string]uint32
	switch kind.Value {
	case "func":
		kindByte = ast.KindFunc
		nameMap = p.funcMap
	case "table":
		kindByte = ast.KindTable
		nameMap = p.tableMap
	case "memory":
		kindByte = ast.KindMemory
		nameMap = p.memMap
	case "global":
		kindByte = ast.KindGlobal
		nameMap = p.globalMap
	default:
		return fmt.Errorf("unknown export kind: %s", kind.Value)
	}

	idx, err := p.parseIdx(nameMap)
	if err != nil {
		return err
	}

	if _, err := p.expect(token.RParen); err != nil {
		return err
	}
	if _, err := p.expect(token.RParen); err != nil {
		return err
	}

	p.mod.Exports = append(p.mod.Exports, ast.Export{Name: name.Value, Kind: kindByte, Idx: idx})
	return nil
}
