package component

import (
	"fmt"
	"sort"
)

// InstanceGraph tracks dependencies between core instances
type InstanceGraph struct {
	Edges     map[int][]int
	Instances []*ParsedCoreInstance
}

// NewInstanceGraph builds a dependency graph
func NewInstanceGraph(instances []CoreInstance) *InstanceGraph {
	return NewInstanceGraphWithComponent(instances, nil)
}

// NewInstanceGraphWithComponent builds a dependency graph including FromExports
// dependencies derived from core index space mappings.
func NewInstanceGraphWithComponent(instances []CoreInstance, comp *Component) *InstanceGraph {
	g := &InstanceGraph{
		Instances: make([]*ParsedCoreInstance, len(instances)),
		Edges:     make(map[int][]int),
	}

	for i, ci := range instances {
		g.Instances[i] = ci.Parsed
	}

	var coreIndexToInstance map[byte]map[uint32]int
	if comp != nil {
		coreIndexToInstance = buildCoreIndexMappings(comp)
	}

	for i, inst := range g.Instances {
		if inst == nil {
			continue
		}
		switch inst.Kind {
		case CoreInstanceInstantiate:
			for _, arg := range inst.Args {
				depIdx := int(arg.InstanceIndex)
				if depIdx < len(g.Instances) {
					g.Edges[depIdx] = append(g.Edges[depIdx], i)
				}
			}
		case CoreInstanceFromExports:
			if coreIndexToInstance != nil {
				deps := make(map[int]bool)
				for _, exp := range inst.Exports {
					if kindMap, ok := coreIndexToInstance[exp.Kind]; ok {
						if depIdx, ok := kindMap[exp.Index]; ok {
							deps[depIdx] = true
						}
					}
				}
				for depIdx := range deps {
					if depIdx < len(g.Instances) {
						g.Edges[depIdx] = append(g.Edges[depIdx], i)
					}
				}
			}
		}
	}

	return g
}

// buildCoreIndexMappings maps core indices to their source instances
func buildCoreIndexMappings(comp *Component) map[byte]map[uint32]int {
	result := make(map[byte]map[uint32]int)

	// Process memory, table, global aliases (not func - uses CoreFuncIndexSpace)
	for _, alias := range comp.Aliases {
		if alias.Parsed == nil {
			continue
		}
		if alias.Parsed.Sort == 0x00 {
			kind := alias.Parsed.CoreSort
			if kind == CoreExportFunc {
				continue // func has interleaved canon entries
			}
			if result[kind] == nil {
				result[kind] = make(map[uint32]int)
			}
			idx := uint32(len(result[kind]))
			result[kind][idx] = int(alias.Parsed.Instance)
		}
	}

	if len(comp.CoreFuncIndexSpace) > 0 {
		result[CoreExportFunc] = make(map[uint32]int)
		for i, entry := range comp.CoreFuncIndexSpace {
			if entry.Kind == CoreFuncAliasExport {
				result[CoreExportFunc][uint32(i)] = entry.InstanceIdx
			}
		}
	}

	return result
}

// TopologicalSort returns instances in dependency order (dependencies first).
// Returns error if cycle detected.
func (g *InstanceGraph) TopologicalSort() ([]int, error) {
	n := len(g.Instances)
	inDegree := make([]int, n)

	for _, dependents := range g.Edges {
		for _, depIdx := range dependents {
			if depIdx < n {
				inDegree[depIdx]++
			}
		}
	}

	var queue []int
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	var result []int
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, neighbor := range g.Edges[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != n {
		return nil, fmt.Errorf("cycle detected in instance graph")
	}

	return result, nil
}

// ModuleInstantiations returns module instantiations in topological order.
// Returns nil on cycle. Use ModuleInstantiationsWithError for error details.
func (g *InstanceGraph) ModuleInstantiations() []ModuleInstantiation {
	result, _ := g.ModuleInstantiationsWithError()
	return result
}

// ModuleInstantiationsWithError returns module instantiations or a cycle error
func (g *InstanceGraph) ModuleInstantiationsWithError() ([]ModuleInstantiation, error) {
	order, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	var result []ModuleInstantiation
	for _, idx := range order {
		inst := g.Instances[idx]
		if inst != nil && inst.Kind == CoreInstanceInstantiate {
			result = append(result, ModuleInstantiation{
				InstanceIndex: idx,
				ModuleIndex:   int(inst.ModuleIndex),
				Args:          inst.Args,
			})
		}
	}

	return result, nil
}

// ModuleInstantiation describes a module instantiation
type ModuleInstantiation struct {
	Args          []CoreInstanceArg
	InstanceIndex int
	ModuleIndex   int
}

// InstanceDeps returns direct dependencies of an Instantiate instance
func (g *InstanceGraph) InstanceDeps(idx int) []int {
	if idx >= len(g.Instances) || g.Instances[idx] == nil {
		return nil
	}

	inst := g.Instances[idx]
	if inst.Kind != CoreInstanceInstantiate {
		return nil
	}

	deps := make([]int, 0, len(inst.Args))
	for _, arg := range inst.Args {
		deps = append(deps, int(arg.InstanceIndex))
	}
	return deps
}

// ExportsOf returns exports of a FromExports instance
func (g *InstanceGraph) ExportsOf(idx int) []CoreInstanceExport {
	if idx >= len(g.Instances) || g.Instances[idx] == nil {
		return nil
	}

	inst := g.Instances[idx]
	if inst.Kind != CoreInstanceFromExports {
		return nil
	}

	return inst.Exports
}

// FindInstanceByExport returns the instance index exporting the name, or -1
func (g *InstanceGraph) FindInstanceByExport(name string) int {
	for i, inst := range g.Instances {
		if inst == nil || inst.Kind != CoreInstanceFromExports {
			continue
		}
		for _, exp := range inst.Exports {
			if exp.Name == name {
				return i
			}
		}
	}
	return -1
}

// InstantiationLayers groups instances by dependency level for parallel instantiation.
// Returns nil on cycle.
func (g *InstanceGraph) InstantiationLayers() [][]int {
	result, _ := g.InstantiationLayersWithError()
	return result
}

// InstantiationLayersWithError groups instances by dependency level or returns cycle error
func (g *InstanceGraph) InstantiationLayersWithError() ([][]int, error) {
	order, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	layerOf := make(map[int]int)

	for _, idx := range order {
		inst := g.Instances[idx]
		if inst == nil {
			layerOf[idx] = 0
			continue
		}

		maxDepLayer := -1
		if inst.Kind == CoreInstanceInstantiate {
			for _, arg := range inst.Args {
				depLayer := layerOf[int(arg.InstanceIndex)]
				if depLayer > maxDepLayer {
					maxDepLayer = depLayer
				}
			}
		}
		layerOf[idx] = maxDepLayer + 1
	}

	maxLayer := 0
	for _, layer := range layerOf {
		if layer > maxLayer {
			maxLayer = layer
		}
	}

	layers := make([][]int, maxLayer+1)
	for idx, layer := range layerOf {
		layers[layer] = append(layers[layer], idx)
	}

	for i := range layers {
		sort.Ints(layers[i])
	}

	return layers, nil
}

// String returns a debug representation
func (g *InstanceGraph) String() string {
	var s string
	for i, inst := range g.Instances {
		if inst == nil {
			s += fmt.Sprintf("[%d] nil\n", i)
			continue
		}
		switch inst.Kind {
		case CoreInstanceInstantiate:
			s += fmt.Sprintf("[%d] instantiate module %d (deps: %v)\n",
				i, inst.ModuleIndex, g.InstanceDeps(i))
		case CoreInstanceFromExports:
			s += fmt.Sprintf("[%d] from-exports (%d exports)\n",
				i, len(inst.Exports))
		}
	}
	return s
}
