package testbed

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/runtime"
)

type packedCanonHost struct{ peeks, mirrors int }

func (*packedCanonHost) Namespace() string { return "test:canon/host@0.1.0" }
func (h *packedCanonHost) Peek(_ context.Context, data []byte) uint32 {
	h.peeks++
	var sum uint32
	for _, b := range data {
		sum += uint32(b)
	}
	return sum
}
func (h *packedCanonHost) Mirror(_ context.Context, data []byte) []byte {
	h.mirrors++
	return append(append([]byte(nil), data...), 0xa5)
}

func TestPackedCanonicalSections_HostCallsAndInstanceIsolation(t *testing.T) {
	ctx := t.Context()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	host := &packedCanonHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("packed_canon.wasm")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.LoadComponent(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatal(err)
	}
	instantiate := func() *runtime.Instance {
		inst, err := mod.InstantiateWithConfig(ctx, &engine.InstanceConfig{EntryExport: "call-mirror"})
		if err != nil {
			t.Fatal(err)
		}
		return inst
	}
	check := func(inst *runtime.Instance, value []byte) {
		t.Helper()
		got, err := inst.Call(ctx, "call-peek", value)
		if err != nil {
			t.Fatal(err)
		}
		var want uint32
		for _, b := range value {
			want += uint32(b)
		}
		if got != want {
			t.Fatalf("peek=%v want %d", got, want)
		}
		got, err = inst.Call(ctx, "call-mirror", value)
		if err != nil {
			t.Fatal(err)
		}
		result, ok := got.([]byte)
		if !ok {
			t.Fatalf("mirror returned %T", got)
		}
		expected := append(append([]byte(nil), value...), 0xa5)
		if !bytes.Equal(result, expected) {
			t.Fatalf("mirror=%x want %x", result, expected)
		}
	}
	a, b := instantiate(), instantiate()
	check(a, []byte{1, 2, 3})
	check(b, []byte{4, 5, 6, 7})
	check(a, []byte{8, 9})
	if err := a.Close(ctx); err != nil {
		t.Fatal(err)
	}
	check(b, []byte{10, 11, 12})
	c := instantiate()
	check(c, []byte{13, 14})
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
	check(c, []byte{15, 16})
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if host.peeks != 6 || host.mirrors != 6 {
		t.Fatalf("host calls peek=%d mirror=%d want 6 each", host.peeks, host.mirrors)
	}
}
