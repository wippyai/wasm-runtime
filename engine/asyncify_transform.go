package engine

import "github.com/wippyai/wasm-runtime/asyncify"

// IsAsyncified checks if a WASM module has been asyncified.
var IsAsyncified = asyncify.IsAsyncified
