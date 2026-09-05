package engine

import (
	"context"
	"fmt"
	"go.bytecodealliance.org/wit"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestCanonicalHostInvalidBindingTraps(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"nil module", "", "module is nil"},
		{"missing memory", `(module)`, "module has no memory"},
		{"missing allocator", `(module (memory (export "memory") 1))`, "cabi_realloc not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			rt := wazero.NewRuntime(ctx)
			defer rt.Close(ctx)
			var mod api.Module
			if tc.source != "" {
				b, err := wat.Compile(tc.source)
				if err != nil {
					t.Fatal(err)
				}
				mod, err = rt.Instantiate(ctx, b)
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "missing memory" {
				mod = memorylessModule{mod}
			}
			called := false
			w, err := NewLowerWrapper(&component.LowerDef{Name: "invalid-binding"}, func() { called = true })
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tc.want) {
					t.Errorf("want trap %q, got %v", tc.want, r)
				}
				if called {
					t.Error("host ran with invalid binding")
				}
			}()
			w.callHandler(ctx, mod, nil)
		})
	}
}

type memorylessModule struct{ api.Module }

func (memorylessModule) Memory() api.Memory { return nil }

func TestCheckedHostRejectsBeforeLifting(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	called := false
	checked := false
	w, err := NewLowerWrapper(&component.LowerDef{Name: "bounded", Params: []wit.Type{&wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}}}, CheckedHostFunction{
		Handler: func([]byte) { called = true },
		Validate: func(context.Context, api.Module, []uint64) error {
			checked = true
			return fmt.Errorf("oversized input")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "oversized input") {
			t.Fatalf("expected validation trap, got %v", r)
		}
		if !checked || called {
			t.Fatalf("checked=%v called=%v", checked, called)
		}
	}()
	w.BuildRawFunc()(ctx, inst.instance, []uint64{0, 1 << 27})
}
