package settings_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestNormalizeExternalBaseURL(t *testing.T) {
	if got := settings.NormalizeExternalBaseURL("  http://example.com:8787/  "); got != "http://example.com:8787" {
		t.Fatalf("got %q", got)
	}
}

func TestMigrateExternalBaseURLFromEnv(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREATORR_PUBLIC_BASE_URL", "http://creatorr.example.com:8787/")
	if err := settings.MigrateExternalBaseURLFromEnv(d); err != nil {
		t.Fatal(err)
	}
	got, err := settings.ExternalBaseURL(d)
	if err != nil || got != "http://creatorr.example.com:8787" {
		t.Fatalf("got %q %v", got, err)
	}
	// Second migrate leaves settings value (does not overwrite).
	t.Setenv("CREATORR_PUBLIC_BASE_URL", "http://other.example.com:9")
	if err := settings.MigrateExternalBaseURLFromEnv(d); err != nil {
		t.Fatal(err)
	}
	got, err = settings.ExternalBaseURL(d)
	if err != nil || got != "http://creatorr.example.com:8787" {
		t.Fatalf("overwrite? %q %v", got, err)
	}
}
