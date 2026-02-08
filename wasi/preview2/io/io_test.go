package io

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestErrorHost_MethodErrorToDebugString(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewErrorHost(resources)
	ctx := context.Background()

	err := preview2.NewErrorResource("test error message")
	handle := resources.Add(err)

	result := host.MethodErrorToDebugString(ctx, handle)
	if result != "test error message" {
		t.Errorf("expected 'test error message', got '%s'", result)
	}

	result = host.MethodErrorToDebugString(ctx, 9999)
	if result != "unknown error" {
		t.Errorf("expected 'unknown error' for invalid handle, got '%s'", result)
	}
}

func TestPollHost_Poll(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewPollHost(resources)
	ctx := context.Background()

	p1 := &preview2.PollableResource{}
	p1.SetReady(true)
	h1 := resources.Add(p1)

	p2 := &preview2.PollableResource{}
	p2.SetReady(false)
	h2 := resources.Add(p2)

	p3 := &preview2.PollableResource{}
	p3.SetReady(true)
	h3 := resources.Add(p3)

	ready := host.Poll(ctx, []uint32{h1, h2, h3})

	if len(ready) != 2 {
		t.Errorf("expected 2 ready pollables, got %d", len(ready))
	}
	if ready[0] != 0 || ready[1] != 2 {
		t.Errorf("expected indices [0, 2], got %v", ready)
	}
}

func TestPollHost_MethodPollableReady(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewPollHost(resources)
	ctx := context.Background()

	p := &preview2.PollableResource{}
	p.SetReady(true)
	handle := resources.Add(p)

	if !host.MethodPollableReady(ctx, handle) {
		t.Error("expected pollable to be ready")
	}

	p.SetReady(false)
	if host.MethodPollableReady(ctx, handle) {
		t.Error("expected pollable to not be ready")
	}
}

func TestPollHost_MethodPollableBlock(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewPollHost(resources)
	ctx := context.Background()

	p := &preview2.PollableResource{}
	handle := resources.Add(p)

	host.MethodPollableBlock(ctx, handle)
	if !p.Ready() {
		t.Error("expected pollable to be ready after block")
	}
}

func TestStreamsHost_InputStreamRead(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewInputStreamResource([]byte("hello world"))
	handle := resources.Add(stream)

	data, err := host.MethodInputStreamRead(ctx, handle, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got '%s'", data)
	}

	data, err = host.MethodInputStreamRead(ctx, handle, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != " world" {
		t.Errorf("expected ' world', got '%s'", data)
	}
}

func TestStreamsHost_InputStreamBlockingRead(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewInputStreamResource([]byte("test"))
	handle := resources.Add(stream)

	data, err := host.MethodInputStreamBlockingRead(ctx, handle, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "test" {
		t.Errorf("expected 'test', got '%s'", data)
	}
}

func TestStreamsHost_InputStreamSkip(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewInputStreamResource([]byte("hello world"))
	handle := resources.Add(stream)

	n, err := host.MethodInputStreamSkip(ctx, handle, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 6 {
		t.Errorf("expected to skip 6 bytes, skipped %d", n)
	}

	data, readErr := host.MethodInputStreamRead(ctx, handle, 10)
	if readErr != nil {
		t.Fatalf("unexpected error: %v", readErr)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world' after skip, got '%s'", data)
	}
}

func TestStreamsHost_InputStreamSubscribe(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewInputStreamResource([]byte("test"))
	handle := resources.Add(stream)

	pollableHandle := host.MethodInputStreamSubscribe(ctx, handle)

	r, ok := resources.Get(pollableHandle)
	if !ok {
		t.Fatal("expected pollable to be in resource table")
	}
	if p, ok := r.(*preview2.PollableResource); ok {
		if !p.Ready() {
			t.Error("expected subscribed pollable to be ready")
		}
	} else {
		t.Error("expected resource to be a PollableResource")
	}
}

func TestStreamsHost_OutputStreamWrite(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewOutputStreamResource(nil)
	handle := resources.Add(stream)

	err := host.MethodOutputStreamWrite(ctx, handle, []byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = host.MethodOutputStreamWrite(ctx, handle, []byte(" world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(stream.Bytes()) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", stream.Bytes())
	}
}

func TestStreamsHost_OutputStreamCheckWrite(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewOutputStreamResource(nil)
	handle := resources.Add(stream)

	size, err := host.MethodOutputStreamCheckWrite(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size == 0 {
		t.Error("expected non-zero check-write size")
	}
}

func TestStreamsHost_OutputStreamFlush(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewOutputStreamResource(nil)
	handle := resources.Add(stream)

	err := host.MethodOutputStreamFlush(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamsHost_OutputStreamSubscribe(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewOutputStreamResource(nil)
	handle := resources.Add(stream)

	pollableHandle := host.MethodOutputStreamSubscribe(ctx, handle)

	r, ok := resources.Get(pollableHandle)
	if !ok {
		t.Fatal("expected pollable to be in resource table")
	}
	if p, ok := r.(*preview2.PollableResource); ok {
		if !p.Ready() {
			t.Error("expected subscribed pollable to be ready")
		}
	} else {
		t.Error("expected resource to be a PollableResource")
	}
}

func TestStreamsHost_OutputStreamWriteZeroes(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	stream := preview2.NewOutputStreamResource(nil)
	handle := resources.Add(stream)

	err := host.MethodOutputStreamWriteZeroes(ctx, handle, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := stream.Bytes()
	if len(data) != 10 {
		t.Errorf("expected 10 zero bytes, got %d", len(data))
	}
	for i, b := range data {
		if b != 0 {
			t.Errorf("expected byte %d to be 0, got %d", i, b)
		}
	}
}

func TestStreamsHost_OutputStreamSplice(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	inStream := preview2.NewInputStreamResource([]byte("test data"))
	inHandle := resources.Add(inStream)

	outStream := preview2.NewOutputStreamResource(nil)
	outHandle := resources.Add(outStream)

	n, err := host.MethodOutputStreamSplice(ctx, outHandle, inHandle, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Errorf("expected to splice 4 bytes, spliced %d", n)
	}

	if string(outStream.Bytes()) != "test" {
		t.Errorf("expected 'test', got '%s'", outStream.Bytes())
	}
}

func TestStreamsHost_InvalidHandles(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewStreamsHost(resources)
	ctx := context.Background()

	_, err := host.MethodInputStreamRead(ctx, 9999, 10)
	if err == nil {
		t.Error("expected error for invalid input stream handle")
	}

	writeErr := host.MethodOutputStreamWrite(ctx, 9999, []byte("test"))
	if writeErr == nil {
		t.Error("expected error for invalid output stream handle")
	}
}
