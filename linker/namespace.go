package linker

import (
	"strings"
	"sync"

	"github.com/tetratelabs/wazero/api"
)

// Version represents a semantic version for namespace matching
type Version struct {
	Major uint32
	Minor uint32
	Patch uint32
}

// ParseVersion parses a version string like "0.2.0" or "0.2"
func ParseVersion(s string) (Version, bool) {
	if s == "" {
		return Version{}, false
	}

	var v Version
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return Version{}, false
	}

	for i, p := range parts {
		if p == "" {
			return Version{}, false
		}
		var n uint32
		for _, c := range p {
			if c < '0' || c > '9' {
				return Version{}, false
			}
			// Check for overflow before multiplication
			if n > 429496729 || (n == 429496729 && c > '5') {
				return Version{}, false
			}
			n = n*10 + uint32(c-'0')
		}
		switch i {
		case 0:
			v.Major = n
		case 1:
			v.Minor = n
		case 2:
			v.Patch = n
		}
	}
	return v, true
}

// Compatible returns true if v is semver-compatible with want.
// Compatible means same major, and v.Minor >= want.Minor
func (v Version) Compatible(want Version) bool {
	if v.Major != want.Major {
		return false
	}
	if v.Minor < want.Minor {
		return false
	}
	if v.Minor == want.Minor && v.Patch < want.Patch {
		return false
	}
	return true
}

// String returns the version as "major.minor.patch"
func (v Version) String() string {
	return strings.Join([]string{
		uintToStr(v.Major),
		uintToStr(v.Minor),
		uintToStr(v.Patch),
	}, ".")
}

func uintToStr(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// FuncDef defines a host function
type FuncDef struct {
	Name        string
	Handler     api.GoModuleFunc
	ParamTypes  []api.ValueType
	ResultTypes []api.ValueType
}

// GetHandler returns the function handler (implements resolve.HostFuncDef).
func (f *FuncDef) GetHandler() api.GoModuleFunc { return f.Handler }

// GetParamTypes returns the parameter types (implements resolve.HostFuncDef).
func (f *FuncDef) GetParamTypes() []api.ValueType { return f.ParamTypes }

// GetResultTypes returns the result types (implements resolve.HostFuncDef).
func (f *FuncDef) GetResultTypes() []api.ValueType { return f.ResultTypes }

// Namespace represents a hierarchical namespace node with optional version
type Namespace struct {
	version  *Version
	funcs    map[string]*FuncDef
	children map[string]*Namespace
	parent   *Namespace
	name     string
	mu       sync.RWMutex
}

// NewNamespace creates a root namespace
func NewNamespace() *Namespace {
	return &Namespace{
		funcs:    make(map[string]*FuncDef),
		children: make(map[string]*Namespace),
	}
}

// Name returns the namespace name
func (ns *Namespace) Name() string {
	return ns.name
}

// Version returns the namespace version, or nil if unversioned
func (ns *Namespace) Version() *Version {
	return ns.version
}

// FullPath returns the full namespace path like "wasi:io/streams@0.2.0"
func (ns *Namespace) FullPath() string {
	if ns.parent == nil {
		return ns.name
	}
	parentPath := ns.parent.FullPath()
	if parentPath == "" {
		if ns.version != nil {
			return ns.name + "@" + ns.version.String()
		}
		return ns.name
	}
	if ns.version != nil {
		return parentPath + "/" + ns.name + "@" + ns.version.String()
	}
	return parentPath + "/" + ns.name
}

// Instance returns or creates a child namespace with the given name.
// Instance accepts name with optional version: "wasi:io/streams@0.2.0"
func (ns *Namespace) Instance(name string) *Namespace {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	// Parse name and optional version
	parsedName, version := parseNameVersion(name)

	// Determine the key to use for lookup/storage
	var key string
	if version != nil {
		key = parsedName + "@" + version.String()
	} else {
		key = parsedName
	}

	// Check if child already exists
	if child, ok := ns.children[key]; ok {
		return child
	}

	// Create new child
	child := &Namespace{
		name:     parsedName,
		version:  version,
		funcs:    make(map[string]*FuncDef),
		children: make(map[string]*Namespace),
		parent:   ns,
	}
	ns.children[key] = child
	return child
}

// DefineFunc registers a host function in this namespace.
// DefineFunc overwrites any existing function with the same name.
func (ns *Namespace) DefineFunc(name string, fn api.GoModuleFunc, params, results []api.ValueType) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	ns.funcs[name] = &FuncDef{
		Name:        name,
		Handler:     fn,
		ParamTypes:  params,
		ResultTypes: results,
	}
}

// GetFunc returns a function by name, or nil if not found
func (ns *Namespace) GetFunc(name string) *FuncDef {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.funcs[name]
}

// GetChild returns a child namespace by name, or nil if not found
func (ns *Namespace) GetChild(name string) *Namespace {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.children[name]
}

// Resolve looks up a function by full path: "wasi:io/streams@0.2.0#read"
// Resolve supports semver-compatible matching when exact version not found.
func (ns *Namespace) Resolve(path string) *FuncDef {
	return ns.ResolveWithSemver(path, true)
}

// ResolveExact looks up a function requiring exact version match
func (ns *Namespace) ResolveExact(path string) *FuncDef {
	return ns.ResolveWithSemver(path, false)
}

// ResolveWithSemver looks up a function with configurable semver matching
func (ns *Namespace) ResolveWithSemver(path string, semverMatching bool) *FuncDef {
	// Split path into namespace and function: "ns#func"
	idx := strings.LastIndex(path, "#")
	if idx < 0 {
		return nil
	}
	nsPath := path[:idx]
	funcName := path[idx+1:]

	// Navigate to namespace
	target := ns.resolveNamespace(nsPath, semverMatching)
	if target == nil {
		return nil
	}

	return target.GetFunc(funcName)
}

// resolveNamespace finds a namespace by path with optional semver matching
func (ns *Namespace) resolveNamespace(path string, semverMatching bool) *Namespace {
	if path == "" {
		return ns
	}

	// Parse path segments
	segments := parseNamespacePath(path)
	current := ns

	for _, seg := range segments {
		current.mu.RLock()

		// Try exact match first
		if child, ok := current.children[seg.name]; ok && seg.version == nil {
			current.mu.RUnlock()
			current = child
			continue
		}

		// Try versioned match
		if seg.version != nil {
			versionedName := seg.name + "@" + seg.version.String()
			if child, ok := current.children[versionedName]; ok {
				current.mu.RUnlock()
				current = child
				continue
			}

			// Try semver-compatible match only if enabled
			if semverMatching {
				var bestMatch *Namespace
				var bestVersion *Version
				for key, child := range current.children {
					if !strings.HasPrefix(key, seg.name+"@") {
						continue
					}
					if child.version != nil && child.version.Compatible(*seg.version) {
						if bestVersion == nil || child.version.Minor > bestVersion.Minor ||
							(child.version.Minor == bestVersion.Minor && child.version.Patch > bestVersion.Patch) {
							bestMatch = child
							bestVersion = child.version
						}
					}
				}
				if bestMatch != nil {
					current.mu.RUnlock()
					current = bestMatch
					continue
				}
			}
		}

		current.mu.RUnlock()
		return nil
	}

	return current
}

// AllFuncs returns all functions defined in this namespace
func (ns *Namespace) AllFuncs() map[string]*FuncDef {
	ns.mu.RLock()
	defer ns.mu.RUnlock()

	result := make(map[string]*FuncDef, len(ns.funcs))
	for k, v := range ns.funcs {
		result[k] = v
	}
	return result
}

// AllChildren returns all child namespaces
func (ns *Namespace) AllChildren() map[string]*Namespace {
	ns.mu.RLock()
	defer ns.mu.RUnlock()

	result := make(map[string]*Namespace, len(ns.children))
	for k, v := range ns.children {
		result[k] = v
	}
	return result
}

// pathSegment represents a parsed namespace path segment
type pathSegment struct {
	version *Version
	name    string
}

// parseNamespacePath parses "wasi:io/streams@0.2.0" into segments
func parseNamespacePath(path string) []pathSegment {
	var segments []pathSegment

	// Split by "/" but handle ":" specially for package prefix
	colonIdx := strings.Index(path, ":")
	if colonIdx > 0 {
		// First segment is package: "wasi:io" -> "wasi:io"
		slashIdx := strings.Index(path[colonIdx:], "/")
		if slashIdx > 0 {
			first := path[:colonIdx+slashIdx]
			segments = append(segments, parseSingleSegment(first))
			path = path[colonIdx+slashIdx+1:]
		} else {
			segments = append(segments, parseSingleSegment(path))
			return segments
		}
	}

	// Split remaining by "/"
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			segments = append(segments, parseSingleSegment(part))
		}
	}

	return segments
}

// parseSingleSegment parses "streams@0.2.0" into name and version
func parseSingleSegment(s string) pathSegment {
	name, version := parseNameVersion(s)
	return pathSegment{name: name, version: version}
}

// parseNameVersion splits "name@version" into name and parsed version
func parseNameVersion(s string) (string, *Version) {
	idx := strings.LastIndex(s, "@")
	if idx < 0 {
		return s, nil
	}
	name := s[:idx]
	versionStr := s[idx+1:]
	if v, ok := ParseVersion(versionStr); ok {
		return name, &v
	}
	return s, nil
}
