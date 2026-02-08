package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero/api"
	"go.uber.org/zap"
)

// ErrorKind categorizes errors for integration with external error handling.
type ErrorKind string

const (
	KindUnknown           ErrorKind = "Unknown"
	KindCanceled          ErrorKind = "Canceled"
	KindTimeout           ErrorKind = "Timeout"
	KindInternal          ErrorKind = "Internal"
	KindInvalid           ErrorKind = "Invalid"
	KindResourceExhausted ErrorKind = "ResourceExhausted"
)

func ClassifyError(err error) ErrorKind {
	if err == nil {
		return KindUnknown
	}
	if errors.Is(err, context.Canceled) {
		return KindCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return KindTimeout
	}
	return KindUnknown
}

// Asyncify implements the Binaryen asyncify protocol (wasm-opt --asyncify).
//
// States: 0=Normal, 1=Unwinding (saving stack), 2=Rewinding (restoring stack)
//
// Memory layout at dataAddr:
//   - [0:4] stack pointer (grows upward from dataAddr+8)
//   - [4:8] stack end
//   - [8:stackSize] stack data
type Asyncify struct {
	exports struct {
		getState    api.Function
		startUnwind api.Function
		stopUnwind  api.Function
		startRewind api.Function
		stopRewind  api.Function
	}
	memory    api.Memory
	mu        sync.Mutex
	state     int32
	dataAddr  uint32
	stackSize uint32
}

const AsyncifyDataAddr uint32 = 16
const AsyncifyDefaultStackSize uint32 = 1024

type AsyncifyConfig struct {
	StackSize uint32
	DataAddr  uint32
}

func NewAsyncify() *Asyncify {
	return &Asyncify{
		state:     0,
		dataAddr:  AsyncifyDataAddr,
		stackSize: AsyncifyDefaultStackSize,
	}
}

func (a *Asyncify) SetStackSize(size uint32) {
	a.stackSize = size
}

func (a *Asyncify) SetDataAddr(addr uint32) {
	a.dataAddr = addr
}

// Init initializes asyncify. Call after module instantiation.
func (a *Asyncify) Init(mod api.Module) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.memory = mod.Memory()
	if a.memory == nil {
		return fmt.Errorf("asyncify: module has no memory")
	}

	a.exports.getState = mod.ExportedFunction("asyncify_get_state")
	a.exports.startUnwind = mod.ExportedFunction("asyncify_start_unwind")
	a.exports.stopUnwind = mod.ExportedFunction("asyncify_stop_unwind")
	a.exports.startRewind = mod.ExportedFunction("asyncify_start_rewind")
	a.exports.stopRewind = mod.ExportedFunction("asyncify_stop_rewind")

	if a.exports.getState == nil {
		return fmt.Errorf("asyncify: module missing asyncify_get_state export (run wasm-opt --asyncify)")
	}

	stackPtr := a.dataAddr + 8
	stackEnd := stackPtr + a.stackSize

	if !a.memory.WriteUint32Le(a.dataAddr, stackPtr) {
		return fmt.Errorf("asyncify: failed to write stack pointer")
	}
	if !a.memory.WriteUint32Le(a.dataAddr+4, stackEnd) {
		return fmt.Errorf("asyncify: failed to write stack end")
	}

	return nil
}

func (a *Asyncify) GetState(_ context.Context) int32 {
	return atomic.LoadInt32(&a.state)
}

// SyncState reads state from WASM module. Allocates; use only for debugging.
func (a *Asyncify) SyncState(ctx context.Context) int32 {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.exports.getState == nil {
		return atomic.LoadInt32(&a.state)
	}

	results, err := a.exports.getState.Call(ctx)
	if err != nil || len(results) == 0 {
		return atomic.LoadInt32(&a.state)
	}
	atomic.StoreInt32(&a.state, int32(results[0]))
	return atomic.LoadInt32(&a.state)
}

func (a *Asyncify) IsNormal(_ context.Context) bool {
	return atomic.LoadInt32(&a.state) == 0
}

func (a *Asyncify) IsUnwinding(_ context.Context) bool {
	return atomic.LoadInt32(&a.state) == 1
}

func (a *Asyncify) IsRewinding(_ context.Context) bool {
	return atomic.LoadInt32(&a.state) == 2
}

func (a *Asyncify) StartUnwind(ctx context.Context) error {
	if a.exports.startUnwind != nil {
		_, err := a.exports.startUnwind.Call(ctx, uint64(a.dataAddr))
		if err == nil {
			atomic.StoreInt32(&a.state, 1)
		}
		return err
	}
	atomic.StoreInt32(&a.state, 1)
	return nil
}

func (a *Asyncify) StopUnwind(ctx context.Context) error {
	if a.exports.stopUnwind != nil {
		_, err := a.exports.stopUnwind.Call(ctx)
		if err == nil {
			atomic.StoreInt32(&a.state, 0)
		}
		return err
	}
	atomic.StoreInt32(&a.state, 0)
	return nil
}

func (a *Asyncify) StartRewind(ctx context.Context) error {
	if a.exports.startRewind != nil {
		_, err := a.exports.startRewind.Call(ctx, uint64(a.dataAddr))
		if err == nil {
			atomic.StoreInt32(&a.state, 2)
		}
		return err
	}
	atomic.StoreInt32(&a.state, 2)
	return nil
}

func (a *Asyncify) StopRewind(ctx context.Context) error {
	if a.exports.stopRewind != nil {
		_, err := a.exports.stopRewind.Call(ctx)
		if err == nil {
			atomic.StoreInt32(&a.state, 0)
		}
		return err
	}
	atomic.StoreInt32(&a.state, 0)
	return nil
}

// ResetStack resets the stack pointer. Call before each new async operation.
func (a *Asyncify) ResetStack() {
	if a.memory != nil {
		stackPtr := a.dataAddr + 8
		if !a.memory.WriteUint32Le(a.dataAddr, stackPtr) {
			Logger().Warn("ResetStack: failed to write stack pointer to asyncify data",
				zap.Uint32("dataAddr", a.dataAddr),
				zap.Uint32("stackPtr", stackPtr))
		}
	}
}

type CommandID = uint16

// PendingOp represents an async operation yielded during suspension.
type PendingOp interface {
	CmdID() CommandID
	Execute(ctx context.Context) (uint64, error)
}

type StepStatus int

const (
	StepContinue StepStatus = iota // yielded an operation, expects resume
	StepIdle                       // waiting for external message
	StepDone                       // execution complete
)

type StepResult struct {
	PendingOp PendingOp
	Error     error
	ErrorKind ErrorKind
	Results   []uint64
	Status    StepStatus
}

type YieldResult struct {
	Error error
	Value uint64
}

// Scheduler manages async execution with step-based control for integration
// with external event loops.
type Scheduler struct {
	fn          api.Function
	pendingOp   PendingOp
	err         error
	asyncify    *Asyncify
	args        []uint64
	result      uint64
	initialized bool
}

func NewScheduler(asyncify *Asyncify) *Scheduler {
	return &Scheduler{
		asyncify: asyncify,
	}
}

func (s *Scheduler) SetPending(op PendingOp) {
	s.pendingOp = op
}

func (s *Scheduler) GetResult() (uint64, error) {
	return s.result, s.err
}

func (s *Scheduler) ClearPending() {
	s.pendingOp = nil
	s.result = 0
	s.err = nil
}

// Execute initializes execution. Call Step() to advance.
func (s *Scheduler) Execute(ctx context.Context, fn api.Function, args ...uint64) error {
	if !s.asyncify.IsNormal(ctx) {
		return fmt.Errorf("scheduler: asyncify not in normal state")
	}
	s.fn = fn
	s.args = args
	s.initialized = true
	s.asyncify.ResetStack()
	return nil
}

// Step advances execution. Pass nil for first call, or YieldResult to resume.
func (s *Scheduler) Step(ctx context.Context, yr *YieldResult) (StepResult, error) {
	if err := ctx.Err(); err != nil {
		return StepResult{Error: err, ErrorKind: ClassifyError(err)}, err
	}
	if !s.initialized {
		err := fmt.Errorf("scheduler: call Execute first")
		return StepResult{Error: err, ErrorKind: KindInvalid}, err
	}

	if yr != nil {
		s.result = yr.Value
		s.err = yr.Error
		if s.err != nil {
			return StepResult{Error: s.err, ErrorKind: ClassifyError(s.err)}, s.err
		}
		if err := s.asyncify.StartRewind(ctx); err != nil {
			err = fmt.Errorf("scheduler: start rewind: %w", err)
			return StepResult{Error: err, ErrorKind: KindInternal}, err
		}
	}

	results, callErr := s.fn.Call(ctx, s.args...)

	if s.asyncify.IsUnwinding(ctx) {
		if err := s.asyncify.StopUnwind(ctx); err != nil {
			err = fmt.Errorf("scheduler: stop unwind: %w", err)
			return StepResult{Error: err, ErrorKind: KindInternal}, err
		}
		if s.pendingOp == nil {
			err := fmt.Errorf("scheduler: no pending operation after unwind")
			return StepResult{Error: err, ErrorKind: KindInternal}, err
		}
		op := s.pendingOp
		s.pendingOp = nil
		return StepResult{Status: StepContinue, PendingOp: op}, nil
	}

	if callErr != nil {
		return StepResult{Error: callErr, ErrorKind: ClassifyError(callErr)}, callErr
	}

	if !s.asyncify.IsNormal(ctx) {
		err := fmt.Errorf("scheduler: unexpected state after call")
		return StepResult{Error: err, ErrorKind: KindInternal}, err
	}

	s.initialized = false
	return StepResult{Status: StepDone, Results: results}, nil
}

func (s *Scheduler) Reset() {
	s.fn = nil
	s.args = nil
	s.pendingOp = nil
	s.result = 0
	s.err = nil
	s.initialized = false
}

// Run executes with internal event loop. Convenience wrapper over Execute/Step.
func (s *Scheduler) Run(ctx context.Context, fn api.Function, args ...uint64) ([]uint64, error) {
	if err := s.Execute(ctx, fn, args...); err != nil {
		return nil, err
	}

	var yr *YieldResult
	for {
		sr, err := s.Step(ctx, yr)
		if err != nil {
			return nil, err
		}

		switch sr.Status {
		case StepDone:
			return sr.Results, nil
		case StepContinue:
			val, opErr := sr.PendingOp.Execute(ctx)
			yr = &YieldResult{Value: val, Error: opErr}
		}
	}
}

type ctxKeyScheduler struct{}
type ctxKeyAsyncify struct{}

func WithScheduler(ctx context.Context, s *Scheduler) context.Context {
	return context.WithValue(ctx, ctxKeyScheduler{}, s)
}

func GetScheduler(ctx context.Context) *Scheduler {
	if v := ctx.Value(ctxKeyScheduler{}); v != nil {
		return v.(*Scheduler)
	}
	return nil
}

func WithAsyncify(ctx context.Context, a *Asyncify) context.Context {
	return context.WithValue(ctx, ctxKeyAsyncify{}, a)
}

func GetAsyncify(ctx context.Context) *Asyncify {
	if v := ctx.Value(ctxKeyAsyncify{}); v != nil {
		return v.(*Asyncify)
	}
	return nil
}

// Suspend registers op and starts unwinding. Called by host handlers.
func Suspend(ctx context.Context, op PendingOp) error {
	sched := GetScheduler(ctx)
	async := GetAsyncify(ctx)

	if sched == nil || async == nil {
		return fmt.Errorf("suspend: scheduler or asyncify not in context")
	}

	sched.SetPending(op)
	return async.StartUnwind(ctx)
}

// Resume gets the operation result and stops rewinding. Called during rewind.
func Resume(ctx context.Context) (uint64, error) {
	sched := GetScheduler(ctx)
	async := GetAsyncify(ctx)

	if sched == nil || async == nil {
		return 0, fmt.Errorf("resume: scheduler or asyncify not in context")
	}

	result, err := sched.GetResult()
	if err != nil {
		return 0, err
	}

	if err := async.StopRewind(ctx); err != nil {
		return 0, err
	}

	sched.ClearPending()
	return result, nil
}

// MakeAsyncHandler wraps an operation factory into a suspend/resume-aware handler.
func MakeAsyncHandler(createOp func(ctx context.Context, mod api.Module, stack []uint64) PendingOp) api.GoModuleFunc {
	return func(ctx context.Context, mod api.Module, stack []uint64) {
		async := GetAsyncify(ctx)
		if async == nil {
			// No asyncify - can't suspend, just return
			return
		}

		if async.IsRewinding(ctx) {
			result, err := Resume(ctx)
			if err == nil && len(stack) > 0 {
				stack[0] = result
			}
			return
		}

		op := createOp(ctx, mod, stack)
		if op != nil {
			if err := Suspend(ctx, op); err != nil {
				Logger().Warn("MakeAsyncHandler: failed to suspend for pending operation",
					zap.Error(err))
			}
		}
	}
}
