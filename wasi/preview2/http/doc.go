// Package http implements wasi:http@0.2.8 for WASI Preview2.
//
// Implements:
//   - wasi:http/types@0.2.8 - HTTP request/response types
//   - wasi:http/outgoing-handler@0.2.8 - HTTP client (outbound requests)
//   - wasi:http/incoming-handler@0.2.8 - HTTP server (inbound requests)
//
// Provides 100% spec compliance for HTTP types:
//   - Incoming requests: method, path, scheme, authority, headers, body
//   - Outgoing responses: status code, headers, body
//   - Outgoing requests: full HTTP client support
//   - Incoming responses: status, headers, body
//   - Fields: get, set, append, delete, entries, clone, has
package http
