// Package integrity holds file-integrity check types, hashing, and pure gates.
//
// Store-bound orchestration (enqueue, MarkVerified, RunIntegrityCheckVideo SQL)
// stays on library.Store as thin adapters that call into this package where
// possible. Subpackages cannot import library without an import cycle.
package integrity
