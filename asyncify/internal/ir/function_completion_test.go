package ir

import "testing"

func TestFunctionCompletionDistinguishesLexicalAndBranchExits(t *testing.T) {
	for _, tc := range []struct {
		body string
		kind CompletionKind
	}{
		{`call $yield i64.const 7`, DirectCompletion},
		{`call $yield unreachable select`, UnreachableCompletion},
		{`i64.const 7 call $yield br 0 unreachable select`, PortCompletion},
	} {
		t.Run(tc.body, func(t *testing.T) {
			values, lowered := lowerValueFixture(t, `(module (import "env" "yield" (func $yield)) (func (result i64) `+tc.body+`))`)
			completion, err := lowered.Completion()
			if err != nil {
				t.Fatal(err)
			}
			if completion.Kind() != tc.kind {
				t.Fatalf("completion=%v, want %v", completion.Kind(), tc.kind)
			}
			got, present := completion.ResultValue(0)
			scope := values.scopes[0]
			switch tc.kind {
			case DirectCompletion:
				if !present || got != scope.exits[0].from[0] {
					t.Fatal("direct completion lost source result")
				}
			case PortCompletion:
				if !present || got != scope.results[0] || scope.exits[0].validationReachable {
					t.Fatal("function branch lost root result despite unreachable lexical exit")
				}
			case UnreachableCompletion:
				if present || got != 0 {
					t.Fatal("unreachable completion fabricated runtime result")
				}
			}
			for _, index := range []int{-1, 1} {
				if _, ok := completion.ResultValue(index); ok {
					t.Fatal("invalid result position accepted")
				}
			}
			lowered.rootMaterialized = !lowered.rootMaterialized
			if _, err := lowered.Completion(); err == nil {
				t.Fatal("completion accepted inconsistent root materialization")
			}
		})
	}
}
