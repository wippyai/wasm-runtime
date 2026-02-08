// Package layout provides Canonical ABI layout calculations for WIT types.
//
// This package computes size, alignment, and field offsets per the Component
// Model specification. These calculations determine how WIT types are represented
// in linear memory.
//
// # Layout Rules
//
// The Canonical ABI defines specific layout rules:
//   - Primitives: size equals alignment (u8=1, u32=4, u64=8, etc.)
//   - Records: fields laid out sequentially with padding for alignment
//   - Variants: discriminant followed by largest payload case
//   - Lists/Strings: (pointer, length) pair in memory, content elsewhere
//
// # Usage
//
//	info := layout.Calc(witType)
//	// info.Size, info.Align, info.Fields available
//
// This package is internal to the transcoder.
package layout
