package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

func TestAsyncify_NewAndDefaults(t *testing.T) {
	a := NewAsyncify()

	if a.state != 0 {
		t.Errorf("expected initial state 0, got %d", a.state)
	}
	if a.dataAddr != AsyncifyDataAddr {
		t.Errorf("expected dataAddr %d, got %d", AsyncifyDataAddr, a.dataAddr)
	}
	if a.stackSize != AsyncifyDefaultStackSize {
		t.Errorf("expected stackSize %d, got %d", AsyncifyDefaultStackSize, a.stackSize)
	}
}

func TestAsyncify_SetStackSize(t *testing.T) {
	a := NewAsyncify()
	a.SetStackSize(4096)

	if a.stackSize != 4096 {
		t.Errorf("expected stackSize 4096, got %d", a.stackSize)
	}
}

func TestAsyncify_SetDataAddr(t *testing.T) {
	a := NewAsyncify()
	a.SetDataAddr(256)

	if a.dataAddr != 256 {
		t.Errorf("expected dataAddr 256, got %d", a.dataAddr)
	}
}

func TestIsAsyncified(t *testing.T) {
	// Non-asyncified WASM (minimal valid module)
	nonAsync := []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version
	}

	if IsAsyncified(nonAsync) {
		t.Error("expected non-asyncified module to return false")
	}

	// Fake asyncified WASM (contains export name)
	asyncified := append(append([]byte{}, nonAsync...), []byte("asyncify_start_unwind")...)

	if !IsAsyncified(asyncified) {
		t.Error("expected asyncified module to return true")
	}
}

func TestScheduler_Basic(t *testing.T) {
	a := NewAsyncify()
	s := NewScheduler(a)

	if s.asyncify != a {
		t.Error("scheduler should reference asyncify")
	}

	if s.pendingOp != nil {
		t.Error("pending op should be nil initially")
	}
}

// MockPendingOp for testing
type mockPendingOp struct {
	err    error
	result uint64
	called bool
}

func (m *mockPendingOp) CmdID() CommandID { return 0 }

func (m *mockPendingOp) Execute(ctx context.Context) (uint64, error) {
	m.called = true
	return m.result, m.err
}

func TestScheduler_SetPending(t *testing.T) {
	a := NewAsyncify()
	s := NewScheduler(a)

	op := &mockPendingOp{result: 42}
	s.SetPending(op)

	if s.pendingOp != op {
		t.Error("pending op not set")
	}

	s.ClearPending()
	if s.pendingOp != nil {
		t.Error("pending op should be cleared")
	}
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	a := NewAsyncify()
	s := NewScheduler(a)

	// Add to context
	ctx = WithAsyncify(ctx, a)
	ctx = WithScheduler(ctx, s)

	// Retrieve
	if GetAsyncify(ctx) != a {
		t.Error("failed to get asyncify from context")
	}
	if GetScheduler(ctx) != s {
		t.Error("failed to get scheduler from context")
	}

	// Empty context
	if GetAsyncify(context.Background()) != nil {
		t.Error("should return nil for empty context")
	}
	if GetScheduler(context.Background()) != nil {
		t.Error("should return nil for empty context")
	}
}

func TestAsyncify_StateConcurrency(t *testing.T) {
	// Asyncify is designed to be used from a single goroutine per WASM instance.
	// This test verifies that SetStackSize works correctly in single-threaded usage.
	a := NewAsyncify()
	a.SetStackSize(2048)
	a.SetDataAddr(1024)

	if a.stackSize != 2048 {
		t.Errorf("expected stackSize 2048, got %d", a.stackSize)
	}
	if a.dataAddr != 1024 {
		t.Errorf("expected dataAddr 1024, got %d", a.dataAddr)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorKind
	}{
		{"nil error", nil, KindUnknown},
		{"context canceled", context.Canceled, KindCanceled},
		{"deadline exceeded", context.DeadlineExceeded, KindTimeout},
		{"wrapped canceled", errors.Join(errors.New("wrap"), context.Canceled), KindCanceled},
		{"generic error", errors.New("some error"), KindUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyError(tc.err)
			if got != tc.expected {
				t.Errorf("ClassifyError(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		})
	}
}

func TestStepResult_ErrorKind(t *testing.T) {
	sr := StepResult{
		Status:    StepDone,
		Error:     context.Canceled,
		ErrorKind: KindCanceled,
	}

	if sr.ErrorKind != KindCanceled {
		t.Errorf("expected KindCanceled, got %v", sr.ErrorKind)
	}
}

func TestScheduler_StepWithoutExecute(t *testing.T) {
	a := NewAsyncify()
	s := NewScheduler(a)

	_, err := s.Step(context.Background(), nil)
	if err == nil {
		t.Error("expected error when Step called without Execute")
	}
}

func TestScheduler_StepWithCanceledContext(t *testing.T) {
	a := NewAsyncify()
	s := NewScheduler(a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sr, err := s.Step(ctx, nil)
	if err == nil {
		t.Error("expected error with canceled context")
	}
	if sr.ErrorKind != KindCanceled {
		t.Errorf("expected KindCanceled, got %v", sr.ErrorKind)
	}
}

func TestScheduler_Reset(t *testing.T) {
	a := NewAsyncify()
	s := NewScheduler(a)

	op := &mockPendingOp{result: 42}
	s.SetPending(op)
	s.result = 100
	s.err = errors.New("test")
	s.initialized = true

	s.Reset()

	if s.pendingOp != nil {
		t.Error("pending op should be nil after reset")
	}
	if s.result != 0 {
		t.Error("result should be 0 after reset")
	}
	if s.err != nil {
		t.Error("err should be nil after reset")
	}
	if s.initialized {
		t.Error("initialized should be false after reset")
	}
}

func TestYieldResult(t *testing.T) {
	yr := &YieldResult{
		Value: 42,
		Error: nil,
	}

	if yr.Value != 42 {
		t.Errorf("expected Value 42, got %d", yr.Value)
	}

	yr.Error = errors.New("test error")
	if yr.Error == nil {
		t.Error("expected non-nil error")
	}
}

func TestStepStatus_Values(t *testing.T) {
	if StepContinue != 0 {
		t.Errorf("StepContinue should be 0, got %d", StepContinue)
	}
	if StepIdle != 1 {
		t.Errorf("StepIdle should be 1, got %d", StepIdle)
	}
	if StepDone != 2 {
		t.Errorf("StepDone should be 2, got %d", StepDone)
	}
}

func TestErrorKind_Values(t *testing.T) {
	kinds := []ErrorKind{
		KindUnknown,
		KindCanceled,
		KindTimeout,
		KindInternal,
		KindInvalid,
		KindResourceExhausted,
	}

	expected := []string{
		"Unknown",
		"Canceled",
		"Timeout",
		"Internal",
		"Invalid",
		"ResourceExhausted",
	}

	for i, k := range kinds {
		if string(k) != expected[i] {
			t.Errorf("ErrorKind %d = %q, want %q", i, k, expected[i])
		}
	}
}

// Extended asyncify tests covering additional edge cases and state transitions

func TestAsyncify_StateChecks(t *testing.T) {
	t.Run("NoExports", func(t *testing.T) {
		a := NewAsyncify()
		ctx := context.Background()

		// Without Init, these use atomic state
		if !a.IsNormal(ctx) {
			t.Error("should be normal initially")
		}
		if a.IsUnwinding(ctx) {
			t.Error("should not be unwinding initially")
		}
		if a.IsRewinding(ctx) {
			t.Error("should not be rewinding initially")
		}

		// GetState should return state
		if a.GetState(ctx) != 0 {
			t.Errorf("expected state 0, got %d", a.GetState(ctx))
		}
	})
}

func TestAsyncify_StartStopMethods(t *testing.T) {
	t.Run("NoExports", func(t *testing.T) {
		a := NewAsyncify()
		ctx := context.Background()

		// Without exports (no Init), these methods set atomic state
		err := a.StartUnwind(ctx)
		if err != nil {
			t.Fatalf("StartUnwind error: %v", err)
		}
		if a.GetState(ctx) != 1 {
			t.Errorf("expected state 1 after StartUnwind, got %d", a.GetState(ctx))
		}

		err = a.StopUnwind(ctx)
		if err != nil {
			t.Fatalf("StopUnwind error: %v", err)
		}
		if a.GetState(ctx) != 0 {
			t.Errorf("expected state 0 after StopUnwind, got %d", a.GetState(ctx))
		}

		err = a.StartRewind(ctx)
		if err != nil {
			t.Fatalf("StartRewind error: %v", err)
		}
		if a.GetState(ctx) != 2 {
			t.Errorf("expected state 2 after StartRewind, got %d", a.GetState(ctx))
		}

		err = a.StopRewind(ctx)
		if err != nil {
			t.Fatalf("StopRewind error: %v", err)
		}
		if a.GetState(ctx) != 0 {
			t.Errorf("expected state 0 after StopRewind, got %d", a.GetState(ctx))
		}
	})
}

func TestAsyncify_SyncState(t *testing.T) {
	t.Run("NoExports", func(t *testing.T) {
		a := NewAsyncify()
		ctx := context.Background()

		// Without exports, SyncState returns atomic state
		state := a.SyncState(ctx)
		if state != 0 {
			t.Errorf("expected state 0, got %d", state)
		}
	})
}

func TestAsyncify_ResetStack(t *testing.T) {
	t.Run("NoMemory", func(t *testing.T) {
		a := NewAsyncify()

		// ResetStack without memory should not panic
		a.ResetStack()
	})
}

func TestSuspend(t *testing.T) {
	t.Run("NoContextValues", func(t *testing.T) {
		ctx := context.Background()
		op := &mockPendingOp{result: 42}

		err := Suspend(ctx, op)
		if err == nil {
			t.Error("expected error when scheduler/asyncify not in context")
		}
	})

	t.Run("WithContext", func(t *testing.T) {
		ctx := context.Background()
		a := NewAsyncify()
		s := NewScheduler(a)

		ctx = WithAsyncify(ctx, a)
		ctx = WithScheduler(ctx, s)

		op := &mockPendingOp{result: 42}
		err := Suspend(ctx, op)
		if err != nil {
			t.Fatalf("Suspend error: %v", err)
		}

		// Should have set pending op
		if s.pendingOp != op {
			t.Error("pending op not set by Suspend")
		}

		// Should have started unwind
		if a.GetState(ctx) != 1 {
			t.Errorf("expected state 1 after Suspend, got %d", a.GetState(ctx))
		}
	})
}

func TestResume(t *testing.T) {
	t.Run("NoContextValues", func(t *testing.T) {
		ctx := context.Background()

		_, err := Resume(ctx)
		if err == nil {
			t.Error("expected error when scheduler/asyncify not in context")
		}
	})

	t.Run("WithContext", func(t *testing.T) {
		ctx := context.Background()
		a := NewAsyncify()
		s := NewScheduler(a)

		// Set up state as if we're resuming
		s.result = 42
		s.err = nil
		a.state = 2 // rewinding

		ctx = WithAsyncify(ctx, a)
		ctx = WithScheduler(ctx, s)

		result, err := Resume(ctx)
		if err != nil {
			t.Fatalf("Resume error: %v", err)
		}

		if result != 42 {
			t.Errorf("expected result 42, got %d", result)
		}

		// Should have stopped rewind
		if a.GetState(ctx) != 0 {
			t.Errorf("expected state 0 after Resume, got %d", a.GetState(ctx))
		}

		// Should have cleared pending
		if s.pendingOp != nil {
			t.Error("pending op should be cleared after Resume")
		}
	})

	t.Run("WithError", func(t *testing.T) {
		ctx := context.Background()
		a := NewAsyncify()
		s := NewScheduler(a)

		testErr := context.Canceled
		s.result = 0
		s.err = testErr

		ctx = WithAsyncify(ctx, a)
		ctx = WithScheduler(ctx, s)

		_, err := Resume(ctx)
		if !errors.Is(err, testErr) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestMakeAsyncHandler(t *testing.T) {
	t.Run("NoAsyncify", func(t *testing.T) {
		handler := MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			return &mockPendingOp{result: 42}
		})

		ctx := context.Background()

		// Without asyncify in context, handler should return immediately
		handler(ctx, nil, nil)
		// Should not panic
	})

	t.Run("Rewinding", func(t *testing.T) {
		a := NewAsyncify()
		s := NewScheduler(a)

		// Simulate rewinding state
		a.state = 2
		s.result = 99

		ctx := context.Background()
		ctx = WithAsyncify(ctx, a)
		ctx = WithScheduler(ctx, s)

		callCount := 0
		handler := MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			callCount++
			return &mockPendingOp{result: 42}
		})

		stack := make([]uint64, 1)
		handler(ctx, nil, stack)

		// Should not call createOp during rewind
		if callCount != 0 {
			t.Error("should not call createOp during rewind")
		}

		// Result should be set in stack
		if stack[0] != 99 {
			t.Errorf("expected stack[0] = 99, got %d", stack[0])
		}
	})

	t.Run("Normal", func(t *testing.T) {
		a := NewAsyncify()
		s := NewScheduler(a)

		ctx := context.Background()
		ctx = WithAsyncify(ctx, a)
		ctx = WithScheduler(ctx, s)

		callCount := 0
		op := &mockPendingOp{result: 42}
		handler := MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			callCount++
			return op
		})

		stack := make([]uint64, 1)
		handler(ctx, nil, stack)

		// Should call createOp during normal execution
		if callCount != 1 {
			t.Errorf("expected callCount 1, got %d", callCount)
		}

		// Should have started unwind
		if a.GetState(ctx) != 1 {
			t.Errorf("expected state 1 after handler, got %d", a.GetState(ctx))
		}

		// Op should be set
		if s.pendingOp != op {
			t.Error("pending op should be set")
		}
	})

	t.Run("NilOp", func(t *testing.T) {
		a := NewAsyncify()
		s := NewScheduler(a)

		ctx := context.Background()
		ctx = WithAsyncify(ctx, a)
		ctx = WithScheduler(ctx, s)

		handler := MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			return nil
		})

		handler(ctx, nil, nil)

		// Should not start unwind if op is nil
		if a.GetState(ctx) != 0 {
			t.Errorf("expected state 0 with nil op, got %d", a.GetState(ctx))
		}
	})
}

func TestScheduler_GetResult(t *testing.T) {
	a := NewAsyncify()
	s := NewScheduler(a)

	s.result = 123
	s.err = context.Canceled

	val, err := s.GetResult()
	if val != 123 {
		t.Errorf("expected value 123, got %d", val)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
