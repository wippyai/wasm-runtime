package semantics

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLocalKnowledgeInvalidation(t *testing.T) {
	var k LocalKnowledge
	value := Value{ID: 1, LocalIdx: 10, Type: wasm.ValI32}
	k.Record(0, value)
	if _, ok := k.Lookup(0); ok {
		t.Fatal("recorded outside certified region")
	}
	k.Observe(PreserveLocals)
	k.Record(0, value)
	k.Record(1, value)
	k.Invalidate(0)
	if _, ok := k.Lookup(0); ok {
		t.Fatal("write did not invalidate")
	}
	if got, ok := k.Lookup(1); !ok || got != value {
		t.Fatal("invalidated unrelated cell")
	}
	k.Observe(ForgetLocals)
	k.Observe(PreserveLocals)
	if _, ok := k.Lookup(1); ok {
		t.Fatal("knowledge survived region boundary")
	}
}

func TestLocalKnowledgeEffectDefaultsToBoundary(t *testing.T) {
	for _, op := range []byte{wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpElse, wasm.OpEnd, wasm.OpCall, wasm.OpCallIndirect, 0xff} {
		if LocalKnowledgeEffect(wasm.Instruction{Opcode: op}, GuestExecution) != ForgetLocals {
			t.Fatalf("unsafe effect for %x", op)
		}
	}
	if LocalKnowledgeEffect(wasm.Instruction{Opcode: wasm.OpLocalGet}, RewindRouting) != ForgetLocals {
		t.Fatal("synthetic operation retained knowledge")
	}
}

func TestLocalKnowledgeOwnershipIgnoresInstructionAnnotation(t *testing.T) {
	if LocalKnowledgeEffect(wasm.Instruction{Opcode: wasm.OpLocalGet, Synthetic: true}, GuestExecution) != PreserveLocals {
		t.Fatal("byte annotation overrode guest action ownership")
	}
	if LocalKnowledgeEffect(wasm.Instruction{Opcode: wasm.OpLocalGet}, ExecutionDomain(255)) != ForgetLocals {
		t.Fatal("unknown domain preserved local knowledge")
	}
}
