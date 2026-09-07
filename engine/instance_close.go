package engine

import "context"

// beginClose elects one teardown owner after execution is drained. The mutex
// protects only publication; resource destructors run outside it. Other callers
// can abandon their wait without abandoning or stealing the teardown owner.
func (i *WazeroInstance) beginClose(ctx context.Context) (bool, error) {
	for {
		i.closeMu.Lock()
		if i.closed {
			err := i.closeErr
			i.closeMu.Unlock()
			return false, err
		}
		if pending := i.closeAttempt; pending != nil {
			i.closeMu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
		i.closeAttempt = make(chan struct{})
		i.closeMu.Unlock()
		return true, nil
	}
}

func (i *WazeroInstance) finishClose(err error) {
	i.closeMu.Lock()
	i.closeErr = err
	i.closed = true
	close(i.closeAttempt)
	i.closeAttempt = nil
	i.closeMu.Unlock()
}

// closeResult distinguishes a terminal destructor error from an incomplete
// join. An engine may reclaim shared backend state only after all instances
// reach this terminal state, irrespective of whether execution already stopped.
func (i *WazeroInstance) closeResult() (bool, error) {
	i.closeMu.Lock()
	defer i.closeMu.Unlock()
	return i.closed, i.closeErr
}
