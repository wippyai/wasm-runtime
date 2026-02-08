// Package engine orchestrates asyncify transformation.
//
// Transformation pipeline:
//  1. Parse module, build call graph, identify async functions
//  2. Add globals, allocate scratch locals
//  3. Compute live locals at each async call site
//  4. Apply asyncify instrumentation to each function
//  5. Add exports, encode output
package engine
