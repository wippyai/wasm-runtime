package ir

import "fmt"

// CompletionKind describes the normal execution exit of the lowered function.
// It does not describe the separate dummy return used while unwinding.
type CompletionKind uint8

const (
	InvalidCompletion CompletionKind = iota
	UnreachableCompletion
	DirectCompletion
	PortCompletion
)

// FunctionCompletion is an immutable view of the source function's exit.
// A materialized function label can complete through a branch even when lexical
// fallthrough is unreachable, so it must be distinguished from a direct exit.
type FunctionCompletion struct {
	values *ValuePlan
	kind   CompletionKind
}

func (c FunctionCompletion) Kind() CompletionKind { return c.kind }

// ResultValue identifies a direct source result or a materialized root port.
// An unreachable completion has no runtime result, including for typed values
// that are present only on the source validation stack.
func (c FunctionCompletion) ResultValue(index int) (ValueID, bool) {
	if c.values == nil || c.kind == InvalidCompletion || c.kind == UnreachableCompletion {
		return 0, false
	}
	scope := c.values.scopes[0]
	if index < 0 || index >= len(scope.results) {
		return 0, false
	}
	if c.kind == PortCompletion {
		return scope.results[index], true
	}
	value := scope.exits[0].from[index]
	return value, value != 0
}

func (l *LoweredControl) Completion() (FunctionCompletion, error) {
	if l.values == nil {
		return FunctionCompletion{}, fmt.Errorf("asyncify: completion has no source value plan")
	}
	scope, ok := l.values.scopes[0]
	if !ok || scope.kind != FunctionScope || len(scope.exits) != 1 {
		return FunctionCompletion{}, fmt.Errorf("asyncify: completion has no source function exit")
	}
	hasFunctionBranch := l.values.control.hasFunctionBranch
	mayFallthrough := scope.exits[0].validationReachable
	if l.source != nil {
		hasFunctionBranch = l.source.HasFunctionBranch()
		mayFallthrough = l.source.RootMayFallthrough()
	}
	if l.rootMaterialized != hasFunctionBranch {
		return FunctionCompletion{}, fmt.Errorf("asyncify: completion disagrees with function label materialization")
	}
	// The canonical wrapper exists exactly when the source names the function
	// label as a branch target. Scope storage verification separately proves the
	// wrapper and its physical result-port assignment.
	kind := DirectCompletion
	if hasFunctionBranch {
		kind = PortCompletion
	} else if !mayFallthrough {
		kind = UnreachableCompletion
	}
	return FunctionCompletion{values: l.values, kind: kind}, nil
}
