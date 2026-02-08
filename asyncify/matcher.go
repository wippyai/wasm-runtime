package asyncify

import (
	"strings"

	"github.com/wippyai/wasm-runtime/asyncify/internal/engine"
)

// ExactMatcher matches exact "module.name" or just "name" patterns.
type ExactMatcher struct {
	patterns map[string]bool
}

// NewExactMatcher creates a matcher from a list of patterns.
// Patterns can be "name" (matches any module) or "module.name" (exact match).
func NewExactMatcher(patterns []string) *ExactMatcher {
	m := &ExactMatcher{patterns: make(map[string]bool)}
	for _, p := range patterns {
		m.patterns[p] = true
	}
	return m
}

// Match returns true if the import matches any pattern.
func (m *ExactMatcher) Match(module, name string) bool {
	if m.patterns[module+"."+name] {
		return true
	}
	return m.patterns[name]
}

// WildcardMatcher matches import patterns with wildcard support.
//
// Supports patterns like:
//   - "module.name" - exact match
//   - "name" - matches any module with this function name
//   - "module.*" - matches all imports from module
//   - "*" - matches everything
type WildcardMatcher struct {
	exact       map[string]bool // exact "module.name" matches
	names       map[string]bool // unqualified "name" matches
	moduleWilds map[string]bool // "module.*" matches
	matchAll    bool            // "*" matches everything
}

// NewWildcardMatcher creates a matcher with wildcard support.
func NewWildcardMatcher(patterns []string) *WildcardMatcher {
	m := &WildcardMatcher{
		exact:       make(map[string]bool),
		names:       make(map[string]bool),
		moduleWilds: make(map[string]bool),
	}
	for _, p := range patterns {
		if p == "*" {
			m.matchAll = true
		} else if strings.HasSuffix(p, ".*") {
			m.moduleWilds[strings.TrimSuffix(p, ".*")] = true
		} else if strings.Contains(p, ".") {
			m.exact[p] = true
		} else {
			m.names[p] = true
		}
	}
	return m
}

// Match returns true if the import matches any pattern.
func (m *WildcardMatcher) Match(module, name string) bool {
	if m.matchAll {
		return true
	}
	if m.moduleWilds[module] {
		return true
	}
	if m.exact[module+"."+name] {
		return true
	}
	return m.names[name]
}

// WITMatcher matches WIT-style import namespaces.
//
// Supports patterns like:
//   - "wasi:io/poll@0.2.0#block" - exact match
//   - "wasi:io/poll#block" - any version
//   - "wasi:io/poll@0.2.*#block" - version prefix
type WITMatcher struct {
	exact    map[string]bool
	noVer    map[string]bool
	prefixes []string
}

// NewWITMatcher creates a WIT-style matcher.
func NewWITMatcher(patterns []string) *WITMatcher {
	m := &WITMatcher{
		exact: make(map[string]bool),
		noVer: make(map[string]bool),
	}
	for _, p := range patterns {
		if strings.HasSuffix(p, "*") {
			m.prefixes = append(m.prefixes, strings.TrimSuffix(p, "*"))
		} else if !strings.Contains(p, "@") {
			m.noVer[p] = true
		} else {
			m.exact[p] = true
		}
	}
	return m
}

// Match returns true if the WIT import matches any pattern.
func (m *WITMatcher) Match(module, name string) bool {
	// WIT imports: module="wasi:io/poll@0.2.0", name="block"
	full := module + "#" + name

	if m.exact[full] {
		return true
	}

	noVer := stripVersion(module) + "#" + name
	if m.noVer[noVer] {
		return true
	}

	for _, prefix := range m.prefixes {
		if strings.HasPrefix(full, prefix) {
			return true
		}
	}

	return false
}

func stripVersion(ns string) string {
	if idx := strings.Index(ns, "@"); idx >= 0 {
		return ns[:idx]
	}
	return ns
}

// CompositeMatcher combines multiple matchers.
type CompositeMatcher struct {
	matchers []ImportMatcher
}

// NewCompositeMatcher creates a matcher that matches if any sub-matcher matches.
func NewCompositeMatcher(matchers ...ImportMatcher) *CompositeMatcher {
	return &CompositeMatcher{matchers: matchers}
}

// Match returns true if any sub-matcher matches.
func (m *CompositeMatcher) Match(module, name string) bool {
	for _, matcher := range m.matchers {
		if matcher.Match(module, name) {
			return true
		}
	}
	return false
}

// FunctionMatcher determines if a function should be included or excluded.
type FunctionMatcher = engine.FunctionMatcher

// FunctionNameMatcher matches functions by exact name.
type FunctionNameMatcher struct {
	names map[string]bool
}

// NewFunctionNameMatcher creates a matcher from a list of function names.
func NewFunctionNameMatcher(names []string) *FunctionNameMatcher {
	m := &FunctionNameMatcher{names: make(map[string]bool)}
	for _, n := range names {
		m.names[n] = true
	}
	return m
}

// MatchFunction returns true if the function name matches.
func (m *FunctionNameMatcher) MatchFunction(name string) bool {
	return m.names[name]
}

// FunctionPrefixMatcher matches functions by name prefix.
type FunctionPrefixMatcher struct {
	prefixes []string
}

// NewFunctionPrefixMatcher creates a matcher that matches functions starting with any prefix.
func NewFunctionPrefixMatcher(prefixes []string) *FunctionPrefixMatcher {
	return &FunctionPrefixMatcher{prefixes: prefixes}
}

// MatchFunction returns true if the function name starts with any prefix.
func (m *FunctionPrefixMatcher) MatchFunction(name string) bool {
	for _, p := range m.prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// CompositeFunctionMatcher combines multiple function matchers.
type CompositeFunctionMatcher struct {
	matchers []FunctionMatcher
}

// NewCompositeFunctionMatcher creates a matcher that matches if any sub-matcher matches.
func NewCompositeFunctionMatcher(matchers ...FunctionMatcher) *CompositeFunctionMatcher {
	return &CompositeFunctionMatcher{matchers: matchers}
}

// MatchFunction returns true if any sub-matcher matches.
func (m *CompositeFunctionMatcher) MatchFunction(name string) bool {
	for _, matcher := range m.matchers {
		if matcher.MatchFunction(name) {
			return true
		}
	}
	return false
}
