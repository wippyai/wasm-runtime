package linker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

const acquireHostName = "test:minimal/host@0.1.0"

func acquireComputeHandler() api.GoModuleFunc {
	return func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] *= stack[1]
	}
}

func acquireHostBuilder(ctx context.Context, rt wazero.Runtime, name string) func() (api.Module, error) {
	return func() (api.Module, error) {
		return rt.NewHostModuleBuilder(name).
			NewFunctionBuilder().
			WithGoModuleFunction(acquireComputeHandler(), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
			Export("compute").
			Instantiate(ctx)
	}
}

func virtualComputeSource() *coreInstance {
	virt := NewVirtualInstance("host")
	virt.Define("compute", Entity{
		Kind: EntityFunc,
		Source: HostFunc{
			Def: &FuncDef{
				Name:        "compute",
				Handler:     acquireComputeHandler(),
				ParamTypes:  []api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
				ResultTypes: []api.ValueType{api.ValueTypeI32},
			},
		},
	})
	return &coreInstance{virtual: virt}
}

func newAcquireInstance(l *Linker) *Instance {
	return &Instance{
		pre:            &InstancePre{linker: l},
		bridgeModules:  make(map[string]bool),
		virtualBridges: make(map[string]bool),
	}
}

func linkerRefCount(l *Linker, name string) (int, bool) {
	l.hostModuleMu.Lock()
	defer l.hostModuleMu.Unlock()
	n, ok := l.bridgeRefCount[name]
	return n, ok
}

func compileComputeConsumer(ctx context.Context, t *testing.T, rt wazero.Runtime, importMod string) wazero.CompiledModule {
	t.Helper()
	wasm, err := wat.Compile(fmt.Sprintf(`(module
		(import %q "compute" (func $compute (param i32 i32) (result i32)))
		(func (export "run") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			call $compute
		)
	)`, importMod))
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	return compiled
}

func instantiateAndCallCompute(ctx context.Context, rt wazero.Runtime, compiled wazero.CompiledModule, guestName string) error {
	guest, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(guestName))
	if err != nil {
		return err
	}
	defer guest.Close(ctx)

	fn := guest.ExportedFunction("run")
	if fn == nil {
		return fmt.Errorf("guest %q missing run", guestName)
	}
	results, err := fn.Call(ctx, 6, 7)
	if err != nil {
		return err
	}
	if len(results) != 1 || results[0] != 42 {
		return fmt.Errorf("run(6, 7) = %v, want [42]", results)
	}
	return nil
}

func TestBridgeAcquire_ConcurrentAcquireVsFinalRelease(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	defer l.Close()

	compiled := compileComputeConsumer(ctx, t, rt, acquireHostName)
	defer compiled.Close(ctx)

	const goroutines = 8
	const rounds = 40

	for round := 0; round < rounds; round++ {
		holder := newAcquireInstance(l)
		if _, err := holder.createBridgeFrom(ctx, acquireHostName, virtualComputeSource(), nil, nil, nil); err != nil {
			t.Fatalf("round %d seed createBridgeFrom: %v", round, err)
		}
		if n, ok := linkerRefCount(l, acquireHostName); !ok || n != 1 {
			t.Fatalf("round %d seed ref count = %d present=%v, want 1", round, n, ok)
		}

		var wg sync.WaitGroup
		errCh := make(chan error, goroutines*2+1)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := holder.Close(ctx); err != nil {
				errCh <- fmt.Errorf("seed close: %w", err)
			}
		}()

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				owned := make(map[string]bool)
				mod, _, err := l.getOrCreateHostModuleAndAcquire(ctx, acquireHostName, owned, acquireHostBuilder(ctx, rt, acquireHostName))
				if err != nil {
					errCh <- fmt.Errorf("helper acquire %d: %w", id, err)
					return
				}
				if mod == nil || rt.Module(acquireHostName) == nil {
					errCh <- fmt.Errorf("helper acquire %d: host module not instantiated", id)
					l.releaseBridgeRefs(ctx, owned)
					return
				}
				if !owned[acquireHostName] {
					errCh <- fmt.Errorf("helper acquire %d: owned ref not recorded", id)
					return
				}
				if err := instantiateAndCallCompute(ctx, rt, compiled, fmt.Sprintf("helper-guest-%d-%d", round, id)); err != nil {
					errCh <- fmt.Errorf("helper guest %d: %w", id, err)
				}
				l.releaseBridgeRefs(ctx, owned)
			}(i)

			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				inst := newAcquireInstance(l)
				if _, err := inst.createBridgeFrom(ctx, acquireHostName, virtualComputeSource(), nil, nil, nil); err != nil {
					errCh <- fmt.Errorf("createBridgeFrom %d: %w", id, err)
					return
				}
				if !inst.bridgeModules[acquireHostName] {
					errCh <- fmt.Errorf("createBridgeFrom %d: owned ref not recorded", id)
					_ = inst.Close(ctx)
					return
				}
				if rt.Module(acquireHostName) == nil {
					errCh <- fmt.Errorf("createBridgeFrom %d: host module not instantiated", id)
					_ = inst.Close(ctx)
					return
				}
				if err := instantiateAndCallCompute(ctx, rt, compiled, fmt.Sprintf("bridge-guest-%d-%d", round, id)); err != nil {
					errCh <- fmt.Errorf("bridge guest %d: %w", id, err)
				}
				if err := inst.Close(ctx); err != nil {
					errCh <- fmt.Errorf("createBridgeFrom close %d: %w", id, err)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Errorf("round %d: %v", round, err)
		}

		if n, ok := linkerRefCount(l, acquireHostName); ok {
			t.Fatalf("round %d leftover ref count = %d", round, n)
		}
		if rt.Module(acquireHostName) != nil {
			t.Fatalf("round %d host module still instantiated after last release", round)
		}
	}
}

func TestBridgeAcquire_OncePerInstanceBalance(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	defer l.Close()

	compiled := compileComputeConsumer(ctx, t, rt, acquireHostName)
	defer compiled.Close(ctx)

	owned := make(map[string]bool)
	builder := acquireHostBuilder(ctx, rt, acquireHostName)
	if _, _, err := l.getOrCreateHostModuleAndAcquire(ctx, acquireHostName, owned, builder); err != nil {
		t.Fatalf("first helper acquire: %v", err)
	}
	if _, _, err := l.getOrCreateHostModuleAndAcquire(ctx, acquireHostName, owned, builder); err != nil {
		t.Fatalf("duplicate helper acquire: %v", err)
	}
	if n, ok := linkerRefCount(l, acquireHostName); !ok || n != 1 {
		t.Fatalf("helper duplicate-name ref count = %d present=%v, want 1", n, ok)
	}
	if !owned[acquireHostName] {
		t.Fatal("helper did not record owned ref")
	}

	mod, created, err := l.getOrCreateHostModule(ctx, acquireHostName, builder)
	if err != nil {
		t.Fatalf("non-owning getOrCreateHostModule: %v", err)
	}
	if mod == nil || created {
		t.Fatalf("non-owning getOrCreateHostModule mod=%v created=%v, want reused module", mod, created)
	}
	if n, ok := linkerRefCount(l, acquireHostName); !ok || n != 1 {
		t.Fatalf("non-owning getOrCreateHostModule changed ref count to %d present=%v", n, ok)
	}

	l.releaseBridgeRefs(ctx, owned)
	if n, ok := linkerRefCount(l, acquireHostName); ok {
		t.Fatalf("helper release leftover ref count = %d", n)
	}

	inst := newAcquireInstance(l)
	src := virtualComputeSource()
	if _, err := inst.createBridgeFrom(ctx, acquireHostName, src, nil, nil, nil); err != nil {
		t.Fatalf("first createBridgeFrom: %v", err)
	}
	if _, err := inst.createBridgeFrom(ctx, acquireHostName, src, nil, nil, nil); err != nil {
		t.Fatalf("duplicate createBridgeFrom: %v", err)
	}
	if n, ok := linkerRefCount(l, acquireHostName); !ok || n != 1 {
		t.Fatalf("same-instance createBridgeFrom ref count = %d present=%v, want 1", n, ok)
	}

	other := newAcquireInstance(l)
	if _, err := other.createBridgeFrom(ctx, acquireHostName, virtualComputeSource(), nil, nil, nil); err != nil {
		t.Fatalf("second instance createBridgeFrom: %v", err)
	}
	if n, ok := linkerRefCount(l, acquireHostName); !ok || n != 2 {
		t.Fatalf("two-instance ref count = %d present=%v, want 2", n, ok)
	}

	if err := instantiateAndCallCompute(ctx, rt, compiled, "once-guest"); err != nil {
		t.Fatalf("guest after balanced acquire: %v", err)
	}

	if err := inst.Close(ctx); err != nil {
		t.Fatalf("close first instance: %v", err)
	}
	if n, ok := linkerRefCount(l, acquireHostName); !ok || n != 1 {
		t.Fatalf("after first Close ref count = %d present=%v, want 1", n, ok)
	}
	if rt.Module(acquireHostName) == nil {
		t.Fatal("host module closed while second instance still holds a ref")
	}

	if err := other.Close(ctx); err != nil {
		t.Fatalf("close second instance: %v", err)
	}
	if n, ok := linkerRefCount(l, acquireHostName); ok {
		t.Fatalf("after last Close leftover ref count = %d", n)
	}
	if rt.Module(acquireHostName) != nil {
		t.Fatal("host module still instantiated after last instance Close")
	}
}

func TestBridgeAcquire_FailedBuilderNoLease(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	defer l.Close()

	owned := make(map[string]bool)
	buildErr := errors.New("build failed")
	mod, created, err := l.getOrCreateHostModuleAndAcquire(ctx, acquireHostName, owned, func() (api.Module, error) {
		return nil, buildErr
	})
	if !errors.Is(err, buildErr) {
		t.Fatalf("err = %v, want %v", err, buildErr)
	}
	if mod != nil || created {
		t.Fatalf("failed builder returned mod=%v created=%v", mod, created)
	}
	if owned[acquireHostName] {
		t.Fatal("failed builder recorded an owned ref")
	}
	if n, ok := linkerRefCount(l, acquireHostName); ok {
		t.Fatalf("failed builder leased ref count = %d", n)
	}
	if rt.Module(acquireHostName) != nil {
		t.Fatal("failed builder left a host module instantiated")
	}

	if _, _, err := l.getOrCreateHostModuleAndAcquire(ctx, acquireHostName, owned, acquireHostBuilder(ctx, rt, acquireHostName)); err != nil {
		t.Fatalf("acquire after failed builder: %v", err)
	}
	if n, ok := linkerRefCount(l, acquireHostName); !ok || n != 1 {
		t.Fatalf("successful acquire after failure ref count = %d present=%v, want 1", n, ok)
	}
	l.releaseBridgeRefs(ctx, owned)
}

func TestBridgeAcquire_RejectsMissingOwnerBeforeBuild(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	l := NewWithDefaults(rt)
	called := false
	mod, created, err := l.getOrCreateHostModuleAndAcquire(ctx, acquireHostName, nil, func() (api.Module, error) {
		called = true
		return nil, nil
	})
	if err == nil || mod != nil || created || called {
		t.Fatal("missing ownership must not create an unleased bridge")
	}
}
