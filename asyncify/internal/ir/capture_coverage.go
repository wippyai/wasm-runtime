package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// sourceActionEvent is the source-derived order contract for semantic actions.
// It intentionally excludes byte-oriented carrier, structure and routing
// actions: those may be introduced around a source construct, but cannot add,
// remove, duplicate or move one of these source facts.
type sourceActionEvent struct {
	selector      *IfNode
	readOwner     Node
	transferOwner Node
	capture       *IfNode
	source        *InstrNode
	returnNode    *InstrNode
	branchNode    *InstrNode
	trapNode      *InstrNode
	readScope     LabelID
	transferScope LabelID
	readPosition  int
	readRole      ValueKind
	readArm       ControlArm
	transferArm   ControlArm
	entry         bool
	selectorKind  ActionKind
	selectorArm   ControlArm
}

// verifyActionCoverage binds semantic action order to the owned source tree.
// The first action owned by an async if must be its capture. Every source
// instruction, async-if entry capture, and materialized scope-result exit is
// represented once in source traversal order. It derives expected transfers
// from source scopes, never from lowered terminal opcodes or emitted bytes.
func (a *Analysis) verifyActionCoverage(lowered *LoweredControl) error {
	if lowered.values == nil {
		// linearizeControl is the explicit structural test primitive and has no
		// source value/capture contract. Production Linearize always has values.
		return nil
	}
	hasFunctionBranch := a.hasFunctionBranch
	suspends := func(node Node) bool { return a.suspends[node] }
	lexicalExit := func(owner Node, arm ControlArm) bool { return true }
	if lowered.source != nil {
		hasFunctionBranch = lowered.source.HasFunctionBranch()
		suspends = lowered.source.NodeSuspends
		lexicalExit = func(owner Node, arm ControlArm) bool {
			if owner == a.root {
				return lowered.source.RootMayFallthrough()
			}
			return lowered.source.LexicalExit(owner, arm)
		}
	}
	if lowered.rootMaterialized != hasFunctionBranch {
		return fmt.Errorf("asyncify: materialized root action coverage disagrees with source branch facts")
	}
	cursor := actionCoverageCursor{actions: lowered.actions, suspends: suspends, entered: make(map[*IfNode]bool)}
	reads := func(owner Node, label LabelID, role ValueKind, arm ControlArm, count int) error {
		for position := 0; position < count; position++ {
			if err := cursor.consume(sourceActionEvent{readOwner: owner, readScope: label, readRole: role, readArm: arm, readPosition: position}); err != nil {
				return err
			}
		}
		return nil
	}
	var visit func(Node) error
	visit = func(node Node) error {
		if lowered.source != nil && !lowered.source.Active(node) {
			return nil
		}
		switch source := node.(type) {
		case *SeqNode:
			for _, child := range source.Children {
				if err := visit(child); err != nil {
					return err
				}
			}
		case *BlockNode:
			if len(source.ParamTypes) > 0 {
				if err := cursor.consume(sourceActionEvent{transferOwner: source, transferScope: source.label, entry: true}); err != nil {
					return err
				}
			}
			if err := reads(source, source.label, ControlParameter, NoArm, len(source.ParamTypes)); err != nil {
				return err
			}
			if err := visit(source.Body); err != nil {
				return err
			}
			if len(source.ResultTypes) != 0 && lexicalExit(source, NoArm) {
				if err := cursor.consume(sourceActionEvent{transferOwner: source, transferScope: source.label, transferArm: NoArm}); err != nil {
					return err
				}
			}
			if err := reads(source, source.label, ControlResult, NoArm, len(source.ResultTypes)); err != nil {
				return err
			}
		case *IfNode:
			if suspends(source) {
				if err := cursor.consume(sourceActionEvent{capture: source}); err != nil {
					return err
				}
				if err := cursor.consume(sourceActionEvent{selector: source, selectorKind: SourceSelectorRoute, selectorArm: ThenArm}); err != nil {
					return err
				}
			} else if len(source.ParamTypes) > 0 {
				if err := cursor.consume(sourceActionEvent{selector: source, selectorKind: SourceSelectorStore}); err != nil {
					return err
				}
				if err := cursor.consume(sourceActionEvent{transferOwner: source, transferScope: source.label, entry: true}); err != nil {
					return err
				}
				if err := cursor.consume(sourceActionEvent{selector: source, selectorKind: SourceSelectorLoad}); err != nil {
					return err
				}
			}

			if err := reads(source, source.label, ControlParameter, ThenArm, len(source.ParamTypes)); err != nil {
				return err
			}
			if err := visit(source.Then); err != nil {
				return err
			}
			if len(source.ResultTypes) != 0 && lexicalExit(source, ThenArm) {
				if err := cursor.consume(sourceActionEvent{transferOwner: source, transferScope: source.label, transferArm: ThenArm}); err != nil {
					return err
				}
			}
			if source.Else != nil || needIdentityElse(source) {
				if suspends(source) {
					if err := cursor.consume(sourceActionEvent{selector: source, selectorKind: SourceSelectorRoute, selectorArm: ElseArm}); err != nil {
						return err
					}
				}
				if err := reads(source, source.label, ControlParameter, ElseArm, len(source.ParamTypes)); err != nil {
					return err
				}
			}
			if source.Else != nil {
				if err := visit(source.Else); err != nil {
					return err
				}
			}
			if len(source.ResultTypes) != 0 && lexicalExit(source, ElseArm) {
				if err := cursor.consume(sourceActionEvent{transferOwner: source, transferScope: source.label, transferArm: ElseArm}); err != nil {
					return err
				}
			}
			if err := reads(source, source.label, ControlResult, NoArm, len(source.ResultTypes)); err != nil {
				return err
			}
		case *InstrNode:
			switch source.Instr.Opcode {
			case wasm.OpReturn:
				if err := cursor.consume(sourceActionEvent{returnNode: source}); err != nil {
					return err
				}
			case wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable:
				if err := cursor.consume(sourceActionEvent{branchNode: source}); err != nil {
					return err
				}
			case wasm.OpUnreachable:
				if err := cursor.consume(sourceActionEvent{trapNode: source}); err != nil {
					return err
				}
			default:
				if err := cursor.consume(sourceActionEvent{source: source}); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("asyncify: unknown source node in capture coverage")
		}
		return nil
	}
	if err := visit(a.root); err != nil {
		return err
	}
	if lowered.rootMaterialized && len(a.functionResults) != 0 && lexicalExit(a.root, NoArm) {
		if err := cursor.consume(sourceActionEvent{transferOwner: a.root, transferScope: 0, transferArm: NoArm}); err != nil {
			return err
		}
	}
	if lowered.rootMaterialized {
		if err := reads(a.root, 0, ControlResult, NoArm, len(a.functionResults)); err != nil {
			return err
		}
	}
	return cursor.finish()
}

// actionCoverageCursor consumes the immutable lowered stream once. Even actions
// without a source event pass through next, so capture-first checks also cover
// routing and structural actions, including the suffix after the last event.
// Returned action pointers are borrowed read-only within this verifier; the
// cursor never exports or mutates lowered payload storage.
type actionCoverageCursor struct {
	suspends func(Node) bool
	entered  map[*IfNode]bool
	actions  []Action
	index    int
	events   int
}

func (c *actionCoverageCursor) next() (*Action, int, error) {
	for c.index < len(c.actions) {
		index := c.index
		action := &c.actions[index]
		c.index++
		if owner, ok := action.origin.owner.(*IfNode); ok && c.suspends(owner) && !c.entered[owner] {
			if action.kind != AsyncIfEntryCapture {
				return nil, index, fmt.Errorf("asyncify: if-owned action precedes its entry capture at action %d", index)
			}
			c.entered[owner] = true
		}
		if action.kind == SourceInstruction || action.kind == SourceReturn || action.kind == SourceBranch || action.kind == SourceTrap || action.kind == AsyncIfEntryCapture || action.kind == ScopeResultTransfer || action.kind == ScopeEntryTransfer || action.kind == SourcePortRead || isSelectorAction(action.kind) {
			return action, index, nil
		}
	}
	return nil, c.index, nil
}

func (c *actionCoverageCursor) consume(want sourceActionEvent) error {
	action, index, err := c.next()
	if err != nil {
		return err
	}
	if action == nil {
		return fmt.Errorf("asyncify: missing capture/source event at position %d", c.events)
	}
	if err := matchSourceActionEvent(action, index, want); err != nil {
		return err
	}
	c.events++
	return nil
}

func (c *actionCoverageCursor) finish() error {
	action, index, err := c.next()
	if err != nil {
		return err
	}
	if action != nil {
		return fmt.Errorf("asyncify: unexpected capture/source action at action %d", index)
	}
	return nil
}

func matchSourceActionEvent(action *Action, actionIndex int, want sourceActionEvent) error {
	if isSelectorAction(action.kind) {
		if want.selector == nil || action.origin.owner != want.selector || action.selector.owner != want.selector || action.kind != want.selectorKind || action.selector.arm != want.selectorArm {
			return fmt.Errorf("asyncify: selector event order mismatch at action %d", actionIndex)
		}
	} else if action.kind == SourcePortRead {
		read := action.portRead
		if want.readOwner == nil || action.origin.owner != want.readOwner || read.owner != want.readOwner || read.scope != want.readScope || read.role != want.readRole || read.arm != want.readArm || read.position != want.readPosition {
			return fmt.Errorf("asyncify: port read event order mismatch at action %d", actionIndex)
		}
	} else if action.kind == AsyncIfEntryCapture {
		if want.capture == nil || action.origin.owner != want.capture || action.capture.owner != want.capture {
			return fmt.Errorf("asyncify: capture/source event order mismatch at action %d", actionIndex)
		}
	} else if action.kind == ScopeResultTransfer || action.kind == ScopeEntryTransfer {
		if want.entry != (action.kind == ScopeEntryTransfer) || want.transferOwner == nil || action.origin.owner != want.transferOwner || action.transfer.owner != want.transferOwner || action.transfer.scope != want.transferScope || action.transfer.arm != want.transferArm {
			return fmt.Errorf("asyncify: capture/source event order mismatch at action %d", actionIndex)
		}
	} else if action.kind == SourceReturn {
		if want.returnNode == nil || action.origin.owner != want.returnNode || action.origin.source != want.returnNode || action.returnOp.owner != want.returnNode {
			return fmt.Errorf("asyncify: capture/source event order mismatch at action %d", actionIndex)
		}
	} else if action.kind == SourceBranch {
		if want.branchNode == nil || action.origin.owner != want.branchNode || action.origin.source != want.branchNode || action.branch.owner != want.branchNode {
			return fmt.Errorf("asyncify: capture/source event order mismatch at action %d", actionIndex)
		}
	} else if action.kind == SourceTrap {
		if want.trapNode == nil || action.origin.owner != want.trapNode || action.origin.source != want.trapNode || action.trap.owner != want.trapNode {
			return fmt.Errorf("asyncify: capture/source event order mismatch at action %d", actionIndex)
		}
	} else if want.source == nil || action.origin.source != want.source || action.origin.owner != want.source {
		return fmt.Errorf("asyncify: capture/source event order mismatch at action %d", actionIndex)
	}
	return nil
}
