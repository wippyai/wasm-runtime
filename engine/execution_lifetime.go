package engine

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

var errExecutionLifetimeStopped = errors.New("instance execution lifetime stopped")

// ErrCloseFromExecution means shutdown has started, but the caller holds an
// execution lease. A caller outside that execution must retry Close to reclaim
// resources. Host callbacks must pass their supplied context to Close.
var ErrCloseFromExecution = errors.New("instance close deferred until current execution returns")

type executionLeaseKey struct{}

// executionLease is both the unique active marker and the context wrapper
// handed to guest code. Its parent context and marker ancestry are assigned
// before publication and never changed. It is deliberately never pooled: host
// code may retain the context after a guest step returns.
type executionLease struct {
	context.Context
	lifetime  *executionLifetime
	parent    *executionLease
	closeCall *executionCall // set only for one-shot synchronous calls
	active    atomic.Bool
}

func (e *executionLease) Value(key any) any {
	if _, ok := key.(executionLeaseKey); ok {
		return e
	}
	return e.Context.Value(key)
}

// finish owns release exactly once; nil represents an untracked no-op lease.
// Marking inactive precedes the lifetime
// decrement so a re-entrant Close observes that this context no longer owns
// guest execution, while the active count still forces it to join safely.
func (e *executionLease) finish() {
	if e == nil || !e.active.CompareAndSwap(true, false) {
		return
	}
	e.lifetime.mu.Lock()
	e.lifetime.active--
	if e.lifetime.active == 0 && e.lifetime.stopped {
		close(e.lifetime.drained)
	}
	e.lifetime.mu.Unlock()
	if e.closeCall != nil {
		e.closeCall.close()
	}
}

// sameExecutionContext proves identity only for pointer-backed contexts. An
// arbitrary context implementation may be a non-comparable value, so direct
// interface equality can panic. Equal value contexts are also not sufficient:
// their Values may represent distinct step ownership. The standard background
// context is immutable and has no caller values, deadline, or cancellation.
func sameExecutionContext(a, b context.Context) bool {
	if a == context.Background() && b == context.Background() {
		return true
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta == nil || ta != tb || ta.Kind() != reflect.Pointer {
		return false
	}
	return a == b
}

func (l *executionLifetime) heldBy(ctx context.Context) bool {
	marker, _ := ctx.Value(executionLeaseKey{}).(*executionLease)
	for ; marker != nil; marker = marker.parent {
		if marker.lifetime == l && marker.active.Load() {
			return true
		}
	}
	return false
}

// executionLifetime separates instance shutdown from per-call cancellation.
// Stop prevents new entries and cancels existing calls. Waiting for their
// returned leases is a separate action so a close notifier never joins itself.
// Only after wait succeeds may the owner reclaim guest/host resources.
type executionLifetime struct {
	ctx    context.Context
	cancel context.CancelFunc
	// Closed once, when stopped and the final admitted execution has returned.
	drained chan struct{}
	mu      sync.Mutex
	active  uint64
	stopped bool
}

func newExecutionLifetime() *executionLifetime {
	ctx, cancel := context.WithCancel(context.Background())
	drained := make(chan struct{})
	return &executionLifetime{ctx: ctx, cancel: cancel, drained: drained}
}

// executionCall holds cancellation across an entire synchronous call or
// Asyncify session. Each executing step/lift separately enters and leaves the
// instance lifetime. Waiting for a message retains no execution lease.
type executionCall struct {
	lifetime *executionLifetime
	// parent is the immutable initial call context. A session step supplied
	// with this exact context can derive directly from ctx without a second
	// cancellation bridge: ctx already inherits its values, deadline and cause.
	parent     context.Context
	ctx        context.Context
	cancel     context.CancelFunc
	stopNotify func() bool
	closeOnce  sync.Once
}

func (l *executionLifetime) newCall(parent context.Context) (*executionCall, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	stopped := l.stopped
	l.mu.Unlock()
	if stopped {
		return nil, errExecutionLifetimeStopped
	}
	ctx, cancel := context.WithCancel(parent)
	call := &executionCall{lifetime: l, parent: parent, ctx: ctx, cancel: cancel}
	call.stopNotify = context.AfterFunc(l.ctx, cancel)
	if l.ctx.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		call.close()
		return nil, err
	}
	return call, nil
}
func (c *executionCall) close() { c.closeOnce.Do(func() { c.stopNotify(); c.cancel() }) }

// admitLease makes a fully initialized lease active. The caller owns its
// allocation: ordinary entries use a standalone lease while a session step
// embeds one by value in its guest context. base is always the cancellation
// context prepared for this entry, never a wrapper that exposes the lease.
func (c *executionCall) admitLease(lease *executionLease) error {
	if lease == nil || lease.Context == nil {
		panic("cannot admit a lease without a base context")
	}
	if lease.lifetime != nil || lease.parent != nil || lease.active.Load() {
		panic("cannot admit a lease more than once")
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}

	l := c.lifetime
	parent, _ := lease.Context.Value(executionLeaseKey{}).(*executionLease)
	// These identity and ancestry fields never change after this point. The
	// lease is not reachable through a guest context until the caller returns.
	lease.lifetime = l
	lease.parent = parent

	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return errExecutionLifetimeStopped
	}
	l.active++
	l.mu.Unlock()
	lease.active.Store(true)
	if err := c.ctx.Err(); err != nil {
		lease.finish()
		return err
	}
	return nil
}

// enterLease admits a fresh, non-pooled standalone lease. Session steps use
// admitLease with the lease embedded in stepExecutionContext instead.
func (c *executionCall) enterLease(base context.Context, closeCall *executionCall) (*executionLease, error) {
	lease := &executionLease{Context: base, closeCall: closeCall}
	if err := c.admitLease(lease); err != nil {
		return nil, err
	}
	return lease, nil
}

// enter is the synchronous-call convenience. Asyncify retains executionCall
// across suspension and uses its enter method separately for each active step.
func (l *executionLifetime) enter(parent context.Context) (context.Context, func(), error) {
	call, err := l.newCall(parent)
	if err != nil {
		return nil, nil, err
	}
	lease, err := call.enterLease(call.ctx, call)
	if err != nil {
		call.close()
		return nil, nil, err
	}
	return lease, lease.finish, nil
}
func (l *executionLifetime) stop() {
	l.mu.Lock()
	if !l.stopped {
		l.stopped = true
		if l.active == 0 {
			close(l.drained)
		}
	}
	l.mu.Unlock()
	l.cancel()
}

// wait joins admitted executions; the caller must stop first. A canceled wait
// does not authorize resource release. It can be retried with another context.
func (l *executionLifetime) wait(ctx context.Context) error {
	l.mu.Lock()
	if !l.stopped {
		l.mu.Unlock()
		return errors.New("cannot join a running lifetime")
	}
	drained := l.drained
	l.mu.Unlock()
	select {
	case <-drained:
		return nil
	default:
	}
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Instances without an installed budget/lifetime keep the existing fast path.
func (i *WazeroInstance) enterExecution(ctx context.Context) (context.Context, func(), error) {
	if i.lifetime == nil {
		return ctx, finishUntrackedExecution, nil
	}
	return i.lifetime.enter(ctx)
}
func finishUntrackedExecution() {}

func (i *WazeroInstance) beginSessionExecution(parent context.Context) (*executionCall, context.Context, func(), error) {
	if i.lifetime == nil {
		return nil, parent, finishUntrackedExecution, nil
	}
	call, err := i.lifetime.newCall(parent)
	if err != nil {
		return nil, nil, nil, err
	}
	lease, err := call.enterLease(call.ctx, nil)
	if err != nil {
		call.close()
		return nil, nil, nil, err
	}
	return call, lease, lease.finish, nil
}

type executionDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (c *executionDeadlineContext) Deadline() (time.Time, bool) { return c.deadline, true }

// prepareSessionExecution creates only the cancelable base for one active
// session entry. The caller admits either a standalone lease (for lowering and
// lifting) or a lease embedded in the step guest context. Its cleanup is not
// installed until that admission succeeds, so a rejected entry cannot cancel
// the previous suspended operation.
func (cs *CallSession) prepareSessionExecution(parent context.Context) (context.Context, func(), error) {
	if cs.execution == nil {
		return parent, nil, nil
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	// StartCall commonly receives the same context that later drives every
	// Step/LiftResult. The retained call context is its cancellation child, so
	// a disposable child of it already preserves the initial values, deadline
	// and cancellation cause, including lifetime shutdown. It replaces the
	// per-step WithCancelCause + AfterFunc bridge while releaseStepContext still
	// invalidates a suspended host operation before the next step.
	if sameExecutionContext(parent, cs.execution.parent) {
		ctx, cancel := context.WithCancel(cs.execution.ctx)
		if err := ctx.Err(); err != nil {
			cancel()
			return nil, nil, err
		}
		return ctx, cancel, nil
	}

	ctx, cancel := context.WithCancelCause(parent)
	stop := context.AfterFunc(cs.execution.ctx, func() { cancel(context.Cause(cs.execution.ctx)) })
	if cs.execution.ctx.Err() != nil {
		cancel(context.Cause(cs.execution.ctx))
	}
	cleanup := func() { stop(); cancel(context.Canceled) }
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, nil, err
	}
	// A new step consumes the preceding operation's result only after its new
	// lease has been admitted. A stopped lifetime must leave the old suspended
	// operation untouched for terminal cleanup.
	result := ctx
	if initial, ok := cs.execution.ctx.Deadline(); ok {
		if current, has := ctx.Deadline(); !has || initial.Before(current) {
			result = &executionDeadlineContext{Context: ctx, deadline: initial}
		}
	}
	return result, cleanup, nil
}

// enterSessionExecution retains the plain lease context shape for lowering,
// lifting and direct lifetime tests. Step uses enterStepExecution below so its
// guest engine context can own the lease in the same allocation.
func (cs *CallSession) enterSessionExecution(parent context.Context) (context.Context, *executionLease, error) {
	base, cleanup, err := cs.prepareSessionExecution(parent)
	if err != nil {
		return nil, nil, err
	}
	if cs.execution == nil {
		return base, nil, nil
	}
	lease, err := cs.execution.enterLease(base, nil)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	cs.releaseStepContext()
	cs.stepContextCleanup = cleanup
	return lease, lease, nil
}

// enterStepExecution returns the one fresh guest context for CallSession.Step.
// It embeds the active lease by value, so no separate lease wrapper is placed
// below the engine context. LiftResult deliberately continues to use
// enterSessionExecution and adds no step metadata.
func (cs *CallSession) enterStepExecution(parent context.Context) (context.Context, *executionLease, error) {
	base, cleanup, err := cs.prepareSessionExecution(parent)
	if err != nil {
		return nil, nil, err
	}
	if cs.execution == nil {
		return withSessionCallContext(base, cs), nil, nil
	}
	step := newStepExecutionContext(base, cs)
	if err := cs.execution.admitLease(&step.lease); err != nil {
		cleanup()
		return nil, nil, err
	}
	cs.releaseStepContext()
	cs.stepContextCleanup = cleanup
	return step, &step.lease, nil
}

func (cs *CallSession) releaseStepContext() {
	if cs.stepContextCleanup != nil {
		cs.stepContextCleanup()
		cs.stepContextCleanup = nil
	}
}
func (cs *CallSession) closeExecution() {
	cs.releaseStepContext()
	if cs.execution != nil {
		cs.execution.close()
	}
}
