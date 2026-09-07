package budget

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
)

func TestIndependentOwnersAndRollback(t *testing.T) {
	b := New(100)
	guest, e := b.Reserve(60)
	if e != nil {
		t.Fatal(e)
	}
	host, e := b.Reserve(30)
	if e != nil {
		t.Fatal(e)
	}
	if e = guest.Grow(11); !errors.Is(e, ErrLimit) || b.Usage().Used != 90 {
		t.Fatal("denial changed accounting")
	}
	if e = guest.Grow(10); e != nil {
		t.Fatal(e)
	}
	if e = guest.Shrink(10); e != nil {
		t.Fatal(e)
	}
	if e = host.Shrink(31); e == nil || b.Usage().Used != 90 {
		t.Fatal("over-release changed accounting")
	}
	guest.Release()
	guest.Release()
	if b.Usage().Used != 30 {
		t.Fatal("guest released host ownership")
	}
	if e = guest.Grow(1); !errors.Is(e, ErrReleased) {
		t.Fatal("released owner reacquired capacity")
	}
	host.Release()
	if b.Usage().Used != 0 || b.Usage().Peak != 100 {
		t.Fatal("incorrect terminal accounting")
	}
}
func TestConcurrentAdmissionCannotOverbook(t *testing.T) {
	b := New(7)
	start, release := make(chan struct{}), make(chan struct{})
	var admitted atomic.Int32
	var ready, done sync.WaitGroup
	ready.Add(32)
	done.Add(32)
	for range 32 {
		go func() {
			defer done.Done()
			<-start
			r, e := b.Reserve(1)
			if e == nil {
				admitted.Add(1)
			}
			ready.Done()
			<-release
			if r != nil {
				r.Release()
			}
		}()
	}
	close(start)
	ready.Wait()
	if admitted.Load() != 7 || b.Usage().Used != 7 {
		t.Errorf("admitted=%d usage=%+v", admitted.Load(), b.Usage())
	}
	close(release)
	done.Wait()
	if b.Usage().Used != 0 {
		t.Fatal("charges retained")
	}
}
func TestOverflowAndZero(t *testing.T) {
	b := New(math.MaxUint64)
	r, e := b.Reserve(math.MaxUint64)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Grow(1); !errors.Is(e, ErrLimit) {
		t.Fatal("overflow accepted")
	}
	r.Release()
	if _, e = New(0).Reserve(1); !errors.Is(e, ErrLimit) {
		t.Fatal("zero treated as unlimited")
	}
}
