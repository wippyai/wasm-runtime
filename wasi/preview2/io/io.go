package io

import "github.com/wippyai/wasm-runtime/wasi/preview2"

// Host aggregates all IO hosts for convenience.
type Host struct {
	Error   *ErrorHost
	Poll    *PollHost
	Streams *StreamsHost
}

// NewHost creates all IO hosts
func NewHost(resources *preview2.ResourceTable) *Host {
	return &Host{
		Error:   NewErrorHost(resources),
		Poll:    NewPollHost(resources),
		Streams: NewStreamsHost(resources),
	}
}
