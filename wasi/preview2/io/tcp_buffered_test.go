package io

import (
	"bytes"
	"context"
	std "io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func newPipeStreams(t *testing.T) (*StreamsHost, *preview2.ResourceTable, net.Conn, uint32, uint32, *preview2.TCPSocketResource) {
	t.Helper()
	left, right := net.Pipe()
	socket := preview2.NewTCPSocketResource(4)
	socket.SetConn(left)
	socket.SetState(preview2.TCPStateConnected)

	inStream := preview2.NewTCPInputStreamResource(socket)
	outStream := preview2.NewTCPOutputStreamResource(socket)

	resources := preview2.NewResourceTable()
	inHandle := resources.Add(inStream)
	outHandle := resources.Add(outStream)

	host := NewStreamsHost(resources)
	return host, resources, right, inHandle, outHandle, socket
}

func assertPanics(t *testing.T, op string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic during %s, got none", op)
		}
	}()
	f()
}

func TestTCPBuffered_Subscribe_LiveAndTraps(t *testing.T) {
	host, resources, right, inHandle, outHandle, socket := newPipeStreams(t)
	defer right.Close()
	defer socket.Drop()
	ctx := context.Background()

	// 1. Live Subscribe on TCP input stream delegates to subscriber
	inPollHandle := host.MethodInputStreamSubscribe(ctx, inHandle)
	inPollRes, ok := resources.Get(inPollHandle)
	if !ok {
		t.Fatal("expected input pollable resource")
	}
	inPollable, ok := inPollRes.(preview2.Pollable)
	if !ok {
		t.Fatal("expected Pollable interface for input pollable")
	}

	// Initially input buffer is empty, so pollable is not ready
	if inPollable.Ready() {
		t.Fatal("input pollable should not be ready when buffer is empty")
	}

	// Write data on peer side
	done := make(chan struct{})
	go func() {
		_, _ = right.Write([]byte("ping"))
		close(done)
	}()
	<-done

	// Await ready
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	inPollable.Block(waitCtx)
	if !inPollable.Ready() {
		t.Fatal("input pollable should be ready after peer write")
	}

	// 2. Live Subscribe on TCP output stream delegates to subscriber
	outPollHandle := host.MethodOutputStreamSubscribe(ctx, outHandle)
	outPollRes, ok := resources.Get(outPollHandle)
	if !ok {
		t.Fatal("expected output pollable resource")
	}
	outPollable, ok := outPollRes.(preview2.Pollable)
	if !ok {
		t.Fatal("expected Pollable interface for output pollable")
	}
	if !outPollable.Ready() {
		t.Fatal("output pollable should initially be ready for writing")
	}

	// 3. Fallback on finite in-memory stream produces a ready pollable
	finiteStream := preview2.NewInputStreamResource([]byte("finite"))
	finiteHandle := resources.Add(finiteStream)
	finitePollHandle := host.MethodInputStreamSubscribe(ctx, finiteHandle)
	finitePollRes, ok := resources.Get(finitePollHandle)
	if !ok {
		t.Fatal("expected finite pollable resource")
	}
	if p, ok := finitePollRes.(preview2.Pollable); !ok || !p.Ready() {
		t.Fatal("fallback finite stream subscribe must return ready pollable")
	}

	// 4. Invalid handle and type traps on subscribe
	assertPanics(t, "MethodInputStreamSubscribe invalid handle", func() {
		host.MethodInputStreamSubscribe(ctx, 999999)
	})
	assertPanics(t, "MethodInputStreamSubscribe wrong type (output stream handle)", func() {
		host.MethodInputStreamSubscribe(ctx, outHandle)
	})
	assertPanics(t, "MethodOutputStreamSubscribe invalid handle", func() {
		host.MethodOutputStreamSubscribe(ctx, 999999)
	})
	assertPanics(t, "MethodOutputStreamSubscribe wrong type (input stream handle)", func() {
		host.MethodOutputStreamSubscribe(ctx, inHandle)
	})
}

func TestTCPBuffered_HalfClose_Readiness(t *testing.T) {
	host, resources, right, inHandle, _, socket := newPipeStreams(t)
	defer socket.Drop()
	ctx := context.Background()

	// Subscribe input stream
	inPollHandle := host.MethodInputStreamSubscribe(ctx, inHandle)
	inPollRes, _ := resources.Get(inPollHandle)
	inPollable := inPollRes.(preview2.Pollable)

	if inPollable.Ready() {
		t.Fatal("pollable should not be ready initially")
	}

	// Half-close peer side (sending EOF)
	if err := right.Close(); err != nil {
		t.Fatalf("failed to close peer: %v", err)
	}

	// Half-close must make input pollable ready
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	inPollable.Block(waitCtx)
	if !inPollable.Ready() {
		t.Fatal("pollable should be ready after peer half-close EOF")
	}

	// Blocking read after EOF should return StreamError with Closed: true
	data, streamErr := host.MethodInputStreamBlockingRead(ctx, inHandle, 100)
	if streamErr == nil || !streamErr.Closed {
		t.Fatalf("expected StreamError with Closed: true after EOF, got data=%v err=%v", data, streamErr)
	}
}

func TestTCPBuffered_BlockingRead_ImmediateZeroAndCancellation(t *testing.T) {
	host, _, right, inHandle, _, socket := newPipeStreams(t)
	defer right.Close()
	defer socket.Drop()

	// Zero length is immediate without waiting or blocking
	zeroData, zeroErr := host.MethodInputStreamBlockingRead(context.Background(), inHandle, 0)
	if zeroErr != nil || len(zeroData) != 0 {
		t.Fatalf("expected empty data and nil error for zero length, got %v, %v", zeroData, zeroErr)
	}

	zeroSkip, zeroSkipErr := host.MethodInputStreamBlockingSkip(context.Background(), inHandle, 0)
	if zeroSkipErr != nil || zeroSkip != 0 {
		t.Fatalf("expected 0 skip and nil error for zero length, got %d, %v", zeroSkip, zeroSkipErr)
	}

	// Blocking read waits cancellably using subscriber.Block(ctx)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	data, err := host.MethodInputStreamBlockingRead(canceledCtx, inHandle, 10)
	if err == nil || !err.LastOpFailed {
		t.Fatalf("expected LastOpFailed on canceled blocking read, got data=%v, err=%v", data, err)
	}

	skipN, skipErr := host.MethodInputStreamBlockingSkip(canceledCtx, inHandle, 10)
	if skipErr == nil || !skipErr.LastOpFailed {
		t.Fatalf("expected LastOpFailed on canceled blocking skip, got n=%d, err=%v", skipN, skipErr)
	}
}

func TestTCPBuffered_BlockingRead_UnblocksOnData(t *testing.T) {
	host, _, right, inHandle, _, socket := newPipeStreams(t)
	defer right.Close()
	defer socket.Drop()

	msg := []byte("hello wasi io")
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = right.Write(msg)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	readData, err := host.MethodInputStreamBlockingRead(ctx, inHandle, uint64(len(msg)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(readData, msg) {
		t.Fatalf("expected %q, got %q", string(msg), string(readData))
	}
}

func TestTCPBuffered_Write_ErrWritePermit_Traps(t *testing.T) {
	host, _, right, _, outHandle, socket := newPipeStreams(t)
	defer right.Close()
	defer socket.Drop()
	ctx := context.Background()

	// Write without check-write traps ErrWritePermit
	assertPanics(t, "MethodOutputStreamWrite without check-write", func() {
		_ = host.MethodOutputStreamWrite(ctx, outHandle, []byte("fail"))
	})

	// CheckWrite gives permit
	permit, err := host.MethodOutputStreamCheckWrite(ctx, outHandle)
	if err != nil || permit == 0 {
		t.Fatalf("CheckWrite failed: permit=%d, err=%v", permit, err)
	}

	// Write exceeding permit traps ErrWritePermit
	tooBig := make([]byte, permit+1)
	assertPanics(t, "MethodOutputStreamWrite exceeding permit", func() {
		_ = host.MethodOutputStreamWrite(ctx, outHandle, tooBig)
	})

	// CheckWrite again
	permit, err = host.MethodOutputStreamCheckWrite(ctx, outHandle)
	if err != nil || permit == 0 {
		t.Fatalf("CheckWrite failed: permit=%d, err=%v", permit, err)
	}

	// Write within permit succeeds
	go func() {
		buf := make([]byte, 5)
		_, _ = right.Read(buf)
	}()

	writeErr := host.MethodOutputStreamWrite(ctx, outHandle, []byte("valid"))
	if writeErr != nil {
		t.Fatalf("expected nil error on valid write, got %v", writeErr)
	}

	// Second write without renewed check-write traps ErrWritePermit
	assertPanics(t, "MethodOutputStreamWrite second write without check-write", func() {
		_ = host.MethodOutputStreamWrite(ctx, outHandle, []byte("again"))
	})
}

func TestTCPBuffered_WriteZeroes_BoundsAndTraps(t *testing.T) {
	host, _, right, _, outHandle, socket := newPipeStreams(t)
	defer right.Close()
	defer socket.Drop()
	ctx := context.Background()

	// The host allocation limit returns an owned error without allocating the payload.
	limitErr := host.MethodOutputStreamWriteZeroes(ctx, outHandle, preview2.MaxAllocationSize+100)
	if limitErr == nil || !limitErr.LastOpFailed || limitErr.LastOpFailedErr == 0 {
		t.Fatalf("invalid allocation-limit error: %#v", limitErr)
	}

	// WriteZeroes exceeding permit traps ErrWritePermit
	assertPanics(t, "MethodOutputStreamWriteZeroes without CheckWrite", func() {
		_ = host.MethodOutputStreamWriteZeroes(ctx, outHandle, 100)
	})

	// CheckWrite then write zeroes
	permit, err := host.MethodOutputStreamCheckWrite(ctx, outHandle)
	if err != nil || permit < 10 {
		t.Fatalf("CheckWrite failed: permit=%d, err=%v", permit, err)
	}

	go func() {
		buf := make([]byte, 10)
		_, _ = std.ReadFull(right, buf)
	}()

	zeroesErr := host.MethodOutputStreamWriteZeroes(ctx, outHandle, 10)
	if zeroesErr != nil {
		t.Fatalf("expected nil error, got %v", zeroesErr)
	}
}

func TestTCPBuffered_BlockingWriteAndFlush_Contract(t *testing.T) {
	host, _, right, _, outHandle, socket := newPipeStreams(t)
	defer right.Close()
	defer socket.Drop()
	ctx := context.Background()

	// Enforce WASI <= 4096 trap
	assertPanics(t, "MethodOutputStreamBlockingWriteAndFlush > 4096", func() {
		oversized := make([]byte, 4097)
		_ = host.MethodOutputStreamBlockingWriteAndFlush(ctx, outHandle, oversized)
	})

	assertPanics(t, "MethodOutputStreamBlockingWriteZeroesAndFlush > 4096", func() {
		_ = host.MethodOutputStreamBlockingWriteZeroesAndFlush(ctx, outHandle, 4097)
	})

	// Blocking write and flush within 4096 succeeds and flushes to peer
	payload := []byte("hello blocking write and flush")
	var readBuf bytes.Buffer
	readDone := make(chan struct{})

	go func() {
		defer close(readDone)
		buf := make([]byte, len(payload))
		_, _ = std.ReadFull(right, buf)
		readBuf.Write(buf)
	}()

	err := host.MethodOutputStreamBlockingWriteAndFlush(ctx, outHandle, payload)
	if err != nil {
		t.Fatalf("unexpected error on blocking-write-and-flush: %v", err)
	}

	<-readDone
	if !bytes.Equal(readBuf.Bytes(), payload) {
		t.Fatalf("expected %q, got %q", string(payload), readBuf.String())
	}

	// Blocking write zeroes and flush
	zeroesDone := make(chan struct{})
	go func() {
		defer close(zeroesDone)
		buf := make([]byte, 64)
		_, _ = std.ReadFull(right, buf)
		for _, b := range buf {
			if b != 0 {
				t.Errorf("expected zero byte, got %d", b)
			}
		}
	}()

	zErr := host.MethodOutputStreamBlockingWriteZeroesAndFlush(ctx, outHandle, 64)
	if zErr != nil {
		t.Fatalf("unexpected error on blocking-write-zeroes-and-flush: %v", zErr)
	}
	<-zeroesDone
}

func TestTCPBuffered_Splice_ChecksPermitAvoidsDataLoss(t *testing.T) {
	// Source pipe
	host, _, rightSrc, srcInHandle, _, srcSocket := newPipeStreams(t)
	defer rightSrc.Close()
	defer srcSocket.Drop()

	// Destination pipe
	leftDst, rightDst := net.Pipe()
	defer rightDst.Close()
	dstSocket := preview2.NewTCPSocketResource(4)
	dstSocket.SetConn(leftDst)
	dstSocket.SetState(preview2.TCPStateConnected)
	defer dstSocket.Drop()

	dstOutStream := preview2.NewTCPOutputStreamResource(dstSocket)
	hostDst := host
	dstOutHandle := hostDst.resources.Add(dstOutStream)

	ctx := context.Background()

	// Write data to src
	_, err := rightSrc.Write([]byte("protected-data"))
	if err != nil {
		t.Fatalf("failed to write to source: %v", err)
	}

	// Flush the dst stream so check-write returns 0
	_ = hostDst.MethodOutputStreamFlush(ctx, dstOutHandle)

	// Verify dst permit is 0 while flushing or when destination cannot accept writes
	permit, _ := hostDst.MethodOutputStreamCheckWrite(ctx, dstOutHandle)
	if permit == 0 {
		// When permit is 0, Splice should transfer 0 and MUST NOT consume input
		n, spliceErr := hostDst.MethodOutputStreamSplice(ctx, dstOutHandle, srcInHandle, 14)
		if spliceErr != nil {
			t.Fatalf("unexpected splice error: %v", spliceErr)
		}
		if n != 0 {
			t.Fatalf("expected 0 bytes spliced when permit is 0, got %d", n)
		}

		// Source data must NOT be lost!
		readBack, readErr := hostDst.MethodInputStreamRead(ctx, srcInHandle, 14)
		if readErr != nil {
			t.Fatalf("read after splice failed: %v", readErr)
		}
		if string(readBack) != "protected-data" {
			t.Fatalf("data was lost during splice: expected 'protected-data', got %q", string(readBack))
		}
	}
}

func TestTCPBuffered_BlockingSplice_TransfersAndCancels(t *testing.T) {
	host, _, rightSrc, srcInHandle, _, srcSocket := newPipeStreams(t)
	defer rightSrc.Close()
	defer srcSocket.Drop()

	leftDst, rightDst := net.Pipe()
	defer rightDst.Close()
	dstSocket := preview2.NewTCPSocketResource(4)
	dstSocket.SetConn(leftDst)
	dstSocket.SetState(preview2.TCPStateConnected)
	defer dstSocket.Drop()

	dstOutStream := preview2.NewTCPOutputStreamResource(dstSocket)
	dstOutHandle := host.resources.Add(dstOutStream)

	ctx := context.Background()

	// 1. Zero length returns immediately
	n, zErr := host.MethodOutputStreamBlockingSplice(ctx, dstOutHandle, srcInHandle, 0)
	if zErr != nil || n != 0 {
		t.Fatalf("expected 0, nil for zero length, got %d, %v", n, zErr)
	}

	// 2. Cancellation while waiting
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, cErr := host.MethodOutputStreamBlockingSplice(canceledCtx, dstOutHandle, srcInHandle, 10)
	if cErr == nil || !cErr.LastOpFailed {
		t.Fatalf("expected LastOpFailed on canceled blocking-splice, got %v", cErr)
	}

	// 3. Successful blocking splice
	payload := []byte("spliced-data-payload")
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer to source
	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)
		_, _ = rightSrc.Write(payload)
	}()

	// Reader from destination
	dstBuf := make([]byte, len(payload))
	go func() {
		defer wg.Done()
		_, _ = std.ReadFull(rightDst, dstBuf)
	}()

	spliced, err := host.MethodOutputStreamBlockingSplice(ctx, dstOutHandle, srcInHandle, uint64(len(payload)))
	if err != nil {
		t.Fatalf("blocking splice error: %v", err)
	}
	if spliced != uint64(len(payload)) {
		t.Fatalf("expected %d bytes spliced, got %d", len(payload), spliced)
	}

	wg.Wait()
	if !bytes.Equal(dstBuf, payload) {
		t.Fatalf("expected %q at destination, got %q", string(payload), string(dstBuf))
	}
}
