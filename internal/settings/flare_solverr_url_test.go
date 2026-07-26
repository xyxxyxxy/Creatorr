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

func TestFlareSolverrURLFromEnv(t *testing.T) {
	t.Setenv(settings.EnvFlareSolverrURL, "http://creatorr-flaresolverr:8191/")
	if got := settings.FlareSolverrURL(); got != "http://creatorr-flaresolverr:8191" {
		t.Fatalf("got %q", got)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://other.example.com:9")
	if got := settings.FlareSolverrURL(); got != "http://other.example.com:9" {
		t.Fatalf("got %q", got)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "")
	if got := settings.FlareSolverrURL(); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestDropFlareSolverrURLSetting(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "flare_solverr_url", "http://old.example.com"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://creatorr-flaresolverr:8191")
	if err := settings.DropFlareSolverrURLSetting(d); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, "flare_solverr_url").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("legacy key still present: %d", n)
	}
	if err := settings.EnsureDefaultDomain(d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`UPDATE domains SET use_flaresolverr = 1 WHERE domain = ?`, settings.DomainDefault); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "")
	if err := settings.DropFlareSolverrURLSetting(d); err != nil {
		t.Fatal(err)
	}
	def, err := settings.DefaultLimits(d)
	if err != nil {
		t.Fatal(err)
	}
	if def.UseFlareSolverr {
		t.Fatal("Use FlareSolverr still on after empty env")
	}
}
