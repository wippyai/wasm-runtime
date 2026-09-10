package semantics

import "github.com/wippyai/wasm-runtime/wasm"

// ExecutionDomain describes when a materialization may write its storage.
// Guest writes run only after rewind has reached the suspended continuation;
// routing writes may execute earlier and must never alias restored guest slots.
type ExecutionDomain uint8

const (
	GuestExecution ExecutionDomain = iota
	RewindRouting
)

// Valid reports whether the domain names an execution policy owned by lowering.
func (d ExecutionDomain) Valid() bool {
	return d == GuestExecution || d == RewindRouting
}

// Value identifies a definition independently of its reusable storage slot.
// A slot may hold many successive values; equal LocalIdx does not imply equal ID.
type Value struct {
	ID       uint64
	LocalIdx uint32
	Type     wasm.ValType
}

// Materialization is one checked lowering decision. Reuse retains a previously
// defined immutable value. LocalRead records the source cell for a local snapshot.
// Ordinary temporary definitions have LocalRead=false and Reuse=false.
type Materialization struct {
	Value
	SourceLocal uint32
	Domain      ExecutionDomain
	LocalRead   bool
	Reuse       bool
}
