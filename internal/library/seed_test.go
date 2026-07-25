package library_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSeedDefaults(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	rootPath := filepath.Join(dir, "library")
	cfg := config.Config{LibraryRoot: rootPath}
	if err := library.SeedDefaults(d, cfg); err != nil {
		t.Fatal(err)
	}
	// idempotent
	if err := library.SeedDefaults(d, cfg); err != nil {
		t.Fatal(err)
	}

	var roots, profiles int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM root_folders`).Scan(&roots)
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM quality_profiles`).Scan(&profiles)
	if roots != 1 || profiles != 3 {
		t.Fatalf("roots=%d profiles=%d", roots, profiles)
	}
	var name, path string
	var ttl any
	_ = d.SQL.QueryRow(`SELECT name, path, retention_ttl_seconds FROM root_folders`).Scan(&name, &path, &ttl)
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	wantName := filepath.Base(absRoot)
	if name != wantName || path != absRoot || ttl != nil {
		t.Fatalf("root name=%q path=%q ttl=%v want name %q path %q", name, path, ttl, wantName, absRoot)
	}

	rows, err := d.SQL.Query(`SELECT id, name, format_selector FROM quality_profiles ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []struct{ name, format string }{
		{library.DefaultProfileName, library.DefaultFormat},
		{library.Profile1080Name, library.Profile1080Format},
		{library.Profile720Name, library.Profile720Format},
	}
	i := 0
	for rows.Next() {
		if i >= len(want) {
			t.Fatalf("extra profile row")
		}
		var id int64
		var pname, fmtSel string
		if err := rows.Scan(&id, &pname, &fmtSel); err != nil {
			t.Fatal(err)
		}
		if pname != want[i].name || fmtSel != want[i].format {
			t.Fatalf("profile[%d] %q %q", i, pname, fmtSel)
		}
		if i == 0 && id != 1 {
			t.Fatalf("best should be id 1, got %d", id)
		}
		i++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if i != len(want) {
		t.Fatalf("got %d profiles, want %d", i, len(want))
	}
}
