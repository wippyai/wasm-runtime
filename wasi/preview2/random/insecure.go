package random

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

var (
	insecureRand   = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // intentionally insecure per WASI spec
	insecureRandMu sync.Mutex
)

type InsecureRandomHost struct{}

func NewInsecureRandomHost() *InsecureRandomHost {
	return &InsecureRandomHost{}
}

func (h *InsecureRandomHost) Namespace() string {
	return "wasi:random/insecure@0.2.0"
}

func (h *InsecureRandomHost) GetInsecureRandomBytes(_ context.Context, len uint64) []byte {
	buf := make([]byte, len)
	insecureRandMu.Lock()
	_, _ = insecureRand.Read(buf)
	insecureRandMu.Unlock()
	return buf
}

func (h *InsecureRandomHost) GetInsecureRandomU64(_ context.Context) uint64 {
	insecureRandMu.Lock()
	result := insecureRand.Uint64()
	insecureRandMu.Unlock()
	return result
}
