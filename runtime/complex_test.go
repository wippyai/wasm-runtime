package runtime

import (
	"context"
	"math"
	"os"
	"testing"

	"go.bytecodealliance.org/wit"
)

var complexWasm []byte

func init() {
	var err error
	complexWasm, err = os.ReadFile("../testbed/complex.wasm")
	if err != nil {
		return
	}
}

// complexHost implements test:complex/host
type complexHost struct{}

func (h *complexHost) Namespace() string { return "test:complex/host@0.1.0" }

func (h *complexHost) SumList(ctx context.Context, numbers []int32) int64 {
	var sum int64
	for _, n := range numbers {
		sum += int64(n)
	}
	return sum
}

func (h *complexHost) CreatePoint(ctx context.Context, x, y int32) map[string]any {
	return map[string]any{"x": x, "y": y}
}

func (h *complexHost) PointDistance(ctx context.Context, a, b map[string]any) float64 {
	ax := a["x"].(int32)
	ay := a["y"].(int32)
	bx := b["x"].(int32)
	by := b["y"].(int32)
	dx := float64(bx - ax)
	dy := float64(by - ay)
	return math.Sqrt(dx*dx + dy*dy)
}

func (h *complexHost) JoinStrings(ctx context.Context, parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

func (h *complexHost) RectArea(ctx context.Context, r map[string]any) int32 {
	tl := r["top-left"].(map[string]any)
	br := r["bottom-right"].(map[string]any)
	width := br["x"].(int32) - tl["x"].(int32)
	height := br["y"].(int32) - tl["y"].(int32)
	if width < 0 {
		width = -width
	}
	if height < 0 {
		height = -height
	}
	return width * height
}

func (h *complexHost) ColorCode(ctx context.Context, c uint32) uint32 {
	switch c {
	case 0:
		return 0xFF0000
	case 1:
		return 0x00FF00
	case 2:
		return 0x0000FF
	}
	return 0
}

func (h *complexHost) ShapeArea(ctx context.Context, s map[string]any) float64 {
	for k, v := range s {
		switch k {
		case "circle":
			radius := v.(uint32)
			return math.Pi * float64(radius) * float64(radius)
		case "square":
			side := v.(uint32)
			return float64(side * side)
		case "rect":
			rect := v.(map[string]any)
			tl := rect["top-left"].(map[string]any)
			br := rect["bottom-right"].(map[string]any)
			width := float64(br["x"].(int32) - tl["x"].(int32))
			height := float64(br["y"].(int32) - tl["y"].(int32))
			if width < 0 {
				width = -width
			}
			if height < 0 {
				height = -height
			}
			return width * height
		case "none":
			return 0
		}
	}
	return 0
}

func (h *complexHost) CheckPermission(ctx context.Context, p, flag uint64) bool {
	return (p & flag) != 0
}

func (h *complexHost) TotalAge(ctx context.Context, people []map[string]any) uint32 {
	var total uint32
	for _, p := range people {
		total += p["age"].(uint32)
	}
	return total
}

func skipIfNoComplex(t *testing.T) {
	if complexWasm == nil {
		t.Skip("complex.wasm not found")
	}
}

func setupComplexInstance(t *testing.T) (*Runtime, *Instance) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := rt.RegisterHost(&complexHost{}); err != nil {
		rt.Close(ctx)
		t.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		rt.Close(ctx)
		t.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		rt.Close(ctx)
		t.Fatal(err)
	}

	return rt, inst
}

// Type definitions
var pointType = &wit.TypeDef{
	Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "x", Type: wit.S32{}},
			{Name: "y", Type: wit.S32{}},
		},
	},
}

var personType = &wit.TypeDef{
	Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "name", Type: wit.String{}},
			{Name: "age", Type: wit.U32{}},
		},
	},
}

var rectangleType = &wit.TypeDef{
	Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "top-left", Type: pointType},
			{Name: "bottom-right", Type: pointType},
		},
	},
}

var colorType = &wit.TypeDef{
	Kind: &wit.Enum{
		Cases: []wit.EnumCase{
			{Name: "red"},
			{Name: "green"},
			{Name: "blue"},
		},
	},
}

var shapeType = &wit.TypeDef{
	Kind: &wit.Variant{
		Cases: []wit.Case{
			{Name: "circle", Type: wit.U32{}},
			{Name: "square", Type: wit.U32{}},
			{Name: "rect", Type: rectangleType},
			{Name: "none", Type: nil},
		},
	},
}

var permissionsType = &wit.TypeDef{
	Kind: &wit.Flags{
		Flags: []wit.Flag{
			{Name: "read"},
			{Name: "write"},
			{Name: "execute"},
		},
	},
}

var errorInfoType = &wit.TypeDef{
	Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "code", Type: wit.U32{}},
			{Name: "message", Type: wit.String{}},
		},
	},
}

var listS32Type = &wit.TypeDef{Kind: &wit.List{Type: wit.S32{}}}
var listStringType = &wit.TypeDef{Kind: &wit.List{Type: wit.String{}}}
var listPointType = &wit.TypeDef{Kind: &wit.List{Type: pointType}}
var listPersonType = &wit.TypeDef{Kind: &wit.List{Type: personType}}

var optionPointType = &wit.TypeDef{Kind: &wit.Option{Type: pointType}}
var optionStringType = &wit.TypeDef{Kind: &wit.Option{Type: wit.String{}}}

var resultDivideType = &wit.TypeDef{Kind: &wit.Result{OK: wit.S32{}, Err: errorInfoType}}
var resultParseType = &wit.TypeDef{Kind: &wit.Result{OK: wit.S32{}, Err: wit.String{}}}

var tupleS32S32Type = &wit.TypeDef{Kind: &wit.Tuple{Types: []wit.Type{wit.S32{}, wit.S32{}}}}
var tupleS32S32S32Type = &wit.TypeDef{Kind: &wit.Tuple{Types: []wit.Type{wit.S32{}, wit.S32{}, wit.S32{}}}}

// Tests

func TestEchoPoint(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := map[string]any{"x": int32(10), "y": int32(20)}
	result, err := inst.CallWithTypes(ctx, "echo-point", []wit.Type{pointType}, []wit.Type{pointType}, input)
	if err != nil {
		t.Fatal(err)
	}

	output := result.(map[string]any)
	if output["x"].(int32) != 10 || output["y"].(int32) != 20 {
		t.Errorf("got %v, want {x:10, y:20}", output)
	}
}

func TestEchoPerson(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := map[string]any{"name": "Alice", "age": uint32(30)}
	result, err := inst.CallWithTypes(ctx, "echo-person", []wit.Type{personType}, []wit.Type{personType}, input)
	if err != nil {
		t.Fatal(err)
	}

	output := result.(map[string]any)
	if output["name"].(string) != "Alice" || output["age"].(uint32) != 30 {
		t.Errorf("got %v, want {name:Alice, age:30}", output)
	}
}

func TestEchoRectangle(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := map[string]any{
		"top-left":     map[string]any{"x": int32(0), "y": int32(0)},
		"bottom-right": map[string]any{"x": int32(100), "y": int32(50)},
	}
	result, err := inst.CallWithTypes(ctx, "echo-rectangle", []wit.Type{rectangleType}, []wit.Type{rectangleType}, input)
	if err != nil {
		t.Fatal(err)
	}

	output := result.(map[string]any)
	tl := output["top-left"].(map[string]any)
	br := output["bottom-right"].(map[string]any)
	if tl["x"].(int32) != 0 || tl["y"].(int32) != 0 {
		t.Errorf("top-left: got %v, want {x:0, y:0}", tl)
	}
	if br["x"].(int32) != 100 || br["y"].(int32) != 50 {
		t.Errorf("bottom-right: got %v, want {x:100, y:50}", br)
	}
}

func TestEchoColor(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	for _, tc := range []struct {
		name  string
		input uint32
	}{
		{"red", 0},
		{"green", 1},
		{"blue", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := inst.CallWithTypes(ctx, "echo-color", []wit.Type{colorType}, []wit.Type{colorType}, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if result.(uint32) != tc.input {
				t.Errorf("got %v, want %v", result, tc.input)
			}
		})
	}
}

func TestEchoShape(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	tests := []struct {
		input map[string]any
		name  string
	}{
		{map[string]any{"circle": uint32(10)}, "circle"},
		{map[string]any{"square": uint32(5)}, "square"},
		{map[string]any{"rect": map[string]any{
			"top-left":     map[string]any{"x": int32(0), "y": int32(0)},
			"bottom-right": map[string]any{"x": int32(10), "y": int32(20)},
		}}, "rect"},
		{map[string]any{"none": nil}, "none"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := inst.CallWithTypes(ctx, "echo-shape", []wit.Type{shapeType}, []wit.Type{shapeType}, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			output := result.(map[string]any)
			if _, ok := output[tc.name]; !ok {
				t.Errorf("expected variant %s, got %v", tc.name, output)
			}
		})
	}
}

func TestEchoPermissions(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	tests := []uint64{0, 1, 2, 3, 4, 7}

	for _, input := range tests {
		result, err := inst.CallWithTypes(ctx, "echo-permissions", []wit.Type{permissionsType}, []wit.Type{permissionsType}, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.(uint64) != input {
			t.Errorf("got %v, want %v", result, input)
		}
	}
}

func TestEchoListS32(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []any{int32(1), int32(2), int32(3), int32(-4), int32(5)}
	result, err := inst.CallWithTypes(ctx, "echo-list-s32", []wit.Type{listS32Type}, []wit.Type{listS32Type}, input)
	if err != nil {
		t.Fatal(err)
	}

	output := result.([]any)
	if len(output) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(output), len(input))
	}
	for i := range input {
		if output[i].(int32) != input[i].(int32) {
			t.Errorf("element %d: got %v, want %v", i, output[i], input[i])
		}
	}
}

func TestEchoListString(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []any{"hello", "world", "foo", "bar"}
	result, err := inst.CallWithTypes(ctx, "echo-list-string", []wit.Type{listStringType}, []wit.Type{listStringType}, input)
	if err != nil {
		t.Fatal(err)
	}

	output := result.([]any)
	if len(output) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(output), len(input))
	}
	for i := range input {
		if output[i].(string) != input[i].(string) {
			t.Errorf("element %d: got %v, want %v", i, output[i], input[i])
		}
	}
}

func TestEchoListPoint(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []any{
		map[string]any{"x": int32(1), "y": int32(2)},
		map[string]any{"x": int32(3), "y": int32(4)},
		map[string]any{"x": int32(-5), "y": int32(6)},
	}
	result, err := inst.CallWithTypes(ctx, "echo-list-point", []wit.Type{listPointType}, []wit.Type{listPointType}, input)
	if err != nil {
		t.Fatal(err)
	}

	output := result.([]any)
	if len(output) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(output), len(input))
	}
	for i := range input {
		inPt := input[i].(map[string]any)
		outPt := output[i].(map[string]any)
		if outPt["x"].(int32) != inPt["x"].(int32) || outPt["y"].(int32) != inPt["y"].(int32) {
			t.Errorf("element %d: got %v, want %v", i, outPt, inPt)
		}
	}
}

func TestMaybePoint(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	// Test Some
	result, err := inst.CallWithTypes(ctx, "maybe-point", []wit.Type{wit.Bool{}}, []wit.Type{optionPointType}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Error("expected Some, got None")
	} else {
		pt := result.(map[string]any)
		if pt["x"].(int32) != 42 || pt["y"].(int32) != 24 {
			t.Errorf("got %v, want {x:42, y:24}", pt)
		}
	}

	// Test None
	result, err = inst.CallWithTypes(ctx, "maybe-point", []wit.Type{wit.Bool{}}, []wit.Type{optionPointType}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected None, got %v", result)
	}
}

func TestMaybeString(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	// Test Some
	result, err := inst.CallWithTypes(ctx, "maybe-string", []wit.Type{wit.Bool{}}, []wit.Type{optionStringType}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Error("expected Some, got None")
	} else if result.(string) != "hello" {
		t.Errorf("got %v, want hello", result)
	}

	// Test None
	result, err = inst.CallWithTypes(ctx, "maybe-string", []wit.Type{wit.Bool{}}, []wit.Type{optionStringType}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected None, got %v", result)
	}
}

func TestTryDivide(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	// Success case
	result, err := inst.CallWithTypes(ctx, "try-divide", []wit.Type{wit.S32{}, wit.S32{}}, []wit.Type{resultDivideType}, int32(10), int32(2))
	if err != nil {
		t.Fatal(err)
	}
	output := result.(map[string]any)
	if _, hasOk := output["ok"]; !hasOk {
		t.Errorf("expected ok, got %v", output)
	} else if output["ok"].(int32) != 5 {
		t.Errorf("got %v, want 5", output["ok"])
	}

	// Error case
	result, err = inst.CallWithTypes(ctx, "try-divide", []wit.Type{wit.S32{}, wit.S32{}}, []wit.Type{resultDivideType}, int32(10), int32(0))
	if err != nil {
		t.Fatal(err)
	}
	output = result.(map[string]any)
	if _, hasErr := output["err"]; !hasErr {
		t.Errorf("expected err, got %v", output)
	} else {
		errInfo := output["err"].(map[string]any)
		if errInfo["code"].(uint32) != 1 {
			t.Errorf("error code: got %v, want 1", errInfo["code"])
		}
		if errInfo["message"].(string) != "division by zero" {
			t.Errorf("error message: got %v, want 'division by zero'", errInfo["message"])
		}
	}
}

func TestTryParse(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	// Success case
	result, err := inst.CallWithTypes(ctx, "try-parse", []wit.Type{wit.String{}}, []wit.Type{resultParseType}, "42")
	if err != nil {
		t.Fatal(err)
	}
	output := result.(map[string]any)
	if _, hasOk := output["ok"]; !hasOk {
		t.Errorf("expected ok, got %v", output)
	} else if output["ok"].(int32) != 42 {
		t.Errorf("got %v, want 42", output["ok"])
	}

	// Error case
	result, err = inst.CallWithTypes(ctx, "try-parse", []wit.Type{wit.String{}}, []wit.Type{resultParseType}, "not-a-number")
	if err != nil {
		t.Fatal(err)
	}
	output = result.(map[string]any)
	if _, hasErr := output["err"]; !hasErr {
		t.Errorf("expected err, got %v", output)
	}
}

func TestSwapPair(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	result, err := inst.CallWithTypes(ctx, "swap-pair", []wit.Type{wit.S32{}, wit.S32{}}, []wit.Type{tupleS32S32Type}, int32(1), int32(2))
	if err != nil {
		t.Fatal(err)
	}
	output := result.([]any)
	if len(output) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(output))
	}
	if output[0].(int32) != 2 || output[1].(int32) != 1 {
		t.Errorf("got %v, want [2, 1]", output)
	}
}

func TestTriple(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	result, err := inst.CallWithTypes(ctx, "triple", []wit.Type{wit.S32{}}, []wit.Type{tupleS32S32S32Type}, int32(5))
	if err != nil {
		t.Fatal(err)
	}
	output := result.([]any)
	if len(output) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(output))
	}
	if output[0].(int32) != 5 || output[1].(int32) != 10 || output[2].(int32) != 15 {
		t.Errorf("got %v, want [5, 10, 15]", output)
	}
}

func TestFilterAdults(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []any{
		map[string]any{"name": "Alice", "age": uint32(25)},
		map[string]any{"name": "Bob", "age": uint32(15)},
		map[string]any{"name": "Charlie", "age": uint32(30)},
		map[string]any{"name": "Diana", "age": uint32(17)},
	}
	result, err := inst.CallWithTypes(ctx, "filter-adults", []wit.Type{listPersonType}, []wit.Type{listPersonType}, input)
	if err != nil {
		t.Fatal(err)
	}

	output := result.([]any)
	if len(output) != 2 {
		t.Fatalf("expected 2 adults, got %d", len(output))
	}
	names := make([]string, len(output))
	for i, p := range output {
		names[i] = p.(map[string]any)["name"].(string)
	}
	if names[0] != "Alice" || names[1] != "Charlie" {
		t.Errorf("got %v, want [Alice, Charlie]", names)
	}
}

func TestEmptyList(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	input := []any{}
	result, err := inst.CallWithTypes(ctx, "echo-list-s32", []wit.Type{listS32Type}, []wit.Type{listS32Type}, input)
	if err != nil {
		t.Fatal(err)
	}
	output := result.([]any)
	if len(output) != 0 {
		t.Errorf("expected empty list, got %v", output)
	}
}

func TestLargeList(t *testing.T) {
	skipIfNoComplex(t)
	rt, inst := setupComplexInstance(t)
	ctx := context.Background()
	defer rt.Close(ctx)
	defer inst.Close(ctx)

	n := 1000
	input := make([]any, n)
	for i := 0; i < n; i++ {
		input[i] = int32(i)
	}
	result, err := inst.CallWithTypes(ctx, "echo-list-s32", []wit.Type{listS32Type}, []wit.Type{listS32Type}, input)
	if err != nil {
		t.Fatal(err)
	}
	output := result.([]any)
	if len(output) != n {
		t.Fatalf("expected %d elements, got %d", n, len(output))
	}
	for i := 0; i < n; i++ {
		if output[i].(int32) != int32(i) {
			t.Errorf("element %d: got %v, want %v", i, output[i], i)
			break
		}
	}
}

// Benchmarks

func BenchmarkEchoPoint(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := map[string]any{"x": int32(10), "y": int32(20)}
	params := []wit.Type{pointType}
	results := []wit.Type{pointType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-point", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

type Point struct {
	X int32
	Y int32
}

type Rectangle struct {
	TopLeft     Point
	BottomRight Point
}

func BenchmarkEchoPointTyped(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{pointType}
	results := []wit.Type{pointType}
	input := Point{X: 10, Y: 20}
	var result Point

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-point", params, results, &result, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoListS32_10(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := make([]any, 10)
	for i := range input {
		input[i] = int32(i)
	}
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-list-s32", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoListS32_10_Typed(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := make([]int32, 10)
	for i := range input {
		input[i] = int32(i)
	}
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-list-s32", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoListS32_10_CallInto(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := make([]int32, 10)
	for i := range input {
		input[i] = int32(i)
	}
	result := make([]int32, 10)
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-list-s32", params, results, &result, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoListS32_100(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := make([]any, 100)
	for i := range input {
		input[i] = int32(i)
	}
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-list-s32", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoListS32_100_Typed(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := make([]int32, 100)
	for i := range input {
		input[i] = int32(i)
	}
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-list-s32", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoListString_10(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := make([]any, 10)
	for i := range input {
		input[i] = "hello"
	}
	params := []wit.Type{listStringType}
	results := []wit.Type{listStringType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-list-string", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoRectangle(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := map[string]any{
		"top-left":     map[string]any{"x": int32(0), "y": int32(0)},
		"bottom-right": map[string]any{"x": int32(100), "y": int32(50)},
	}
	params := []wit.Type{rectangleType}
	results := []wit.Type{rectangleType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-rectangle", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoRectangle_Typed(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := Rectangle{
		TopLeft:     Point{X: 0, Y: 0},
		BottomRight: Point{X: 100, Y: 50},
	}
	params := []wit.Type{rectangleType}
	results := []wit.Type{rectangleType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-rectangle", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEchoRectangle_CallInto(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := Rectangle{
		TopLeft:     Point{X: 0, Y: 0},
		BottomRight: Point{X: 100, Y: 50},
	}
	var result Rectangle
	params := []wit.Type{rectangleType}
	results := []wit.Type{rectangleType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-rectangle", params, results, &result, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHostCallbackSum(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := make([]any, 10)
	for i := range input {
		input[i] = int32(i)
	}
	params := []wit.Type{listS32Type}
	results := []wit.Type{wit.S64{}}

	// Check if function exists
	_, err = inst.CallWithTypes(ctx, "compute-sum", params, results, input)
	if err != nil {
		b.Skip("compute-sum function not found in complex.wasm")
		return
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "compute-sum", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHostCallbackDistance(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	a := map[string]any{"x": int32(0), "y": int32(0)}
	b2 := map[string]any{"x": int32(3), "y": int32(4)}
	params := []wit.Type{pointType, pointType}
	results := []wit.Type{wit.F64{}}

	// Check if function exists
	_, err = inst.CallWithTypes(ctx, "compute-distance", params, results, a, b2)
	if err != nil {
		b.Skip("compute-distance function not found in complex.wasm")
		return
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "compute-distance", params, results, a, b2)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Typed benchmark - should use compiled fast path
func BenchmarkEchoPoint_TypedStruct(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := Point{X: 10, Y: 20}
	params := []wit.Type{pointType}
	results := []wit.Type{pointType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-point", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Typed string list benchmark
func BenchmarkEchoListString_10_Typed(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := []string{"hello", "world", "foo", "bar", "baz", "test", "data", "item", "value", "end"}
	params := []wit.Type{listStringType}
	results := []wit.Type{listStringType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo-list-string", params, results, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// CallInto version for strings
func BenchmarkEchoListString_10_CallInto(b *testing.B) {
	if complexWasm == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasm)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	input := []string{"hello", "world", "foo", "bar", "baz", "test", "data", "item", "value", "end"}
	var result []string
	params := []wit.Type{listStringType}
	results := []wit.Type{listStringType}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-list-string", params, results, &result, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}
