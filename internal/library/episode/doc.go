// Package episode holds year-season helpers, rename/preview DTOs, and messages.
//
// Store-bound assign/reindex/apply/rename passes stay on library.Store (they need
// library path/NFO helpers). This package owns pure naming helpers used by those
// adapters without importing library (avoids an import cycle).
package episode
