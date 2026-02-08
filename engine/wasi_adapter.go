package engine

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	ebadf     = 8          // POSIX EBADF error code
	invalidFD = 0xFFFFFFFF // -1 as uint32
)

// InstantiateWASIWithAdapter instantiates WASI preview1 with adapter functions
// required by componentize-py and similar tools that use the component model adapter.
func InstantiateWASIWithAdapter(ctx context.Context, r wazero.Runtime) (api.Module, error) {
	builder := r.NewHostModuleBuilder("wasi_snapshot_preview1")
	wasi_snapshot_preview1.NewFunctionExporter().ExportFunctions(builder)

	builder = builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
		}), nil, nil).
		Export("reset_adapter_state")

	builder = builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = ebadf
		}), []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("adapter_close_badfd")

	builder = builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = invalidFD
		}), []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("adapter_open_badfd")

	return builder.Instantiate(ctx)
}
