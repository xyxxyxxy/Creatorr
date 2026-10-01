// Package testutil holds shared test openers. Do not import from package library
// internal tests (import cycle); use it from api, web, worker and similar packages.
package testutil

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// OpenDB opens a temp SQLite DB with settings defaults; closed on test cleanup.
func OpenDB(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = settings.SeedDefaults(d)
	return d
}

// OpenStores is OpenDB plus queue and library stores.
func OpenStores(t testing.TB) (*db.DB, *queue.Store, *library.Store) {
	t.Helper()
	d := OpenDB(t)
	q := queue.NewStore(d)
	return d, q, library.NewStore(d, q)
}
