package preview2

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type countedResource struct {
	kind  ResourceType
	drops atomic.Int32
}

func (r *countedResource) Type() ResourceType { return r.kind }
func (r *countedResource) Drop()              { r.drops.Add(1) }

func TestResourceBudgetAndDropOwnership(t *testing.T) {
	table := NewResourceTableWithLimits(3, 1)
	socket := &countedResource{kind: ResourceTCPSocket}
	h := table.Add(socket)
	refused := &countedResource{kind: ResourceUDPSocket}
	if _, err := table.TryAdd(refused); !errors.Is(err, ErrSocketLimit) {
		t.Fatalf("socket limit: %v", err)
	}
	if refused.drops.Load() != 0 {
		t.Fatal("TryAdd consumed rejected resource")
	}
	table.Remove(h)
	table.Remove(h)
	if socket.drops.Load() != 1 {
		t.Fatalf("socket dropped %d times", socket.drops.Load())
	}
	table.Add(refused)
	a, b := &countedResource{}, &countedResource{}
	table.Add(a)
	table.Add(b)
	if _, err := table.TryAdd(&countedResource{}); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("handle limit: %v", err)
	}
	table.Clear()
	table.Clear()
	for _, r := range []*countedResource{refused, a, b} {
		if r.drops.Load() != 1 {
			t.Fatalf("resource dropped %d times", r.drops.Load())
		}
	}
	table.Add(&countedResource{kind: ResourceTCPSocket})
	table.Clear()
}

func TestResourceBudgetConcurrentAdmission(t *testing.T) {
	table := NewResourceTableWithLimits(8, 4)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			r := &countedResource{kind: ResourceTCPSocket}
			if _, err := table.TryAdd(r); err == nil {
				accepted.Add(1)
			} else {
				r.Drop()
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 4 {
		t.Fatalf("accepted %d sockets", accepted.Load())
	}
	table.Clear()
}

func TestResourceAddDropsRejectedResource(t *testing.T) {
	table := NewResourceTableWithLimits(1, 1)
	table.Add(&countedResource{})
	defer table.Clear()
	r := &countedResource{}
	defer func() {
		if !errors.Is(asError(recover()), ErrResourceLimit) {
			t.Error("expected limit trap")
		}
		if r.drops.Load() != 1 {
			t.Error("rejected Add leaked resource")
		}
	}()
	table.Add(r)
}
func asError(v any) error { e, _ := v.(error); return e }
