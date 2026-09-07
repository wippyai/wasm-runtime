package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestRoutingBoundaryPreservesGuestPrefix(t *testing.T) {
	for _, mutation := range []string{"balanced", "leaked-suffix", "consumed-prefix", "changed-identity", "changed-storage", "unbound-entry"} {
		t.Run(mutation, func(t *testing.T) {
			stack := handler.NewStack(0)
			guest := semantics.StoredOperand(1, wasm.ValI32).WithBinding(7)
			if mutation == "unbound-entry" {
				guest = semantics.StoredOperand(1, wasm.ValI32)
			}
			stack.PushOperand(guest)
			var boundary routingBoundary
			err := boundary.advance(true, stack.Len(), stack.At)
			if mutation == "unbound-entry" {
				if err == nil {
					t.Fatal("accepted unbound guest prefix")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stack.Push(2, wasm.ValI32)
			if err := boundary.advance(true, stack.Len(), stack.At); err != nil {
				t.Fatal(err)
			}
			stack.Pop()
			switch mutation {
			case "leaked-suffix":
				stack.Push(2, wasm.ValI32)
			case "consumed-prefix":
				stack.Pop()
			case "changed-identity":
				stack.Pop()
				stack.PushOperand(guest.WithBinding(8))
			case "changed-storage":
				stack.Pop()
				stack.PushOperand(semantics.StoredOperand(3, wasm.ValI32).WithBinding(7))
			}
			err = boundary.advance(false, stack.Len(), stack.At)
			if (err == nil) != (mutation == "balanced") {
				t.Fatalf("unexpected validation: %v", err)
			}
			if mutation == "balanced" {
				stack.Pop()
				// A subsequent segment owns a new, possibly empty prefix.
				if err := boundary.advance(true, stack.Len(), stack.At); err != nil {
					t.Fatal(err)
				}
				if err := boundary.advance(false, stack.Len(), stack.At); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
