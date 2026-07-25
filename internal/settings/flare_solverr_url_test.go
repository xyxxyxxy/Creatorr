package settings_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestNormalizeFlareSolverrURL(t *testing.T) {
	if got := settings.NormalizeFlareSolverrURL("  http://flaresolverr.example.com:8191/  "); got != "http://flaresolverr.example.com:8191" {
		t.Fatalf("got %q", got)
	}
}

func TestMigrateFlareSolverrURLFromEnv(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://creatorr-flaresolverr:8191/")
	if err := settings.MigrateFlareSolverrURLFromEnv(d); err != nil {
		t.Fatal(err)
	}
	got, err := settings.FlareSolverrURL(d)
	if err != nil || got != "http://creatorr-flaresolverr:8191" {
		t.Fatalf("got %q %v", got, err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://other.example.com:9")
	if err := settings.MigrateFlareSolverrURLFromEnv(d); err != nil {
		t.Fatal(err)
	}
	got, err = settings.FlareSolverrURL(d)
	if err != nil || got != "http://creatorr-flaresolverr:8191" {
		t.Fatalf("overwrite? %q %v", got, err)
	}
}
