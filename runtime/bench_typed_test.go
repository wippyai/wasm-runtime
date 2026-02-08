package runtime

import (
	"context"
	"testing"

	"go.bytecodealliance.org/wit"
)

func BenchmarkTransformUsers_10_Typed(b *testing.B) {
	if mapperWasm == nil {
		b.Skip("mapper.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	mod, err := rt.LoadComponent(ctx, mapperWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	users := make([]UserRecord, 10)
	for i := 0; i < 10; i++ {
		users[i] = UserRecord{
			ID:     uint32(i + 1),
			Name:   "User" + string(rune('0'+(i%10))),
			Tags:   []string{"tag1", "tag2", "tag3", "tag4", "tag5"},
			Active: i%2 == 0,
		}
	}

	var result []UserOutput
	params := []wit.Type{userRecordListType}
	results := []wit.Type{userOutputListType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "transform-users", params, results, &result, users)
		if err != nil {
			b.Fatal(err)
		}
	}
}
