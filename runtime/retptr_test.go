package runtime

import (
	"context"
	"testing"

	"go.bytecodealliance.org/wit"
)

// TestRetptr_EmptyList tests retptr handling with empty list
func TestRetptr_EmptyList(t *testing.T) {
	if mapperWasm == nil {
		t.Skip("mapper.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	mod, err := rt.LoadComponent(ctx, mapperWasm)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)

	users := []UserRecord{} // Empty list

	var result []UserOutput
	params := []wit.Type{userRecordListType}
	results := []wit.Type{userOutputListType}
	err = inst.CallInto(ctx, "transform-users", params, results, &result, users)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d elements", len(result))
	}
}

// TestRetptr_LargeList tests retptr handling with 1000 records
func TestRetptr_LargeList(t *testing.T) {
	if mapperWasm == nil {
		t.Skip("mapper.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	mod, err := rt.LoadComponent(ctx, mapperWasm)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)

	// Create 1000 users
	users := make([]UserRecord, 1000)
	for i := 0; i < 1000; i++ {
		users[i] = UserRecord{
			ID:     uint32(i),
			Name:   "User" + string(rune('0'+(i%10))),
			Tags:   []string{"a", "b", "c"},
			Active: i%2 == 0,
		}
	}

	var result []UserOutput
	params := []wit.Type{userRecordListType}
	results := []wit.Type{userOutputListType}
	err = inst.CallInto(ctx, "transform-users", params, results, &result, users)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != 1000 {
		t.Fatalf("expected 1000 results, got %d", len(result))
	}

	// Verify first and last
	if result[0].ID != 0 {
		t.Errorf("result[0].ID: expected 0, got %d", result[0].ID)
	}
	if result[999].ID != 999 {
		t.Errorf("result[999].ID: expected 999, got %d", result[999].ID)
	}
	if result[999].TagCount != 3 {
		t.Errorf("result[999].TagCount: expected 3, got %d", result[999].TagCount)
	}
}

// TestRetptr_ListInt32 tests list<s32> (uses retptr)
func TestRetptr_ListInt32(t *testing.T) {
	if complexWasm == nil {
		t.Skip("complex.wasm not found")
	}
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []int32{1, -2, 3, -4, 5, 100, -100, 0, 2147483647, -2147483648}

	var result []int32
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}
	err := inst.CallInto(ctx, "echo-list-s32", params, results, &result, input)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i := range input {
		if result[i] != input[i] {
			t.Errorf("element %d: got %d, want %d", i, result[i], input[i])
		}
	}
}

// TestRetptr_ListInt32_Empty tests empty list<s32>
func TestRetptr_ListInt32_Empty(t *testing.T) {
	if complexWasm == nil {
		t.Skip("complex.wasm not found")
	}
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []int32{}

	var result []int32
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}
	err := inst.CallInto(ctx, "echo-list-s32", params, results, &result, input)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty list, got %d elements", len(result))
	}
}

// TestRetptr_ListInt32_Large tests large list<s32>
func TestRetptr_ListInt32_Large(t *testing.T) {
	if complexWasm == nil {
		t.Skip("complex.wasm not found")
	}
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := make([]int32, 10000)
	for i := range input {
		input[i] = int32(i - 5000)
	}

	var result []int32
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}
	err := inst.CallInto(ctx, "echo-list-s32", params, results, &result, input)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i := range input {
		if result[i] != input[i] {
			t.Errorf("element %d: got %d, want %d", i, result[i], input[i])
			break
		}
	}
}

// TestRetptr_ListString tests list<string> (uses retptr)
func TestRetptr_ListString(t *testing.T) {
	if complexWasm == nil {
		t.Skip("complex.wasm not found")
	}
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []string{"hello", "world", "", "test", "foo bar baz"}

	var result []string
	params := []wit.Type{listStringType}
	results := []wit.Type{listStringType}
	err := inst.CallInto(ctx, "echo-list-string", params, results, &result, input)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i := range input {
		if result[i] != input[i] {
			t.Errorf("element %d: got %q, want %q", i, result[i], input[i])
		}
	}
}
