package preview2

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stallConn wraps a net.Conn and deliberately does not support deadlines or half-close,
// forcing a full Close to unblock any pending Read or Write.
type stallConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *stallConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func (c *stallConn) SetDeadline(time.Time) error {
	return errors.New("unsupported deadline")
}

func (c *stallConn) SetReadDeadline(time.Time) error {
	return errors.New("unsupported read deadline")
}

func (c *stallConn) SetWriteDeadline(time.Time) error {
	return errors.New("unsupported write deadline")
}

func TestTCPStreamAbortStalledReadWritePumpsExitBeforeReturn(t *testing.T) {
	for _, abortFrom := range []string{"input", "output"} {
		t.Run("abort_from_"+abortFrom, func(t *testing.T) {
			left, right := net.Pipe()
			defer right.Close()

			wrapped := &stallConn{Conn: left}
			socket := NewTCPSocketResource(4)
			socket.SetConn(wrapped)
			socket.SetState(TCPStateConnected)

			input := NewTCPInputStreamResource(socket)
			output := NewTCPOutputStreamResource(socket)

			// Verify input pump is running and waiting for data.
			// Stall the output pump by writing data that peer won't read.
			permit, err := output.CheckWrite()
			if err != nil {
				t.Fatalf("unexpected check-write error: %v", err)
			}
			if permit == 0 {
				t.Fatal("expected non-zero write permit")
			}
			if err := output.Write([]byte("stalled payload")); err != nil {
				t.Fatalf("unexpected write error: %v", err)
			}

			// Ensure both pumps are in active I/O.
			// input pump is blocked in wrapped.Read; output pump is blocked in wrapped.Write.
			time.Sleep(20 * time.Millisecond)
			select {
			case <-input.buffer.doneCh:
				t.Fatal("input pump exited prematurely")
			default:
			}
			select {
			case <-output.buffer.doneCh:
				t.Fatal("output pump exited prematurely")
			default:
			}

			// Half-close Drop must NOT unblock stalled pumps since deadlines/CloseRead/CloseWrite are unsupported.
			input.Drop()
			output.Drop()
			time.Sleep(20 * time.Millisecond)
			select {
			case <-input.buffer.doneCh:
				t.Fatal("input pump exited from half-close Drop alone")
			default:
			}
			select {
			case <-output.buffer.doneCh:
				t.Fatal("output pump exited from half-close Drop alone")
			default:
			}

			// AbortSocket must force full connection close and join BOTH pumps before returning.
			if abortFrom == "input" {
				input.AbortSocket()
			} else {
				output.AbortSocket()
			}

			// Verify both pumps have exited at the instant AbortSocket returns.
			select {
			case <-input.buffer.doneCh:
			default:
				t.Fatal("input pump did not exit before AbortSocket returned")
			}
			select {
			case <-output.buffer.doneCh:
			default:
				t.Fatal("output pump did not exit before AbortSocket returned")
			}

			if !wrapped.closed.Load() {
				t.Fatal("expected connection to be physically closed")
			}
			if socket.State() != TCPStateClosed {
				t.Fatalf("expected socket state TCPStateClosed, got %v", socket.State())
			}
		})
	}
}

func TestTCPStreamAbortTableRemovalIdempotentAndQuotaOrdering(t *testing.T) {
	budget := NewSocketBudget(1)
	table := NewResourceTableWithBudget(10, budget)

	left, right := net.Pipe()
	defer right.Close()

	wrapped := &stallConn{Conn: left}
	socket := NewTCPSocketResource(4)
	socket.SetConn(wrapped)
	socket.SetState(TCPStateConnected)

	socketHandle, err := table.TryAdd(socket)
	if err != nil {
		t.Fatalf("failed to add socket: %v", err)
	}

	if budget.Available() != 0 {
		t.Fatalf("expected 0 available socket budget, got %d", budget.Available())
	}

	input := NewTCPInputStreamResource(socket)
	output := NewTCPOutputStreamResource(socket)

	inputHandle, err := table.TryAdd(input)
	if err != nil {
		t.Fatalf("failed to add input stream: %v", err)
	}
	outputHandle, err := table.TryAdd(output)
	if err != nil {
		t.Fatalf("failed to add output stream: %v", err)
	}

	// 1. Calling AbortSocket closes the socket and joins pumps, but does NOT release table lease itself.
	input.AbortSocket()

	select {
	case <-input.buffer.doneCh:
	default:
		t.Fatal("input pump did not exit")
	}
	select {
	case <-output.buffer.doneCh:
	default:
		t.Fatal("output pump did not exit")
	}

	if !wrapped.closed.Load() {
		t.Fatal("expected physical connection close")
	}

	// Quota must remain allocated to the table handle until table removal.
	if budget.Available() != 0 {
		t.Fatalf("AbortSocket released table lease early; expected 0 available, got %d", budget.Available())
	}

	// 2. Table removal owns quota release through physical cleanup.
	table.Remove(socketHandle)
	if budget.Available() != 1 {
		t.Fatalf("expected budget 1 after table removal, got %d", budget.Available())
	}

	// Removing stream handles is safe and cleans up handle capacity.
	table.Remove(inputHandle)
	table.Remove(outputHandle)

	// Can allocate a new socket now that budget has been released.
	newSocket := NewTCPSocketResource(4)
	newHandle, err := table.TryAdd(newSocket)
	if err != nil {
		t.Fatalf("failed to allocate socket after budget release: %v", err)
	}
	table.Remove(newHandle)
}

func TestTCPStreamAbortSimultaneousWithTableRemoval(t *testing.T) {
	for i := 0; i < 50; i++ {
		budget := NewSocketBudget(1)
		table := NewResourceTableWithBudget(10, budget)

		left, right := net.Pipe()
		wrapped := &stallConn{Conn: left}
		socket := NewTCPSocketResource(4)
		socket.SetConn(wrapped)
		socket.SetState(TCPStateConnected)

		socketHandle, err := table.TryAdd(socket)
		if err != nil {
			t.Fatalf("iteration %d: failed to add socket: %v", i, err)
		}
		input := NewTCPInputStreamResource(socket)
		output := NewTCPOutputStreamResource(socket)
		inputHandle, err := table.TryAdd(input)
		if err != nil {
			t.Fatalf("iteration %d: failed to add input: %v", i, err)
		}
		outputHandle, err := table.TryAdd(output)
		if err != nil {
			t.Fatalf("iteration %d: failed to add output: %v", i, err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(5)

		go func() {
			defer wg.Done()
			<-start
			input.AbortSocket()
		}()
		go func() {
			defer wg.Done()
			<-start
			output.AbortSocket()
		}()
		go func() {
			defer wg.Done()
			<-start
			table.Remove(socketHandle)
		}()
		go func() {
			defer wg.Done()
			<-start
			table.Remove(inputHandle)
		}()
		go func() {
			defer wg.Done()
			<-start
			table.Remove(outputHandle)
		}()

		close(start)
		wg.Wait()
		_ = right.Close()

		if budget.Available() != 1 {
			t.Fatalf("iteration %d: expected budget 1, got %d", i, budget.Available())
		}
		if socket.State() != TCPStateClosed {
			t.Fatalf("iteration %d: expected TCPStateClosed, got %v", i, socket.State())
		}
	}
}

func TestTCPStreamAbortRepeatedAndFutureOperationsClosed(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()

	socket := NewTCPSocketResource(4)
	socket.SetConn(left)
	socket.SetState(TCPStateConnected)

	input := NewTCPInputStreamResource(socket)
	output := NewTCPOutputStreamResource(socket)

	// Repeated aborts on both streams and socket directly must be idempotent.
	for i := 0; i < 5; i++ {
		input.AbortSocket()
		output.AbortSocket()
		socket.Drop()
	}

	// Future operations on input stream must fail with closed error.
	if _, err := input.Read(10); err == nil {
		t.Fatal("expected error reading from aborted input stream")
	} else {
		var se *StreamError
		if !errors.As(err, &se) || !se.Closed {
			t.Fatalf("expected StreamError{Closed: true}, got: %v", err)
		}
	}
	if !input.Ready() {
		t.Fatal("expected dropped input stream to be ready")
	}
	select {
	case <-input.Notify():
	default:
		t.Fatal("expected input notify channel to be closed")
	}

	// Future operations on output stream must fail with closed error.
	if err := output.Write([]byte("payload")); err == nil {
		t.Fatal("expected error writing to aborted output stream")
	} else {
		var se *StreamError
		if !errors.As(err, &se) || !se.Closed {
			t.Fatalf("expected StreamError{Closed: true}, got: %v", err)
		}
	}
	if _, err := output.CheckWrite(); err == nil {
		t.Fatal("expected error on CheckWrite for aborted output stream")
	} else {
		var se *StreamError
		if !errors.As(err, &se) || !se.Closed {
			t.Fatalf("expected StreamError{Closed: true}, got: %v", err)
		}
	}
	if err := output.Flush(); err == nil {
		t.Fatal("expected error on Flush for aborted output stream")
	} else {
		var se *StreamError
		if !errors.As(err, &se) || !se.Closed {
			t.Fatalf("expected StreamError{Closed: true}, got: %v", err)
		}
	}
	if !output.Ready() {
		t.Fatal("expected dropped output stream to be ready")
	}
	select {
	case <-output.Notify():
	default:
		t.Fatal("expected output notify channel to be closed")
	}

	// Future operations on socket must reflect closed state.
	if socket.State() != TCPStateClosed {
		t.Fatalf("expected TCPStateClosed, got %v", socket.State())
	}
	if socket.Conn() != nil {
		t.Fatal("expected conn to be nil")
	}
	socket.SetState(TCPStateConnected)
	if socket.State() != TCPStateClosed {
		t.Fatal("dropped socket revived state")
	}

	lateConn, peer := net.Pipe()
	defer peer.Close()
	socket.SetConn(lateConn)
	if socket.Conn() != nil {
		t.Fatal("dropped socket accepted late connection")
	}
}

func TestTCPStreamAbortNilAndUninitializedSafe(t *testing.T) {
	// Nil receivers
	var nilInput *TCPInputStreamResource
	nilInput.AbortSocket() // must not panic

	var nilOutput *TCPOutputStreamResource
	nilOutput.AbortSocket() // must not panic

	// Uninitialized struct pointers
	uninitInput := &TCPInputStreamResource{}
	uninitInput.AbortSocket() // must not panic
	if _, err := uninitInput.Read(10); err == nil {
		t.Fatal("expected error from uninitialized input stream Read")
	}
	if !uninitInput.Ready() {
		t.Fatal("expected uninitialized input stream to be ready")
	}
	select {
	case <-uninitInput.Notify():
	default:
		t.Fatal("expected uninitialized input notify channel to be closed")
	}

	uninitOutput := &TCPOutputStreamResource{}
	uninitOutput.AbortSocket() // must not panic
	if err := uninitOutput.Write([]byte("x")); err == nil {
		t.Fatal("expected error from uninitialized output stream Write")
	}
	if _, err := uninitOutput.CheckWrite(); err == nil {
		t.Fatal("expected error from uninitialized output stream CheckWrite")
	}
	if err := uninitOutput.Flush(); err == nil {
		t.Fatal("expected error from uninitialized output stream Flush")
	}
	if !uninitOutput.Ready() {
		t.Fatal("expected uninitialized output stream to be ready")
	}
	select {
	case <-uninitOutput.Notify():
	default:
		t.Fatal("expected uninitialized output notify channel to be closed")
	}

	// New with nil socket
	nilSocketInput := NewTCPInputStreamResource(nil)
	nilSocketInput.AbortSocket() // must not panic

	nilSocketOutput := NewTCPOutputStreamResource(nil)
	nilSocketOutput.AbortSocket() // must not panic

	// New with closed/dropped socket
	closedSocket := NewTCPSocketResource(4)
	closedSocket.Drop()
	closedInput := NewTCPInputStreamResource(closedSocket)
	closedInput.AbortSocket() // must not panic

	closedOutput := NewTCPOutputStreamResource(closedSocket)
	closedOutput.AbortSocket() // must not panic
}

func TestTCPStreamDropHalfCloseAndSubscriptionNonOwning(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()

	socket := NewTCPSocketResource(4)
	socket.SetConn(left)
	socket.SetState(TCPStateConnected)

	input := NewTCPInputStreamResource(socket)
	output := NewTCPOutputStreamResource(socket)

	// Subscriptions borrow stream; dropping does not affect socket or stream.
	subIn := input.Subscribe()
	subIn.Drop()
	subOut := output.Subscribe()
	subOut.Drop()

	if socket.State() != TCPStateConnected {
		t.Fatal("subscription drop closed socket")
	}

	// Normal input.Drop() is half-close: socket remains connected, output remains writable.
	input.Drop()
	if socket.State() != TCPStateConnected {
		t.Fatal("input.Drop() closed socket instead of half-close")
	}
	permit, err := output.CheckWrite()
	if err != nil || permit == 0 {
		t.Fatalf("output stream not writable after input.Drop(): permit=%d err=%v", permit, err)
	}

	// AbortSocket then fully closes the socket.
	output.AbortSocket()
	if socket.State() != TCPStateClosed {
		t.Fatalf("expected TCPStateClosed after AbortSocket, got %v", socket.State())
	}
}
