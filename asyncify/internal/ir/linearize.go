package ir

import "github.com/wippyai/wasm-runtime/wasm"

// LinearizeConfig holds configuration for linearization.
type LinearizeConfig struct {
	AsyncFuncs     map[uint32]bool
	Module         *wasm.Module
	AllocLocal     func(wasm.ValType) uint32
	StateGlobal    uint32
	StateRewinding int32
}

// Linearize transforms an IR tree for asyncify support.
// It converts result-bearing control flow with async calls into
// linearized form that can be rewound properly.
func Linearize(tree Node, config *LinearizeConfig) []wasm.Instruction {
	l := &linearizer{
		config: config,
	}
	return l.emit(tree)
}

type linearizer struct {
	config *LinearizeConfig
}

func (l *linearizer) emit(node Node) []wasm.Instruction {
	switch n := node.(type) {
	case *SeqNode:
		return l.emitSeq(n)
	case *BlockNode:
		return l.emitBlock(n)
	case *IfNode:
		return l.emitIf(n)
	case *InstrNode:
		return []wasm.Instruction{n.Instr}
	}
	return nil
}

func (l *linearizer) emitSeq(seq *SeqNode) []wasm.Instruction {
	var result []wasm.Instruction
	for _, child := range seq.Children {
		result = append(result, l.emit(child)...)
	}
	return result
}

func (l *linearizer) emitBlock(block *BlockNode) []wasm.Instruction {
	hasAsync := branchHasAsync(block.Body, l.config.AsyncFuncs)
	hasResult := len(block.ResultTypes) > 0
	hasParams := len(block.ParamTypes) > 0

	if !hasAsync && !hasResult && !hasParams {
		// No async, no result, no params - emit as-is
		var result []wasm.Instruction
		result = append(result, wasm.Instruction{
			Opcode: block.Opcode,
			Imm:    block.Imm,
		})
		result = append(result, l.emit(block.Body)...)
		result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})
		return result
	}

	// Allocate param locals (params are consumed from parent stack)
	var paramLocals []uint32
	for _, pt := range block.ParamTypes {
		paramLocals = append(paramLocals, l.config.AllocLocal(pt))
	}

	// Allocate result locals
	var resultLocals []uint32
	for _, rt := range block.ResultTypes {
		resultLocals = append(resultLocals, l.config.AllocLocal(rt))
	}

	var result []wasm.Instruction

	// Save params from stack (reverse order - last param is on top)
	for i := len(paramLocals) - 1; i >= 0; i-- {
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalSet,
			Imm:    wasm.LocalImm{LocalIdx: paramLocals[i]},
		})
	}

	// Emit void block/loop
	result = append(result, wasm.Instruction{
		Opcode: block.Opcode,
		Imm:    wasm.BlockImm{Type: -64}, // void
	})

	// Load params at start of block body (in order)
	for _, pl := range paramLocals {
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: pl},
		})
	}

	// Emit body
	body := l.emit(block.Body)
	body = transformBranches(body, 0, resultLocals, l.config.AllocLocal)
	result = append(result, body...)

	// Store results (reverse order - last on top of stack)
	for i := len(resultLocals) - 1; i >= 0; i-- {
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalSet,
			Imm:    wasm.LocalImm{LocalIdx: resultLocals[i]},
		})
	}

	result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})

	// Load results (in order)
	for _, rl := range resultLocals {
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: rl},
		})
	}

	return result
}

func (l *linearizer) emitIf(ifNode *IfNode) []wasm.Instruction {
	thenHasAsync := branchHasAsync(ifNode.Then, l.config.AsyncFuncs)
	elseHasAsync := ifNode.Else != nil && branchHasAsync(ifNode.Else, l.config.AsyncFuncs)
	hasResult := len(ifNode.ResultTypes) > 0
	hasParams := len(ifNode.ParamTypes) > 0
	hasAsync := thenHasAsync || elseHasAsync

	if !hasAsync && !hasResult && !hasParams {
		// No async, no result, no params - emit as-is
		var result []wasm.Instruction
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpIf,
			Imm:    ifNode.Imm,
		})
		result = append(result, l.emit(ifNode.Then)...)
		if ifNode.Else != nil {
			result = append(result, wasm.Instruction{Opcode: wasm.OpElse})
			result = append(result, l.emit(ifNode.Else)...)
		}
		result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})
		return result
	}

	// Need linearization: save condition and emit as two separate ifs
	condLocal := l.config.AllocLocal(wasm.ValI32)

	// Allocate param locals (params are below condition on stack)
	var paramLocals []uint32
	for _, pt := range ifNode.ParamTypes {
		paramLocals = append(paramLocals, l.config.AllocLocal(pt))
	}

	var resultLocals []uint32
	for _, rt := range ifNode.ResultTypes {
		resultLocals = append(resultLocals, l.config.AllocLocal(rt))
	}

	var result []wasm.Instruction

	// Save condition first (it's on top of stack), but only during normal execution.
	// During rewind, the condition local was already restored by the prelude,
	// so we must not overwrite it with a potentially different recomputed value.
	// if (rewinding) { drop } else { local.set $cond }
	result = append(result, l.emitRewindingCheck()...)
	result = append(result, wasm.Instruction{
		Opcode: wasm.OpIf,
		Imm:    wasm.BlockImm{Type: -64}, // void
	})
	result = append(result, wasm.Instruction{Opcode: wasm.OpDrop})
	result = append(result, wasm.Instruction{Opcode: wasm.OpElse})
	result = append(result, wasm.Instruction{
		Opcode: wasm.OpLocalSet,
		Imm:    wasm.LocalImm{LocalIdx: condLocal},
	})
	result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})

	// Save params from stack (reverse order - last param was below condition)
	// Also guard with rewinding check to preserve restored values during rewind
	if len(paramLocals) > 0 {
		result = append(result, l.emitRewindingCheck()...)
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpIf,
			Imm:    wasm.BlockImm{Type: -64}, // void
		})
		// During rewind: drop the params (they're restored from memory)
		for range paramLocals {
			result = append(result, wasm.Instruction{Opcode: wasm.OpDrop})
		}
		result = append(result, wasm.Instruction{Opcode: wasm.OpElse})
		// During normal: save params to locals
		for i := len(paramLocals) - 1; i >= 0; i-- {
			result = append(result, wasm.Instruction{
				Opcode: wasm.OpLocalSet,
				Imm:    wasm.LocalImm{LocalIdx: paramLocals[i]},
			})
		}
		result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})
	}

	// Then branch condition
	// When BOTH branches have async: just use saved condition (determines which to resume)
	// When only then has async: rewinding || cond (must enter to resume)
	// When only else has async: !rewinding && cond (skip during rewind)
	if thenHasAsync && elseHasAsync {
		// Both have async: just use saved condition
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: condLocal},
		})
	} else if thenHasAsync {
		// rewinding || cond
		result = append(result, l.emitRewindingCheck()...)
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: condLocal},
		})
		result = append(result, wasm.Instruction{Opcode: wasm.OpI32Or})
	} else {
		// !rewinding && cond
		result = append(result, l.emitNotRewindingCheck()...)
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: condLocal},
		})
		result = append(result, wasm.Instruction{Opcode: wasm.OpI32And})
	}

	result = append(result, wasm.Instruction{
		Opcode: wasm.OpIf,
		Imm:    wasm.BlockImm{Type: -64}, // void
	})

	// Load params at start of then body
	for _, pl := range paramLocals {
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: pl},
		})
	}

	// Then body
	thenBody := l.emit(ifNode.Then)
	thenBody = transformBranches(thenBody, 0, resultLocals, l.config.AllocLocal)
	result = append(result, thenBody...)

	// Store then results (reverse order)
	for i := len(resultLocals) - 1; i >= 0; i-- {
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalSet,
			Imm:    wasm.LocalImm{LocalIdx: resultLocals[i]},
		})
	}

	result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})

	// Else branch (if present)
	if ifNode.Else != nil {
		// When BOTH branches have async: just use !cond (determines which to resume)
		// When only else has async: rewinding || !cond (must enter to resume)
		// When neither has async: !rewinding && !cond (skip during rewind)
		if thenHasAsync && elseHasAsync {
			// Both have async: just use negated saved condition
			result = append(result, wasm.Instruction{
				Opcode: wasm.OpLocalGet,
				Imm:    wasm.LocalImm{LocalIdx: condLocal},
			})
			result = append(result, wasm.Instruction{Opcode: wasm.OpI32Eqz})
		} else if elseHasAsync {
			// rewinding || !cond
			result = append(result, l.emitRewindingCheck()...)
			result = append(result, wasm.Instruction{
				Opcode: wasm.OpLocalGet,
				Imm:    wasm.LocalImm{LocalIdx: condLocal},
			})
			result = append(result, wasm.Instruction{Opcode: wasm.OpI32Eqz})
			result = append(result, wasm.Instruction{Opcode: wasm.OpI32Or})
		} else {
			// !rewinding && !cond
			result = append(result, l.emitNotRewindingCheck()...)
			result = append(result, wasm.Instruction{
				Opcode: wasm.OpLocalGet,
				Imm:    wasm.LocalImm{LocalIdx: condLocal},
			})
			result = append(result, wasm.Instruction{Opcode: wasm.OpI32Eqz})
			result = append(result, wasm.Instruction{Opcode: wasm.OpI32And})
		}

		result = append(result, wasm.Instruction{
			Opcode: wasm.OpIf,
			Imm:    wasm.BlockImm{Type: -64}, // void
		})

		// Load params at start of else body
		for _, pl := range paramLocals {
			result = append(result, wasm.Instruction{
				Opcode: wasm.OpLocalGet,
				Imm:    wasm.LocalImm{LocalIdx: pl},
			})
		}

		// Else body
		elseBody := l.emit(ifNode.Else)
		elseBody = transformBranches(elseBody, 0, resultLocals, l.config.AllocLocal)
		result = append(result, elseBody...)

		// Store else results (reverse order)
		for i := len(resultLocals) - 1; i >= 0; i-- {
			result = append(result, wasm.Instruction{
				Opcode: wasm.OpLocalSet,
				Imm:    wasm.LocalImm{LocalIdx: resultLocals[i]},
			})
		}

		result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})
	}

	// Load results (in order)
	for _, rl := range resultLocals {
		result = append(result, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: rl},
		})
	}

	return result
}

// emitRewindingCheck emits: global.get $state; i32.const $rewinding; i32.eq
func (l *linearizer) emitRewindingCheck() []wasm.Instruction {
	return []wasm.Instruction{
		{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: l.config.StateGlobal}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: l.config.StateRewinding}},
		{Opcode: wasm.OpI32Eq},
	}
}

// emitNotRewindingCheck emits: global.get $state; i32.const $rewinding; i32.ne
func (l *linearizer) emitNotRewindingCheck() []wasm.Instruction {
	return []wasm.Instruction{
		{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: l.config.StateGlobal}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: l.config.StateRewinding}},
		{Opcode: wasm.OpI32Ne},
	}
}

// branchHasAsync checks if a node contains async calls
func branchHasAsync(node Node, asyncFuncs map[uint32]bool) bool {
	switch n := node.(type) {
	case *SeqNode:
		for _, child := range n.Children {
			if branchHasAsync(child, asyncFuncs) {
				return true
			}
		}
	case *BlockNode:
		return branchHasAsync(n.Body, asyncFuncs)
	case *IfNode:
		if branchHasAsync(n.Then, asyncFuncs) {
			return true
		}
		if n.Else != nil && branchHasAsync(n.Else, asyncFuncs) {
			return true
		}
	case *InstrNode:
		if n.Instr.Opcode == wasm.OpCall {
			if imm, ok := n.Instr.Imm.(wasm.CallImm); ok {
				return asyncFuncs[imm.FuncIdx]
			}
		}
		if n.Instr.Opcode == wasm.OpCallIndirect {
			return true // indirect calls are considered async
		}
		if n.Instr.Opcode == wasm.OpCallRef {
			return true // call_ref is async (dynamic dispatch)
		}
	}
	return false
}

// transformBranches adds local.set before br/br_if/br_table instructions
// that target the specified depth (for result-bearing blocks flattened to void).
func transformBranches(instrs []wasm.Instruction, targetDepth uint32, resultLocals []uint32, allocLocal func(wasm.ValType) uint32) []wasm.Instruction {
	if len(resultLocals) == 0 {
		return instrs
	}

	var result []wasm.Instruction
	depth := uint32(0)

	for i := 0; i < len(instrs); i++ {
		instr := instrs[i]

		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
			depth++
			result = append(result, instr)

		case wasm.OpEnd:
			if depth > 0 {
				depth--
			}
			result = append(result, instr)

		case wasm.OpBr:
			imm := instr.Imm.(wasm.BranchImm)
			if imm.LabelIdx == depth+targetDepth {
				// br targets our flattened block - store values first
				for j := len(resultLocals) - 1; j >= 0; j-- {
					result = append(result, wasm.Instruction{
						Opcode: wasm.OpLocalSet,
						Imm:    wasm.LocalImm{LocalIdx: resultLocals[j]},
					})
				}
			}
			result = append(result, instr)

		case wasm.OpBrIf:
			imm := instr.Imm.(wasm.BranchImm)
			if imm.LabelIdx == depth+targetDepth && len(resultLocals) > 0 {
				// br_if with value to our block: stack is [values..., cond]
				// If branch taken: values become block result
				// If branch not taken: values must remain on stack for subsequent code
				// Transform to: save cond, save values, reload cond, br_if, reload values
				condLocal := allocLocal(wasm.ValI32)
				// Save condition
				result = append(result, wasm.Instruction{
					Opcode: wasm.OpLocalSet,
					Imm:    wasm.LocalImm{LocalIdx: condLocal},
				})
				// Store result values
				for j := len(resultLocals) - 1; j >= 0; j-- {
					result = append(result, wasm.Instruction{
						Opcode: wasm.OpLocalSet,
						Imm:    wasm.LocalImm{LocalIdx: resultLocals[j]},
					})
				}
				// Reload condition and branch
				result = append(result, wasm.Instruction{
					Opcode: wasm.OpLocalGet,
					Imm:    wasm.LocalImm{LocalIdx: condLocal},
				})
				result = append(result, instr)
				// Reload values for fallthrough path
				for _, rl := range resultLocals {
					result = append(result, wasm.Instruction{
						Opcode: wasm.OpLocalGet,
						Imm:    wasm.LocalImm{LocalIdx: rl},
					})
				}
				continue
			}
			result = append(result, instr)

		case wasm.OpBrTable:
			// br_table may target our block - check all labels
			imm := instr.Imm.(wasm.BrTableImm)
			targetsOurs := false
			for _, lbl := range imm.Labels {
				if lbl == depth+targetDepth {
					targetsOurs = true
					break
				}
			}
			if imm.Default == depth+targetDepth {
				targetsOurs = true
			}
			if targetsOurs && len(resultLocals) > 0 {
				// br_table with value: stack is [values..., index]
				// br_table always branches (no fallthrough), so we just store values
				indexLocal := allocLocal(wasm.ValI32)
				// Save index
				result = append(result, wasm.Instruction{
					Opcode: wasm.OpLocalSet,
					Imm:    wasm.LocalImm{LocalIdx: indexLocal},
				})
				// Store values
				for j := len(resultLocals) - 1; j >= 0; j-- {
					result = append(result, wasm.Instruction{
						Opcode: wasm.OpLocalSet,
						Imm:    wasm.LocalImm{LocalIdx: resultLocals[j]},
					})
				}
				// Reload index and branch
				result = append(result, wasm.Instruction{
					Opcode: wasm.OpLocalGet,
					Imm:    wasm.LocalImm{LocalIdx: indexLocal},
				})
			}
			result = append(result, instr)

		default:
			result = append(result, instr)
		}
	}

	return result
}
