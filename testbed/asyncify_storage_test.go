package testbed

import (
	"crypto/sha256"
	"fmt"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/engine"
)

// These two pinned standalone demo binaries use a fixed stack/data layout and
// contain no allocator or memory.grow. Their test host explicitly owns appended
// pages. This is a fixture contract, not a reservation policy for arbitrary WASM.
func initDemoAsyncify(code []byte, control *engine.Asyncify, mod api.Module) error {
	switch fmt.Sprintf("%x", sha256.Sum256(code)) {
	case "ef0f65adf79e7d7a50831198404517381d146614b1bef07b24ebf9aa727e001c",
		"fadde9e8e5e9e7c634c9f3b33503174d13b2a4ff985483ce1a3b18dbede1c907":
	default:
		return fmt.Errorf("demo changed: re-audit ownership of appended memory before reserving its Asyncify stack")
	}
	// Two pages hold the 64 KiB stack plus its eight-byte header.
	pages, ok := mod.Memory().Grow(2)
	if !ok || pages > 65533 {
		return fmt.Errorf("cannot reserve demo suspension storage")
	}
	control.SetDataAddr(pages * 65536)
	control.SetStackSize(engine.DefaultAsyncifyStackBytes)
	return control.Init(mod)
}
