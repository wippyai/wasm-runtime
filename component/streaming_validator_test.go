package component

import (
	"testing"

	"github.com/wippyai/wasm-runtime/component/internal/arena"
)

// TestResolveValType_OwnType verifies that resolveValType correctly handles OwnType.
// The parser should return OwnType{TypeIndex: N} for `own<T>` (0x69 + index),
// and resolveValType should create a arena.DefinedKindOwn type in the arena.
func TestResolveValType_OwnType(t *testing.T) {
	v := NewStreamingValidator()
	state := arena.NewState(arena.KindComponent)

	// First, add a resource type at index 0 (simulating input-stream resource)
	resourceID := v.types.AllocResource()
	state.AddType(arena.AnyTypeID{Kind: arena.TypeKindResource, ID: resourceID})

	// Now test resolving OwnType{TypeIndex: 0} which references the resource
	ownVal := OwnType{TypeIndex: 0}

	valType, err := v.resolveValType(state, ownVal)
	if err != nil {
		t.Fatalf("resolveValType(OwnType) error: %v", err)
	}

	// The result should NOT be primitive
	if valType.IsPrimitive() {
		t.Error("expected non-primitive ValType for own")
	}

	// Check that the defined type is arena.DefinedKindOwn
	defType := v.types.GetDefined(valType.TypeID)
	if defType == nil {
		t.Fatal("defType is nil")
	}

	if defType.Kind != arena.DefinedKindOwn {
		t.Errorf("expected arena.DefinedKindOwn (9), got %d", defType.Kind)
	}
}

// TestResolveValType_BorrowType verifies that resolveValType correctly handles BorrowType.
func TestResolveValType_BorrowType(t *testing.T) {
	v := NewStreamingValidator()
	state := arena.NewState(arena.KindComponent)

	// Add a resource type at index 0
	resourceID := v.types.AllocResource()
	state.AddType(arena.AnyTypeID{Kind: arena.TypeKindResource, ID: resourceID})

	// Test resolving BorrowType{TypeIndex: 0}
	borrowVal := BorrowType{TypeIndex: 0}

	valType, err := v.resolveValType(state, borrowVal)
	if err != nil {
		t.Fatalf("resolveValType(BorrowType) error: %v", err)
	}

	if valType.IsPrimitive() {
		t.Error("expected non-primitive ValType for borrow")
	}

	defType := v.types.GetDefined(valType.TypeID)
	if defType == nil {
		t.Fatal("defType is nil")
	}

	if defType.Kind != arena.DefinedKindBorrow {
		t.Errorf("expected arena.DefinedKindBorrow (10), got %d", defType.Kind)
	}
}
