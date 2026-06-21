// Package types defines the compiled type structures for fast transcoding.
//
// CompiledType holds precomputed layout information (size, alignment, offsets)
// for efficient encoding/decoding of WIT types. By compiling type metadata once,
// the transcoder avoids repeated layout calculations during hot paths.
//
// # Key Types
//
//   - CompiledType: Cached type metadata with layout info
//   - Kind: Type discriminator (primitive, record, list, variant, etc.)
//
// This package is internal to the transcoder.
package types
