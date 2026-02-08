package http

// IncomingHandlerNamespace is the WASI HTTP incoming handler namespace.
const IncomingHandlerNamespace = "wasi:http/incoming-handler@0.2.8"

// IncomingHandlerHost implements wasi:http/incoming-handler@0.2.8.
// This is an EXPORT interface - the WASM component exports the handle function.
// The host does not need to implement this, the component exports it.
type IncomingHandlerHost struct{}

// Namespace returns the WASI namespace.
func (h *IncomingHandlerHost) Namespace() string {
	return IncomingHandlerNamespace
}
