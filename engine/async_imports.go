package engine

import (
	"fmt"
	"sort"

	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/linker"
	"github.com/wippyai/wasm-runtime/wasm"
)

// coreModuleMeta contains cached parsed metadata for a core module.
type coreModuleMeta struct {
	mod         *wasm.Module
	funcExports map[string]uint32
	callees     map[uint32][]uint32
	hasIndirect map[uint32]bool
	imports     []wasm.Import
	numImported int
}

// parseCoreModuleMeta parses a core WebAssembly binary and extracts imports, exports,
// and intra-module call relationships.
func parseCoreModuleMeta(i int, modBytes []byte) (*coreModuleMeta, error) {
	mod, err := wasm.ParseModule(modBytes)
	if err != nil {
		return nil, fmt.Errorf("parse core module %d: %w", i, err)
	}

	if len(mod.Funcs) != len(mod.Code) {
		return nil, fmt.Errorf("core module %d function/body count mismatch: %d/%d", i, len(mod.Funcs), len(mod.Code))
	}
	totalFuncs := uint64(mod.NumImportedFuncs()) + uint64(len(mod.Funcs))

	meta := &coreModuleMeta{
		mod:         mod,
		numImported: mod.NumImportedFuncs(),
		funcExports: make(map[string]uint32),
		callees:     make(map[uint32][]uint32),
		hasIndirect: make(map[uint32]bool),
	}

	for _, imp := range mod.Imports {
		if imp.Desc.Kind == wasm.KindFunc {
			meta.imports = append(meta.imports, imp)
		}
	}

	for _, exp := range mod.Exports {
		if exp.Kind == wasm.KindFunc {
			if uint64(exp.Idx) >= totalFuncs {
				return nil, fmt.Errorf("core module %d export %q function index %d out of bounds", i, exp.Name, exp.Idx)
			}
			meta.funcExports[exp.Name] = exp.Idx
		}
	}

	for codeIdx, body := range mod.Code {
		callerIdx := uint32(meta.numImported + codeIdx)
		instrs, err := wasm.DecodeInstructions(body.Code)
		if err != nil {
			return nil, fmt.Errorf("decode instructions in core module %d func %d: %w", i, callerIdx, err)
		}
		calleeSeen := make(map[uint32]bool)
		for _, instr := range instrs {
			switch instr.Opcode {
			case wasm.OpCall, wasm.OpReturnCall:
				if imm, ok := instr.Imm.(wasm.CallImm); ok {
					if uint64(imm.FuncIdx) >= totalFuncs {
						return nil, fmt.Errorf("core module %d func %d call index %d out of bounds", i, callerIdx, imm.FuncIdx)
					}
					if !calleeSeen[imm.FuncIdx] {
						calleeSeen[imm.FuncIdx] = true
						meta.callees[callerIdx] = append(meta.callees[callerIdx], imm.FuncIdx)
					}
				}
			case wasm.OpCallIndirect, wasm.OpReturnCallIndirect, wasm.OpCallRef, wasm.OpReturnCallRef:
				meta.hasIndirect[callerIdx] = true
			}
		}
	}

	return meta, nil
}

// deriveAsyncifyImports computes the sound set of import signatures requiring asyncify transformation.
// Returns an error if any module parsing or bytecode decoding fails, enabling fail-closed instantiation.
func (m *WazeroModule) deriveAsyncifyImports() ([]string, error) {
	m.hostFuncsMu.RLock()
	defer m.hostFuncsMu.RUnlock()

	var asyncHostFuncs []HostFunc
	for _, hf := range m.hostFuncs {
		if hf.IsAsync {
			asyncHostFuncs = append(asyncHostFuncs, hf)
		}
	}

	if len(asyncHostFuncs) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var imports []string

	add := func(module, name string) {
		if module == "" && name == "" {
			return
		}
		for _, v := range funcNameVariants(name) {
			pattern := v
			if module != "" {
				pattern = module + "#" + v
			}
			if !seen[pattern] {
				seen[pattern] = true
				imports = append(imports, pattern)
			}
		}
	}

	// 1. Standalone core module (no canon registry)
	if m.canonRegistry == nil {
		for _, hf := range asyncHostFuncs {
			add(hf.Namespace, hf.Name)
		}
		if len(m.rawBytes) > 0 {
			mod, err := wasm.ParseModule(m.rawBytes)
			if err != nil {
				// Fallback to stock Wazero reflection if compiled module is present
				if m.compiled != nil {
					for _, fn := range m.compiled.ImportedFunctions() {
						modName, funcName, isImport := fn.Import()
						if !isImport {
							continue
						}
						for _, hf := range asyncHostFuncs {
							if isCompatibleImport(hf.Namespace, hf.Name, modName, funcName) {
								add(modName, funcName)
							}
						}
					}
				} else {
					return nil, fmt.Errorf("parse core module imports: %w", err)
				}
			} else {
				for _, imp := range mod.Imports {
					if imp.Desc.Kind == wasm.KindFunc {
						for _, hf := range asyncHostFuncs {
							if isCompatibleImport(hf.Namespace, hf.Name, imp.Module, imp.Name) {
								add(imp.Module, imp.Name)
							}
						}
					}
				}
			}
		}
		sort.Strings(imports)
		return imports, nil
	}

	// 2. Component Module: Parse all core modules once and cache metadata
	if m.validated == nil || m.validated.Raw == nil {
		return nil, fmt.Errorf("component validation data is missing")
	}
	comp := m.validated.Raw

	parsedModules := make([]*coreModuleMeta, len(comp.CoreModules))
	for i, modBytes := range comp.CoreModules {
		meta, err := parseCoreModuleMeta(i, modBytes)
		if err != nil {
			return nil, err
		}
		parsedModules[i] = meta
	}

	// 3. Collect compatible canon lowers from host registrations
	asyncCanonFuncIndices := make(map[uint32]bool)

	// In comp.Canons:
	for _, c := range comp.Canons {
		if c.Parsed == nil || c.Parsed.Kind != component.CanonLower {
			continue
		}
		fIdx := c.Parsed.FuncIndex
		importName := findComponentImportName(comp, fIdx)
		lowerNs, lowerFunc := splitLowerName(importName)
		for _, hf := range asyncHostFuncs {
			if isCompatibleImport(hf.Namespace, hf.Name, lowerNs, lowerFunc) {
				asyncCanonFuncIndices[fIdx] = true
				if lowerNs != "" {
					add(lowerNs, lowerFunc)
					if hf.Name != lowerFunc {
						add(lowerNs, hf.Name)
					}
				}
				add(hf.Namespace, hf.Name)
			}
		}
	}

	// In canon registry:
	for _, hf := range asyncHostFuncs {
		lowers := m.findCompatibleLowerDefs(hf.Namespace, hf.Name)
		if len(lowers) > 0 {
			for _, l := range lowers {
				asyncCanonFuncIndices[l.FuncIdx] = true
				lowerNs, lowerFunc := splitLowerName(l.Name)
				if lowerNs != "" {
					add(lowerNs, lowerFunc)
					if hf.Name != lowerFunc {
						add(lowerNs, hf.Name)
					}
				}
			}
			add(hf.Namespace, hf.Name)
		}
	}

	numInstances := len(comp.CoreInstances)
	numCoreFuncs := len(comp.CoreFuncIndexSpace)

	// Fail closed on invalid core instance source aliases in Aliases section
	for _, alias := range comp.Aliases {
		if alias.Parsed != nil && alias.Parsed.Sort == 0x00 && alias.Parsed.TargetKind == 0x01 {
			if int(alias.Parsed.Instance) < 0 || int(alias.Parsed.Instance) >= numInstances {
				return nil, fmt.Errorf("invalid core instance index %d in alias %q (total instances: %d)", alias.Parsed.Instance, alias.Parsed.Name, numInstances)
			}
		}
	}

	// Validate all core instances and arguments fail-closed
	for instIdx, ci := range comp.CoreInstances {
		if ci.Parsed == nil {
			return nil, fmt.Errorf("core instance %d parsed metadata missing", instIdx)
		}
		switch ci.Parsed.Kind {
		case component.CoreInstanceInstantiate:
			modIdx := int(ci.Parsed.ModuleIndex)
			if modIdx < 0 || modIdx >= len(parsedModules) {
				return nil, fmt.Errorf("invalid core module index %d for core instance %d (total modules: %d)", modIdx, instIdx, len(parsedModules))
			}
			for _, arg := range ci.Parsed.Args {
				if arg.Kind == component.CoreInstantiateInstance {
					if int(arg.InstanceIndex) < 0 || int(arg.InstanceIndex) >= numInstances {
						return nil, fmt.Errorf("invalid core instance argument index %d in argument %q for core instance %d (total instances: %d)", arg.InstanceIndex, arg.Name, instIdx, numInstances)
					}
				}
			}
		case component.CoreInstanceFromExports:
			for _, exp := range ci.Parsed.Exports {
				if exp.Kind == component.CoreExportFunc {
					if int(exp.Index) < 0 || int(exp.Index) >= numCoreFuncs {
						return nil, fmt.Errorf("invalid core func index %d in export %q of core instance %d (total core funcs: %d)", exp.Index, exp.Name, instIdx, numCoreFuncs)
					}
				}
			}
		default:
			return nil, fmt.Errorf("unknown core instance kind 0x%02x for core instance %d", ci.Parsed.Kind, instIdx)
		}
	}

	// Validate comp.CoreFuncIndexSpace fail-closed
	for i, entry := range comp.CoreFuncIndexSpace {
		if entry.Kind == component.CoreFuncAliasExport {
			if entry.InstanceIdx < 0 || entry.InstanceIdx >= numInstances {
				return nil, fmt.Errorf("invalid core instance index %d in core func alias export %q (entry %d, total instances: %d)", entry.InstanceIdx, entry.ExportName, i, numInstances)
			}
		}
	}

	// Build linear dependency graph
	var adj [][]int
	addNode := func() int {
		id := len(adj)
		adj = append(adj, nil)
		return id
	}

	// 1. CoreFunc nodes: 0 .. numCoreFuncs-1
	for i := 0; i < numCoreFuncs; i++ {
		addNode()
	}

	// 2. InstanceFunc nodes for instantiate instances
	instFuncBase := make([]int, numInstances)
	for instIdx, ci := range comp.CoreInstances {
		if ci.Parsed.Kind == component.CoreInstanceInstantiate {
			modIdx := int(ci.Parsed.ModuleIndex)
			meta := parsedModules[modIdx]
			totalFuncs := meta.numImported + len(meta.mod.Code)
			instFuncBase[instIdx] = len(adj)
			for f := 0; f < totalFuncs; f++ {
				addNode()
			}
		}
	}

	// 3. Export nodes
	type exportKey struct {
		name    string
		instIdx int
	}
	exportNodes := make(map[exportKey]int)
	getExportNode := func(instIdx int, name string) int {
		k := exportKey{name: name, instIdx: instIdx}
		if id, ok := exportNodes[k]; ok {
			return id
		}
		id := addNode()
		exportNodes[k] = id
		return id
	}

	// Construct reverse dependency edges once:
	// A. CoreFuncAliasExport: if instance export is async -> core func is async
	for i, entry := range comp.CoreFuncIndexSpace {
		if entry.Kind == component.CoreFuncAliasExport {
			from := getExportNode(entry.InstanceIdx, entry.ExportName)
			to := i
			adj[from] = append(adj[from], to)
		}
	}

	// B. CoreInstanceFromExports: if core func is async -> instance export is async
	for instIdx, ci := range comp.CoreInstances {
		if ci.Parsed.Kind == component.CoreInstanceFromExports {
			for _, exp := range ci.Parsed.Exports {
				if exp.Kind == component.CoreExportFunc {
					from := int(exp.Index)
					to := getExportNode(instIdx, exp.Name)
					adj[from] = append(adj[from], to)
				}
			}
		}
	}

	// C. CoreInstanceInstantiate:
	for instIdx, ci := range comp.CoreInstances {
		if ci.Parsed.Kind != component.CoreInstanceInstantiate {
			continue
		}
		modIdx := int(ci.Parsed.ModuleIndex)
		meta := parsedModules[modIdx]

		argProviders := make(map[string]int, len(ci.Parsed.Args))
		for _, arg := range ci.Parsed.Args {
			if arg.Kind == component.CoreInstantiateInstance {
				argProviders[arg.Name] = int(arg.InstanceIndex)
			}
		}

		// Imports: if provider instance export is async -> instance func import is async
		for impIdx, imp := range meta.imports {
			if provIdx, ok := argProviders[imp.Module]; ok {
				from := getExportNode(provIdx, imp.Name)
				to := instFuncBase[instIdx] + impIdx
				adj[from] = append(adj[from], to)
			}
		}

		// Intra-module direct calls: if callee is async -> caller is async
		for callerIdx, callees := range meta.callees {
			to := instFuncBase[instIdx] + int(callerIdx)
			for _, calleeIdx := range callees {
				from := instFuncBase[instIdx] + int(calleeIdx)
				adj[from] = append(adj[from], to)
			}
		}

		// Exports: if defined func is async -> instance export is async
		for expName, funcIdx := range meta.funcExports {
			from := instFuncBase[instIdx] + int(funcIdx)
			to := getExportNode(instIdx, expName)
			adj[from] = append(adj[from], to)
		}
	}

	// Initialize queue and mark initial seeds
	isAsync := make([]bool, len(adj))
	queue := make([]int, 0, len(adj))

	markAsync := func(u int) {
		if !isAsync[u] {
			isAsync[u] = true
			queue = append(queue, u)
		}
	}

	// Seed 1: Canon lower async funcs
	for i, entry := range comp.CoreFuncIndexSpace {
		if entry.Kind == component.CoreFuncCanonLower {
			if asyncCanonFuncIndices[entry.FuncIndex] {
				markAsync(i)
			}
		}
	}

	// Seed 2: Direct host async imports in core modules
	for instIdx, ci := range comp.CoreInstances {
		if ci.Parsed.Kind != component.CoreInstanceInstantiate {
			continue
		}
		modIdx := int(ci.Parsed.ModuleIndex)
		meta := parsedModules[modIdx]
		for impIdx, imp := range meta.imports {
			for _, hf := range asyncHostFuncs {
				if isCompatibleImport(hf.Namespace, hf.Name, imp.Module, imp.Name) {
					markAsync(instFuncBase[instIdx] + impIdx)
					add(imp.Module, imp.Name)
					break
				}
			}
		}
	}

	// Seed 3: Conservative indirect roots independent of local async set (call_indirect and call_ref)
	for instIdx, ci := range comp.CoreInstances {
		if ci.Parsed.Kind != component.CoreInstanceInstantiate {
			continue
		}
		modIdx := int(ci.Parsed.ModuleIndex)
		meta := parsedModules[modIdx]
		for callerIdx := range meta.hasIndirect {
			markAsync(instFuncBase[instIdx] + int(callerIdx))
		}
	}

	// Queue false->true propagation (bounded complexity by graph size O(V+E))
	for head := 0; head < len(queue); head++ {
		u := queue[head]
		for _, v := range adj[u] {
			if !isAsync[v] {
				isAsync[v] = true
				queue = append(queue, v)
			}
		}
	}

	// Collect all async imports across all module instances
	for instIdx, ci := range comp.CoreInstances {
		if ci.Parsed.Kind != component.CoreInstanceInstantiate {
			continue
		}
		modIdx := int(ci.Parsed.ModuleIndex)
		meta := parsedModules[modIdx]
		for impIdx, imp := range meta.imports {
			if isAsync[instFuncBase[instIdx]+impIdx] {
				add(imp.Module, imp.Name)
			}
		}
	}

	// Direct import check across all core modules in component (fallback safety)
	for _, meta := range parsedModules {
		for _, imp := range meta.imports {
			for _, hf := range asyncHostFuncs {
				if isCompatibleImport(hf.Namespace, hf.Name, imp.Module, imp.Name) {
					add(imp.Module, imp.Name)
					break
				}
			}
		}
	}

	sort.Strings(imports)
	return imports, nil
}

// findComponentImportName resolves the full qualified import name (e.g. "namespace#func")
// for a component function index.
func findComponentImportName(comp *component.Component, funcIdx uint32) string {
	if comp == nil || int(funcIdx) >= len(comp.FuncIndexSpace) {
		return ""
	}
	funcEntry := comp.FuncIndexSpace[funcIdx]
	instanceCount := uint32(0)
	for _, imp := range comp.Imports {
		if imp.ExternKind == component.ExternInstance {
			if instanceCount == funcEntry.InstanceIdx {
				return imp.Name + "#" + funcEntry.ExportName
			}
			instanceCount++
		}
	}
	return ""
}

// findCompatibleLowerDefs looks up ALL canon.lower definitions compatible with a namespace and function name.
// Uses exact matching and semver matching: host version X.Y.Z satisfies component import X.Y.W where W <= Z.
func (m *WazeroModule) findCompatibleLowerDefs(namespace, name string) []*component.LowerDef {
	if m.canonRegistry == nil {
		return nil
	}

	nameVariants := funcNameVariants(name)

	seen := make(map[*component.LowerDef]bool)
	var matches []*component.LowerDef

	addLower := func(ld *component.LowerDef) {
		if ld != nil && !seen[ld] {
			seen[ld] = true
			matches = append(matches, ld)
		}
	}

	// 1. Try exact namespace match first
	for _, n := range nameVariants {
		importName := namespace + "#" + n
		if ld := m.canonRegistry.FindLower(importName); ld != nil {
			addLower(ld)
		}
		if ld := m.canonRegistry.FindLower(n); ld != nil {
			addLower(ld)
		}
	}

	// 2. Search all lowers for semver-compatible match
	hostBase, hostVersion, hasHostVersion := parseNamespaceVersion(namespace)
	if hasHostVersion {
		for _, lowerDef := range m.canonRegistry.AllLowers() {
			lowerNs, lowerFunc := splitLowerName(lowerDef.Name)
			if lowerNs == "" {
				continue
			}

			funcMatches := false
			for _, n := range nameVariants {
				if lowerFunc == n || kebabToWitName(lowerFunc) == n || witToKebabName(lowerFunc) == n {
					funcMatches = true
					break
				}
			}
			if !funcMatches {
				continue
			}

			compBase, compVersion, hasCompVersion := parseNamespaceVersion(lowerNs)
			if !hasCompVersion {
				continue
			}

			// Host version X.Y.Z satisfies component import X.Y.W where W <= Z.
			// Newer guest requiring unsupported host version is NOT widened.
			if hostBase == compBase && hostVersion.Compatible(compVersion) {
				addLower(lowerDef)
			}
		}
	}

	// 3. If validated component is available, ensure any duplicate lower definitions
	// in comp.Canons (which may have been overwritten in FindLower map) are also included.
	if m.validated != nil && m.validated.Raw != nil {
		comp := m.validated.Raw
		for _, c := range comp.Canons {
			if c.Parsed == nil || c.Parsed.Kind != component.CanonLower {
				continue
			}
			fIdx := c.Parsed.FuncIndex
			importName := findComponentImportName(comp, fIdx)
			lowerNs, lowerFunc := splitLowerName(importName)
			if lowerNs == "" {
				continue
			}
			funcMatches := false
			for _, n := range nameVariants {
				if lowerFunc == n || kebabToWitName(lowerFunc) == n || witToKebabName(lowerFunc) == n {
					funcMatches = true
					break
				}
			}
			if !funcMatches {
				continue
			}

			compBase, compVersion, hasCompVersion := parseNamespaceVersion(lowerNs)
			isCompatible := false
			if hasHostVersion && hasCompVersion && hostBase == compBase && hostVersion.Compatible(compVersion) {
				isCompatible = true
			} else if !hasHostVersion && namespace == lowerNs {
				isCompatible = true
			}

			if isCompatible {
				alreadyPresent := false
				for _, match := range matches {
					if match.FuncIdx == fIdx {
						alreadyPresent = true
						break
					}
				}
				if !alreadyPresent {
					baseLower := m.canonRegistry.FindLower(importName)
					ld := &component.LowerDef{
						Name:       importName,
						FuncIdx:    fIdx,
						MemoryIdx:  c.Parsed.GetMemoryIndex(),
						ReallocIdx: c.Parsed.GetReallocIndex(),
					}
					if baseLower != nil {
						ld.Params = baseLower.Params
						ld.Results = baseLower.Results
						ld.ParamNames = baseLower.ParamNames
						ld.IsAsync = baseLower.IsAsync
					}
					addLower(ld)
				}
			}
		}
	}

	return matches
}

// findLowerDef looks up a canon.lower definition for a namespace and function name.
func (m *WazeroModule) findLowerDef(namespace, name string) *component.LowerDef {
	if m.canonRegistry == nil {
		return nil
	}

	nameVariants := funcNameVariants(name)

	for _, n := range nameVariants {
		importName := namespace + "#" + n
		if ld := m.canonRegistry.FindLower(importName); ld != nil {
			return ld
		}
		if ld := m.canonRegistry.FindLower(n); ld != nil {
			return ld
		}
	}

	lowers := m.findCompatibleLowerDefs(namespace, name)
	if len(lowers) > 0 {
		return lowers[0]
	}
	return nil
}

// isCompatibleImport checks whether a candidate (impMod, impName) matches a registered
// or lowered function (targetNs, targetName), taking semver compatibility and method aliases into account.
// Guarantees that newer guest requiring unsupported host version is NOT widened.
func isCompatibleImport(targetNs, targetName, impMod, impName string) bool {
	nameMatches := false
	for _, v := range funcNameVariants(targetName) {
		if impName == v || kebabToWitName(impName) == v || witToKebabName(impName) == v {
			nameMatches = true
			break
		}
	}
	if !nameMatches {
		return false
	}

	if targetNs == impMod {
		return true
	}

	targetBase, targetVer, hasTargetVer := parseNamespaceVersion(targetNs)
	impBase, impVer, hasImpVer := parseNamespaceVersion(impMod)
	if hasTargetVer && hasImpVer && targetBase == impBase {
		return targetVer.Compatible(impVer)
	}

	return false
}

// funcNameVariants generates canonical WIT and kebab-case name variants for matching.
func funcNameVariants(name string) []string {
	variants := []string{name}
	if wit := kebabToWitName(name); wit != name {
		variants = append(variants, wit)
	}
	if kebab := witToKebabName(name); kebab != name {
		variants = append(variants, kebab)
	}
	return variants
}

// parseNamespaceVersion splits "wasi:io/streams@0.2.8" into base path and version
func parseNamespaceVersion(namespace string) (basePath string, version linker.Version, hasVersion bool) {
	idx := -1
	for i := len(namespace) - 1; i >= 0; i-- {
		if namespace[i] == '@' {
			idx = i
			break
		}
	}
	if idx == -1 {
		return namespace, linker.Version{}, false
	}
	basePath = namespace[:idx]
	version, hasVersion = linker.ParseVersion(namespace[idx+1:])
	return basePath, version, hasVersion
}

// splitLowerName splits "wasi:io/streams@0.2.0#read" into namespace and function
func splitLowerName(name string) (namespace, funcName string) {
	idx := -1
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '#' {
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", name
	}
	return name[:idx], name[idx+1:]
}
