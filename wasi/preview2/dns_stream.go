package preview2

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
)

const (
	// MaxResolveAddresses is the maximum number of IP addresses retained in a resolve snapshot.
	MaxResolveAddresses = 64
	// MaxResolveAddressBytes is the maximum sum of bytes of all IP address strings in a resolve snapshot.
	MaxResolveAddressBytes = 4096
)

// ErrResolveLimit is stored when a resolve snapshot exceeds MaxResolveAddresses or MaxResolveAddressBytes.
var ErrResolveLimit = errors.New("resolve address limit exceeded")

var (
	errNilResolveOperation    = errors.New("nil pending resolve operation")
	errNilResolveResult       = errors.New("nil DNS result from pending resolve operation")
	errMalformedResolveResult = errors.New("pending resolve operation did not produce DNS addresses")
)

// ResolveAddressStreamResource iterates over DNS resolution results.
//
// A pending stream retains its TCPNetworkOperation for the full stream lifetime,
// including after a successful or failed Take. Drop always cancels and joins that
// operation before clearing result and operation references.
type ResolveAddressStreamResource struct {
	err          error
	op           TCPNetworkOperation
	transferDone chan struct{}
	notifyCh     chan struct{}
	addresses    []string
	offset       int
	dropOnce     sync.Once
	mu           sync.Mutex
	dropped      bool
	taken        bool
	transferring bool
}

var _ Resource = (*ResolveAddressStreamResource)(nil)

// NewResolveAddressStreamResource snapshots addresses into a bounded ready stream.
// Oversized input stores ErrResolveLimit and retains no addresses.
func NewResolveAddressStreamResource(addresses []string) *ResolveAddressStreamResource {
	addrs, err := snapshotResolveAddresses(addresses)
	return &ResolveAddressStreamResource{
		addresses: addrs,
		err:       err,
		taken:     true,
	}
}

// NewPendingResolveAddressStreamResource owns op until Drop. A nil op is stored as an error.
func NewPendingResolveAddressStreamResource(op TCPNetworkOperation) *ResolveAddressStreamResource {
	if valueIsNil(op) {
		return &ResolveAddressStreamResource{
			err:   errNilResolveOperation,
			taken: true,
		}
	}
	return &ResolveAddressStreamResource{op: op}
}

func (r *ResolveAddressStreamResource) Type() ResourceType { return ResourceIPNameLookup }

// Drop cancels and joins the pending operation, then clears retained results.
// Concurrent callers wait for that join to finish.
func (r *ResolveAddressStreamResource) Drop() {
	r.dropOnce.Do(r.dropJoinClear)
}

func (r *ResolveAddressStreamResource) dropJoinClear() {
	r.mu.Lock()
	r.dropped = true
	r.wakeLocked()
	op := r.op
	transferDone := r.transferDone
	r.mu.Unlock()

	if op != nil {
		_ = op.Close()
	}
	if transferDone != nil {
		<-transferDone
	}

	r.mu.Lock()
	r.addresses = nil
	r.offset = 0
	r.op = nil
	r.mu.Unlock()
}

// ReadNext returns the next address or nil. Pending and error states yield nil.
func (r *ResolveAddressStreamResource) ReadNext() *string {
	addr, _, _ := r.Next()
	return addr
}

// Next returns the next address. ready is false while the pending operation is unresolved.
// Exhausted streams return nil, nil, true. Errors persist across calls.
func (r *ResolveAddressStreamResource) Next() (address *string, err error, ready bool) { //nolint:revive // Readiness-last matches resolve-next-address would-block.
	r.mu.Lock()
	if addr, ready, ok, nextErr := r.consumeLocked(); ok {
		r.mu.Unlock()
		return addr, nextErr, ready
	}
	op := r.op
	r.beginTransferLocked()
	r.mu.Unlock()

	closer, takeErr, takeReady := op.Take()

	r.mu.Lock()
	addr, ready, dispose, nextErr := r.finishTakeLocked(closer, takeErr, takeReady)
	r.mu.Unlock()

	defer r.finishTransfer()
	closeCloser(dispose)
	return addr, nextErr, ready
}

func (r *ResolveAddressStreamResource) consumeLocked() (*string, bool, bool, error) {
	if r.err != nil {
		return nil, true, true, r.err
	}
	if r.offset < len(r.addresses) {
		addr := r.addresses[r.offset]
		r.offset++
		return &addr, true, true, nil
	}
	if r.dropped {
		return nil, true, true, nil
	}
	if r.transferring {
		return nil, false, true, nil
	}
	if r.taken || r.op == nil {
		return nil, true, true, nil
	}
	if !r.op.Ready() {
		return nil, false, true, nil
	}
	return nil, false, false, nil
}

func (r *ResolveAddressStreamResource) finishTakeLocked(closer io.Closer, takeErr error, takeReady bool) (*string, bool, io.Closer, error) {
	if !takeReady {
		if r.dropped {
			r.taken = true
			return nil, true, closer, nil
		}
		r.taken = false
		return nil, false, closer, nil
	}
	r.taken = true
	if takeErr != nil {
		r.err = takeErr
		r.wakeLocked()
		return nil, true, closer, takeErr
	}
	addrs, snapErr := snapshotFromCloser(closer)
	if snapErr != nil {
		r.err = snapErr
		r.wakeLocked()
		return nil, true, closer, snapErr
	}
	if r.dropped {
		return nil, true, closer, nil
	}
	r.addresses = addrs
	r.wakeLocked()
	addr, ready, _, err := r.consumeLocked()
	return addr, ready, closer, err
}

func (r *ResolveAddressStreamResource) beginTransferLocked() {
	r.transferring = true
	if r.transferDone == nil {
		r.transferDone = make(chan struct{})
	}
}

func (r *ResolveAddressStreamResource) finishTransfer() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.transferring {
		return
	}
	r.transferring = false
	if r.transferDone != nil {
		close(r.transferDone)
		r.transferDone = nil
	}
}

func (r *ResolveAddressStreamResource) wakeLocked() {
	if r.notifyCh != nil {
		close(r.notifyCh)
		r.notifyCh = nil
	}
}

func (r *ResolveAddressStreamResource) readyLocked() bool {
	if r.dropped || r.err != nil || r.offset < len(r.addresses) || r.taken || r.op == nil {
		return true
	}
	return r.op.Ready()
}

// Pollable returns a borrowed pollable. Its Drop does not close the stream.
func (r *ResolveAddressStreamResource) Pollable() Pollable {
	return (*resolveAddressStreamPollable)(r)
}

type resolveAddressStreamPollable ResolveAddressStreamResource

var (
	_ Pollable       = (*resolveAddressStreamPollable)(nil)
	_ NotifyPollable = (*resolveAddressStreamPollable)(nil)
)

func (*resolveAddressStreamPollable) Type() ResourceType { return ResourcePollable }
func (*resolveAddressStreamPollable) Drop()              {}

func (p *resolveAddressStreamPollable) Ready() bool {
	r := (*ResolveAddressStreamResource)(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readyLocked()
}

func (p *resolveAddressStreamPollable) Notify() <-chan struct{} {
	r := (*ResolveAddressStreamResource)(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readyLocked() {
		return closedPollableChan
	}
	if r.op != nil {
		return r.op.Notify()
	}
	if r.notifyCh == nil {
		r.notifyCh = make(chan struct{})
	}
	return r.notifyCh
}

func (p *resolveAddressStreamPollable) Block(ctx context.Context) {
	for !p.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}

func snapshotFromCloser(closer io.Closer) ([]string, error) {
	if valueIsNil(closer) {
		return nil, errNilResolveResult
	}
	provider, ok := closer.(interface{ DNSAddresses() []string })
	if !ok || valueIsNil(provider) {
		if !ok {
			return nil, errMalformedResolveResult
		}
		return nil, errNilResolveResult
	}
	return snapshotResolveAddresses(provider.DNSAddresses())
}

func snapshotResolveAddresses(addresses []string) ([]string, error) {
	if len(addresses) > MaxResolveAddresses {
		return nil, ErrResolveLimit
	}
	total := 0
	for _, addr := range addresses {
		if len(addr) > MaxResolveAddressBytes-total {
			return nil, ErrResolveLimit
		}
		total += len(addr)
	}
	if len(addresses) == 0 {
		return nil, nil
	}
	out := make([]string, len(addresses))
	for i, addr := range addresses {
		out[i] = strings.Clone(addr)
	}
	return out, nil
}

func closeCloser(closer io.Closer) {
	if !valueIsNil(closer) {
		_ = closer.Close()
	}
}

func valueIsNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}
