// Package random implements WASI random number generation interfaces.
//
// Implements:
//   - wasi:random/random@0.2.0 - Cryptographically secure random bytes
//   - wasi:random/insecure@0.2.0 - Fast non-cryptographic random
//   - wasi:random/insecure-seed@0.2.0 - Random seed for hash tables
//
// Secure random uses crypto/rand. Insecure random uses math/rand.
package random
