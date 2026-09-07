package ir

import (
	"strings"
	"testing"
)

// The end of the source stream is not the end of the lowered action stream.
// In particular, a trailing routing action must still satisfy capture-first.
func TestStreamingCoverageChecksTrailingActions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		want      string
		kind      ActionKind
		suspended bool
	}{
		{"extra source", "unexpected capture/source", SourceInstruction, false},
		{"routing before capture", "precedes its entry capture", RoutingInstruction, true},
		{"structure before capture", "precedes its entry capture", GuestStructureInstruction, true},
		{"ordinary routing", "", RoutingInstruction, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lowered := nestedCaptureCoverageFixture(t)
			owner := &IfNode{}
			if tc.suspended {
				lowered.values.control.suspends[owner] = true
			}
			lowered.actions = append(lowered.actions, Action{kind: tc.kind, origin: instructionOrigin{owner: owner}})
			err := lowered.values.control.verifyActionCoverage(lowered)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestStreamingCoverageRejectsMissingFinalEvent(t *testing.T) {
	lowered := nestedCaptureCoverageFixture(t)
	for i := len(lowered.actions) - 1; i >= 0; i-- {
		if lowered.actions[i].kind == SourceInstruction {
			lowered.actions = append(lowered.actions[:i], lowered.actions[i+1:]...)
			break
		}
	}
	if err := lowered.values.control.verifyActionCoverage(lowered); err == nil {
		t.Fatal("accepted missing final source event")
	}
}
