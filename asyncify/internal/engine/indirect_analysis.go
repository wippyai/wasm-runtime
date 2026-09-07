package engine

import (
	"bytes"
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TableInfo holds the static reachability state for a single table.
type TableInfo struct {
	FuncsByCanonical map[int][]uint32
	Funcs            []uint32
	IsClosed         bool
}

// ModuleIndirectAnalysis holds the result of table classification and indirect call resolution.
type ModuleIndirectAnalysis struct {
	ResolvedCallGraph CallGraph
	ConservativeRoots map[uint32]bool
	Tables            []TableInfo
}

// AnalyzeIndirectCalls performs sound, precise indirect-call suspension analysis.
//
// For provably closed internal unexported/unimported tables with known element funcrefs
// and no mutations (table.set, table.init, table.copy, table.grow, table.fill),
// it resolves possible call_indirect targets by matching structural function signatures.
//
// If a module or signature involves unsupported recursive types (TypeDefKindRec),
// subtyping hierarchies (TypeDefKindSub with parents or non-final), or extended reference/heap types
// (ExtValKindRef), the analysis conservatively falls back (marking tables open or callers as conservative roots)
// to guarantee that no valid suspending target is ever omitted.
//
// Unknown, imported, exported, or mutable tables, or call_ref instructions stay conservative
// and are recorded in ConservativeRoots so they propagate to all callers.
func AnalyzeIndirectCalls(m *wasm.Module, ignoreIndirect bool) (*ModuleIndirectAnalysis, error) {
	// Synthetic graph nodes occupy the upper half of the index space.
	if uint64(m.NumImportedFuncs())+uint64(len(m.Code)) >= uint64(GroupNodeBase) {
		return nil, fmt.Errorf("too many functions for indirect-call analysis")
	}
	numImportedTables := uint32(m.NumImportedTables())
	numDefinedTables := uint32(len(m.Tables))
	totalTables := numImportedTables + numDefinedTables

	tables := make([]TableInfo, totalTables)
	for i := uint32(0); i < totalTables; i++ {
		if i >= numImportedTables {
			tables[i].IsClosed = true
			tables[i].FuncsByCanonical = make(map[int][]uint32)
		} else {
			tables[i].IsClosed = false // imported tables are not closed
		}
	}

	// 1. Exported tables cannot be closed (external host/modules may invoke or mutate).
	for _, exp := range m.Exports {
		if exp.Kind == wasm.KindTable {
			if exp.Idx < totalTables {
				tables[exp.Idx].IsClosed = false
			}
		}
	}

	// 2. Check table initializers (if present in defined tables).
	for i, t := range m.Tables {
		tableIdx := numImportedTables + uint32(i)
		if len(t.Init) > 0 {
			_, isFunc, ok := parseElemExpr(t.Init)
			if !ok || isFunc {
				tables[tableIdx].IsClosed = false
			}
		}
	}

	// 3. Inspect element segments to collect known element funcrefs in O(1) per element.
	numImportedFuncs := uint32(m.NumImportedFuncs())
	totalFuncs := numImportedFuncs + uint32(len(m.Code))

	seenFuncs := make([]map[uint32]bool, totalTables)
	for i := range seenFuncs {
		seenFuncs[i] = make(map[uint32]bool)
	}

	for _, elem := range m.Elements {
		// Bit 0 = 0 indicates an active element segment (loaded at instantiation).
		// Bit 1 = 1 indicates passive (loaded via table.init) or declarative (flag 3 or 7).
		isActive := (elem.Flags & 0x01) == 0
		if !isActive {
			continue
		}

		targetTable := uint32(0)
		hasExplicitTable := (elem.Flags&0x02 != 0) && (elem.Flags&0x01 == 0)
		if hasExplicitTable {
			targetTable = elem.TableIdx
		}

		if targetTable >= totalTables {
			continue
		}

		usesExprs := (elem.Flags & 0x04) != 0
		if !usesExprs {
			for _, f := range elem.FuncIdxs {
				if f >= totalFuncs {
					tables[targetTable].IsClosed = false
				} else if !seenFuncs[targetTable][f] {
					seenFuncs[targetTable][f] = true
					tables[targetTable].Funcs = append(tables[targetTable].Funcs, f)
				}
			}
		} else {
			for _, expr := range elem.Exprs {
				funcIdx, isFunc, ok := parseElemExpr(expr)
				if !ok {
					tables[targetTable].IsClosed = false
				} else if isFunc {
					if funcIdx >= totalFuncs {
						tables[targetTable].IsClosed = false
					} else if !seenFuncs[targetTable][funcIdx] {
						seenFuncs[targetTable][funcIdx] = true
						tables[targetTable].Funcs = append(tables[targetTable].Funcs, funcIdx)
					}
				}
			}
		}
	}

	// Tables without known element funcrefs cannot be proven closed (conservative fallback).
	for i := range tables {
		if len(tables[i].Funcs) == 0 {
			tables[i].IsClosed = false
		}
	}

	// 4. Fall back conservatively if module uses unsupported GC recursive types or declared subtyping hierarchies.
	if hasUnsupportedGCTypes(m) {
		for i := range tables {
			tables[i].IsClosed = false
		}
	}

	// 5. Scan function bodies: detect table mutations, direct calls, call_ref, and indirect calls.
	type indirectCallSite struct {
		callerIdx uint32
		tableIdx  uint32
		typeIdx   uint32
	}

	var indirectSites []indirectCallSite
	conservativeRoots := make(map[uint32]bool)
	cg := make(CallGraph)

	for i, body := range m.Code {
		callerIdx := numImportedFuncs + uint32(i)
		directTargets := make(map[uint32]bool)
		instrs, err := wasm.DecodeInstructions(body.Code)
		if err != nil {
			return nil, fmt.Errorf("decode func %d: %w", callerIdx, err)
		}

		for _, instr := range instrs {
			switch instr.Opcode {
			case wasm.OpTableSet:
				if imm, ok := instr.Imm.(wasm.TableImm); ok {
					if imm.TableIdx < totalTables {
						tables[imm.TableIdx].IsClosed = false
					}
				} else {
					for t := range tables {
						tables[t].IsClosed = false
					}
				}
			case wasm.OpPrefixMisc:
				if imm, ok := instr.Imm.(wasm.MiscImm); ok {
					switch imm.SubOpcode {
					case wasm.MiscTableInit:
						// table.init elemIdx tableIdx -> tableIdx is imm.Operands[1]
						if len(imm.Operands) >= 2 && imm.Operands[1] < totalTables {
							tables[imm.Operands[1]].IsClosed = false
						} else {
							for t := range tables {
								tables[t].IsClosed = false
							}
						}
					case wasm.MiscTableCopy:
						// table.copy dstTable srcTable -> dstTable is imm.Operands[0]
						if len(imm.Operands) >= 1 && imm.Operands[0] < totalTables {
							tables[imm.Operands[0]].IsClosed = false
						} else {
							for t := range tables {
								tables[t].IsClosed = false
							}
						}
					case wasm.MiscTableGrow, wasm.MiscTableFill:
						// table.grow tableIdx / table.fill tableIdx -> tableIdx is imm.Operands[0]
						if len(imm.Operands) >= 1 && imm.Operands[0] < totalTables {
							tables[imm.Operands[0]].IsClosed = false
						} else {
							for t := range tables {
								tables[t].IsClosed = false
							}
						}
					}
				}
			case wasm.OpCall, wasm.OpReturnCall:
				if imm, ok := instr.Imm.(wasm.CallImm); ok {
					if !directTargets[imm.FuncIdx] {
						directTargets[imm.FuncIdx] = true
						cg[callerIdx] = append(cg[callerIdx], imm.FuncIdx)
					}
				}
			case wasm.OpCallRef, wasm.OpReturnCallRef:
				if !ignoreIndirect {
					conservativeRoots[callerIdx] = true
				}
			case wasm.OpCallIndirect, wasm.OpReturnCallIndirect:
				if !ignoreIndirect {
					if imm, ok := instr.Imm.(wasm.CallIndirectImm); ok {
						indirectSites = append(indirectSites, indirectCallSite{
							callerIdx: callerIdx,
							tableIdx:  imm.TableIdx,
							typeIdx:   imm.TypeIdx,
						})
					} else {
						conservativeRoots[callerIdx] = true
					}
				}
			}
		}
	}

	// 6. For closed tables, index functions by canonical structural type signature in O(1) per lookup.
	tc := newTypeCanonizer()
	for t := range tables {
		if tables[t].IsClosed {
			for _, f := range tables[t].Funcs {
				ft := m.GetFuncType(f)
				cid, ok := tc.getID(ft)
				if !ok {
					// Function signature uses unsupported reference / heap types or subtyping.
					// Conservatively mark table open so we never miss valid suspending targets.
					tables[t].IsClosed = false
					tables[t].FuncsByCanonical = make(map[int][]uint32)
					break
				}
				tables[t].FuncsByCanonical[cid] = append(tables[t].FuncsByCanonical[cid], f)
			}
		}
	}

	// 7. Construct group nodes for (table, canonicalType) to avoid quadratic edge multiplication.
	type groupKey struct {
		tableIdx uint32
		cid      int
	}
	groupNodes := make(map[groupKey]uint32)
	nextGroupID := GroupNodeBase

	for t := range tables {
		if tables[t].IsClosed {
			for cid, funcs := range tables[t].FuncsByCanonical {
				if len(funcs) == 0 {
					continue
				}
				gk := groupKey{tableIdx: uint32(t), cid: cid}
				gid := nextGroupID
				if gid < GroupNodeBase {
					return nil, fmt.Errorf("too many indirect-call signature groups")
				}
				nextGroupID++
				groupNodes[gk] = gid
				// Element collection already deduplicated these targets. Reusing
				// the immutable slice avoids a quadratic scan per signature group.
				cg[gid] = funcs
			}
		}
	}

	// 8. Resolve indirect call sites: connect caller to group node (deduplicated per caller).
	callerGroupEdges := make(map[uint64]bool)

	for _, site := range indirectSites {
		if site.tableIdx >= totalTables || !tables[site.tableIdx].IsClosed {
			conservativeRoots[site.callerIdx] = true
			continue
		}
		callType := getFuncTypeByIdx(m, site.typeIdx)
		if callType == nil {
			conservativeRoots[site.callerIdx] = true
			continue
		}
		cid, ok := tc.getID(callType)
		if !ok {
			// Call site signature uses unsupported reference / heap types -> conservative fallback
			conservativeRoots[site.callerIdx] = true
			continue
		}
		gk := groupKey{tableIdx: site.tableIdx, cid: cid}
		gid, exists := groupNodes[gk]
		if !exists {
			// In WebAssembly, a call_indirect whose signature does not match any table entry
			// traps at runtime, so it cannot execute any suspending function.
			continue
		}
		edgeKey := (uint64(site.callerIdx) << 32) | uint64(gid)
		if !callerGroupEdges[edgeKey] {
			callerGroupEdges[edgeKey] = true
			cg[site.callerIdx] = append(cg[site.callerIdx], gid)
		}
	}

	return &ModuleIndirectAnalysis{
		Tables:            tables,
		ConservativeRoots: conservativeRoots,
		ResolvedCallGraph: cg,
	}, nil
}

func parseElemExpr(expr []byte) (funcIdx uint32, isFunc bool, ok bool) {
	if len(expr) < 2 {
		return 0, false, false
	}
	r := bytes.NewReader(expr)
	op, err := r.ReadByte()
	if err != nil {
		return 0, false, false
	}
	switch op {
	case wasm.OpRefNull:
		_, err := wasm.ReadLEB128s64(r)
		if err != nil {
			return 0, false, false
		}
		end, err := r.ReadByte()
		if err != nil || end != wasm.OpEnd || r.Len() > 0 {
			return 0, false, false
		}
		return 0, false, true
	case wasm.OpRefFunc:
		f, err := wasm.ReadLEB128u(r)
		if err != nil {
			return 0, false, false
		}
		end, err := r.ReadByte()
		if err != nil || end != wasm.OpEnd || r.Len() > 0 {
			return 0, false, false
		}
		return f, true, true
	default:
		return 0, false, false
	}
}

// hasUnsupportedGCTypes reports whether the module uses recursive type groups
// or declared subtyping hierarchies that require full GC subtype checking.
func hasUnsupportedGCTypes(m *wasm.Module) bool {
	for i := range m.TypeDefs {
		td := &m.TypeDefs[i]
		switch td.Kind {
		case wasm.TypeDefKindRec:
			return true
		case wasm.TypeDefKindSub:
			if td.Sub != nil && (len(td.Sub.Parents) > 0 || !td.Sub.Final) {
				return true
			}
		}
	}
	return false
}

// funcTypeSimpleSignature extracts the structural parameter and result value types.
// If any parameter or result contains extended reference / heap types (ExtValKindRef),
// it returns ok = false to signal that the signature cannot be treated as a simple
// structural signature and must conservatively fall back.
func funcTypeSimpleSignature(ft *wasm.FuncType) (params []wasm.ValType, results []wasm.ValType, ok bool) {
	if ft == nil {
		return nil, nil, false
	}

	if len(ft.ExtParams) > 0 {
		params = make([]wasm.ValType, len(ft.ExtParams))
		for i, p := range ft.ExtParams {
			if p.Kind == wasm.ExtValKindRef {
				return nil, nil, false
			}
			params[i] = p.ValType
		}
	} else {
		params = ft.Params
	}

	// Check ExtResults as well (fixes bug where result-only extended types were missed!)
	if len(ft.ExtResults) > 0 {
		results = make([]wasm.ValType, len(ft.ExtResults))
		for i, r := range ft.ExtResults {
			if r.Kind == wasm.ExtValKindRef {
				return nil, nil, false
			}
			results[i] = r.ValType
		}
	} else {
		results = ft.Results
	}

	return params, results, true
}

type signatureKey struct {
	params  string
	results string
}

type typeCanonizer struct {
	typeMap map[signatureKey]int
	types   []*wasm.FuncType
}

func newTypeCanonizer() *typeCanonizer {
	return &typeCanonizer{
		typeMap: make(map[signatureKey]int),
	}
}

func (tc *typeCanonizer) getID(ft *wasm.FuncType) (int, bool) {
	if ft == nil {
		return -1, false
	}
	params, results, ok := funcTypeSimpleSignature(ft)
	if !ok {
		return -1, false
	}
	// A byte separator is ambiguous: f64's value-type byte is itself '|'.
	key := signatureKey{params: string(params), results: string(results)}
	if id, exists := tc.typeMap[key]; exists {
		return id, true
	}
	id := len(tc.types)
	tc.types = append(tc.types, ft)
	tc.typeMap[key] = id
	return id, true
}

func getFuncTypeByIdx(m *wasm.Module, typeIdx uint32) *wasm.FuncType {
	if len(m.TypeDefs) > 0 {
		flatIdx := uint32(0)
		for i := range m.TypeDefs {
			td := &m.TypeDefs[i]
			switch td.Kind {
			case wasm.TypeDefKindFunc:
				if flatIdx == typeIdx {
					return td.Func
				}
				flatIdx++
			case wasm.TypeDefKindSub:
				if flatIdx == typeIdx {
					if td.Sub != nil && td.Sub.CompType.Kind == wasm.CompKindFunc {
						return td.Sub.CompType.Func
					}
					return nil
				}
				flatIdx++
			case wasm.TypeDefKindRec:
				if td.Rec != nil {
					for j := range td.Rec.Types {
						if flatIdx == typeIdx {
							if td.Rec.Types[j].CompType.Kind == wasm.CompKindFunc {
								return td.Rec.Types[j].CompType.Func
							}
							return nil
						}
						flatIdx++
					}
				}
			}
		}
		return nil
	}

	if int(typeIdx) >= len(m.Types) {
		return nil
	}
	return &m.Types[typeIdx]
}
