package preview2

import (
	"errors"
	"sync"
	"testing"
)

func TestSocketLeaseAdoptionOwnsRelease(t *testing.T) {
	budget := NewSocketBudget(1)
	table := NewResourceTableWithBudget(4, budget)
	defer table.Close()
	lease, err := budget.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	handle, err := table.TryAddWithSocketLease(NewTCPSocketResource(0), lease)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release() // Stale producer reference must not release the live socket.
	if budget.Used() != 1 {
		t.Fatal("stale lease released adopted reservation")
	}
	table.Remove(handle)
	if budget.Used() != 0 {
		t.Fatal("table did not release adopted reservation")
	}
}

func TestSocketLeaseConcurrentAdoptAndRelease(t *testing.T) {
	for i := 0; i < 200; i++ {
		budget := NewSocketBudget(1)
		table := NewResourceTableWithBudget(4, budget)
		lease, err := budget.Acquire()
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); <-start; lease.Release() }()
		close(start)
		child := NewTCPSocketResource(0)
		handle, err := table.TryAddWithSocketLease(child, lease)
		wg.Wait()
		if err == nil {
			if budget.Used() != 1 {
				t.Fatal("adopted released lease")
			}
			table.Remove(handle)
		} else {
			if !errors.Is(err, ErrInvalidLease) {
				t.Fatal(err)
			}
			child.Drop()
		}
		table.Close()
		if budget.Used() != 0 {
			t.Fatal("unbalanced socket lease")
		}
	}
}
