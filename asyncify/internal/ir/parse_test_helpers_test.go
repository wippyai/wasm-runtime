package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func mustParse(t *testing.T, instructions []wasm.Instruction, module ...*wasm.Module) Node {
	t.Helper()
	tree, err := Parse(instructions, module...)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
