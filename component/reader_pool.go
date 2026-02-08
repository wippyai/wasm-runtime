package component

import (
	"bytes"
	"io"
	"sync"
)

// readerPool pools bytes.Reader instances to reduce allocations
var readerPool = sync.Pool{
	New: func() interface{} {
		return &bytes.Reader{}
	},
}

// getReader gets a pooled reader initialized with data
func getReader(data []byte) *bytes.Reader {
	r := readerPool.Get().(*bytes.Reader)
	r.Reset(data)
	return r
}

// putReader returns a reader to the pool
func putReader(r *bytes.Reader) {
	readerPool.Put(r)
}

// readByte reads a single byte efficiently without allocation
func readByte(r io.Reader) (byte, error) {
	if br, ok := r.(io.ByteReader); ok {
		return br.ReadByte()
	}
	// Fallback for readers that don't implement ByteReader
	var b [1]byte
	_, err := r.Read(b[:])
	return b[0], err
}
