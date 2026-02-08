package runtime

import (
	"context"

	"github.com/wippyai/wasm-runtime/errors"
	"github.com/wippyai/wasm-runtime/wat"
)

func (r *Runtime) LoadWAT(ctx context.Context, watText, witTypes string) (*Module, error) {
	wasm, err := wat.Compile(watText)
	if err != nil {
		return nil, errors.ParseFailed("WAT", err)
	}

	return r.LoadWASM(ctx, wasm, witTypes)
}
