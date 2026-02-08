// Package ir provides intermediate representation for asyncify transformation.
//
// Parses instruction sequences into a tree representing control flow
// (blocks, loops, if/else), then linearizes async-containing branches
// for instrumentation.
//
// # Linearization
//
// When a block/if/loop contains async calls:
//   - Result-bearing blocks become void blocks with result locals
//   - Branches targeting result blocks get local.set injected
//   - If/else with async in one branch gets flattened to sequential ifs
package ir
