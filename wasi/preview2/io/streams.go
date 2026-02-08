package io

import (
	"context"
	"errors"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type StreamsHost struct {
	resources *preview2.ResourceTable
}

func NewStreamsHost(resources *preview2.ResourceTable) *StreamsHost {
	return &StreamsHost{resources: resources}
}

func (h *StreamsHost) Namespace() string {
	return "wasi:io/streams@0.2.8"
}

func (h *StreamsHost) MethodInputStreamRead(_ context.Context, self uint32, length uint64) ([]byte, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil, &preview2.StreamError{Closed: true}
	}

	// Use interface to support any input stream type (InputStreamResource, TCPInputStreamResource, etc.)
	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return nil, &preview2.StreamError{Closed: true}
	}

	data, err := stream.Read(length)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return nil, se
		}
		return nil, &preview2.StreamError{LastOpFailed: true}
	}

	return data, nil
}

func (h *StreamsHost) MethodInputStreamBlockingRead(ctx context.Context, self uint32, length uint64) ([]byte, *preview2.StreamError) {
	return h.MethodInputStreamRead(ctx, self, length)
}

func (h *StreamsHost) MethodInputStreamSkip(_ context.Context, self uint32, length uint64) (uint64, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	data, err := stream.Read(length)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return 0, se
		}
		return 0, &preview2.StreamError{LastOpFailed: true}
	}

	return uint64(len(data)), nil
}

func (h *StreamsHost) MethodInputStreamBlockingSkip(ctx context.Context, self uint32, length uint64) (uint64, *preview2.StreamError) {
	return h.MethodInputStreamSkip(ctx, self, length)
}

func (h *StreamsHost) MethodInputStreamSubscribe(_ context.Context, _ uint32) uint32 {
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

func (h *StreamsHost) MethodOutputStreamCheckWrite(_ context.Context, self uint32) (uint64, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	// Use interface to support any output stream type
	stream, ok := r.(interface{ CheckWrite() (uint64, error) })
	if !ok {
		// Default to allowing writes if CheckWrite not implemented
		return 1024 * 1024, nil
	}

	size, err := stream.CheckWrite()
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return 0, se
		}
		return 0, &preview2.StreamError{LastOpFailed: true}
	}

	return size, nil
}

func (h *StreamsHost) MethodOutputStreamWrite(_ context.Context, self uint32, contents []byte) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	// Use interface to support any output stream type (OutputStreamResource, TCPOutputStreamResource, etc.)
	stream, ok := r.(interface{ Write([]byte) error })
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	err := stream.Write(contents)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return se
		}
		return &preview2.StreamError{LastOpFailed: true}
	}

	return nil
}

func (h *StreamsHost) MethodOutputStreamBlockingWriteAndFlush(ctx context.Context, self uint32, contents []byte) *preview2.StreamError {
	return h.MethodOutputStreamWrite(ctx, self, contents)
}

func (h *StreamsHost) MethodOutputStreamFlush(_ context.Context, self uint32) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	// Check if the stream supports flushing
	if flusher, ok := r.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return &preview2.StreamError{LastOpFailed: true}
		}
	}
	return nil
}

func (h *StreamsHost) MethodOutputStreamBlockingFlush(ctx context.Context, self uint32) *preview2.StreamError {
	return h.MethodOutputStreamFlush(ctx, self)
}

func (h *StreamsHost) MethodOutputStreamSubscribe(_ context.Context, _ uint32) uint32 {
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

func (h *StreamsHost) MethodOutputStreamWriteZeroes(_ context.Context, self uint32, length uint64) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Write([]byte) error })
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	// Limit allocation to prevent DoS
	if length > preview2.MaxAllocationSize {
		return &preview2.StreamError{LastOpFailed: true}
	}

	zeroes := make([]byte, length)
	err := stream.Write(zeroes)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return se
		}
		return &preview2.StreamError{LastOpFailed: true}
	}

	return nil
}

func (h *StreamsHost) MethodOutputStreamBlockingWriteZeroesAndFlush(ctx context.Context, self uint32, length uint64) *preview2.StreamError {
	return h.MethodOutputStreamWriteZeroes(ctx, self, length)
}

func (h *StreamsHost) MethodOutputStreamSplice(_ context.Context, self uint32, src uint32, length uint64) (uint64, *preview2.StreamError) {
	srcR, ok := h.resources.Get(src)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	dstR, ok := h.resources.Get(self)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	srcStream, ok := srcR.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	dstStream, ok := dstR.(interface{ Write([]byte) error })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	data, err := srcStream.Read(length)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return 0, se
		}
		return 0, &preview2.StreamError{LastOpFailed: true}
	}

	err = dstStream.Write(data)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return 0, se
		}
		return 0, &preview2.StreamError{LastOpFailed: true}
	}

	return uint64(len(data)), nil
}

func (h *StreamsHost) MethodOutputStreamBlockingSplice(ctx context.Context, self uint32, src uint32, length uint64) (uint64, *preview2.StreamError) {
	return h.MethodOutputStreamSplice(ctx, self, src, length)
}

func (h *StreamsHost) ResourceDropInputStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *StreamsHost) ResourceDropOutputStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *StreamsHost) Register() map[string]any {
	return map[string]any{
		"[method]input-stream.read":          h.MethodInputStreamRead,
		"[method]input-stream.blocking-read": h.MethodInputStreamBlockingRead,
		"[method]input-stream.skip":          h.MethodInputStreamSkip,
		"[method]input-stream.blocking-skip": h.MethodInputStreamBlockingSkip,
		"[method]input-stream.subscribe":     h.MethodInputStreamSubscribe,
		// Output stream methods
		"[method]output-stream.check-write":                     h.MethodOutputStreamCheckWrite,
		"[method]output-stream.write":                           h.MethodOutputStreamWrite,
		"[method]output-stream.blocking-write-and-flush":        h.MethodOutputStreamBlockingWriteAndFlush,
		"[method]output-stream.flush":                           h.MethodOutputStreamFlush,
		"[method]output-stream.blocking-flush":                  h.MethodOutputStreamBlockingFlush,
		"[method]output-stream.subscribe":                       h.MethodOutputStreamSubscribe,
		"[method]output-stream.write-zeroes":                    h.MethodOutputStreamWriteZeroes,
		"[method]output-stream.blocking-write-zeroes-and-flush": h.MethodOutputStreamBlockingWriteZeroesAndFlush,
		"[method]output-stream.splice":                          h.MethodOutputStreamSplice,
		"[method]output-stream.blocking-splice":                 h.MethodOutputStreamBlockingSplice,
		// Resource destructors
		"[resource-drop]input-stream":  h.ResourceDropInputStream,
		"[resource-drop]output-stream": h.ResourceDropOutputStream,
	}
}
