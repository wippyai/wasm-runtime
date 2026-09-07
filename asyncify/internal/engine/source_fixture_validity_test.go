package engine

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestSourceStressFixturesAreValid(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	for _, tc := range stressTests {
		if tc.wantErr {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			raw, err := parseWAT(tc.wat)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := rt.CompileModule(ctx, raw)
			if err != nil {
				t.Fatalf("invalid source fixture: %v", err)
			}
			compiled.Close(ctx)
		})
	}
}

func TestMediumBenchmarkSourceIsValid(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	if _, err := rt.CompileModule(ctx, createMediumModule(10).Encode()); err != nil {
		t.Fatal(err)
	}
}
