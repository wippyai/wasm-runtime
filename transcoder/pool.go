package transcoder

import "sync"

const (
	// Pool limits to prevent memory bloat
	poolMaxCap64  = 1024 // max uint64 elements
	poolInitCap64 = 16
)

// uint64 buffer pool for flattening
var buf64Pool = sync.Pool{
	New: func() any {
		buf := make([]uint64, 0, poolInitCap64)
		return &buf
	},
}

func getBuf64() *[]uint64 {
	return buf64Pool.Get().(*[]uint64)
}

func putBuf64(buf *[]uint64) {
	if buf == nil || cap(*buf) > poolMaxCap64 {
		return // reject oversized
	}
	*buf = (*buf)[:0]
	buf64Pool.Put(buf)
}
