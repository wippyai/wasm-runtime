package engine

import (
	"context"

	"github.com/tetratelabs/wazero/experimental"
	"github.com/wippyai/wasm-runtime/linker"
)

// This notification is synchronous and deliberately does not join execution:
// wazero may notify while the startup/call lease itself is still active.
func (l *executionLifetime) withCoreCloseNotifier(ctx context.Context, cfg *InstanceConfig, admission *linker.MemoryAdmission) context.Context {
	var observe func(context.Context, uint32)
	if cfg != nil {
		observe = cfg.OnCoreModuleClosed
	}
	return experimental.WithCloseNotifier(ctx, experimental.CloseNotifyFunc(func(ctx context.Context, code uint32) {
		l.stop()
		if admission != nil {
			admission.Stop()
		}
		if observe != nil {
			observe(ctx, code)
		}
	}))
}

func (l *executionLifetime) startupResult(ctx context.Context) error {
	l.mu.Lock()
	stopped := l.stopped
	l.mu.Unlock()
	if stopped {
		return errExecutionLifetimeStopped
	}
	return ctx.Err()
}
