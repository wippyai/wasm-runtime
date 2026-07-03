// Package http implements wasi:http@0.2.8 for WASI Preview2.
//
// Implements:
//   - wasi:http/types@0.2.8 - HTTP request/response types
//   - wasi:http/outgoing-handler@0.2.8 - HTTP client (outbound requests)
//   - wasi:http/incoming-handler@0.2.8 - HTTP server (inbound requests)
//
// Implements wasi:http/types and wasi:http/outgoing-handler with core
// functionality. Some edge cases (trailers, informational responses) are
// not yet fully supported. Core functionality includes:
//   - Incoming requests: method, path, scheme, authority, headers, body
//   - Outgoing responses: status code, headers, body
//   - Outgoing requests: full HTTP client support
//   - Incoming responses: status, headers, body
//   - Fields: get, set, append, delete, entries, clone, has
package http
