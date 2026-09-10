package engine

import (
	"context"

	"github.com/tetratelabs/wazero/api"
)

// Embed the stock interface to preserve wazero's sealed metadata contract.
// Every executable method is overridden; internal canonical bindings continue
// to use raw functions inside their enclosing lowering/step/lifting lease.
type instanceFunction struct {
	api.Function
	owner *WazeroInstance
}

func (f *instanceFunction) Call(ctx context.Context, params ...uint64) ([]uint64, error) {
	ctx, finish, err := f.owner.enterExecution(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	if err := f.owner.checkSuspended(nil); err != nil {
		return nil, err
	}
	return f.Function.Call(ctx, params...)
}

func (f *instanceFunction) CallWithStack(ctx context.Context, stack []uint64) error {
	ctx, finish, err := f.owner.enterExecution(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err := f.owner.checkSuspended(nil); err != nil {
		return err
	}
	return f.Function.CallWithStack(ctx, stack)
}
