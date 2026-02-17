package wasmruntime

// Memory represents WASM linear memory
type Memory interface {
	Read(offset uint32, length uint32) ([]byte, error)
	Write(offset uint32, data []byte) error
	ReadU8(offset uint32) (uint8, error)
	ReadU16(offset uint32) (uint16, error)
	ReadU32(offset uint32) (uint32, error)
	ReadU64(offset uint32) (uint64, error)
	WriteU8(offset uint32, value uint8) error
	WriteU16(offset uint32, value uint16) error
	WriteU32(offset uint32, value uint32) error
	WriteU64(offset uint32, value uint64) error
}

// MemorySizer provides the current size of WASM linear memory in bytes.
type MemorySizer interface {
	Size() uint32
}

// Allocator allocates memory in WASM linear memory
type Allocator interface {
	Alloc(size, align uint32) (uint32, error)
	Free(ptr, size, align uint32)
}
