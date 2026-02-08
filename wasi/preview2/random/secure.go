package random

import (
	"context"
	"crypto/rand"
	"encoding/binary"
)

type SecureRandomHost struct{}

func NewSecureRandomHost() *SecureRandomHost {
	return &SecureRandomHost{}
}

func (h *SecureRandomHost) Namespace() string {
	return "wasi:random/random@0.2.0"
}

// MaxRandomBytes limits single-call allocation to prevent DoS (1MB).
const MaxRandomBytes = 1 << 20

func (h *SecureRandomHost) GetRandomBytes(_ context.Context, len uint64) []byte {
	if len > MaxRandomBytes {
		len = MaxRandomBytes
	}
	buf := make([]byte, len)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand.Read should never fail on a properly configured system
		// Return nil rather than panic to avoid guest terminating host
		return nil
	}
	return buf
}

func (h *SecureRandomHost) GetRandomU64(_ context.Context) uint64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Return 0 on error rather than panic
		return 0
	}
	return binary.LittleEndian.Uint64(buf[:])
}
