// Package component parses WebAssembly Component Model binaries.
//
// Use DecodeAndValidate to parse a component binary with full type resolution,
// or DecodeWithOptions for raw section access without validation overhead.
//
// Type indices in the binary reference into TypeIndexSpace, which is built
// incrementally as sections are parsed. Aliases can create forward references
// that require deferred resolution via typeAlias.
package component
