package testbed

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"

	"github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type isoHost struct {
	peeks [][]byte
	mu    sync.Mutex
}

func (h *isoHost) Namespace() string { return "test:iso/host@0.1.0" }

func (h *isoHost) Peek(_ context.Context, data []byte) uint32 {
	cp := append([]byte(nil), data...)
	h.mu.Lock()
	h.peeks = append(h.peeks, cp)
	h.mu.Unlock()
	return isoChecksum(cp)
}

func (h *isoHost) snapshot() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]byte, len(h.peeks))
	for i, p := range h.peeks {
		out[i] = append([]byte(nil), p...)
	}
	return out
}

func isoChecksum(data []byte) uint32 {
	var h uint32 = 2166136261
	for _, b := range data {
		h ^= uint32(b)
		h *= 16777619
	}
	return h
}

func loadTwoCoreHost(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("two_core_host.wasm")
	if err != nil {
		t.Fatalf("two_core_host.wasm: %v", err)
	}
	return data
}

func callTyped[T any](ctx context.Context, t *testing.T, inst *runtime.Instance, name string, args ...any) T {
	t.Helper()
	got, err := inst.Call(ctx, name, args...)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	v, ok := got.(T)
	if !ok {
		t.Fatalf("call %s returned %T (%v), want %T", name, got, got, *new(T))
	}
	return v
}

func asListBytes(t *testing.T, got any) []byte {
	t.Helper()
	switch v := got.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		t.Fatalf("list result type %T (%v)", got, got)
		return nil
	}
}

func TestComponentIsolation_TwoAliveSameCompiledModule(t *testing.T) {
	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer rt.Close(ctx)

	host := &isoHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}
	mod, err := rt.LoadComponent(ctx, loadTwoCoreHost(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("compile: %v", err)
	}

	cfg := &engine.InstanceConfig{EntryExport: "echo-list"}
	instA, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	instB, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	if instA.MemorySize() < 131072 {
		t.Fatalf("A MemorySize=%d, want canonical main >= 131072", instA.MemorySize())
	}
	if instB.MemorySize() < 131072 {
		t.Fatalf("B MemorySize=%d, want canonical main >= 131072", instB.MemorySize())
	}

	payloadA := []byte{0xAA, 0x01, 0x11, 0x21}
	payloadB := []byte{0xBB, 0x02, 0x12, 0x22}
	echoA := []byte("alpha-A")
	echoB := []byte("bravo-B-longer")

	gotEchoA, err := instA.Call(ctx, "echo-list", echoA)
	if err != nil {
		t.Fatalf("A echo-list: %v", err)
	}
	gotEchoB, err := instB.Call(ctx, "echo-list", echoB)
	if err != nil {
		t.Fatalf("B echo-list: %v", err)
	}
	if !bytes.Equal(asListBytes(t, gotEchoA), echoA) {
		t.Fatalf("A echo-list got %q want %q", asListBytes(t, gotEchoA), echoA)
	}
	if !bytes.Equal(asListBytes(t, gotEchoB), echoB) {
		t.Fatalf("B echo-list got %q want %q", asListBytes(t, gotEchoB), echoB)
	}

	if callTyped[uint32](ctx, t, instA, "stamp", uint32(0xAA)) != 0xAA {
		t.Fatal("A stamp")
	}
	if callTyped[uint32](ctx, t, instB, "stamp", uint32(0xBB)) != 0xBB {
		t.Fatal("B stamp")
	}
	if got := callTyped[uint32](ctx, t, instA, "read-stamp"); got != 0xAA {
		t.Fatalf("A read-stamp=%d want 0xAA", got)
	}
	if got := callTyped[uint32](ctx, t, instB, "read-stamp"); got != 0xBB {
		t.Fatalf("B read-stamp=%d want 0xBB", got)
	}

	if callTyped[uint32](ctx, t, instA, "peek-host", payloadA) != isoChecksum(payloadA) {
		t.Fatal("A peek-host")
	}
	if callTyped[uint32](ctx, t, instB, "peek-host", payloadB) != isoChecksum(payloadB) {
		t.Fatal("B peek-host")
	}
	peeks := host.snapshot()
	if len(peeks) != 2 {
		t.Fatalf("host peek count=%d want 2", len(peeks))
	}
	if !bytes.Equal(peeks[0], payloadA) {
		t.Fatalf("host callback 0=%v want %v", peeks[0], payloadA)
	}
	if !bytes.Equal(peeks[1], payloadB) {
		t.Fatalf("host callback 1=%v want %v", peeks[1], payloadB)
	}

	if err := instA.Close(ctx); err != nil {
		t.Fatalf("close A: %v", err)
	}
	if got := callTyped[uint32](ctx, t, instB, "read-stamp"); got != 0xBB {
		t.Fatalf("B read-stamp after A close=%d want 0xBB", got)
	}
	gotEchoB2, err := instB.Call(ctx, "echo-list", echoB)
	if err != nil {
		t.Fatalf("B echo-list after A close: %v", err)
	}
	if !bytes.Equal(asListBytes(t, gotEchoB2), echoB) {
		t.Fatalf("B echo-list after A close got %q want %q", asListBytes(t, gotEchoB2), echoB)
	}
	if callTyped[uint32](ctx, t, instB, "peek-host", payloadB) != isoChecksum(payloadB) {
		t.Fatal("B peek-host after A close")
	}

	instC, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("instantiate C: %v", err)
	}
	if callTyped[uint32](ctx, t, instC, "stamp", uint32(0xCC)) != 0xCC {
		t.Fatal("C stamp")
	}
	if got := callTyped[uint32](ctx, t, instB, "read-stamp"); got != 0xBB {
		t.Fatalf("B read-stamp after C stamp=%d want 0xBB", got)
	}
	if err := instB.Close(ctx); err != nil {
		t.Fatalf("close B: %v", err)
	}
	if got := callTyped[uint32](ctx, t, instC, "read-stamp"); got != 0xCC {
		t.Fatalf("C read-stamp after B close=%d want 0xCC", got)
	}
	if err := instC.Close(ctx); err != nil {
		t.Fatalf("close C: %v", err)
	}
}

func TestComponentIsolation_RepeatedInstantiateAfterClose(t *testing.T) {
	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer rt.Close(ctx)
	if err := rt.RegisterHost(&isoHost{}); err != nil {
		t.Fatalf("register host: %v", err)
	}
	mod, err := rt.LoadComponent(ctx, loadTwoCoreHost(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("compile: %v", err)
	}
	for i := 1; i <= 3; i++ {
		inst, err := mod.InstantiateWithConfig(ctx, &engine.InstanceConfig{EntryExport: "echo-list"})
		if err != nil {
			t.Fatalf("instantiate #%d: %v", i, err)
		}
		marker := uint32(0xA0 + i)
		payload := []byte{byte(marker), byte(i), 0x7E}
		if callTyped[uint32](ctx, t, inst, "stamp", marker) != marker {
			t.Fatalf("stamp #%d", i)
		}
		if got := callTyped[uint32](ctx, t, inst, "read-stamp"); got != marker {
			t.Fatalf("read-stamp #%d = %d", i, got)
		}
		got, err := inst.Call(ctx, "echo-list", payload)
		if err != nil {
			t.Fatalf("echo-list #%d: %v", i, err)
		}
		if !bytes.Equal(asListBytes(t, got), payload) {
			t.Fatalf("echo-list #%d got %v want %v", i, asListBytes(t, got), payload)
		}
		if callTyped[uint32](ctx, t, inst, "peek-host", payload) != isoChecksum(payload) {
			t.Fatalf("peek-host #%d", i)
		}
		if err := inst.Close(ctx); err != nil {
			t.Fatalf("close #%d: %v", i, err)
		}
	}
}

func TestComponentIsolation_DefaultVsEntryExportMemory(t *testing.T) {
	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer rt.Close(ctx)
	if err := rt.RegisterHost(&isoHost{}); err != nil {
		t.Fatalf("register host: %v", err)
	}
	mod, err := rt.LoadComponent(ctx, loadTwoCoreHost(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("compile: %v", err)
	}

	t.Run("default instantiate selects canonical memory", func(t *testing.T) {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("default instantiate: %v", err)
		}
		defer inst.Close(ctx)
		if inst.MemorySize() < 131072 {
			t.Fatalf("default MemorySize=%d, want canonical main >= 131072 (first core is 65536)", inst.MemorySize())
		}
	})

	t.Run("EntryExport same core independent instances", func(t *testing.T) {
		a, err := mod.InstantiateWithConfig(ctx, &engine.InstanceConfig{EntryExport: "echo-list"})
		if err != nil {
			t.Fatalf("echo-list: %v", err)
		}
		defer a.Close(ctx)
		b, err := mod.InstantiateWithConfig(ctx, &engine.InstanceConfig{EntryExport: "echo"})
		if err != nil {
			t.Fatalf("echo: %v", err)
		}
		defer b.Close(ctx)
		if a.MemorySize() < 131072 || b.MemorySize() < 131072 {
			t.Fatalf("EntryExport memory echo-list=%d echo=%d, want >= 131072", a.MemorySize(), b.MemorySize())
		}
		if a.MemorySize() != b.MemorySize() {
			t.Fatalf("same-core sizes differ: %d vs %d", a.MemorySize(), b.MemorySize())
		}
		if callTyped[uint32](ctx, t, a, "stamp", uint32(0x11)) != 0x11 {
			t.Fatal("stamp A")
		}
		if callTyped[uint32](ctx, t, b, "stamp", uint32(0x22)) != 0x22 {
			t.Fatal("stamp B")
		}
		if got := callTyped[uint32](ctx, t, a, "read-stamp"); got != 0x11 {
			t.Fatalf("A read-stamp=%d want 0x11", got)
		}
		if got := callTyped[uint32](ctx, t, b, "read-stamp"); got != 0x22 {
			t.Fatalf("B read-stamp=%d want 0x22", got)
		}
	})
}

func TestComponentIsolation_WASITwoAlive(t *testing.T) {
	data, err := os.ReadFile("reinit.wasm")
	if err != nil {
		t.Skip("reinit.wasm not found")
	}
	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	if err := rt.RegisterWASI(preview2.New()); err != nil {
		t.Fatalf("register WASI: %v", err)
	}
	if err := rt.RegisterHost(reinitSeedHost{}); err != nil {
		t.Fatalf("register seed: %v", err)
	}
	mod, err := rt.LoadComponent(ctx, data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("compile: %v", err)
	}
	cfg := &engine.InstanceConfig{EntryExport: "probe"}
	instA, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	instB, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	gotA, err := instA.Call(ctx, "probe")
	if err != nil {
		t.Fatalf("probe A: %v", err)
	}
	gotB, err := instB.Call(ctx, "probe")
	if err != nil {
		t.Fatalf("probe B: %v", err)
	}
	if gotA != uint32(42) {
		t.Fatalf("probe A=%v want 42", gotA)
	}
	if gotB != uint32(42) {
		t.Fatalf("probe B=%v want 42", gotB)
	}
	if err := instA.Close(ctx); err != nil {
		t.Fatalf("close A: %v", err)
	}
	gotB2, err := instB.Call(ctx, "probe")
	if err != nil {
		t.Fatalf("probe B after A close: %v", err)
	}
	if gotB2 != uint32(42) {
		t.Fatalf("probe B after A close=%v want 42", gotB2)
	}
	if err := instB.Close(ctx); err != nil {
		t.Fatalf("close B: %v", err)
	}
}

func TestComponentIsolation_StringsHostWritebackAfterSiblingClose(t *testing.T) {
	data, err := os.ReadFile("strings.wasm")
	if err != nil {
		t.Skip("strings.wasm not found")
	}
	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	host := &isoStringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}
	mod, err := rt.LoadComponent(ctx, data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("compile: %v", err)
	}
	cfg := &engine.InstanceConfig{EntryExport: "process"}
	instA, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	instB, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	gotA, err := instA.Call(ctx, "process", "alpha")
	if err != nil {
		t.Fatalf("process A: %v", err)
	}
	gotB, err := instB.Call(ctx, "process", "bravo")
	if err != nil {
		t.Fatalf("process B: %v", err)
	}
	if gotA != "alpha!" {
		t.Fatalf("process A=%q want alpha!", gotA)
	}
	if gotB != "bravo!" {
		t.Fatalf("process B=%q want bravo!", gotB)
	}
	if err := instA.Close(ctx); err != nil {
		t.Fatalf("close A: %v", err)
	}
	gotB2, err := instB.Call(ctx, "process", "charlie")
	if err != nil {
		t.Fatalf("process B after A close: %v", err)
	}
	if gotB2 != "charlie!" {
		t.Fatalf("process B after A close=%q want charlie!", gotB2)
	}
	if err := instB.Close(ctx); err != nil {
		t.Fatalf("close B: %v", err)
	}
}

type isoStringsHost struct{}

func (isoStringsHost) Namespace() string { return "test:strings/host@0.1.0" }

func (isoStringsHost) Log(context.Context, string) {}

func (isoStringsHost) Concat(_ context.Context, a, b string) string { return a + b }
