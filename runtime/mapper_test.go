package runtime

import (
	"context"
	"os"
	"testing"

	"go.bytecodealliance.org/wit"
)

var mapperWasm []byte

func init() {
	var err error
	mapperWasm, err = os.ReadFile("../testbed/mapper.wasm")
	if err != nil {
		return
	}
}

type UserRecord struct {
	Name   string   `wit:"name"`
	Tags   []string `wit:"tags"`
	ID     uint32   `wit:"id"`
	Active bool     `wit:"active"`
}

type UserOutput struct {
	Display  string `wit:"display"`
	ID       uint32 `wit:"id"`
	TagCount uint32 `wit:"tag-count"`
}

var tagListType = &wit.TypeDef{Kind: &wit.List{Type: wit.String{}}}

var userRecordType = &wit.TypeDef{
	Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "id", Type: wit.U32{}},
			{Name: "name", Type: wit.String{}},
			{Name: "tags", Type: tagListType},
			{Name: "active", Type: wit.Bool{}},
		},
	},
}

var userOutputType = &wit.TypeDef{
	Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "id", Type: wit.U32{}},
			{Name: "display", Type: wit.String{}},
			{Name: "tag-count", Type: wit.U32{}},
		},
	},
}

var userRecordListType = &wit.TypeDef{Kind: &wit.List{Type: userRecordType}}
var userOutputListType = &wit.TypeDef{Kind: &wit.List{Type: userOutputType}}

func BenchmarkTransformUsers_10(b *testing.B) {
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

	users := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		users[i] = map[string]any{
			"id":     uint32(i + 1),
			"name":   "User" + string(rune('0'+i)),
			"tags":   []string{"tag1", "tag2", "tag3"},
			"active": i%2 == 0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.Call(ctx, "transform-users", users)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFilterActive_10(b *testing.B) {
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

	users := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		users[i] = map[string]any{
			"id":     uint32(i + 1),
			"name":   "User" + string(rune('0'+i)),
			"tags":   []string{"tag1", "tag2", "tag3"},
			"active": i%2 == 0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.Call(ctx, "filter-active", users)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAggregateTags_10(b *testing.B) {
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

	users := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		users[i] = map[string]any{
			"id":     uint32(i + 1),
			"name":   "User" + string(rune('0'+i)),
			"tags":   []string{"tag1", "tag2", "tag3", "tag" + string(rune('0'+i))},
			"active": i%2 == 0,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.Call(ctx, "aggregate-tags", users)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformUsers_100(b *testing.B) {
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

	users := make([]map[string]any, 100)
	for i := 0; i < 100; i++ {
		users[i] = map[string]any{
			"id":     uint32(i + 1),
			"name":   "User" + string(rune('0'+(i%10))),
			"tags":   []string{"tag1", "tag2", "tag3", "tag4", "tag5"},
			"active": i%2 == 0,
		}
	}

	var result any
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err = inst.Call(ctx, "transform-users", users)
		if err != nil {
			b.Fatalf("call failed: %v", err)
		}
	}
	_ = result
}

func TestSingleUser_Untyped(t *testing.T) {
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

	users := []map[string]any{
		{"id": uint32(42), "name": "Test", "tags": []string{"a", "b"}, "active": true},
	}

	result, err := inst.Call(ctx, "transform-users", users)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	resultList, ok := result.([]any)
	if !ok {
		t.Fatalf("result is not []any: %T", result)
	}

	if len(resultList) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resultList))
	}

	rec := resultList[0].(map[string]any)
	t.Logf("Result: ID=%v, Display=%v, TagCount=%v", rec["id"], rec["display"], rec["tag-count"])

	if rec["id"].(uint32) != 42 {
		t.Errorf("ID: expected 42, got %v", rec["id"])
	}
	if rec["display"].(string) != "Test [a, b]" {
		t.Errorf("Display: expected %q, got %q", "Test [a, b]", rec["display"])
	}
	if rec["tag-count"].(uint32) != 2 {
		t.Errorf("TagCount: expected 2, got %v", rec["tag-count"])
	}
}

func TestSingleUser_Typed(t *testing.T) {
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

	users := []UserRecord{
		{ID: 42, Name: "Test", Tags: []string{"a", "b"}, Active: true},
	}

	var result []UserOutput
	params := []wit.Type{userRecordListType}
	results := []wit.Type{userOutputListType}

	err = inst.CallInto(ctx, "transform-users", params, results, &result, users)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	t.Logf("Result: ID=%d, Display=%q, TagCount=%d", result[0].ID, result[0].Display, result[0].TagCount)

	if result[0].ID != 42 {
		t.Errorf("ID: expected 42, got %d", result[0].ID)
	}
	if result[0].Display != "Test [a, b]" {
		t.Errorf("Display: expected %q, got %q", "Test [a, b]", result[0].Display)
	}
	if result[0].TagCount != 2 {
		t.Errorf("TagCount: expected 2, got %d", result[0].TagCount)
	}
}

func TestTransformUsers_Typed(t *testing.T) {
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

	users := []UserRecord{
		{ID: 1, Name: "Alice", Tags: []string{"admin", "dev"}, Active: true},
		{ID: 2, Name: "Bob", Tags: []string{"user"}, Active: false},
		{ID: 3, Name: "Charlie", Tags: []string{"admin", "ops", "security"}, Active: true},
	}

	var result []UserOutput
	params := []wit.Type{userRecordListType}
	results := []wit.Type{userOutputListType}
	err = inst.CallInto(ctx, "transform-users", params, results, &result, users)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	if result[0].ID != 1 || result[0].Display != "Alice [admin, dev]" || result[0].TagCount != 2 {
		t.Errorf("[0] got %+v", result[0])
	}
	if result[1].ID != 2 || result[1].Display != "Bob [user]" || result[1].TagCount != 1 {
		t.Errorf("[1] got %+v", result[1])
	}
	if result[2].ID != 3 || result[2].Display != "Charlie [admin, ops, security]" || result[2].TagCount != 3 {
		t.Errorf("[2] got %+v", result[2])
	}
}

func TestTypedMatchesUntyped(t *testing.T) {
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

	testCases := []struct {
		name string
		user UserRecord
	}{
		{
			name: "basic user",
			user: UserRecord{ID: 99, Name: "Validator", Tags: []string{"x", "y", "z"}, Active: true},
		},
		{
			name: "empty tags",
			user: UserRecord{ID: 0, Name: "NoTags", Tags: []string{}, Active: false},
		},
		{
			name: "single tag",
			user: UserRecord{ID: 1, Name: "SingleTag", Tags: []string{"only"}, Active: true},
		},
		{
			name: "empty name",
			user: UserRecord{ID: 42, Name: "", Tags: []string{"a", "b"}, Active: false},
		},
		{
			name: "max uint32 ID",
			user: UserRecord{ID: 4294967295, Name: "MaxID", Tags: []string{"test"}, Active: true},
		},
		{
			name: "many tags",
			user: UserRecord{ID: 100, Name: "ManyTags", Tags: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, Active: false},
		},
		{
			name: "special chars in name",
			user: UserRecord{ID: 999, Name: "User-With_Special.Chars!", Tags: []string{"tag"}, Active: true},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			users := []UserRecord{tc.user}
			usersUntyped := []map[string]any{
				{
					"id":     tc.user.ID,
					"name":   tc.user.Name,
					"tags":   tc.user.Tags,
					"active": tc.user.Active,
				},
			}

			resultUntyped, err := inst.Call(ctx, "transform-users", usersUntyped)
			if err != nil {
				t.Fatalf("untyped call failed: %v", err)
			}

			var resultTyped []UserOutput
			params := []wit.Type{userRecordListType}
			results := []wit.Type{userOutputListType}
			err = inst.CallInto(ctx, "transform-users", params, results, &resultTyped, users)
			if err != nil {
				t.Fatalf("typed call failed: %v", err)
			}

			untypedList := resultUntyped.([]any)
			if len(untypedList) != 1 {
				t.Fatalf("untyped: expected 1 result, got %d", len(untypedList))
			}
			if len(resultTyped) != 1 {
				t.Fatalf("typed: expected 1 result, got %d", len(resultTyped))
			}

			untypedRec := untypedList[0].(map[string]any)

			if resultTyped[0].ID != untypedRec["id"].(uint32) {
				t.Errorf("ID mismatch: typed=%d, untyped=%d", resultTyped[0].ID, untypedRec["id"])
			}
			if resultTyped[0].Display != untypedRec["display"].(string) {
				t.Errorf("Display mismatch: typed=%q, untyped=%q", resultTyped[0].Display, untypedRec["display"])
			}
			if resultTyped[0].TagCount != untypedRec["tag-count"].(uint32) {
				t.Errorf("TagCount mismatch: typed=%d, untyped=%d", resultTyped[0].TagCount, untypedRec["tag-count"])
			}
		})
	}
}

func BenchmarkTransformUsers_100_Typed(b *testing.B) {
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

	users := make([]UserRecord, 100)
	for i := 0; i < 100; i++ {
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
