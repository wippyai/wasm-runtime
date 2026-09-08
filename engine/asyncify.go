package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero/api"
	"go.uber.org/zap"

	"github.com/wippyai/wasm-runtime/asyncify"
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
	// Bound once before publication by the instance. Standalone controllers have
	// no owner; their caller must keep the underlying module alive.
	lifetime   *executionLifetime
	generation *asyncifyGeneration
	exports    struct {
		getState    api.Function
		startUnwind api.Function
		stopUnwind  api.Function
		startRewind api.Function
		stopRewind  api.Function
	}
	memory         api.Memory
	module         api.Module
	stateGlobal    api.MutableGlobal
	dataGlobal     api.MutableGlobal
	hostArgs       []uint64
	inlineHostArgs [16]uint64
	mu             sync.Mutex
	state          int32
	dataAddr       uint32
	stackSize      uint32
	trusted        bool
	dataAddrSet    bool
}

// AsyncifyDataAddr is retained for callers which explicitly reserve this region.
// It is never used as an automatic storage location.
const AsyncifyDataAddr uint32 = 16
const AsyncifyDefaultStackSize uint32 = 1024

// DefaultAsyncifyStackBytes bounds each automatically owned suspension stack.
const DefaultAsyncifyStackBytes uint32 = 64 << 10

type AsyncifyConfig struct {
	StackSize uint32
	DataAddr  uint32

	// ownedStackBytes is set only by WazeroInstance's automatic owned-stack
	// path. A public nonzero DataAddr remains caller-managed; zero selects
	// an automatically owned reservation.
	ownedStackBytes uint32
}

func NewAsyncify() *Asyncify {
	return &Asyncify{
		state:     0,
		stackSize: AsyncifyDefaultStackSize,
	}
}

// DirectGlobals reports whether trusted generated controls can use mutable globals.
func (a *Asyncify) DirectGlobals() bool { return a.stateGlobal != nil && a.dataGlobal != nil }

func (a *Asyncify) SetStackSize(size uint32) {
	a.stackSize = size
}

func (a *Asyncify) SetDataAddr(addr uint32) {
	a.dataAddr = addr
	a.dataAddrSet = true
}

// Init initializes caller-owned controls. Instance reconfiguration prepares all
// cores first and commits their headers together under caller serialization.
func (a *Asyncify) Init(mod api.Module) error {
	if a.lifetime != nil || a.generation != nil {
		return fmt.Errorf("asyncify: instance-owned controls cannot be rebound; use EnableAsyncify")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	header, err := a.prepareInit(mod)
	if err != nil {
		return err
	}
	header.commit()
	return nil
}

// prepareInit resolves controls and validates the complete header without writing
// guest memory. The returned slice is borrowed until commit; no guest execution
// or memory growth may occur between preparation and commit.
func (a *Asyncify) prepareInit(mod api.Module) (asyncifyHeader, error) {
	if !a.dataAddrSet {
		return asyncifyHeader{}, fmt.Errorf("asyncify: caller must reserve storage and call SetDataAddr before Init")
	}
	if mod == nil {
		return asyncifyHeader{}, fmt.Errorf("asyncify: module is nil")
	}
	a.memory = mod.Memory()
	if a.memory == nil {
		return asyncifyHeader{}, fmt.Errorf("asyncify: module has no memory")
	}

	a.exports.getState = mod.ExportedFunction("asyncify_get_state")
	a.exports.startUnwind = mod.ExportedFunction("asyncify_start_unwind")
	a.exports.stopUnwind = mod.ExportedFunction("asyncify_stop_unwind")
	a.exports.startRewind = mod.ExportedFunction("asyncify_start_rewind")
	a.exports.stopRewind = mod.ExportedFunction("asyncify_stop_rewind")

	if a.exports.getState == nil {
		return asyncifyHeader{}, fmt.Errorf("asyncify: module missing asyncify_get_state export (run wasm-opt --asyncify)")
	}

	a.module = mod
	a.stateGlobal, a.dataGlobal = nil, nil
	// Provenance is supplied only by the engine after its embedded transform.
	// Export names alone never enable this path. Keep other pointer widths on
	// the original function path, whose guest code defines their semantics.
	if a.trusted {
		state, stateOK := mod.ExportedGlobal("asyncify_state").(api.MutableGlobal)
		data, dataOK := mod.ExportedGlobal("asyncify_data").(api.MutableGlobal)
		if stateOK && dataOK && state.Type() == api.ValueTypeI32 && data.Type() == api.ValueTypeI32 {
			a.stateGlobal, a.dataGlobal = state, data
		}
	}

	pointer := uint64(a.dataAddr) + 8
	end := pointer + uint64(a.stackSize)
	if end > uint64(^uint32(0)) {
		return asyncifyHeader{}, fmt.Errorf("asyncify: stack address overflow")
	}
	// Reading the entire region proves both the header and every byte Asyncify
	// may use for its saved stack are addressable. Checking only eight header
	// bytes lets a valid header point at a stack that runs off linear memory.
	regionBytes := uint32(end - uint64(a.dataAddr))
	bytes, ok := a.memory.Read(a.dataAddr, regionBytes)
	if !ok {
		return asyncifyHeader{}, fmt.Errorf("asyncify: stack region outside memory")
	}
	return asyncifyHeader{bytes: bytes[:asyncifyHeaderBytes], pointer: uint32(pointer), end: uint32(end)}, nil
}

func (a *Asyncify) GetState(_ context.Context) int32 {
	return atomic.LoadInt32(&a.state)
}

// SyncState reads state from WASM module. Allocates; use only for debugging.
// If execution is stopped or the guest read fails, it returns the cached state.
func (a *Asyncify) SyncState(ctx context.Context) int32 {
	ctx, finish, err := a.enterControl(ctx)
	if err != nil {
		return atomic.LoadInt32(&a.state)
	}
	defer finish()
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

// directControl handles only successful transitions of our generated wasm32
// controls. Exceptional cases use the original functions below, preserving their
// trap types, guest-global side effects, and module cancellation behavior.
func (a *Asyncify) directControl(ctx context.Context, expected, next int32, start bool) bool {
	if !a.DirectGlobals() || a.module == nil || ctx.Err() != nil || a.module.IsClosed() {
		return false
	}
	if a.stateGlobal.Get() != uint64(expected) {
		return false
	}
	addr := a.dataGlobal.Get()
	if start {
		addr = uint64(a.dataAddr)
	}
	if addr > uint64(^uint32(0)-4) {
		return false
	}
	ptr, ptrOK := a.memory.ReadUint32Le(uint32(addr))
	end, endOK := a.memory.ReadUint32Le(uint32(addr + 4))
	if !ptrOK || !endOK || ptr > end {
		return false
	}
	a.stateGlobal.Set(uint64(next))
	if start {
		a.dataGlobal.Set(addr)
	}
	atomic.StoreInt32(&a.state, next)
	return true
}

func (a *Asyncify) StartUnwind(ctx context.Context) error {
	ctx, finish, err := a.enterControl(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if a.directControl(ctx, 0, 1, true) {
		return nil
	}
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
	ctx, finish, err := a.enterControl(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if a.directControl(ctx, 1, 0, false) {
		return nil
	}
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
	ctx, finish, err := a.enterControl(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if a.directControl(ctx, 0, 2, true) {
		return nil
	}
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
	ctx, finish, err := a.enterControl(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if a.directControl(ctx, 2, 0, false) {
		return nil
	}
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

// ResetStack resets the stack pointer. On a stopped instance it has no effect.
// Use ResetStackContext when the caller needs the rejection error.
func (a *Asyncify) ResetStack() { _ = a.ResetStackContext(context.Background()) }

// ResetStackContext resets the stack before a new async operation. The caller
// must serialize it with execution, just like the other Asyncify controls.
func (a *Asyncify) ResetStackContext(ctx context.Context) error {
	_, finish, err := a.enterControl(ctx)
	if err != nil {
		return err
	}
	defer finish()
	a.ClearHostArgs()
	if a.memory != nil {
		stackPtr := a.dataAddr + 8
		if !a.memory.WriteUint32Le(a.dataAddr, stackPtr) {
			Logger().Warn("ResetStack: failed to write stack pointer to asyncify data",
				zap.Uint32("dataAddr", a.dataAddr), zap.Uint32("stackPtr", stackPtr))
			return fmt.Errorf("asyncify: failed to reset stack pointer")
		}
	}
	return nil
}

// enterControl borrows an enclosing active execution lease, or admits an
// external control operation. Borrowing keeps internal unwind/rewind allocation
// free while teardown still joins the enclosing call. It is not a concurrency
// lock: callers must serialize controls with the guest's execution protocol.
func (a *Asyncify) enterControl(ctx context.Context) (context.Context, func(), error) {
	if a.generation != nil && a.generation.revoked.Load() {
		return ctx, finishUntrackedExecution, ErrAsyncifySuperseded
	}
	if a.lifetime == nil || a.lifetime.heldBy(ctx) {
		return ctx, finishUntrackedExecution, nil
	}
	return a.lifetime.enter(ctx)
}

// ParkHostArgs copies the Canonical ABI host-call stack for the in-flight
// unwind so rewind can restore retptr and arguments.
func (a *Asyncify) ParkHostArgs(stack []uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(stack) <= len(a.inlineHostArgs) {
		a.hostArgs = a.inlineHostArgs[:len(stack)]
	} else {
		a.hostArgs = make([]uint64, len(stack))
	}
	copy(a.hostArgs, stack)
}

// TakeHostArgs returns an owned copy of the parked host-call stack and clears it.
// Inline storage must not escape: a subsequent ParkHostArgs can reuse it.
func (a *Asyncify) TakeHostArgs() []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	saved := a.hostArgs
	if saved != nil && len(saved) <= len(a.inlineHostArgs) {
		saved = make([]uint64, len(a.hostArgs))
		copy(saved, a.hostArgs)
	}
	a.hostArgs = nil
	return saved
}

// restoreParkedArgs copies into the caller stack before reusing inline storage.
func (a *Asyncify) restoreParkedArgs(stack []uint64) []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	stack = restoreHostArgs(a.hostArgs, stack)
	a.hostArgs = nil
	return stack
}

// ClearHostArgs drops any parked host-call stack.
func (a *Asyncify) ClearHostArgs() {
	a.mu.Lock()
	a.hostArgs = nil
	a.mu.Unlock()
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
	awaiting    bool // true after a yield until exactly one resume result is supplied
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
// Scheduler methods must be serialized by the caller; shutdown leases protect
// resource lifetime, not concurrent scheduling or canonical-session ownership.
func (s *Scheduler) Execute(ctx context.Context, fn api.Function, args ...uint64) error {
	ctx, finish, err := s.asyncify.enterControl(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if !s.asyncify.IsNormal(ctx) {
		return fmt.Errorf("scheduler: asyncify not in normal state")
	}
	if err := s.asyncify.ResetStackContext(ctx); err != nil {
		return err
	}
	s.fn = fn
	s.args = args
	s.initialized = true
	s.awaiting = false
	return nil
}

// Step advances execution. Pass nil for first call, or YieldResult to resume.
func (s *Scheduler) Step(ctx context.Context, yr *YieldResult) (StepResult, error) {
	ctx, finish, err := s.asyncify.enterControl(ctx)
	if err != nil {
		return StepResult{Error: err, ErrorKind: ClassifyError(err)}, err
	}
	defer finish()
	if err := ctx.Err(); err != nil {
		s.asyncify.ClearHostArgs()
		return StepResult{Error: err, ErrorKind: ClassifyError(err)}, err
	}
	if !s.initialized {
		err := fmt.Errorf("scheduler: call Execute first")
		return StepResult{Error: err, ErrorKind: KindInvalid}, err
	}

	if s.awaiting && yr == nil {
		err := fmt.Errorf("scheduler: yielded operation requires a resume result")
		return StepResult{Error: err, ErrorKind: KindInvalid}, err
	}
	if !s.awaiting && yr != nil {
		err := fmt.Errorf("scheduler: resume result supplied without a yielded operation")
		return StepResult{Error: err, ErrorKind: KindInvalid}, err
	}
	if yr != nil {
		s.result = yr.Value
		s.err = yr.Error
		if s.err != nil {
			s.asyncify.ClearHostArgs()
			s.awaiting = false
			return StepResult{Error: s.err, ErrorKind: ClassifyError(s.err)}, s.err
		}
		if err := s.asyncify.StartRewind(ctx); err != nil {
			err = fmt.Errorf("scheduler: start rewind: %w", err)
			return StepResult{Error: err, ErrorKind: KindInternal}, err
		}
		s.awaiting = false
	}

	results, callErr := s.fn.Call(ctx, s.args...)
	if callErr != nil {
		s.asyncify.ClearHostArgs()
		return StepResult{Error: callErr, ErrorKind: ClassifyError(callErr)}, callErr
	}

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
		s.awaiting = true
		return StepResult{Status: StepContinue, PendingOp: op}, nil
	}

	if !s.asyncify.IsNormal(ctx) {
		err := fmt.Errorf("scheduler: unexpected state after call")
		return StepResult{Error: err, ErrorKind: KindInternal}, err
	}

	s.initialized = false
	s.awaiting = false
	return StepResult{Status: StepDone, Results: results}, nil
}

func (s *Scheduler) Reset() {
	s.fn = nil
	s.args = nil
	s.pendingOp = nil
	s.result = 0
	s.err = nil
	s.initialized = false
	s.awaiting = false
	if s.asyncify != nil {
		s.asyncify.ClearHostArgs()
	}
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

// WithAsyncify selects the root controller for direct callers and keeps the
// legacy key for allocation-free engine call contexts.
func WithAsyncify(ctx context.Context, a *Asyncify) context.Context {
	return asyncify.WithRuntimeController(context.WithValue(ctx, ctxKeyAsyncify{}, a), a)
}

func GetAsyncify(ctx context.Context) *Asyncify {
	if state, present := asyncify.RuntimeControllerStateFromContext(ctx); present {
		selected, _ := state.Controller.(*Asyncify)
		return selected
	}
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
	if op == nil {
		return fmt.Errorf("suspend: nil pending operation")
	}
	if sched.pendingOp != nil || sched.awaiting {
		return fmt.Errorf("suspend: scheduler already owns a pending operation")
	}
	// Publish only after the guest accepted the state transition. A failed
	// start must not leave a later Step with a stale operation it could resume.
	if err := async.StartUnwind(ctx); err != nil {
		return err
	}

	sched.SetPending(op)
	return nil
}

// Resume gets the operation result and completes rewind at the async host
// boundary. Every active core has restored the frames above that boundary
// before the lower calls this function, so the complete controller path must
// become normal before Canonical ABI result encoding can synchronously call an
// allocator through an ancestor bridge. Leaving an ancestor rewinding makes
// that fresh allocator entry look like replay and can skip its body.
func Resume(ctx context.Context) (uint64, error) {
	sched := GetScheduler(ctx)
	async := GetAsyncify(ctx)

	if sched == nil || async == nil {
		return 0, fmt.Errorf("resume: scheduler or asyncify not in context")
	}
	if !async.IsRewinding(ctx) {
		return 0, fmt.Errorf("resume: selected controller is not rewinding")
	}

	result, err := sched.GetResult()
	if err != nil {
		return 0, err
	}

	state, present := asyncify.RuntimeControllerStateFromContext(ctx)
	if !present {
		state = asyncify.RuntimeControllerContext{Controller: async}
	}
	for active := true; active; state, active = asyncify.ParentRuntimeController(state) {
		controller := state.Controller
		// Repeated activations share a controller. Stopping the innermost
		// occurrence makes later occurrences normal, so no seen-set is needed.
		if controller == nil || controller.IsNormal(ctx) {
			continue
		}
		if !controller.IsRewinding(ctx) {
			return 0, fmt.Errorf("resume: active controller is not rewinding")
		}
		if err := controller.StopRewind(ctx); err != nil {
			return 0, err
		}
	}

	sched.ClearPending()
	return result, nil
}

// MakeAsyncHandler wraps an operation factory into a suspend/resume-aware handler.
func MakeAsyncHandler(createOp func(ctx context.Context, mod api.Module, stack []uint64) PendingOp) api.GoModuleFunc {
	return func(ctx context.Context, mod api.Module, stack []uint64) {
		async := GetAsyncify(ctx)
		if async == nil {
			panic(fmt.Errorf("asyncify: async host handler invoked without a core controller"))
		}

		if async.IsRewinding(ctx) {
			result, err := Resume(ctx)
			if err != nil {
				panic(fmt.Errorf("asyncify: resume host handler: %w", err))
			}
			if len(stack) > 0 {
				stack[0] = result
			}
			return
		}

		op := createOp(ctx, mod, stack)
		if op != nil {
			if err := Suspend(ctx, op); err != nil {
				panic(fmt.Errorf("asyncify: suspend host handler: %w", err))
			}
		}
	}
}

// Generation owns permission to manipulate a configuration's guest control state;
// executionLifetime independently owns the lifetime of the underlying modules.
type asyncifyGeneration struct{ revoked atomic.Bool }

var ErrAsyncifySuperseded = errors.New("asyncify: controls superseded by reconfiguration")

type asyncifyHeader struct {
	bytes        []byte
	pointer, end uint32
}

// commit cannot fail: preparation borrowed the entire header under the caller's
// serialized execution contract. No rollback writes are necessary.
func (h asyncifyHeader) commit() {
	binary.LittleEndian.PutUint32(h.bytes[:4], h.pointer)
	binary.LittleEndian.PutUint32(h.bytes[4:], h.end)
}
