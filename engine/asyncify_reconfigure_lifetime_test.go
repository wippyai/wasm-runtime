package engine

import (
	"context"
	"errors"
	"testing"
)

func TestEnableAsyncifyRejectsStoppedInstanceBeforeMemoryWrite(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()
	// A canceled shutdown join must retain memory, while rejecting new work.
	_, leave, err := inst.enterExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := inst.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close = %v", err)
	}
	const addr = 4096
	const sentinel = 0xfeedface
	memory := inst.instance.Memory()
	if !memory.WriteUint32Le(addr, sentinel) {
		t.Fatal("write sentinel")
	}
	previous := inst.Asyncify()
	if err := inst.EnableAsyncify(AsyncifyConfig{DataAddr: addr, StackSize: 1024}); err == nil {
		t.Error("reconfiguration accepted after stop")
	}
	if got, ok := memory.ReadUint32Le(addr); !ok || got != sentinel {
		t.Errorf("stopped reconfiguration wrote guest memory: %x", got)
	}
	if inst.Asyncify() != previous {
		t.Error("stopped reconfiguration published controls")
	}
}
