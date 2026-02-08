// Package graph provides dependency resolution for WebAssembly Component Model.
//
// It builds a graph of what each module/instance provides and requires,
// enabling the linker to determine exactly which host functions need binding.
package graph

import (
	"github.com/wippyai/wasm-runtime/component"
)

// Graph represents the dependency graph of a component.
// It tracks what each instance provides (exports) and requires (imports).
// Thread-safe for reads after Build(). Designed to be cached in InstancePre.
type Graph struct {
	// providedByAdapter maps "namespace#funcname" to true if adapter provides it
	providedByAdapter map[string]bool

	// requiredByHost maps "namespace#funcname" to true if host must provide it
	requiredByHost map[string]bool

	// importNamespaces lists all import namespaces (e.g. "wasi:io/streams@0.2.0")
	importNamespaces []string
}

// New creates an empty graph
func New() *Graph {
	return &Graph{
		providedByAdapter: make(map[string]bool),
		requiredByHost:    make(map[string]bool),
	}
}

// Build constructs the dependency graph from a validated component.
// Build is the main entry point - call once and cache the result.
func Build(validated *component.ValidatedComponent) *Graph {
	if validated == nil || validated.Raw == nil {
		return New()
	}

	g := New()
	comp := validated.Raw

	// Phase 1: Collect all import namespaces
	g.collectImports(comp)

	// Phase 2: Find what adapter modules export (via CoreFuncIndexSpace)
	g.findAdapterExports(comp)

	// Phase 3: Determine what host must provide
	g.identifyHostRequirements(comp)

	return g
}

// collectImports gathers all import namespaces from the component
func (g *Graph) collectImports(comp *component.Component) {
	for _, imp := range comp.Imports {
		g.importNamespaces = append(g.importNamespaces, imp.Name)
	}
}

// findAdapterExports identifies functions exported by adapter modules.
// These are functions the adapter handles - host should NOT bind them.
func (g *Graph) findAdapterExports(comp *component.Component) {
	// CoreFuncIndexSpace tells us where each core function comes from.
	// Functions with Kind=CoreFuncAliasExport come from adapter modules.
	for _, entry := range comp.CoreFuncIndexSpace {
		if entry.Kind == component.CoreFuncAliasExport {
			// This function is provided by a core instance (adapter)
			// We need to find which namespace it belongs to by checking
			// which virtual instance exports it
			g.markAdapterProvides(comp, entry)
		}
	}
}

// markAdapterProvides marks a function as provided by the adapter.
// Currently a no-op: adapter-provided functions are tracked via CoreFuncIndexSpace
// during instantiation. Namespace mapping requires tracing the component's instance graph.
func (g *Graph) markAdapterProvides(comp *component.Component, entry component.CoreFuncEntry) {
}

// identifyHostRequirements determines which functions the host must provide.
// This is done by checking FuncIndexSpace for canon.lower entries that
// reference import aliases.
func (g *Graph) identifyHostRequirements(comp *component.Component) {
	// Check FuncIndexSpace for import aliases
	for _, entry := range comp.FuncIndexSpace {
		// entry.InstanceIdx points to the instance, entry.ExportName is the function
		if int(entry.InstanceIdx) < len(comp.Imports) {
			imp := comp.Imports[entry.InstanceIdx]
			key := imp.Name + "#" + entry.ExportName
			g.requiredByHost[key] = true
		}
	}

	// Check CoreFuncIndexSpace for canon.lower operations
	for _, entry := range comp.CoreFuncIndexSpace {
		if entry.Kind == component.CoreFuncCanonLower {
			// This is a lowered function - check if it comes from an import
			if int(entry.FuncIndex) < len(comp.FuncIndexSpace) {
				funcEntry := comp.FuncIndexSpace[entry.FuncIndex]
				if int(funcEntry.InstanceIdx) < len(comp.Imports) {
					imp := comp.Imports[funcEntry.InstanceIdx]
					key := imp.Name + "#" + funcEntry.ExportName
					// Only mark as required if NOT provided by adapter
					if !g.providedByAdapter[key] {
						g.requiredByHost[key] = true
					}
				}
			}
		}
	}
}

// RequiredHostFunctions returns the set of functions the host must provide.
// RequiredHostFunctions uses key format: "namespace#funcname"
func (g *Graph) RequiredHostFunctions() map[string]bool {
	result := make(map[string]bool, len(g.requiredByHost))
	for k, v := range g.requiredByHost {
		result[k] = v
	}
	return result
}

// ProvidedByAdapter returns the set of functions the adapter provides.
// ProvidedByAdapter uses key format: "namespace#funcname"
func (g *Graph) ProvidedByAdapter() map[string]bool {
	result := make(map[string]bool, len(g.providedByAdapter))
	for k, v := range g.providedByAdapter {
		result[k] = v
	}
	return result
}

// IsProvidedByAdapter checks if a specific function is handled by the adapter
func (g *Graph) IsProvidedByAdapter(namespace, funcName string) bool {
	key := namespace + "#" + funcName
	return g.providedByAdapter[key]
}

// IsRequiredFromHost checks if a specific function must come from host
func (g *Graph) IsRequiredFromHost(namespace, funcName string) bool {
	key := namespace + "#" + funcName
	return g.requiredByHost[key]
}

// ImportNamespaces returns all import namespace paths
func (g *Graph) ImportNamespaces() []string {
	result := make([]string, len(g.importNamespaces))
	copy(result, g.importNamespaces)
	return result
}
