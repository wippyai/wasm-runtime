package asyncify_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestTransformRejectsProtocolCollisionsWithoutOutput(t *testing.T) {
	cases := []string{
		`(module (import "env" "asyncify_start_unwind" (func $f)) (func (export "run") call $f))`,
		`(module (import "asyncify" "custom" (func $f)) (start $f))`,
		`(module (import "env" "asyncify_stop_unwind" (func $f)) (table 1 funcref) (elem (i32.const 0) $f))`,
		`(module (import "env" "asyncify_get_state" (func $f)) (export "alias" (func $f)))`,
		`(module (func (export "asyncify_start_rewind")))`,
	}
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	for _, source := range cases {
		input, err := wat.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		// Source validity is independent of our parser and transformation.
		compiled, err := rt.CompileModule(ctx, input)
		if err != nil {
			t.Fatalf("invalid source: %v", err)
		}
		compiled.Close(ctx)
		before := append([]byte(nil), input...)
		for _, cfg := range []asyncify.Config{{}, {AsyncImports: []string{"*"}}} {
			output, err := asyncify.Transform(input, cfg)
			if err == nil || output != nil || !strings.Contains(err.Error(), "conflicting protocol") {
				t.Fatalf("must reject collision without output, got %d bytes, %v", len(output), err)
			}
			if !bytes.Equal(before, input) {
				t.Fatal("rejection modified input")
			}
		}
	}
}

func TestTransformRejectsUnsupportedAddressContracts(t *testing.T) {
	input, err := wat.Compile(`(module (import "env" "yield" (func $f)) (memory 1) (func (export "run") call $f))`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	compiled, err := rt.CompileModule(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Close(ctx)
	for _, cfg := range []asyncify.Config{{Wasm64: true}, {UseSecondaryMemory: true}, {MemoryIndex: 1}} {
		output, err := asyncify.Transform(input, cfg)
		if err == nil || output != nil || !strings.Contains(err.Error(), "not supported") {
			t.Fatalf("accepted unsupported address contract: %v", err)
		}
	}
	for _, imported := range []bool{false, true} {
		m, err := wasm.ParseModule(input)
		if err != nil {
			t.Fatal(err)
		}
		if imported {
			m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "memory", Desc: wasm.ImportDesc{Kind: wasm.KindMemory, Memory: &wasm.MemoryType{Limits: wasm.Limits{Min: 1, Memory64: true}}}})
			m.Memories = nil
		} else {
			m.Memories[0].Limits.Memory64 = true
		}
		output, err := asyncify.Transform(m.Encode(), asyncify.Config{})
		if err == nil || output != nil || !strings.Contains(err.Error(), "memory64 is not supported") {
			t.Fatalf("imported=%v: accepted memory64 input: %v", imported, err)
		}
	}
	// The supported contract must still produce independently valid code.
	output, err := asyncify.Transform(input, asyncify.Config{AsyncImports: []string{"env.yield"}})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err = rt.CompileModule(ctx, output)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Close(ctx)
}
