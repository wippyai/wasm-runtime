package io

import (
	"context"
	"errors"
	"io"

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

func (h *StreamsHost) toStreamError(err error) *preview2.StreamError {
	if err == nil {
		return nil
	}
	var se *preview2.StreamError
	if errors.As(err, &se) {
		if !se.LastOpFailed || se.LastOpFailedErr != 0 {
			return se
		}
	}
	if errors.Is(err, io.EOF) {
		return &preview2.StreamError{Closed: true}
	}
	var errHandle uint32
	if h.resources != nil {
		errHandle = h.resources.Add(preview2.NewErrorResource(err.Error()))
	}
	return &preview2.StreamError{
		LastOpFailed:    true,
		LastOpFailedErr: errHandle,
	}
}

func (h *StreamsHost) MethodInputStreamRead(_ context.Context, self uint32, length uint64) ([]byte, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceInputStream {
		return nil, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return nil, &preview2.StreamError{Closed: true}
	}

	if length == 0 {
		return []byte{}, nil
	}

	if length > preview2.MaxAllocationSize {
		return nil, h.toStreamError(errors.New("stream operation exceeds allocation limit"))
	}

	data, err := stream.Read(length)
	if err != nil {
		return nil, h.toStreamError(err)
	}

	return data, nil
}

func (h *StreamsHost) MethodInputStreamBlockingRead(ctx context.Context, self uint32, length uint64) ([]byte, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceInputStream {
		return nil, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return nil, &preview2.StreamError{Closed: true}
	}

	if length == 0 {
		return []byte{}, nil
	}

	if length > preview2.MaxAllocationSize {
		return nil, h.toStreamError(errors.New("stream operation exceeds allocation limit"))
	}

	var poll preview2.Pollable
	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		poll = subscriber.Subscribe()
	} else if p, ok := r.(preview2.Pollable); ok {
		poll = p
	}

	for {
		if ctx.Err() != nil {
			return nil, h.toStreamError(ctx.Err())
		}

		if poll != nil {
			for !poll.Ready() {
				poll.Block(ctx)
				if ctx.Err() != nil {
					return nil, h.toStreamError(ctx.Err())
				}
			}
		}

		data, err := stream.Read(length)
		if err != nil {
			return nil, h.toStreamError(err)
		}

		if len(data) > 0 || poll == nil {
			return data, nil
		}
	}
}

func (h *StreamsHost) MethodInputStreamSkip(_ context.Context, self uint32, length uint64) (uint64, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceInputStream {
		return 0, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	if length == 0 {
		return 0, nil
	}

	if length > preview2.MaxAllocationSize {
		return 0, h.toStreamError(errors.New("stream operation exceeds allocation limit"))
	}

	if skipper, ok := r.(interface{ Skip(uint64) (uint64, error) }); ok {
		n, err := skipper.Skip(length)
		if err != nil {
			return 0, h.toStreamError(err)
		}
		return n, nil
	}

	data, err := stream.Read(length)
	if err != nil {
		return 0, h.toStreamError(err)
	}

	return uint64(len(data)), nil
}

func (h *StreamsHost) MethodInputStreamBlockingSkip(ctx context.Context, self uint32, length uint64) (uint64, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceInputStream {
		return 0, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	if length == 0 {
		return 0, nil
	}

	if length > preview2.MaxAllocationSize {
		return 0, h.toStreamError(errors.New("stream operation exceeds allocation limit"))
	}

	var poll preview2.Pollable
	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		poll = subscriber.Subscribe()
	} else if p, ok := r.(preview2.Pollable); ok {
		poll = p
	}

	for {
		if ctx.Err() != nil {
			return 0, h.toStreamError(ctx.Err())
		}

		if poll != nil {
			for !poll.Ready() {
				poll.Block(ctx)
				if ctx.Err() != nil {
					return 0, h.toStreamError(ctx.Err())
				}
			}
		}

		if skipper, ok := r.(interface{ Skip(uint64) (uint64, error) }); ok {
			n, err := skipper.Skip(length)
			if err != nil {
				return 0, h.toStreamError(err)
			}
			if n > 0 || poll == nil {
				return n, nil
			}
			continue
		}

		data, err := stream.Read(length)
		if err != nil {
			return 0, h.toStreamError(err)
		}

		if len(data) > 0 || poll == nil {
			return uint64(len(data)), nil
		}
	}
}

func (h *StreamsHost) MethodInputStreamSubscribe(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceInputStream {
		panic("invalid stream handle or type for input stream subscribe")
	}

	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		return h.resources.Add(subscriber.Subscribe())
	}

	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

func (h *StreamsHost) MethodOutputStreamCheckWrite(_ context.Context, self uint32) (uint64, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceOutputStream {
		return 0, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ CheckWrite() (uint64, error) })
	if !ok {
		return 1024 * 1024, nil
	}

	size, err := stream.CheckWrite()
	if err != nil {
		return 0, h.toStreamError(err)
	}

	return size, nil
}

func (h *StreamsHost) MethodOutputStreamWrite(_ context.Context, self uint32, contents []byte) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceOutputStream {
		return &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Write([]byte) error })
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	err := stream.Write(contents)
	if err != nil {
		if errors.Is(err, preview2.ErrWritePermit) {
			panic(err)
		}
		return h.toStreamError(err)
	}

	return nil
}

func (h *StreamsHost) MethodOutputStreamBlockingWriteAndFlush(ctx context.Context, self uint32, contents []byte) *preview2.StreamError {
	if len(contents) > 4096 {
		panic("contents exceed maximum 4096 bytes for blocking-write-and-flush")
	}

	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceOutputStream {
		return &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Write([]byte) error })
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	var poll preview2.Pollable
	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		poll = subscriber.Subscribe()
	} else if p, ok := r.(preview2.Pollable); ok {
		poll = p
	}

	remaining := contents
	for len(remaining) > 0 {
		if ctx.Err() != nil {
			return h.toStreamError(ctx.Err())
		}

		permit := uint64(len(remaining))
		if checker, ok := r.(interface{ CheckWrite() (uint64, error) }); ok {
			p, err := checker.CheckWrite()
			if err != nil {
				return h.toStreamError(err)
			}
			permit = p
		}

		if permit == 0 {
			if poll == nil {
				return &preview2.StreamError{LastOpFailed: true}
			}
			for !poll.Ready() {
				poll.Block(ctx)
				if ctx.Err() != nil {
					return h.toStreamError(ctx.Err())
				}
			}
			continue
		}

		chunkSize := uint64(len(remaining))
		if chunkSize > permit {
			chunkSize = permit
		}

		err := stream.Write(remaining[:chunkSize])
		if err != nil {
			if errors.Is(err, preview2.ErrWritePermit) {
				panic(err)
			}
			return h.toStreamError(err)
		}
		remaining = remaining[chunkSize:]
	}

	if flusher, ok := r.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return h.toStreamError(err)
		}
	}

	if poll != nil {
		for !poll.Ready() {
			poll.Block(ctx)
			if ctx.Err() != nil {
				return h.toStreamError(ctx.Err())
			}
		}
	}

	if checker, ok := r.(interface{ CheckWrite() (uint64, error) }); ok {
		if _, err := checker.CheckWrite(); err != nil {
			return h.toStreamError(err)
		}
	}

	return nil
}

func (h *StreamsHost) MethodOutputStreamFlush(_ context.Context, self uint32) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceOutputStream {
		return &preview2.StreamError{Closed: true}
	}

	if flusher, ok := r.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return h.toStreamError(err)
		}
	}
	return nil
}

func (h *StreamsHost) MethodOutputStreamBlockingFlush(ctx context.Context, self uint32) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceOutputStream {
		return &preview2.StreamError{Closed: true}
	}

	if flusher, ok := r.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return h.toStreamError(err)
		}
	}

	var poll preview2.Pollable
	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		poll = subscriber.Subscribe()
	} else if p, ok := r.(preview2.Pollable); ok {
		poll = p
	}

	if poll != nil {
		for !poll.Ready() {
			poll.Block(ctx)
			if ctx.Err() != nil {
				return h.toStreamError(ctx.Err())
			}
		}
	}

	if checker, ok := r.(interface{ CheckWrite() (uint64, error) }); ok {
		if _, err := checker.CheckWrite(); err != nil {
			return h.toStreamError(err)
		}
	}

	return nil
}

func (h *StreamsHost) MethodOutputStreamSubscribe(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceOutputStream {
		panic("invalid stream handle or type for output stream subscribe")
	}

	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		return h.resources.Add(subscriber.Subscribe())
	}

	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

func (h *StreamsHost) MethodOutputStreamWriteZeroes(_ context.Context, self uint32, length uint64) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok || r.Type() != preview2.ResourceOutputStream {
		return &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Write([]byte) error })
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	if length > preview2.MaxAllocationSize {
		return h.toStreamError(errors.New("stream operation exceeds allocation limit"))
	}
	if _, ok := r.(*preview2.TCPOutputStreamResource); ok && length > preview2.DefaultBufferSize {
		panic(preview2.ErrWritePermit)
	}

	zeroes := make([]byte, length)
	err := stream.Write(zeroes)
	if err != nil {
		if errors.Is(err, preview2.ErrWritePermit) {
			panic(err)
		}
		return h.toStreamError(err)
	}

	return nil
}

func (h *StreamsHost) MethodOutputStreamBlockingWriteZeroesAndFlush(ctx context.Context, self uint32, length uint64) *preview2.StreamError {
	if length > 4096 {
		panic("length exceeds maximum 4096 bytes for blocking-write-zeroes-and-flush")
	}
	zeroes := make([]byte, length)
	return h.MethodOutputStreamBlockingWriteAndFlush(ctx, self, zeroes)
}

func (h *StreamsHost) MethodOutputStreamSplice(_ context.Context, self uint32, src uint32, length uint64) (uint64, *preview2.StreamError) {
	if length == 0 {
		return 0, nil
	}

	dstR, ok := h.resources.Get(self)
	if !ok || dstR.Type() != preview2.ResourceOutputStream {
		return 0, &preview2.StreamError{Closed: true}
	}

	srcR, ok := h.resources.Get(src)
	if !ok || srcR.Type() != preview2.ResourceInputStream {
		return 0, &preview2.StreamError{Closed: true}
	}

	dstStream, ok := dstR.(interface{ Write([]byte) error })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	srcStream, ok := srcR.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	if length > preview2.MaxAllocationSize {
		return 0, h.toStreamError(errors.New("stream operation exceeds allocation limit"))
	}

	dstPermit := length
	if checker, ok := dstR.(interface{ CheckWrite() (uint64, error) }); ok {
		p, err := checker.CheckWrite()
		if err != nil {
			return 0, h.toStreamError(err)
		}
		dstPermit = p
	}

	if dstPermit == 0 {
		return 0, nil
	}

	toRead := length
	if toRead > dstPermit {
		toRead = dstPermit
	}

	data, err := srcStream.Read(toRead)
	if err != nil {
		return 0, h.toStreamError(err)
	}
	if len(data) == 0 {
		return 0, nil
	}

	err = dstStream.Write(data)
	if err != nil {
		if errors.Is(err, preview2.ErrWritePermit) {
			panic(err)
		}
		return 0, h.toStreamError(err)
	}

	return uint64(len(data)), nil
}

func (h *StreamsHost) MethodOutputStreamBlockingSplice(ctx context.Context, self uint32, src uint32, length uint64) (uint64, *preview2.StreamError) {
	if length == 0 {
		return 0, nil
	}

	dstR, ok := h.resources.Get(self)
	if !ok || dstR.Type() != preview2.ResourceOutputStream {
		return 0, &preview2.StreamError{Closed: true}
	}

	srcR, ok := h.resources.Get(src)
	if !ok || srcR.Type() != preview2.ResourceInputStream {
		return 0, &preview2.StreamError{Closed: true}
	}

	var dstPoll, srcPoll preview2.Pollable
	if subscriber, ok := dstR.(interface{ Subscribe() preview2.Pollable }); ok {
		dstPoll = subscriber.Subscribe()
	} else if p, ok := dstR.(preview2.Pollable); ok {
		dstPoll = p
	}
	if subscriber, ok := srcR.(interface{ Subscribe() preview2.Pollable }); ok {
		srcPoll = subscriber.Subscribe()
	} else if p, ok := srcR.(preview2.Pollable); ok {
		srcPoll = p
	}

	for {
		if ctx.Err() != nil {
			return 0, h.toStreamError(ctx.Err())
		}

		for {
			if ctx.Err() != nil {
				return 0, h.toStreamError(ctx.Err())
			}
			dstReady := dstPoll == nil || dstPoll.Ready()
			srcReady := srcPoll == nil || srcPoll.Ready()
			if dstReady && srcReady {
				break
			}
			if !dstReady && !srcReady {
				var dstCh, srcCh <-chan struct{}
				if np, ok := dstPoll.(preview2.NotifyPollable); ok {
					dstCh = np.Notify()
				}
				if np, ok := srcPoll.(preview2.NotifyPollable); ok {
					srcCh = np.Notify()
				}
				if dstCh != nil && srcCh != nil {
					select {
					case <-ctx.Done():
						return 0, h.toStreamError(ctx.Err())
					case <-dstCh:
					case <-srcCh:
					}
					continue
				}
			}
			if !dstReady {
				dstPoll.Block(ctx)
				if ctx.Err() != nil {
					return 0, h.toStreamError(ctx.Err())
				}
			}
			if !srcReady {
				srcPoll.Block(ctx)
				if ctx.Err() != nil {
					return 0, h.toStreamError(ctx.Err())
				}
			}
		}

		n, se := h.MethodOutputStreamSplice(ctx, self, src, length)
		if se != nil {
			return n, se
		}
		if n > 0 || (dstPoll == nil && srcPoll == nil) {
			return n, nil
		}
	}
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
