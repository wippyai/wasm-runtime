package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/component"
	"go.bytecodealliance.org/wit"
)

type enumOnlyError struct{}

func (*enumOnlyError) Error() string        { return "denied" }
func (*enumOnlyError) WITErrorPayload() any { return uint32(1) }

func TestLowerErrorOnlyResult(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)
	for _, indirect := range []bool{false, true} {
		var errType wit.Type
		if indirect {
			errType = &wit.TypeDef{Kind: &wit.Enum{Cases: []wit.EnumCase{{Name: "unknown"}, {Name: "denied"}}}}
		}
		for _, failed := range []bool{false, true} {
			handler := func() *enumOnlyError {
				if failed {
					return &enumOnlyError{}
				}
				return nil
			}
			w, err := NewLowerWrapper(&component.LowerDef{Name: "error-only", Params: []wit.Type{}, Results: []wit.Type{&wit.TypeDef{Kind: &wit.Result{Err: errType}}}}, handler)
			if err != nil {
				t.Fatal(err)
			}
			if w.usesRetptr() != indirect {
				t.Fatal("unexpected ABI return mode")
			}
			stack := []uint64{64}
			w.BuildRawFunc()(ctx, inst.instance, stack)
			want := uint64(0)
			if failed {
				want = 1
			}
			if indirect {
				data, ok := inst.instance.Memory().Read(64, 2)
				if !ok || uint64(data[0]) != want {
					t.Fatalf("invalid memory result: %v", data)
				}
				if failed && data[1] != 1 {
					t.Fatalf("lost enum payload: %v", data)
				}
			} else if stack[0] != want {
				t.Fatalf("invalid stack discriminant: %v", stack)
			}
		}
	}
}
