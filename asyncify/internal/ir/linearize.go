package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// LinearizeConfig holds configuration for linearization.
type LinearizeConfig struct {
	AllocLocal     func(wasm.ValType) uint32
	StateGlobal    uint32
	StateRewinding int32
}

// Linearize lowers the complete validated ValuePlan for source-analysis and
// raw action-contract support. It deliberately retains validation-only source
// tails after unreachable control. Runtime callers must first NormalizeSource
// and then call LinearizeNormalized. Returned instructions exclude the final
// function end, which the engine supplies when constructing its enclosing
// continuation control flow.
func Linearize(values *ValuePlan, config *LinearizeConfig) (*LoweredControl, error) {
	if values == nil {
		return nil, fmt.Errorf("asyncify: missing source value plan")
	}
	// A ValuePlan may retain opaque operations so analysis can describe the
	// source faithfully, but even raw action lowering must not instantiate an
	// operation whose control transfers have no typed source-edge contract.
	// Keep this boundary ahead of storage allocation and instruction emission
	// until those operations acquire explicit actions.
	if values.opaqueControl {
		return nil, fmt.Errorf("asyncify: unsupported unmodeled control transfer")
	}
	if err := values.verifyTransfers(); err != nil {
		return nil, err
	}
	lowered, err := linearizeWithStorage(values.control, config, true, values)
	if err != nil {
		return nil, err
	}
	lowered.values = values
	if err := values.bindContinuations(lowered); err != nil {
		return nil, err
	}
	return lowered, nil
}

// LinearizeNormalized is the runtime lowering entry point. It lowers the
// checked executable source view while retaining the complete validated source
// plan as the owner of value identities.
func LinearizeNormalized(source *NormalizedSource, config *LinearizeConfig) (*LoweredControl, error) {
	if source == nil || source.values == nil {
		return nil, fmt.Errorf("asyncify: missing normalized source")
	}
	if err := source.values.verifyTransfers(); err != nil {
		return nil, err
	}
	if err := source.verifyValueClosure(); err != nil {
		return nil, err
	}
	lowered, err := linearizeWithStorage(source.values.control, config, true, source.values, source)
	if err != nil {
		return nil, err
	}
	if err := source.values.bindContinuations(lowered); err != nil {
		return nil, err
	}
	return lowered, nil
}

// linearizeControl is the unbound structural lowering primitive. It bypasses
// ValuePlan ownership and is limited to structural unit tests.
func linearizeControl(analysis *Analysis, config *LinearizeConfig) (*LoweredControl, error) {
	return linearizeWithStorage(analysis, config, false, nil)
}

func linearizeWithStorage(analysis *Analysis, config *LinearizeConfig, reuse bool, values *ValuePlan, normalized ...*NormalizedSource) (*LoweredControl, error) {
	if analysis == nil || config == nil || config.AllocLocal == nil {
		return nil, fmt.Errorf("asyncify: incomplete control lowering inputs")
	}
	var source *NormalizedSource
	if len(normalized) > 0 {
		source = normalized[0]
	}
	storage, err := planControlStorage(analysis, config.AllocLocal, reuse, source)
	if err != nil {
		return nil, err
	}
	if values != nil {
		if err := storage.bindSourcePorts(values); err != nil {
			return nil, err
		}
	}
	l := &linearizer{source: source, config: routingConfig{StateGlobal: config.StateGlobal, StateRewinding: config.StateRewinding}, analysis: analysis, storage: storage, values: values}
	l.emit(storage.root)
	if l.err != nil {
		return nil, l.err
	}
	lowered, err := analysis.bindLoweredCalls(&l.output, values, storage, config)
	if err != nil {
		return nil, err
	}
	if err := storage.bindSavedCarriers(analysis, lowered, reuse); err != nil {
		return nil, err
	}
	return lowered, nil
}

type controlFrame struct {
	targetLocals []uint32
	targetTypes  []wasm.ValType
	label        LabelID
	opcode       byte
}

// routingConfig deliberately has no allocator: emission consumes fixed carriers.
type routingConfig struct {
	StateGlobal    uint32
	StateRewinding int32
}

type linearizer struct {
	source   *NormalizedSource
	values   *ValuePlan
	output   loweringOutput
	storage  *controlStorage
	err      error
	analysis *Analysis
	frames   []controlFrame
	config   routingConfig
}

func (l *linearizer) emit(node Node) {
	if l.source != nil && node != l.storage.root && !l.source.Active(node) {
		return
	}
	previous := l.output.owner
	l.output.owner = node
	if node == l.storage.root {
		l.output.owner = l.analysis.root
	}
	defer func() { l.output.owner = previous }()

	switch n := node.(type) {
	case *SeqNode:
		l.emitSeq(n)
	case *BlockNode:
		l.emitBlock(n)
	case *IfNode:
		l.emitIf(n)
	case *InstrNode:
		l.emitInstr(n)
	}
}

func (l *linearizer) emitInstr(node *InstrNode) {
	instr, err := wasm.CloneInstruction(node.Instr)
	if err != nil {
		if l.err == nil {
			l.err = err
		}
		return
	}
	if instr.Opcode == wasm.OpReturn && l.values != nil {
		operation, err := l.newSourceReturn(node)
		if err != nil {
			l.err = err
			return
		}
		l.output.sourceReturn(operation)
		return
	}
	if (instr.Opcode == wasm.OpBr || instr.Opcode == wasm.OpBrIf || instr.Opcode == wasm.OpBrTable) && l.values != nil {
		operation, err := l.newSourceBranch(node)
		if err != nil {
			l.err = err
			return
		}
		l.output.sourceBranch(operation)
		return
	}
	if instr.Opcode == wasm.OpUnreachable && l.values != nil {
		operation, err := l.newSourceTrap(node)
		if err != nil {
			l.err = err
			return
		}
		l.output.sourceTrap(operation)
		return
	}
	switch instr.Opcode {
	case wasm.OpBr:
		if len(node.branchTargets) != 1 {
			l.err = fmt.Errorf("asyncify: branch has inconsistent source targets")
			return
		}
		frameIdx := l.targetFrame(node, 0)
		if frameIdx < 0 {
			return
		}
		instr.Imm = wasm.BranchImm{LabelIdx: uint32(len(l.frames) - 1 - frameIdx)}
		if frameIdx >= 0 && frameIdx < len(l.frames) {
			frame := l.frames[frameIdx]
			if len(frame.targetLocals) > 0 {
				for j := len(frame.targetLocals) - 1; j >= 0; j-- {
					l.output.guestCarrier(wasm.Instruction{
						Opcode: wasm.OpLocalSet,
						Imm:    wasm.LocalImm{LocalIdx: frame.targetLocals[j]},
					})
				}
				l.output.source(node, instr)
				return
			}
		}
		l.output.source(node, instr)
		return

	case wasm.OpBrIf:
		if len(node.branchTargets) != 1 {
			l.err = fmt.Errorf("asyncify: branch has inconsistent source targets")
			return
		}
		frameIdx := l.targetFrame(node, 0)
		if frameIdx < 0 {
			return
		}
		instr.Imm = wasm.BranchImm{LabelIdx: uint32(len(l.frames) - 1 - frameIdx)}
		if frameIdx >= 0 && frameIdx < len(l.frames) {
			frame := l.frames[frameIdx]
			if len(frame.targetLocals) > 0 {
				condLocal := l.carriers(node).selector
				l.output.guestCarrier(wasm.Instruction{
					Opcode: wasm.OpLocalSet,
					Imm:    wasm.LocalImm{LocalIdx: condLocal},
				})
				for j := len(frame.targetLocals) - 1; j >= 0; j-- {
					l.output.guestCarrier(wasm.Instruction{
						Opcode: wasm.OpLocalSet,
						Imm:    wasm.LocalImm{LocalIdx: frame.targetLocals[j]},
					})
				}
				l.output.guestCarrier(wasm.Instruction{
					Opcode: wasm.OpLocalGet,
					Imm:    wasm.LocalImm{LocalIdx: condLocal},
				})
				l.output.source(node, instr)
				for _, tl := range frame.targetLocals {
					l.output.guestCarrier(wasm.Instruction{
						Opcode: wasm.OpLocalGet,
						Imm:    wasm.LocalImm{LocalIdx: tl},
					})
				}
				return
			}
		}
		l.output.source(node, instr)
		return

	case wasm.OpBrTable:
		imm := instr.Imm.(wasm.BrTableImm)
		if len(node.branchTargets) != len(imm.Labels)+1 {
			l.err = fmt.Errorf("asyncify: branch table has inconsistent source targets")
			return
		}
		targetFrames := make([]int, len(node.branchTargets))
		for position := range node.branchTargets {
			frameIndex := l.targetFrame(node, position)
			if frameIndex < 0 {
				return
			}
			targetFrames[position] = frameIndex
			depth := uint32(len(l.frames) - 1 - frameIndex)
			if position < len(imm.Labels) {
				imm.Labels[position] = depth
			} else {
				imm.Default = depth
			}
		}
		instr.Imm = imm
		var targetTypes []wasm.ValType
		for targetIndex := range imm.Labels {
			fIdx := targetFrames[targetIndex]
			if fIdx >= 0 && fIdx < len(l.frames) && len(l.frames[fIdx].targetTypes) > 0 {
				targetTypes = l.frames[fIdx].targetTypes
				break
			}
		}
		if len(targetTypes) == 0 {
			fIdx := targetFrames[len(imm.Labels)]
			if fIdx >= 0 && fIdx < len(l.frames) && len(l.frames[fIdx].targetTypes) > 0 {
				targetTypes = l.frames[fIdx].targetTypes
			}
		}

		if len(targetTypes) > 0 {
			indexLocal := l.carriers(node).selector
			l.output.guestCarrier(wasm.Instruction{
				Opcode: wasm.OpLocalSet,
				Imm:    wasm.LocalImm{LocalIdx: indexLocal},
			})
			tempLocals := l.carriers(node).branchValues
			for j := len(targetTypes) - 1; j >= 0; j-- {
				l.output.guestCarrier(wasm.Instruction{
					Opcode: wasm.OpLocalSet,
					Imm:    wasm.LocalImm{LocalIdx: tempLocals[j]},
				})
			}

			visited := make(map[int]bool)
			for targetIndex := range node.branchTargets {
				fIdx := targetFrames[targetIndex]
				if fIdx >= 0 && fIdx < len(l.frames) && !visited[fIdx] {
					visited[fIdx] = true
					frame := l.frames[fIdx]
					if len(frame.targetLocals) == len(targetTypes) {
						for j, tl := range frame.targetLocals {
							l.output.guestCarrier(wasm.Instruction{
								Opcode: wasm.OpLocalGet,
								Imm:    wasm.LocalImm{LocalIdx: tempLocals[j]},
							})
							l.output.guestCarrier(wasm.Instruction{
								Opcode: wasm.OpLocalSet,
								Imm:    wasm.LocalImm{LocalIdx: tl},
							})
						}
					}
				}
			}

			l.output.guestCarrier(wasm.Instruction{
				Opcode: wasm.OpLocalGet,
				Imm:    wasm.LocalImm{LocalIdx: indexLocal},
			})
			l.output.source(node, instr)
			return
		}
		l.output.source(node, instr)
		return

	default:
		l.output.source(node, instr)
		return
	}
}

func (l *linearizer) emitSeq(seq *SeqNode) {
	for _, child := range seq.Children {
		l.emit(child)
	}
}

func (l *linearizer) emitBlock(block *BlockNode) {
	hasAsync := l.maySuspend(block.Body)
	hasResult := len(block.ResultTypes) > 0
	hasParams := len(block.ParamTypes) > 0

	if !hasAsync && !hasResult && !hasParams {
		// No async, no result, no params - emit as-is
		l.output.guestStructure(wasm.Instruction{
			Opcode: block.Opcode,
			Imm:    block.Imm,
		})
		l.frames = append(l.frames, controlFrame{opcode: block.Opcode, label: block.label})
		l.emit(block.Body)
		l.frames = l.frames[:len(l.frames)-1]
		l.output.guestStructure(wasm.Instruction{Opcode: wasm.OpEnd})
		return
	}

	carriers := l.carriers(block)
	paramLocals, resultLocals := carriers.params, carriers.results

	// Save params from stack (reverse order - last param is on top)
	l.emitScopeEntry(block.label, paramLocals)

	// Emit void block/loop
	l.output.guestStructure(wasm.Instruction{
		Opcode: block.Opcode,
		Imm:    wasm.BlockImm{Type: -64}, // void
	})

	// Load params at start of block body (in order)
	l.emitPortReadGroup(block.label, ControlParameter, NoArm, paramLocals)

	var targetLocals []uint32
	var targetTypes []wasm.ValType
	if block.Opcode == wasm.OpLoop {
		targetLocals = paramLocals
		targetTypes = block.ParamTypes
	} else {
		targetLocals = resultLocals
		targetTypes = block.ResultTypes
	}

	// Emit body
	l.frames = append(l.frames, controlFrame{
		label:        block.label,
		opcode:       block.Opcode,
		targetLocals: targetLocals,
		targetTypes:  targetTypes,
	})
	bodyStart := len(l.output.actions)
	l.emit(block.Body)
	body := l.output.actions[bodyStart:]
	l.frames = l.frames[:len(l.frames)-1]

	// Production derives result movement from the immutable scope exit fact.
	// The structural primitive retains its byte-oriented legacy behavior.
	if l.values != nil && len(resultLocals) > 0 {
		owner := Node(block)
		if block == l.storage.root && block != l.values.control.root {
			owner = l.values.control.root // materialized function wrapper, scope 0
		}
		if l.lexicalExit(owner, NoArm) {
			transfer, err := l.newScopeResultTransfer(owner, block.label, NoArm, resultLocals)
			if err != nil {
				l.err = err
				return
			}
			l.output.scopeResultTransfer(transfer)
		}
	} else {
		exitOperands := l.exitOperandCount(block.label, NoArm, body, len(resultLocals))
		for i := len(resultLocals) - 1; i >= len(resultLocals)-exitOperands; i-- {
			l.output.guestCarrier(wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: resultLocals[i]}})
		}
	}

	l.output.guestStructure(wasm.Instruction{Opcode: wasm.OpEnd})

	// Load results (in order)
	l.emitPortReadGroup(block.label, ControlResult, NoArm, resultLocals)
}

func (l *linearizer) emitIf(ifNode *IfNode) {
	thenHasAsync := l.maySuspend(ifNode.Then)
	elseHasAsync := ifNode.Else != nil && l.maySuspend(ifNode.Else)
	hasResult := len(ifNode.ResultTypes) > 0
	hasParams := len(ifNode.ParamTypes) > 0
	hasAsync := thenHasAsync || elseHasAsync

	if !hasAsync && !hasResult && !hasParams {
		// No async, no result, no params - emit as-is
		l.output.guestStructure(wasm.Instruction{
			Opcode: wasm.OpIf,
			Imm:    ifNode.Imm,
		})
		l.frames = append(l.frames, controlFrame{opcode: wasm.OpIf, label: ifNode.label})
		l.emit(ifNode.Then)
		l.frames = l.frames[:len(l.frames)-1]
		if ifNode.Else != nil {
			l.output.guestStructure(wasm.Instruction{Opcode: wasm.OpElse})
			l.frames = append(l.frames, controlFrame{opcode: wasm.OpIf, label: ifNode.label})
			l.emit(ifNode.Else)
			l.frames = l.frames[:len(l.frames)-1]
		}
		l.output.guestStructure(wasm.Instruction{Opcode: wasm.OpEnd})
		return
	}

	if !hasAsync {
		l.emitIfNonAsync(ifNode)
		return
	}

	// Routing consumes the carriers already assigned to this source if.
	carriers := l.carriers(ifNode)
	condLocal := carriers.selector
	paramLocals, resultLocals := carriers.params, carriers.results

	if l.values != nil {
		capture, err := l.newAsyncIfEntryCapture(ifNode, carriers)
		if err != nil {
			l.err = err
			return
		}
		l.output.asyncIfEntryCapture(capture)
	} else {
		// linearizeControl is a test-only structural primitive. Production enters
		// through Linearize and cannot emit this unbound legacy expansion.
		l.emitStructuralAsyncIfEntry(condLocal, paramLocals)
	}

	// Then branch condition
	// When BOTH branches have async: just use saved condition (determines which to resume)
	// When only then has async: rewinding || cond (must enter to resume)
	// When only else has async: !rewinding && cond (skip during rewind)
	if thenHasAsync && elseHasAsync {
		// Both have async: just use saved condition
		l.emitSelector(ifNode, SourceSelectorRoute, ThenArm)
	} else if thenHasAsync {
		// rewinding || cond
		l.emitRewindingCheck()
		l.emitSelector(ifNode, SourceSelectorRoute, ThenArm)
		l.output.routing(wasm.Instruction{Opcode: wasm.OpI32Or})
	} else {
		// !rewinding && cond
		l.emitNotRewindingCheck()
		l.emitSelector(ifNode, SourceSelectorRoute, ThenArm)
		l.output.routing(wasm.Instruction{Opcode: wasm.OpI32And})
	}

	l.output.routing(wasm.Instruction{
		Opcode: wasm.OpIf,
		Imm:    wasm.BlockImm{Type: -64}, // void
	})

	l.emitIfArm(ifNode.label, ThenArm, ifNode.Then, paramLocals, resultLocals, ifNode.ResultTypes)

	l.output.routing(wasm.Instruction{Opcode: wasm.OpEnd})

	// Else branch (if present). A missing else with matching params/results is
	// WASM identity: params become results.
	elseBody := ifNode.Else
	if elseBody == nil && needIdentityElse(ifNode) {
		elseBody = &SeqNode{}
	}
	if elseBody != nil {
		// When BOTH branches have async: just use !cond (determines which to resume)
		// When only else has async: rewinding || !cond (must enter to resume)
		// When else has no async: !rewinding && !cond (skip during rewind)
		if thenHasAsync && elseHasAsync {
			// Both have async: just use negated saved condition
			l.emitSelector(ifNode, SourceSelectorRoute, ElseArm)
			l.output.routing(wasm.Instruction{Opcode: wasm.OpI32Eqz})
		} else if elseHasAsync {
			// rewinding || !cond
			l.emitRewindingCheck()
			l.emitSelector(ifNode, SourceSelectorRoute, ElseArm)
			l.output.routing(wasm.Instruction{Opcode: wasm.OpI32Eqz})
			l.output.routing(wasm.Instruction{Opcode: wasm.OpI32Or})
		} else {
			// !rewinding && !cond
			l.emitNotRewindingCheck()
			l.emitSelector(ifNode, SourceSelectorRoute, ElseArm)
			l.output.routing(wasm.Instruction{Opcode: wasm.OpI32Eqz})
			l.output.routing(wasm.Instruction{Opcode: wasm.OpI32And})
		}

		l.output.routing(wasm.Instruction{
			Opcode: wasm.OpIf,
			Imm:    wasm.BlockImm{Type: -64}, // void
		})

		l.emitIfArm(ifNode.label, ElseArm, elseBody, paramLocals, resultLocals, ifNode.ResultTypes)

		l.output.routing(wasm.Instruction{Opcode: wasm.OpEnd})
	}

	// Load results (in order)
	l.emitPortReadGroup(ifNode.label, ControlResult, NoArm, resultLocals)
}

// emitStructuralAsyncIfEntry preserves the pre-contract byte shape for the
// control-only test primitive. It is unreachable from production Linearize.
func (l *linearizer) emitStructuralAsyncIfEntry(condLocal uint32, paramLocals []uint32) {
	l.emitRewindingCheck()
	l.output.routing(wasm.Instruction{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}})
	l.output.routing(wasm.Instruction{Opcode: wasm.OpDrop})
	l.output.routing(wasm.Instruction{Opcode: wasm.OpElse})
	l.output.routing(wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: condLocal}})
	l.output.routing(wasm.Instruction{Opcode: wasm.OpEnd})
	if len(paramLocals) == 0 {
		return
	}
	l.emitRewindingCheck()
	l.output.routing(wasm.Instruction{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}})
	for range paramLocals {
		l.output.routing(wasm.Instruction{Opcode: wasm.OpDrop})
	}
	l.output.routing(wasm.Instruction{Opcode: wasm.OpElse})
	for index := len(paramLocals) - 1; index >= 0; index-- {
		l.output.routing(wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: paramLocals[index]}})
	}
	l.output.routing(wasm.Instruction{Opcode: wasm.OpEnd})
}

// emitIfNonAsync lowers a result- or param-bearing if with no async call to a
// single void if/else, spilling values to locals the same way emitBlock does.
func (l *linearizer) emitIfNonAsync(ifNode *IfNode) {
	carriers := l.carriers(ifNode)
	paramLocals, resultLocals := carriers.params, carriers.results
	hasParams := len(ifNode.ParamTypes) > 0

	if hasParams {
		l.emitSelector(ifNode, SourceSelectorStore, NoArm)
		l.emitScopeEntry(ifNode.label, paramLocals)
		l.emitSelector(ifNode, SourceSelectorLoad, NoArm)
	}

	l.output.guestStructure(wasm.Instruction{
		Opcode: wasm.OpIf,
		Imm:    wasm.BlockImm{Type: -64},
	})
	l.emitIfArm(ifNode.label, ThenArm, ifNode.Then, paramLocals, resultLocals, ifNode.ResultTypes)

	elseBody := ifNode.Else
	if elseBody == nil && needIdentityElse(ifNode) {
		elseBody = &SeqNode{}
	}
	if elseBody != nil {
		l.output.guestStructure(wasm.Instruction{Opcode: wasm.OpElse})
		l.emitIfArm(ifNode.label, ElseArm, elseBody, paramLocals, resultLocals, ifNode.ResultTypes)
	}

	l.output.guestStructure(wasm.Instruction{Opcode: wasm.OpEnd})
	l.emitPortReadGroup(ifNode.label, ControlResult, NoArm, resultLocals)
}

// emitIfArm emits one void if arm: materialize guest parameters, body, results.
// Parameter snapshots belong to guest execution, even though their local.get
// instructions are generated. Rewind restores their continuation storage; only
// routing predicates are recomputed on the way to the suspended call.
func (l *linearizer) emitIfArm(label LabelID, sourceArm ControlArm, body Node, paramLocals, resultLocals []uint32, resultTypes []wasm.ValType) {
	l.emitPortReadGroup(label, ControlParameter, sourceArm, paramLocals)
	l.frames = append(l.frames, controlFrame{
		label:        label,
		opcode:       wasm.OpIf,
		targetLocals: resultLocals,
		targetTypes:  resultTypes,
	})
	armStart := len(l.output.actions)
	l.emit(body)
	arm := l.output.actions[armStart:]
	l.frames = l.frames[:len(l.frames)-1]
	if l.values != nil && len(resultLocals) > 0 {
		scope, ok := l.values.scopes[label]
		if !ok {
			l.err = fmt.Errorf("asyncify: if arm has no source scope")
			return
		}
		if l.lexicalExit(scope.owner, sourceArm) {
			transfer, err := l.newScopeResultTransfer(scope.owner, label, sourceArm, resultLocals)
			if err != nil {
				l.err = err
				return
			}
			l.output.scopeResultTransfer(transfer)
		}
	} else {
		exitOperands := l.exitOperandCount(label, sourceArm, arm, len(resultLocals))
		for i := len(resultLocals) - 1; i >= len(resultLocals)-exitOperands; i-- {
			l.output.guestCarrier(wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: resultLocals[i]}})
		}
	}
}

func endsWithUnconditionalTerminator(instrs []wasm.Instruction) bool {
	if len(instrs) == 0 {
		return false
	}
	last := instrs[len(instrs)-1]
	switch last.Opcode {
	case wasm.OpBr, wasm.OpBrTable, wasm.OpReturn, wasm.OpUnreachable:
		return true
	}
	return false
}

func needIdentityElse(n *IfNode) bool {
	if n.Else != nil || len(n.ParamTypes) == 0 || len(n.ParamTypes) != len(n.ResultTypes) {
		return false
	}
	for i := range n.ParamTypes {
		if n.ParamTypes[i] != n.ResultTypes[i] {
			return false
		}
	}
	return true
}

// emitRewindingCheck emits: global.get $state; i32.const $rewinding; i32.eq
func (l *linearizer) emitRewindingCheck() {
	l.output.routingBatch([]wasm.Instruction{
		{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: l.config.StateGlobal}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: l.config.StateRewinding}},
		{Opcode: wasm.OpI32Eq},
	})
}

// emitNotRewindingCheck emits: global.get $state; i32.const $rewinding; i32.ne
func (l *linearizer) emitNotRewindingCheck() {
	l.output.routingBatch([]wasm.Instruction{
		{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: l.config.StateGlobal}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: l.config.StateRewinding}},
		{Opcode: wasm.OpI32Ne},
	})
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

// targetFrame binds source labels to the currently lowered source frame. Added
// routing conditions do not change the source identity. If referenced, the
// implicit function label is materialized as an ordinary outer source frame.
func (l *linearizer) targetFrame(node *InstrNode, position int) int {
	if position >= len(node.branchTargets) {
		if l.err == nil {
			l.err = fmt.Errorf("asyncify: source branch has no bound target %d", position)
		}
		return -1
	}
	label := node.branchTargets[position]
	for i := len(l.frames) - 1; i >= 0; i-- {
		if l.frames[i].label == label {
			return i
		}
	}
	if l.err == nil {
		l.err = fmt.Errorf("asyncify: source label %d has no active lowering frame", label)
	}
	return -1
}

func (l *linearizer) maySuspend(node Node) bool {
	if l.source != nil {
		return l.source.NodeSuspends(node)
	}
	result, ok := l.analysis.suspends[node]
	if !ok && l.err == nil {
		l.err = fmt.Errorf("asyncify: lowering encountered a node outside its source analysis")
	}
	return result
}

func (l *linearizer) lexicalExit(owner Node, arm ControlArm) bool {
	if l.source == nil {
		return true
	}
	if owner == l.analysis.root {
		return l.source.RootMayFallthrough()
	}
	return l.source.LexicalExit(owner, arm)
}

func (l *linearizer) emitPortReadGroup(label LabelID, role ValueKind, arm ControlArm, locals []uint32) {
	for position, local := range locals {
		if l.values == nil {
			l.output.guestCarrier(wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: local}})
			continue
		}
		read, err := l.newPortRead(label, role, arm, position, local)
		if err != nil {
			l.err = err
			return
		}
		l.output.sourcePortRead(read)
	}
}

func (l *linearizer) emitStructuralScopeEntry(locals []uint32) {
	for i := len(locals) - 1; i >= 0; i-- {
		l.output.guestCarrier(wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: locals[i]}})
	}
}
