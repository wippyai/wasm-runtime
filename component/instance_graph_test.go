package component

import (
	"testing"
)

func TestNewInstanceGraph_Empty(t *testing.T) {
	g := NewInstanceGraph(nil)
	if g == nil {
		t.Fatal("NewInstanceGraph returned nil")
	}
	if len(g.Instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(g.Instances))
	}
}

func TestNewInstanceGraph_FromExports(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports, Exports: []CoreInstanceExport{{Name: "func1"}}}},
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports, Exports: []CoreInstanceExport{{Name: "func2"}}}},
	}

	g := NewInstanceGraph(instances)

	if len(g.Instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(g.Instances))
	}

	// FromExports instances have no dependencies
	if len(g.Edges) != 0 {
		t.Error("fromexports instances should have no edges")
	}
}

func TestNewInstanceGraph_WithDeps(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports}},
		{Parsed: &ParsedCoreInstance{
			Kind:        CoreInstanceInstantiate,
			ModuleIndex: 0,
			Args:        []CoreInstanceArg{{InstanceIndex: 0}},
		}},
	}

	g := NewInstanceGraph(instances)

	// Instance 1 depends on instance 0
	deps := g.Edges[0]
	if len(deps) != 1 || deps[0] != 1 {
		t.Errorf("expected edge 0->1, got %v", g.Edges)
	}
}

func TestTopologicalSort_Empty(t *testing.T) {
	g := NewInstanceGraph(nil)
	order, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("expected empty order, got %v", order)
	}
}

func TestTopologicalSort_Simple(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports}},
		{Parsed: &ParsedCoreInstance{
			Kind: CoreInstanceInstantiate,
			Args: []CoreInstanceArg{{InstanceIndex: 0}},
		}},
		{Parsed: &ParsedCoreInstance{
			Kind: CoreInstanceInstantiate,
			Args: []CoreInstanceArg{{InstanceIndex: 1}},
		}},
	}

	g := NewInstanceGraph(instances)
	order, err := g.TopologicalSort()

	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(order))
	}

	// Instance 0 must come before 1, and 1 before 2
	indexOf := make(map[int]int)
	for i, idx := range order {
		indexOf[idx] = i
	}

	if indexOf[0] >= indexOf[1] || indexOf[1] >= indexOf[2] {
		t.Errorf("invalid order: %v", order)
	}
}

func TestModuleInstantiations(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports}},
		{Parsed: &ParsedCoreInstance{
			Kind:        CoreInstanceInstantiate,
			ModuleIndex: 5,
			Args:        []CoreInstanceArg{{InstanceIndex: 0}},
		}},
	}

	g := NewInstanceGraph(instances)
	insts := g.ModuleInstantiations()

	// Only instance 1 is an instantiation
	if len(insts) != 1 {
		t.Fatalf("expected 1 instantiation, got %d", len(insts))
	}
	if insts[0].InstanceIndex != 1 {
		t.Errorf("expected instance index 1, got %d", insts[0].InstanceIndex)
	}
	if insts[0].ModuleIndex != 5 {
		t.Errorf("expected module index 5, got %d", insts[0].ModuleIndex)
	}
}

func TestInstanceDeps(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports}},
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports}},
		{Parsed: &ParsedCoreInstance{
			Kind: CoreInstanceInstantiate,
			Args: []CoreInstanceArg{
				{InstanceIndex: 0},
				{InstanceIndex: 1},
			},
		}},
	}

	g := NewInstanceGraph(instances)

	// Instance 2 depends on 0 and 1
	deps := g.InstanceDeps(2)
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}

	// Instance 0 has no deps
	deps = g.InstanceDeps(0)
	if len(deps) != 0 {
		t.Errorf("expected 0 deps for fromexports, got %d", len(deps))
	}

	// Out of range
	deps = g.InstanceDeps(100)
	if deps != nil {
		t.Error("expected nil for out of range")
	}
}

func TestExportsOf(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{
			Kind: CoreInstanceFromExports,
			Exports: []CoreInstanceExport{
				{Name: "func1", Kind: 0, Index: 0},
				{Name: "func2", Kind: 0, Index: 1},
			},
		}},
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceInstantiate}},
	}

	g := NewInstanceGraph(instances)

	exports := g.ExportsOf(0)
	if len(exports) != 2 {
		t.Fatalf("expected 2 exports, got %d", len(exports))
	}
	if exports[0].Name != "func1" || exports[1].Name != "func2" {
		t.Error("wrong export names")
	}

	// Instantiate instance has no exports
	exports = g.ExportsOf(1)
	if exports != nil {
		t.Error("expected nil for instantiate instance")
	}

	// Out of range
	exports = g.ExportsOf(100)
	if exports != nil {
		t.Error("expected nil for out of range")
	}
}

func TestFindInstanceByExport(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{
			Kind:    CoreInstanceFromExports,
			Exports: []CoreInstanceExport{{Name: "memory"}},
		}},
		{Parsed: &ParsedCoreInstance{
			Kind:    CoreInstanceFromExports,
			Exports: []CoreInstanceExport{{Name: "func1"}},
		}},
	}

	g := NewInstanceGraph(instances)

	idx := g.FindInstanceByExport("memory")
	if idx != 0 {
		t.Errorf("expected 0, got %d", idx)
	}

	idx = g.FindInstanceByExport("func1")
	if idx != 1 {
		t.Errorf("expected 1, got %d", idx)
	}

	idx = g.FindInstanceByExport("nonexistent")
	if idx != -1 {
		t.Errorf("expected -1, got %d", idx)
	}
}

func TestInstantiationLayers(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports}}, // layer 0
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports}}, // layer 0
		{Parsed: &ParsedCoreInstance{ // layer 1 (depends on 0)
			Kind: CoreInstanceInstantiate,
			Args: []CoreInstanceArg{{InstanceIndex: 0}},
		}},
		{Parsed: &ParsedCoreInstance{ // layer 1 (depends on 1)
			Kind: CoreInstanceInstantiate,
			Args: []CoreInstanceArg{{InstanceIndex: 1}},
		}},
		{Parsed: &ParsedCoreInstance{ // layer 2 (depends on 2 and 3)
			Kind: CoreInstanceInstantiate,
			Args: []CoreInstanceArg{{InstanceIndex: 2}, {InstanceIndex: 3}},
		}},
	}

	g := NewInstanceGraph(instances)
	layers := g.InstantiationLayers()

	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}

	// Layer 0: instances 0, 1
	if len(layers[0]) != 2 {
		t.Errorf("layer 0: expected 2, got %d", len(layers[0]))
	}

	// Layer 1: instances 2, 3
	if len(layers[1]) != 2 {
		t.Errorf("layer 1: expected 2, got %d", len(layers[1]))
	}

	// Layer 2: instance 4
	if len(layers[2]) != 1 || layers[2][0] != 4 {
		t.Errorf("layer 2: expected [4], got %v", layers[2])
	}
}

func TestInstanceGraph_String(t *testing.T) {
	instances := []CoreInstance{
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceFromExports, Exports: []CoreInstanceExport{{Name: "a"}}}},
		{Parsed: &ParsedCoreInstance{Kind: CoreInstanceInstantiate, ModuleIndex: 0}},
		{Parsed: nil},
	}

	g := NewInstanceGraph(instances)
	s := g.String()

	if len(s) == 0 {
		t.Error("expected non-empty string")
	}
}
