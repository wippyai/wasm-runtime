package engine

// WIT-kebab name conversion utilities
// Consolidates name transformation logic used throughout the engine.

// Prefix constants for WIT resource operation names
const (
	prefixConstructor  = "constructor-"
	prefixResourceDrop = "resource-drop-"
	prefixMethod       = "method-"
	prefixStatic       = "static-"
)

// kebabToWitName converts kebab-case to WIT syntax
// Examples:
//   - "method-pollable-ready" -> "[method]pollable.ready"
//   - "static-descriptor-open-at" -> "[static]descriptor.open-at"
//   - "constructor-fields" -> "[constructor]fields"
//   - "resource-drop-fields" -> "[resource-drop]fields"
//   - "poll" -> "poll" (no change)
func kebabToWitName(kebab string) string {
	// constructor-{resource} -> [constructor]{resource}
	if len(kebab) > len(prefixConstructor) && kebab[:len(prefixConstructor)] == prefixConstructor {
		resource := kebab[len(prefixConstructor):]
		return "[constructor]" + resource
	}

	// resource-drop-{resource} -> [resource-drop]{resource}
	if len(kebab) > len(prefixResourceDrop) && kebab[:len(prefixResourceDrop)] == prefixResourceDrop {
		resource := kebab[len(prefixResourceDrop):]
		return "[resource-drop]" + resource
	}

	// method-{resource}-{function} -> [method]{resource}.{function}
	if len(kebab) > len(prefixMethod) && kebab[:len(prefixMethod)] == prefixMethod {
		rest := kebab[len(prefixMethod):]
		return "[method]" + splitResourceFunction(rest)
	}

	// static-{resource}-{function} -> [static]{resource}.{function}
	if len(kebab) > len(prefixStatic) && kebab[:len(prefixStatic)] == prefixStatic {
		rest := kebab[len(prefixStatic):]
		return "[static]" + splitResourceFunction(rest)
	}
	return kebab
}

// splitResourceFunction splits a kebab string into resource.function format.
// It tries to find the resource-function boundary by looking for known patterns.
// For example: "outgoing-response-set-status-code" -> "outgoing-response.set-status-code"
func splitResourceFunction(rest string) string {
	// Known multi-word resource names (hyphenated)
	knownResources := []string{
		"outgoing-response",
		"outgoing-body",
		"incoming-request",
		"incoming-response",
		"incoming-body",
		"response-outparam",
		"input-stream",
		"output-stream",
		"directory-entry-stream",
		"resolve-address-stream",
		"tcp-socket",
		"udp-socket",
		"incoming-datagram-stream",
		"outgoing-datagram-stream",
	}

	// Try known multi-word resources first
	for _, res := range knownResources {
		if len(rest) > len(res)+1 && rest[:len(res)] == res && rest[len(res)] == '-' {
			function := rest[len(res)+1:]
			return res + "." + function
		}
	}

	// Fall back to first-dash split
	idx := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '-' {
			idx = i
			break
		}
	}
	if idx > 0 {
		resource := rest[:idx]
		function := rest[idx+1:]
		return resource + "." + function
	}

	return rest
}

// witToKebabName converts WIT-style name to kebab-case
// e.g., "[method]descriptor.is-same-object" -> "method-descriptor-is-same-object"
func witToKebabName(wit string) string {
	result := make([]byte, 0, len(wit))
	for i := 0; i < len(wit); i++ {
		c := wit[i]
		switch c {
		case '[':
			// skip
		case ']':
			result = append(result, '-')
		case '.':
			result = append(result, '-')
		default:
			result = append(result, c)
		}
	}
	// Remove trailing dash if any
	if len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	// Remove leading dash if any
	if len(result) > 0 && result[0] == '-' {
		result = result[1:]
	}
	return string(result)
}
