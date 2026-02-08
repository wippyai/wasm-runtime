package asyncify

import "testing"

func TestExactMatcher(t *testing.T) {
	tests := []struct {
		name     string
		module   string
		funcName string
		patterns []string
		want     bool
	}{
		{
			name:     "match by function name only",
			patterns: []string{"sleep"},
			module:   "env",
			funcName: "sleep",
			want:     true,
		},
		{
			name:     "match by module.name",
			patterns: []string{"env.sleep"},
			module:   "env",
			funcName: "sleep",
			want:     true,
		},
		{
			name:     "no match different module",
			patterns: []string{"env.sleep"},
			module:   "other",
			funcName: "sleep",
			want:     false,
		},
		{
			name:     "no match different name",
			patterns: []string{"sleep"},
			module:   "env",
			funcName: "log",
			want:     false,
		},
		{
			name:     "multiple patterns",
			patterns: []string{"read", "write", "env.close"},
			module:   "env",
			funcName: "write",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewExactMatcher(tt.patterns)
			got := m.Match(tt.module, tt.funcName)
			if got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.module, tt.funcName, got, tt.want)
			}
		})
	}
}

func TestWildcardMatcher(t *testing.T) {
	tests := []struct {
		name     string
		module   string
		funcName string
		patterns []string
		want     bool
	}{
		{
			name:     "exact match module.name",
			patterns: []string{"env.sleep"},
			module:   "env",
			funcName: "sleep",
			want:     true,
		},
		{
			name:     "exact match name only",
			patterns: []string{"sleep"},
			module:   "any",
			funcName: "sleep",
			want:     true,
		},
		{
			name:     "module wildcard matches all funcs",
			patterns: []string{"env.*"},
			module:   "env",
			funcName: "sleep",
			want:     true,
		},
		{
			name:     "module wildcard matches other func",
			patterns: []string{"env.*"},
			module:   "env",
			funcName: "log",
			want:     true,
		},
		{
			name:     "module wildcard wrong module",
			patterns: []string{"env.*"},
			module:   "wasi",
			funcName: "sleep",
			want:     false,
		},
		{
			name:     "match all wildcard",
			patterns: []string{"*"},
			module:   "anything",
			funcName: "whatever",
			want:     true,
		},
		{
			name:     "no match",
			patterns: []string{"env.sleep"},
			module:   "env",
			funcName: "log",
			want:     false,
		},
		{
			name:     "multiple patterns with wildcard",
			patterns: []string{"env.*", "wasi.clock"},
			module:   "wasi",
			funcName: "clock",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewWildcardMatcher(tt.patterns)
			got := m.Match(tt.module, tt.funcName)
			if got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.module, tt.funcName, got, tt.want)
			}
		})
	}
}

func TestWITMatcher(t *testing.T) {
	tests := []struct {
		name     string
		module   string
		funcName string
		patterns []string
		want     bool
	}{
		{
			name:     "exact match with version",
			patterns: []string{"wasi:io/poll@0.2.0#block"},
			module:   "wasi:io/poll@0.2.0",
			funcName: "block",
			want:     true,
		},
		{
			name:     "exact match wrong version",
			patterns: []string{"wasi:io/poll@0.2.0#block"},
			module:   "wasi:io/poll@0.2.1",
			funcName: "block",
			want:     false,
		},
		{
			name:     "version-less match",
			patterns: []string{"wasi:io/poll#block"},
			module:   "wasi:io/poll@0.2.0",
			funcName: "block",
			want:     true,
		},
		{
			name:     "version-less match different version",
			patterns: []string{"wasi:io/poll#block"},
			module:   "wasi:io/poll@0.3.0",
			funcName: "block",
			want:     true,
		},
		{
			name:     "version-less no match wrong function",
			patterns: []string{"wasi:io/poll#block"},
			module:   "wasi:io/poll@0.2.0",
			funcName: "ready",
			want:     false,
		},
		{
			name:     "prefix match",
			patterns: []string{"wasi:io/poll@0.2.*"},
			module:   "wasi:io/poll@0.2.5",
			funcName: "block",
			want:     true,
		},
		{
			name:     "prefix match no wildcard",
			patterns: []string{"wasi:io/poll@0.2.*"},
			module:   "wasi:io/poll@0.3.0",
			funcName: "block",
			want:     false,
		},
		{
			name:     "multiple patterns",
			patterns: []string{"wasi:io/poll#block", "wasi:clocks/monotonic-clock#now"},
			module:   "wasi:clocks/monotonic-clock@0.2.0",
			funcName: "now",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewWITMatcher(tt.patterns)
			got := m.Match(tt.module, tt.funcName)
			if got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.module, tt.funcName, got, tt.want)
			}
		})
	}
}

func TestCompositeMatcher(t *testing.T) {
	exact := NewExactMatcher([]string{"env.sleep"})
	wit := NewWITMatcher([]string{"wasi:io/poll#block"})
	composite := NewCompositeMatcher(exact, wit)

	tests := []struct {
		module   string
		funcName string
		want     bool
	}{
		{"env", "sleep", true},
		{"wasi:io/poll@0.2.0", "block", true},
		{"env", "log", false},
		{"wasi:io/poll@0.2.0", "ready", false},
	}

	for _, tt := range tests {
		got := composite.Match(tt.module, tt.funcName)
		if got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.module, tt.funcName, got, tt.want)
		}
	}
}

func TestStripVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"wasi:io/poll@0.2.0", "wasi:io/poll"},
		{"wasi:io/poll@0.2.0-rc1", "wasi:io/poll"},
		{"wasi:io/poll", "wasi:io/poll"},
		{"env", "env"},
		{"foo@bar@baz", "foo"},
	}

	for _, tt := range tests {
		got := stripVersion(tt.input)
		if got != tt.want {
			t.Errorf("stripVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAsyncImportMatcherSupportsHashAndDot(t *testing.T) {
	m := &asyncImportMatcher{
		patterns: []string{
			"wasi:io/poll@0.2.8#[method]pollable.block",
			"wasi:clocks/monotonic-clock@0.2.8.now",
			"sleep",
		},
	}

	if !m.Match("wasi:io/poll@0.2.8", "[method]pollable.block") {
		t.Fatal("hash pattern should match WIT-style import")
	}
	if !m.Match("wasi:clocks/monotonic-clock@0.2.8", "now") {
		t.Fatal("dot pattern should match import")
	}
	if !m.Match("env", "sleep") {
		t.Fatal("name-only pattern should match import")
	}
	if m.Match("env", "other") {
		t.Fatal("unexpected match for unrelated import")
	}
}

func TestFunctionNameMatcher(t *testing.T) {
	m := NewFunctionNameMatcher([]string{"foo", "bar", "baz"})

	tests := []struct {
		name string
		want bool
	}{
		{"foo", true},
		{"bar", true},
		{"baz", true},
		{"qux", false},
		{"foobar", false},
		{"", false},
	}

	for _, tt := range tests {
		got := m.MatchFunction(tt.name)
		if got != tt.want {
			t.Errorf("MatchFunction(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFunctionPrefixMatcher(t *testing.T) {
	m := NewFunctionPrefixMatcher([]string{"async_", "blocking_"})

	tests := []struct {
		name string
		want bool
	}{
		{"async_read", true},
		{"async_write", true},
		{"blocking_call", true},
		{"sync_read", false},
		{"async", false},
		{"", false},
	}

	for _, tt := range tests {
		got := m.MatchFunction(tt.name)
		if got != tt.want {
			t.Errorf("MatchFunction(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCompositeFunctionMatcher(t *testing.T) {
	exact := NewFunctionNameMatcher([]string{"special_func"})
	prefix := NewFunctionPrefixMatcher([]string{"async_"})
	composite := NewCompositeFunctionMatcher(exact, prefix)

	tests := []struct {
		name string
		want bool
	}{
		{"special_func", true},
		{"async_read", true},
		{"sync_read", false},
		{"special", false},
	}

	for _, tt := range tests {
		got := composite.MatchFunction(tt.name)
		if got != tt.want {
			t.Errorf("MatchFunction(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
