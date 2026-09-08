# wasm-runtime

Pure Go WebAssembly Component Model runtime.

## Features

- Component Model with WIT type system and canonical ABI
- WASI Preview 2 (filesystem, sockets, HTTP, clocks, random, CLI)
- Pure Go asyncify transform for async host calls
- WAT text format compiler (no external tools)
- Built on [wazero](https://wazero.io/) (zero dependencies runtime)

## Asyncify execution profile

An executable core may own its function tables. With Asyncify enabled, a core
that imports a table and defines functions is rejected before compilation:
indirect calls through shared guest tables can cross cores without a linker
continuation boundary. Table initializer modules with only imported functions
remain supported, including the canonical adapter fixup pattern. Synchronous
components are not subject to this restriction. Core function and global imports
must also carry numeric values (including SIMD), rather than references: a raw
function reference could otherwise enter a private table and bypass the same
boundary. Canonical ABI resource handles remain supported as integer handles.

Cross-core calls retain a separate continuation stack for each core. A bridge
rejects recursive entry into its already-active cached function, as required
by Wazero's function-call contract; different exports of one core may nest.
After a failed async call, including cancellation, close the instance before
starting another call. Dropping the root session alone cannot safely recover
parked child continuations.

## Usage

```go
rt := runtime.New()
defer rt.Close(ctx)

mod, err := rt.LoadComponent(ctx, wasmBytes)
inst, err := mod.Instantiate(ctx)
defer inst.Close(ctx)

result, err := inst.Call(ctx, "greet", "World")
```

## License

See [LICENSE](LICENSE).

---

This component has been AI generated.
