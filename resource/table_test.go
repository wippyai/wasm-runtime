package resource

import (
	"testing"
)

type testObserver struct {
	events []Event
}

func (o *testObserver) OnResourceEvent(e Event) {
	o.events = append(o.events, e)
}

func TestUnifiedTable_Basic(t *testing.T) {
	table := NewTable()

	// Insert
	h := table.Insert(1, "test")
	if h == 0 {
		t.Fatal("Expected non-zero handle")
	}

	// Get
	val, ok := table.Get(h)
	if !ok {
		t.Fatal("Get failed")
	}
	if val != "test" {
		t.Fatalf("Expected 'test', got %v", val)
	}

	// GetTyped with correct type
	_, ok = table.GetTyped(h, 1)
	if !ok {
		t.Fatal("GetTyped with correct type failed")
	}

	// GetTyped with wrong type
	_, ok = table.GetTyped(h, 2)
	if ok {
		t.Fatal("GetTyped with wrong type should fail")
	}

	// Remove
	val, ok = table.Remove(h)
	if !ok {
		t.Fatal("Remove failed")
	}
	if val != "test" {
		t.Fatalf("Expected 'test', got %v", val)
	}

	// Len should be 0
	if table.Len() != 0 {
		t.Fatal("Expected Len() == 0 after Remove")
	}
}

func TestUnifiedTable_Observer(t *testing.T) {
	table := NewTable()
	obs := &testObserver{}
	table.Subscribe(obs)

	// Insert should trigger EventCreated
	h := table.Insert(1, "test")
	if len(obs.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(obs.events))
	}
	if obs.events[0].Type != EventCreated {
		t.Fatal("Expected EventCreated")
	}
	if obs.events[0].Handle != h {
		t.Fatal("Wrong handle in event")
	}

	// Remove should trigger EventDropped
	table.Remove(h)
	if len(obs.events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(obs.events))
	}
	if obs.events[1].Type != EventDropped {
		t.Fatal("Expected EventDropped")
	}

	// Unsubscribe
	table.Unsubscribe(obs)
	table.Insert(1, "test2")
	if len(obs.events) != 2 {
		t.Fatal("Should not receive events after Unsubscribe")
	}
}

func TestUnifiedTable_Clear(t *testing.T) {
	table := NewTable()

	table.Insert(1, "a")
	table.Insert(1, "b")
	table.Insert(1, "c")

	if table.Len() != 3 {
		t.Fatal("Expected Len() == 3")
	}

	table.Clear()

	if table.Len() != 0 {
		t.Fatal("Expected Len() == 0 after Clear")
	}
}

func TestUnifiedTable_Close(t *testing.T) {
	table := NewTable()

	table.Insert(1, "a")
	table.Insert(1, "b")

	if err := table.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Insert should fail after Close
	h := table.Insert(1, "c")
	if h != 0 {
		t.Fatal("Expected Insert to fail after Close")
	}
}

func TestUnifiedTable_Backend(t *testing.T) {
	table := NewTable()
	backend := table.Backend()

	if backend == nil {
		t.Fatal("Backend() returned nil")
	}

	// Use backend directly for ABI operations
	h := backend.NewFromRep(1, 12345)
	rep, ok := backend.Rep(h)
	if !ok {
		t.Fatal("Rep failed")
	}
	if rep != 12345 {
		t.Fatalf("Expected rep 12345, got %d", rep)
	}
}

type dropCounter struct {
	count int
}

func (d *dropCounter) Drop() {
	d.count++
}

func TestUnifiedTable_DropperInterface(t *testing.T) {
	table := NewTable()
	d := &dropCounter{}

	h := table.Insert(1, d)
	table.Remove(h)

	if d.count != 1 {
		t.Fatalf("Expected Drop() to be called once, called %d times", d.count)
	}
}
