package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/opcode"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

// parseDualIdx handles WAT's optional first index + required second index pattern.
// When first index is present, it's followed by second; otherwise only second appears.
func (p *Parser) parseDualIdx(firstMap, secondMap map[string]uint32) (uint32, uint32, error) {
	var firstIdx, secondIdx uint32
	t := p.peek()
	if t == nil || (t.Type != token.Number && (t.Type != token.Ident || !strings.HasPrefix(t.Value, "$"))) {
		return 0, 0, nil
	}
	saved := p.pos
	idx, err := p.parseIdx(firstMap)
	if err == nil {
		t2 := p.peek()
		if t2 != nil && (t2.Type == token.Number || (t2.Type == token.Ident && strings.HasPrefix(t2.Value, "$"))) {
			firstIdx = idx
			secondIdx, err = p.parseIdx(secondMap)
			if err != nil {
				return 0, 0, err
			}
		} else {
			p.pos = saved
			secondIdx, err = p.parseIdx(secondMap)
			if err != nil {
				return 0, 0, err
			}
		}
	} else {
		p.pos = saved
		secondIdx, err = p.parseIdx(secondMap)
		if err != nil {
			return 0, 0, err
		}
	}
	return firstIdx, secondIdx, nil
}

// parseDualIdxPair handles paired indices where both must appear or neither.
func (p *Parser) parseDualIdxPair(idxMap map[string]uint32) (uint32, uint32, error) {
	var first, second uint32
	t := p.peek()
	if t == nil || (t.Type != token.Number && (t.Type != token.Ident || !strings.HasPrefix(t.Value, "$"))) {
		return 0, 0, nil
	}
	saved := p.pos
	idx, err := p.parseIdx(idxMap)
	if err == nil {
		t2 := p.peek()
		if t2 != nil && (t2.Type == token.Number || (t2.Type == token.Ident && strings.HasPrefix(t2.Value, "$"))) {
			first = idx
			second, err = p.parseIdx(idxMap)
			if err != nil {
				return 0, 0, err
			}
		} else {
			p.pos = saved
		}
	} else {
		p.pos = saved
	}
	return first, second, nil
}

func (p *Parser) parseInstrs(localMap map[string]uint32) ([]ast.Instr, error) {
	var instrs []ast.Instr

	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen {
			break
		}

		if t.Type == token.LParen {
			p.next()
			nested, err := p.parseInstrs(localMap)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, nested...)
			if _, err := p.expect(token.RParen); err != nil {
				return nil, err
			}
			continue
		}

		if t.Type != token.Ident {
			return nil, fmt.Errorf("expected instruction, got %v", t.Type)
		}

		p.next()
		name := t.Value
		line := t.Line

		if info, ok := opcode.Lookup(name); ok {
			result, err := p.parseSimpleInstr(name, info, localMap)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, result...)
			continue
		}

		if memOp, ok := opcode.LookupMemory(name); ok {
			result, err := p.parseMemoryInstr(memOp, localMap)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, result...)
			continue
		}

		if prefOp, ok := opcode.LookupPrefixed(name); ok {
			result, err := p.parsePrefixedInstr(name, prefOp, localMap)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, result...)
			continue
		}

		switch name {
		case "block", "loop":
			label := p.parseLabel()
			bt, err := p.parseBlockType()
			if err != nil {
				return nil, err
			}
			op := ast.OpBlock
			if name == "loop" {
				op = ast.OpLoop
			}
			instrs = append(instrs, ast.Instr{Opcode: op, Imm: bt})
			p.pushLabel(label)
			body, err := p.parseInstrs(localMap)
			p.popLabel()
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, body...)
			instrs = append(instrs, ast.Instr{Opcode: ast.OpEnd})

		case "if":
			label := p.parseLabel()
			bt, err := p.parseBlockType()
			if err != nil {
				return nil, err
			}

			// Check for folded condition: (if (condition) ...)
			if t := p.peek(); t != nil && t.Type == token.LParen {
				saved := p.pos
				p.next()
				if nt := p.peek(); nt != nil && nt.Type == token.Ident && (nt.Value == "then" || nt.Value == "result" || nt.Value == "param") {
					p.pos = saved
				} else {
					cond, err := p.parseInstrs(localMap)
					if err != nil {
						return nil, err
					}
					instrs = append(instrs, cond...)
					if _, err := p.expect(token.RParen); err != nil {
						return nil, err
					}
				}
			}

			instrs = append(instrs, ast.Instr{Opcode: ast.OpIf, Imm: bt})
			p.pushLabel(label)

			// Check for folded then/else: (then ...) (else ...)
			useFlatForm := true
			if t := p.peek(); t != nil && t.Type == token.LParen {
				saved := p.pos
				p.next()
				t, err := p.expect(token.Ident)
				if err != nil {
					p.popLabel()
					return nil, err
				}
				if t.Value == "then" {
					useFlatForm = false
					thenInstrs, err := p.parseInstrs(localMap)
					if err != nil {
						p.popLabel()
						return nil, err
					}
					instrs = append(instrs, thenInstrs...)
					if _, err := p.expect(token.RParen); err != nil {
						p.popLabel()
						return nil, err
					}

					if t := p.peek(); t != nil && t.Type == token.LParen {
						p.next()
						t, err := p.expect(token.Ident)
						if err != nil {
							p.popLabel()
							return nil, err
						}
						if t.Value == "else" {
							instrs = append(instrs, ast.Instr{Opcode: ast.OpElse})
							elseInstrs, err := p.parseInstrs(localMap)
							if err != nil {
								p.popLabel()
								return nil, err
							}
							instrs = append(instrs, elseInstrs...)
							if _, err := p.expect(token.RParen); err != nil {
								return nil, err
							}
						} else {
							p.pos -= 2
						}
					}
					p.popLabel()
					instrs = append(instrs, ast.Instr{Opcode: ast.OpEnd})
				} else {
					// Not (then ...), restore and parse as flat form
					p.pos = saved
				}
			}

			if useFlatForm {
				// Flat form: parse then-body until else or end
				thenInstrs, err := p.parseIfBody(localMap)
				if err != nil {
					p.popLabel()
					return nil, err
				}
				instrs = append(instrs, thenInstrs...)
				p.popLabel()
				instrs = append(instrs, ast.Instr{Opcode: ast.OpEnd})
			}

		case "then", "else":
			continue

		case "end":
			// In flat form, end terminates the enclosing block/loop/if/func.
			// Return immediately - the caller adds OpEnd.
			return instrs, nil

		case "call_indirect", "return_call_indirect":
			isReturn := name == "return_call_indirect"
			tableIdx := uint32(0)
			typeIdx := uint32(0)
			var inlineParams []ast.ValType
			var inlineResults []ast.ValType
			hasType := false

			if t := p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$"))) {
				saved := p.pos
				idx, err := p.parseIdx(p.tableMap)
				if err == nil {
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

		parseTypeLoop:
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
					idx, err := p.parseIdx(p.typeMap)
					if err != nil {
						return nil, err
					}
					typeIdx = idx
					hasType = true
					if _, err := p.expect(token.RParen); err != nil {
						return nil, err
					}
				case "param":
					for {
						pt := p.peek()
						if pt == nil || pt.Type == token.RParen {
							break
						}
						vt, err := p.parseValType()
						if err != nil {
							return nil, err
						}
						inlineParams = append(inlineParams, vt)
					}
					if _, err := p.expect(token.RParen); err != nil {
						return nil, err
					}
				case "result":
					for {
						rt := p.peek()
						if rt == nil || rt.Type == token.RParen {
							break
						}
						vt, err := p.parseValType()
						if err != nil {
							return nil, err
						}
						inlineResults = append(inlineResults, vt)
					}
					if _, err := p.expect(token.RParen); err != nil {
						return nil, err
					}
				default:
					p.pos = saved
					break parseTypeLoop
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

			for {
				if t := p.peek(); t == nil || t.Type == token.RParen {
					break
				}
				if t := p.peek(); t != nil && t.Type == token.LParen {
					p.next()
					ops, err := p.parseInstrs(localMap)
					if err != nil {
						return nil, err
					}
					instrs = append(instrs, ops...)
					if _, err := p.expect(token.RParen); err != nil {
						return nil, err
					}
				} else {
					break
				}
			}
			opcode := ast.OpCallIndirect
			if isReturn {
				opcode = ast.OpReturnCallIndirect
			}
			instrs = append(instrs, ast.Instr{Opcode: opcode, Imm: []uint32{typeIdx, tableIdx}})

		case "br_table":
			var labels []uint32
			for {
				t := p.peek()
				if t == nil || t.Type == token.RParen || t.Type == token.LParen {
					break
				}
				if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
					labelName := t.Value
					p.next()
					if depth, ok := p.resolveLabel(labelName); ok {
						labels = append(labels, depth)
					} else {
						return nil, fmt.Errorf("unknown label: %s", labelName)
					}
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
			for {
				if t := p.peek(); t == nil || t.Type != token.LParen {
					break
				}
				p.next()
				ops, err := p.parseInstrs(localMap)
				if err != nil {
					return nil, err
				}
				instrs = append(instrs, ops...)
				if _, err := p.expect(token.RParen); err != nil {
					return nil, err
				}
			}
			instrs = append(instrs, ast.Instr{Opcode: ast.OpBrTable, Imm: labels})

		case "table.get":
			var idx uint32
			if t := p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$"))) {
				var err error
				idx, err = p.parseIdx(p.tableMap)
				if err != nil {
					return nil, err
				}
			}
			ops, err := p.parseOperands(localMap, 1)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, ops...)
			instrs = append(instrs, ast.Instr{Opcode: ast.OpTableGet, Imm: idx})

		case "table.set":
			var idx uint32
			if t := p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$"))) {
				var err error
				idx, err = p.parseIdx(p.tableMap)
				if err != nil {
					return nil, err
				}
			}
			ops, err := p.parseOperands(localMap, 2)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, ops...)
			instrs = append(instrs, ast.Instr{Opcode: ast.OpTableSet, Imm: idx})

		case "ref.null":
			t, err := p.expect(token.Ident)
			if err != nil {
				return nil, err
			}
			var heapType byte
			switch t.Value {
			case "func", "funcref":
				heapType = ast.RefTypeFuncref
			case "extern", "externref":
				heapType = ast.RefTypeExternref
			default:
				return nil, fmt.Errorf("unknown heap type: %s", t.Value)
			}
			instrs = append(instrs, ast.Instr{Opcode: ast.OpRefNull, Imm: heapType})

		case "ref.func":
			idx, err := p.parseIdx(p.funcMap)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, ast.Instr{Opcode: ast.OpRefFunc, Imm: idx})

		case "select":
			var types []ast.ValType
			for {
				if t := p.peek(); t != nil && t.Type == token.LParen {
					saved := p.pos
					p.next()
					if nt := p.peek(); nt != nil && nt.Type == token.Ident && nt.Value == "result" {
						p.next()
						for {
							if rt := p.peek(); rt == nil || rt.Type == token.RParen {
								p.next()
								break
							}
							vt, err := p.parseValType()
							if err != nil {
								return nil, err
							}
							types = append(types, vt)
						}
					} else {
						p.pos = saved
						break
					}
				} else {
					break
				}
			}
			ops, err := p.parseOperands(localMap, 3)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, ops...)
			if len(types) > 0 {
				instrs = append(instrs, ast.Instr{Opcode: ast.OpSelectTyped, Imm: types})
			} else {
				instrs = append(instrs, ast.Instr{Opcode: ast.OpSelect})
			}

		default:
			return nil, fmt.Errorf("line %d: unknown instruction: %s", line, name)
		}
	}

	return instrs, nil
}

func (p *Parser) parseSimpleInstr(name string, info opcode.Info, localMap map[string]uint32) ([]ast.Instr, error) {
	var result []ast.Instr

	var imm interface{}
	switch info.ImmType {
	case opcode.ImmU32:
		if name == "br" || name == "br_if" {
			t := p.peek()
			if t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
				labelName := t.Value
				p.next()
				if depth, ok := p.resolveLabel(labelName); ok {
					imm = depth
				} else {
					return nil, fmt.Errorf("unknown label: %s", labelName)
				}
			} else {
				idx, err := p.parseU32()
				if err != nil {
					return nil, err
				}
				imm = idx
			}
		} else {
			var nameMap map[string]uint32
			switch name {
			case "local.get", "local.set", "local.tee":
				nameMap = localMap
			case "global.get", "global.set":
				nameMap = p.globalMap
			case "call", "return_call":
				nameMap = p.funcMap
			}
			idx, err := p.parseIdx(nameMap)
			if err != nil {
				return nil, err
			}
			imm = idx
		}

	case opcode.ImmI32:
		t, err := p.expect(token.Number)
		if err != nil {
			return nil, err
		}
		s := strings.ReplaceAll(t.Value, "_", "")
		val, err := strconv.ParseInt(s, 0, 32)
		if err != nil {
			uval, err2 := strconv.ParseUint(s, 0, 32)
			if err2 != nil {
				return nil, fmt.Errorf("invalid i32: %s", t.Value)
			}
			imm = int32(uval)
		} else {
			imm = int32(val)
		}

	case opcode.ImmI64:
		t, err := p.expect(token.Number)
		if err != nil {
			return nil, err
		}
		s := strings.ReplaceAll(t.Value, "_", "")
		val, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			uval, err2 := strconv.ParseUint(s, 0, 64)
			if err2 != nil {
				return nil, fmt.Errorf("invalid i64: %s", t.Value)
			}
			imm = int64(uval)
		} else {
			imm = val
		}

	case opcode.ImmF32:
		val, err := p.parseF32()
		if err != nil {
			return nil, err
		}
		imm = val

	case opcode.ImmF64:
		val, err := p.parseF64()
		if err != nil {
			return nil, err
		}
		imm = val

	case opcode.ImmBlock:
		bt, err := p.parseBlockType()
		if err != nil {
			return nil, err
		}
		imm = bt

	case opcode.ImmMemIdx:
		if t := p.peek(); t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
			p.next()
			if idx, ok := p.memMap[t.Value]; ok {
				imm = idx
			} else {
				return nil, fmt.Errorf("unknown memory: %s", t.Value)
			}
		} else if t != nil && t.Type == token.Number {
			p.next()
			val, err := strconv.ParseUint(t.Value, 0, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid memory index: %s", t.Value)
			}
			imm = uint32(val)
		} else {
			imm = uint32(0)
		}

	case opcode.ImmNone, opcode.ImmMemarg:
		// No immediate
	}

	if info.Operands > 0 {
		ops, err := p.parseOperands(localMap, info.Operands)
		if err != nil {
			return nil, err
		}
		result = append(result, ops...)
	} else if info.Operands == -1 {
		for {
			if t := p.peek(); t == nil || t.Type != token.LParen {
				break
			}
			p.next()
			ops, err := p.parseInstrs(localMap)
			if err != nil {
				return nil, err
			}
			result = append(result, ops...)
			if _, err := p.expect(token.RParen); err != nil {
				return nil, err
			}
		}
	}

	result = append(result, ast.Instr{Opcode: info.Opcode, Imm: imm})
	return result, nil
}

func (p *Parser) parseMemoryInstr(memOp opcode.MemoryOp, localMap map[string]uint32) ([]ast.Instr, error) {
	var result []ast.Instr

	ma := ast.Memarg{Align: memOp.NaturalAlign, Offset: 0}

	// Check for optional memory index (multi-memory)
	if t := p.peek(); t != nil {
		if t.Type == token.Number {
			// Check if this is a memory index
			saved := p.pos
			p.next()
			next := p.peek()
			// Accept if followed by: LParen (folded operand), offset=/align=, RParen (end), or Ident (flat form next instr)
			isMemIdx := next == nil || next.Type == token.LParen || next.Type == token.RParen ||
				(next.Type == token.Ident && !strings.HasPrefix(next.Value, "$"))
			if isMemIdx {
				idx, err := strconv.ParseUint(t.Value, 0, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid memory index: %s", t.Value)
				}
				ma.MemIdx = uint32(idx)
			} else {
				// Not a memory index, restore position
				p.pos = saved
			}
		} else if t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
			if idx, ok := p.memMap[t.Value]; ok {
				p.next()
				ma.MemIdx = idx
			}
		}
	}

	for {
		t := p.peek()
		if t == nil || t.Type != token.Ident {
			break
		}
		if strings.HasPrefix(t.Value, "offset=") {
			p.next()
			offset, err := strconv.ParseUint(t.Value[7:], 0, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid offset: %s", t.Value)
			}
			ma.Offset = uint32(offset)
		} else if strings.HasPrefix(t.Value, "align=") {
			p.next()
			align, err := strconv.ParseUint(t.Value[6:], 0, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid align: %s", t.Value)
			}
			switch align {
			case 1:
				ma.Align = 0
			case 2:
				ma.Align = 1
			case 4:
				ma.Align = 2
			case 8:
				ma.Align = 3
			default:
				ma.Align = uint32(align)
			}
		} else {
			break
		}
	}

	ops, err := p.parseOperands(localMap, memOp.Operands)
	if err != nil {
		return nil, err
	}
	result = append(result, ops...)

	result = append(result, ast.Instr{Opcode: memOp.Opcode, Imm: ma})
	return result, nil
}

func (p *Parser) parseOperands(localMap map[string]uint32, count int) ([]ast.Instr, error) {
	var result []ast.Instr
	for i := 0; i < count; i++ {
		if t := p.peek(); t != nil && t.Type == token.LParen {
			p.next()
			ops, err := p.parseInstrs(localMap)
			if err != nil {
				return nil, err
			}
			result = append(result, ops...)
			if _, err := p.expect(token.RParen); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (p *Parser) parseBlockType() (ast.BlockType, error) {
	bt := ast.BlockType{Simple: ast.BlockTypeEmpty, TypeIdx: -1}

parseBlockLoop:
	for {
		t := p.peek()
		if t == nil || t.Type != token.LParen {
			break
		}

		saved := p.pos
		p.next()
		t, err := p.expect(token.Ident)
		if err != nil {
			return bt, err
		}

		switch t.Value {
		case "type":
			idx, err := p.parseIdx(p.typeMap)
			if err != nil {
				return bt, err
			}
			if _, err := p.expect(token.RParen); err != nil {
				return bt, err
			}
			bt.TypeIdx = int32(idx)
			if int(idx) < len(p.mod.Types) {
				ft := p.mod.Types[idx]
				bt.Params = ft.Params
				bt.Results = ft.Results
			}
			return bt, nil
		case "param":
			for {
				if pt := p.peek(); pt == nil || pt.Type == token.RParen {
					break
				}
				vt, err := p.parseValType()
				if err != nil {
					return bt, err
				}
				bt.Params = append(bt.Params, vt)
			}
			if _, err := p.expect(token.RParen); err != nil {
				return bt, err
			}
		case "result":
			for {
				if rt := p.peek(); rt == nil || rt.Type == token.RParen {
					break
				}
				vt, err := p.parseValType()
				if err != nil {
					return bt, err
				}
				bt.Results = append(bt.Results, vt)
			}
			if _, err := p.expect(token.RParen); err != nil {
				return bt, err
			}
		default:
			p.pos = saved
			break parseBlockLoop
		}
	}

	if len(bt.Params) == 0 && len(bt.Results) == 0 {
		bt.Simple = ast.BlockTypeEmpty
	} else if len(bt.Params) == 0 && len(bt.Results) == 1 {
		bt.Simple = byte(bt.Results[0])
	} else {
		ft := ast.FuncType{Params: bt.Params, Results: bt.Results}
		for i, t := range p.mod.Types {
			if t.Equal(ft) {
				bt.TypeIdx = int32(i)
				return bt, nil
			}
		}
		bt.TypeIdx = int32(len(p.mod.Types))
		p.mod.Types = append(p.mod.Types, ft)
	}

	return bt, nil
}

func (p *Parser) parseIfBody(localMap map[string]uint32) ([]ast.Instr, error) {
	var instrs []ast.Instr

	for {
		t := p.peek()
		if t == nil || t.Type == token.RParen {
			break
		}

		if t.Type == token.Ident {
			if t.Value == "else" {
				p.next()
				instrs = append(instrs, ast.Instr{Opcode: ast.OpElse})
				continue
			}
			if t.Value == "end" {
				p.next()
				return instrs, nil
			}
		}

		if t.Type == token.LParen {
			p.next()
			nested, err := p.parseInstrs(localMap)
			if err != nil {
				return nil, err
			}
			instrs = append(instrs, nested...)
			if _, err := p.expect(token.RParen); err != nil {
				return nil, err
			}
			continue
		}

		if t.Type != token.Ident {
			return nil, fmt.Errorf("expected instruction, got %v", t.Type)
		}

		p.next()
		name := t.Value

		result, err := p.parseFlatInstr(name, localMap)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, result...)
	}

	return instrs, nil
}

func (p *Parser) parseFlatInstr(name string, localMap map[string]uint32) ([]ast.Instr, error) {
	switch name {
	case "block", "loop", "if":
		return p.parseFlatBlock(name, localMap)

	case "ref.null":
		ins, err := p.parseRefNull()
		if err != nil {
			return nil, err
		}
		return []ast.Instr{ins}, nil

	case "ref.func":
		ins, err := p.parseRefFunc()
		if err != nil {
			return nil, err
		}
		return []ast.Instr{ins}, nil

	case "ref.is_null":
		return []ast.Instr{{Opcode: ast.OpRefIsNull}}, nil

	case "select":
		types, err := p.parseSelectTypes()
		if err != nil {
			return nil, err
		}
		if len(types) > 0 {
			return []ast.Instr{{Opcode: ast.OpSelectTyped, Imm: types}}, nil
		}
		return []ast.Instr{{Opcode: ast.OpSelect}}, nil

	case "table.get":
		idx := p.parseTableIdx()
		return []ast.Instr{{Opcode: ast.OpTableGet, Imm: idx}}, nil

	case "table.set":
		idx := p.parseTableIdx()
		return []ast.Instr{{Opcode: ast.OpTableSet, Imm: idx}}, nil

	case "br_table":
		labels, err := p.parseBrTableLabels()
		if err != nil {
			return nil, err
		}
		return []ast.Instr{{Opcode: ast.OpBrTable, Imm: labels}}, nil

	case "call_indirect":
		tableIdx, typeIdx, err := p.parseCallIndirectArgs()
		if err != nil {
			return nil, err
		}
		return []ast.Instr{{Opcode: ast.OpCallIndirect, Imm: []uint32{typeIdx, tableIdx}}}, nil

	case "return_call_indirect":
		tableIdx, typeIdx, err := p.parseCallIndirectArgs()
		if err != nil {
			return nil, err
		}
		return []ast.Instr{{Opcode: ast.OpReturnCallIndirect, Imm: []uint32{typeIdx, tableIdx}}}, nil
	}

	if info, ok := opcode.Lookup(name); ok {
		return p.parseSimpleInstr(name, info, localMap)
	}

	if memOp, ok := opcode.LookupMemory(name); ok {
		return p.parseMemoryInstr(memOp, localMap)
	}

	if prefOp, ok := opcode.LookupPrefixed(name); ok {
		return p.parsePrefixedInstr(name, prefOp, localMap)
	}

	return nil, fmt.Errorf("unknown instruction: %s", name)
}

func (p *Parser) parseFlatBlock(name string, localMap map[string]uint32) ([]ast.Instr, error) {
	var opcode byte
	switch name {
	case "block":
		opcode = ast.OpBlock
	case "loop":
		opcode = ast.OpLoop
	case "if":
		opcode = ast.OpIf
	}

	label := p.parseLabel()
	bt, err := p.parseBlockType()
	if err != nil {
		return nil, err
	}

	var instrs []ast.Instr
	instrs = append(instrs, ast.Instr{Opcode: opcode, Imm: bt})

	p.pushLabel(label)
	body, err := p.parseIfBody(localMap)
	p.popLabel()
	if err != nil {
		return nil, err
	}

	instrs = append(instrs, body...)
	instrs = append(instrs, ast.Instr{Opcode: ast.OpEnd})
	return instrs, nil
}

func (p *Parser) parsePrefixedInstr(name string, prefOp opcode.PrefixedOp, localMap map[string]uint32) ([]ast.Instr, error) {
	var instrs []ast.Instr
	var imm interface{} = prefOp.Subop

	switch name {
	case "memory.init":
		memIdx, dataIdx, err := p.parseDualIdx(p.memMap, p.dataMap)
		if err != nil {
			return nil, err
		}
		ops, err := p.parseOperands(localMap, 3)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, ops...)
		imm = []uint32{prefOp.Subop, dataIdx, memIdx}

	case "data.drop":
		dataIdx, err := p.parseIdx(p.dataMap)
		if err != nil {
			return nil, err
		}
		imm = []uint32{prefOp.Subop, dataIdx}

	case "table.init":
		tableIdx, elemIdx, err := p.parseDualIdx(p.tableMap, p.elemMap)
		if err != nil {
			return nil, err
		}
		ops, err := p.parseOperands(localMap, 3)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, ops...)
		imm = []uint32{prefOp.Subop, elemIdx, tableIdx}

	case "elem.drop":
		elemIdx, err := p.parseIdx(p.elemMap)
		if err != nil {
			return nil, err
		}
		imm = []uint32{prefOp.Subop, elemIdx}

	case "table.copy":
		var destTable, srcTable uint32
		if t := p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$"))) {
			idx, err := p.parseIdx(p.tableMap)
			if err != nil {
				return nil, err
			}
			destTable = idx
			srcTable, err = p.parseIdx(p.tableMap)
			if err != nil {
				return nil, err
			}
		}
		ops, err := p.parseOperands(localMap, 3)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, ops...)
		imm = []uint32{prefOp.Subop, destTable, srcTable}

	case "table.grow", "table.fill":
		var tableIdx uint32
		if t := p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$"))) {
			idx, err := p.parseIdx(p.tableMap)
			if err != nil {
				return nil, err
			}
			tableIdx = idx
		}
		opCount := 2
		if name == "table.fill" {
			opCount = 3
		}
		ops, err := p.parseOperands(localMap, opCount)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, ops...)
		imm = []uint32{prefOp.Subop, tableIdx}

	case "table.size":
		var tableIdx uint32
		if t := p.peek(); t != nil && (t.Type == token.Number || (t.Type == token.Ident && strings.HasPrefix(t.Value, "$"))) {
			idx, err := p.parseIdx(p.tableMap)
			if err != nil {
				return nil, err
			}
			tableIdx = idx
		}
		imm = []uint32{prefOp.Subop, tableIdx}

	case "memory.fill":
		var memIdx uint32
		if t := p.peek(); t != nil && t.Type == token.Ident && strings.HasPrefix(t.Value, "$") {
			if idx, ok := p.memMap[t.Value]; ok {
				p.next()
				memIdx = idx
			}
		} else if t != nil && t.Type == token.Number {
			saved := p.pos
			p.next()
			if next := p.peek(); next != nil && next.Type == token.LParen {
				val, err := strconv.ParseUint(t.Value, 0, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid memory index: %s", t.Value)
				}
				memIdx = uint32(val)
			} else {
				p.pos = saved
			}
		}
		ops, err := p.parseOperands(localMap, 3)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, ops...)
		imm = []uint32{prefOp.Subop, memIdx}

	case "memory.copy":
		destMem, srcMem, err := p.parseDualIdxPair(p.memMap)
		if err != nil {
			return nil, err
		}
		ops, err := p.parseOperands(localMap, 3)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, ops...)
		imm = []uint32{prefOp.Subop, destMem, srcMem}

	default:
		ops, err := p.parseOperands(localMap, prefOp.Operands)
		if err != nil {
			return nil, err
		}
		instrs = append(instrs, ops...)
	}

	instrs = append(instrs, ast.Instr{Opcode: ast.OpPrefixMisc, Imm: imm})
	return instrs, nil
}
