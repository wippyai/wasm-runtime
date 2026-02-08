package engine

import "testing"

func TestKebabToWitName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"poll", "poll"},
		{"get-value", "get-value"},
		{"method-pollable-ready", "[method]pollable.ready"},
		{"method-descriptor-read", "[method]descriptor.read"},
		{"static-descriptor-open-at", "[static]descriptor.open-at"},
		{"static-network-create", "[static]network.create"},
		{"method-a-b", "[method]a.b"},
		{"static-x-y", "[static]x.y"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := kebabToWitName(tt.input)
			if result != tt.expected {
				t.Errorf("kebabToWitName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWitToKebabName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"poll", "poll"},
		{"get-value", "get-value"},
		{"[method]pollable.ready", "method-pollable-ready"},
		{"[method]descriptor.is-same-object", "method-descriptor-is-same-object"},
		{"[static]descriptor.open-at", "static-descriptor-open-at"},
		{"[resource-drop]pollable", "resource-drop-pollable"},
		{"[constructor]stream", "constructor-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := witToKebabName(tt.input)
			if result != tt.expected {
				t.Errorf("witToKebabName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestKebabWitRoundtrip(t *testing.T) {
	// Test that converting to WIT and back preserves the original
	original := "method-descriptor-read"
	wit := kebabToWitName(original)
	back := witToKebabName(wit)
	if back != original {
		t.Errorf("roundtrip failed: %q -> %q -> %q", original, wit, back)
	}
}

func TestKebabToWitName_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Too short for method-
		{"method", "method"},
		// method- prefix but no dash after resource - still converts (single-word resource, no function)
		{"method-abc", "[method]abc"},
		// static- prefix but no dash after resource - still converts (single-word resource, no function)
		{"static-xyz", "[static]xyz"},
		// Too short for static-
		{"static", "static"},
		// Empty string
		{"", ""},
		// Just dashes
		{"-", "-"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := kebabToWitName(tt.input)
			if result != tt.expected {
				t.Errorf("kebabToWitName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestKebabToWitName_MultiWordResources(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Multi-word resources should be recognized
		{"method-outgoing-response-set-status-code", "[method]outgoing-response.set-status-code"},
		{"method-outgoing-response-body", "[method]outgoing-response.body"},
		{"method-outgoing-body-write", "[method]outgoing-body.write"},
		{"static-outgoing-body-finish", "[static]outgoing-body.finish"},
		{"static-response-outparam-set", "[static]response-outparam.set"},
		{"method-input-stream-read", "[method]input-stream.read"},
		{"method-output-stream-write", "[method]output-stream.write"},
		{"method-tcp-socket-connect", "[method]tcp-socket.connect"},
		{"constructor-outgoing-response", "[constructor]outgoing-response"},
		{"resource-drop-outgoing-response", "[resource-drop]outgoing-response"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := kebabToWitName(tt.input)
			if result != tt.expected {
				t.Errorf("kebabToWitName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWitToKebabName_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"[]", ""},
		{".", ""},
		{"[", ""},
		{"]", ""},
		{"a", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := witToKebabName(tt.input)
			if result != tt.expected {
				t.Errorf("witToKebabName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
