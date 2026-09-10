package linker

import (
	"context"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestInstancePreConcurrentCloseKeepsSiblingCode(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCompilationCache(cache))
	defer rt.Close(ctx)
	code, err := wat.Compile(`(module (func (export "answer") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := rt.CompileModule(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := rt.CompileModule(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	defer sibling.Close(ctx)
	pre := &InstancePre{compiled: []wazero.CompiledModule{first}}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 100 {
				_ = pre.CompiledModules()
				if err := pre.Close(ctx); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	mod, err := rt.InstantiateModule(ctx, sibling, wazero.NewModuleConfig())
	if err != nil {
		t.Fatal("closing one owner evicted sibling code:", err)
	}
	got, err := mod.ExportedFunction("answer").Call(ctx)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("sibling result %v, %v", got, err)
	}
	if _, err := pre.NewInstance(ctx); err == nil {
		t.Fatal("closed template accepted new instance")
	}
}
