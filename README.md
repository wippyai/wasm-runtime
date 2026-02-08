# wasm-runtime

Pure Go WebAssembly Component Model runtime.

## Features

- Component Model with WIT type system and canonical ABI
- WASI Preview 2 (filesystem, sockets, HTTP, clocks, random, CLI)
- Pure Go asyncify transform for async host calls
- WAT text format compiler (no external tools)
- Built on [wazero](https://wazero.io/) (zero dependencies runtime)

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
