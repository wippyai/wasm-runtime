These component binaries are fixtures for engine ownership, canonical binding,
and Asyncify session regressions. Each filename is the SHA-256 of the exact WAT
string passed to `componentFixture` in the engine tests. The WAT in those tests
is the source of truth; changing it requires regenerating the corresponding binary.

Normal tests do not require `wasm-tools`. To regenerate, install `wasm-tools` and
run from the module root:

```sh
WIPPY_UPDATE_COMPONENT_FIXTURES=1 go test ./engine
```

Review the source change and generated binary together. Remove obsolete fixtures
when a source change leaves an old hash unused. Some fixtures deliberately omit
required canonical options to test error handling, so parsing them does not imply
that they pass Component Model validation.
