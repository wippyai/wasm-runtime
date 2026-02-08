package clocks

import (
	"context"
	"time"
)

type WallClockHost struct{}

func NewWallClockHost() *WallClockHost {
	return &WallClockHost{}
}

func (h *WallClockHost) Namespace() string {
	return "wasi:clocks/wall-clock@0.2.3"
}

type Datetime struct {
	Seconds     uint64
	Nanoseconds uint32
}

func (h *WallClockHost) Now(_ context.Context) Datetime {
	now := time.Now()
	return Datetime{
		Seconds:     uint64(now.Unix()),
		Nanoseconds: uint32(now.Nanosecond()),
	}
}

func (h *WallClockHost) Resolution(_ context.Context) Datetime {
	return Datetime{
		Seconds:     1,
		Nanoseconds: 0,
	}
}
