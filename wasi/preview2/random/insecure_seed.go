package random

import (
	"context"
	"time"
)

type InsecureSeedHost struct{}

func NewInsecureSeedHost() *InsecureSeedHost {
	return &InsecureSeedHost{}
}

func (h *InsecureSeedHost) Namespace() string {
	return "wasi:random/insecure-seed@0.2.0"
}

func (h *InsecureSeedHost) InsecureSeed(_ context.Context) (uint64, uint64) {
	now := time.Now().UnixNano()
	return uint64(now), uint64(now >> 32)
}
