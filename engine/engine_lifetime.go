package engine

import (
	"context"
	"fmt"
)

// Startup is owned until the new instance is published or all its partial
// resources are disposed. Engine shutdown joins that ownership before closing
// the backend, so construction cannot publish into a closed runtime.
func (e *WazeroEngine) enterInstantiation(ctx context.Context) (context.Context, func(), error) {
	e.modulesMu.Lock()
	defer e.modulesMu.Unlock()
	if e.closed {
		return nil, nil, fmt.Errorf("engine is closed")
	}
	return e.startups.enter(ctx)
}

func (e *WazeroEngine) registerInstance(i *WazeroInstance) error {
	e.modulesMu.Lock()
	defer e.modulesMu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed during instance startup")
	}
	e.instances[i] = struct{}{}
	return nil
}

func (e *WazeroEngine) removeInstance(i *WazeroInstance) {
	e.modulesMu.Lock()
	delete(e.instances, i)
	e.modulesMu.Unlock()
}

// Close stops admission and execution first. A failed join retains every
// unfinished owner for a later retry; it never authorizes backend teardown.
func (e *WazeroEngine) Close(ctx context.Context) error {
	e.closeMu.Lock()
	e.modulesMu.Lock()
	e.closed = true
	if e.startups != nil {
		e.startups.stop()
	}
	instances := make([]*WazeroInstance, 0, len(e.instances))
	for i := range e.instances {
		if i.lifetime != nil {
			i.lifetime.stop()
		}
		instances = append(instances, i)
	}
	e.modulesMu.Unlock()
	e.closeMu.Unlock()

	// A callback may initiate shutdown, but cannot synchronously join itself.
	if e.startups != nil && e.startups.heldBy(ctx) {
		return ErrCloseFromExecution
	}
	for _, i := range instances {
		if i.lifetime != nil && i.lifetime.heldBy(ctx) {
			return ErrCloseFromExecution
		}
	}

	// Serialize cleanup attempts without making canceled callers wait on a mutex
	// held across another caller's execution join or resource destructor.
	for {
		e.modulesMu.Lock()
		if e.closeComplete {
			err := e.closeErr
			e.modulesMu.Unlock()
			return err
		}
		if pending := e.closeAttempt; pending != nil {
			e.modulesMu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		e.closeAttempt = make(chan struct{})
		e.modulesMu.Unlock()
		break
	}
	defer func() {
		e.modulesMu.Lock()
		close(e.closeAttempt)
		e.closeAttempt = nil
		e.modulesMu.Unlock()
	}()

	if e.startups != nil {
		if err := e.startups.wait(ctx); err != nil {
			return err
		}
	}
	// All constructors have either published before stop or rolled back. Join
	// ALL surviving instances before reclaiming any engine-wide resource.
	for _, i := range instances {
		if i.lifetime != nil {
			if err := i.lifetime.wait(ctx); err != nil {
				return err
			}
		}
	}
	e.modulesMu.Lock()
	firstErr := e.closeErr
	e.modulesMu.Unlock()
	for _, i := range instances {
		err := i.Close(ctx)
		complete, terminalErr := i.closeResult()
		if !complete {
			// A different caller can own resource teardown after execution drains.
			// Its canceled waiter does not authorize closing shared compiled/runtime
			// state. Retain this engine attempt for an external retry.
			return err
		}
		if terminalErr != nil && firstErr == nil {
			firstErr = terminalErr
			e.modulesMu.Lock()
			e.closeErr = terminalErr
			e.modulesMu.Unlock()
		}
	}
	e.modulesMu.Lock()
	mods := e.modules
	e.modules = nil
	e.modulesMu.Unlock()
	for _, m := range mods {
		if err := m.close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := e.runtime.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	e.modulesMu.Lock()
	e.closeErr, e.closeComplete = firstErr, true
	e.modulesMu.Unlock()
	return firstErr
}
