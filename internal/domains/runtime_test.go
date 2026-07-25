package domains_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestSetPausedDoesNotCreateDomainsRow(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)

	if err := domains.SetPaused(d, "example.com", true); err != nil {
		t.Fatal(err)
	}
	paused, err := domains.IsPaused(d, "example.com")
	if err != nil || !paused {
		t.Fatalf("paused=%v err=%v", paused, err)
	}
	var n int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM domains WHERE domain = ?`, "example.com").Scan(&n)
	if n != 0 {
		t.Fatalf("domains row created: count=%d", n)
	}

	if err := domains.SetPaused(d, "example.com", false); err != nil {
		t.Fatal(err)
	}
	paused, err = domains.IsPaused(d, "example.com")
	if err != nil || paused {
		t.Fatalf("after resume paused=%v err=%v", paused, err)
	}
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM domain_runtime WHERE domain = ?`, "example.com").Scan(&n)
	if n != 0 {
		t.Fatalf("runtime row should be gone: count=%d", n)
	}
}

func TestSetPausedRejectsInvalid(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	if err := domains.SetPaused(d, "example,com", true); err == nil {
		t.Fatal("expected invalid hostname rejected")
	}
	if err := domains.SetPaused(d, "default", true); err == nil {
		t.Fatal("expected reserved rejected")
	}
}
