package domains_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestFlareSolverrURLPerDomain(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "")

	url, err := domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "" {
		t.Fatalf("default off: got %q, %v", url, err)
	}

	err = domains.UpdateLimits(d, "example.com", "", "", "", "on")
	if err == nil || !strings.Contains(err.Error(), "CREATORR_FLARESOLVERR_URL") {
		t.Fatalf("opt-in without URL: want env required, got %v", err)
	}
	err = settings.SetDomainDefault(d, 30, 8, 1, "10M", "1", true)
	if err == nil || !strings.Contains(err.Error(), "CREATORR_FLARESOLVERR_URL") {
		t.Fatalf("default on without URL: want env required, got %v", err)
	}

	t.Setenv(settings.EnvFlareSolverrURL, "http://flaresolverr.example.com")
	if err := domains.UpdateLimits(d, "example.com", "", "", "", "on"); err != nil {
		t.Fatal(err)
	}
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "http://flaresolverr.example.com" {
		t.Fatalf("opt-in with URL: got %q, %v", url, err)
	}

	// Off stores NULL (same as inherit); with defaults Flare always off that clears pre-solve.
	if err := domains.UpdateLimits(d, "example.com", "", "", "", "off"); err != nil {
		t.Fatal(err)
	}
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "" {
		t.Fatalf("off→NULL: got %q, %v", url, err)
	}
	meta, ok, err := domains.Get(d, "example.com")
	if err != nil || !ok {
		t.Fatalf("get host: ok=%v err=%v", ok, err)
	}
	if meta.UseFlareSolverr.Valid {
		t.Fatalf("off should store NULL, got Valid=%v Bool=%v", meta.UseFlareSolverr.Valid, meta.UseFlareSolverr.Bool)
	}

	if err := domains.UpdateLimits(d, "example.com", "", "", "", "on"); err != nil {
		t.Fatal(err)
	}
	if err := domains.UpdateLimits(d, "example.com", "", "", "", "default"); err != nil {
		t.Fatal(err)
	}
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "" {
		t.Fatalf("inherit default off: got %q, %v", url, err)
	}

	// Missing host row uses default (off)
	_ = domains.Delete(d, "example.com")
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "" {
		t.Fatalf("missing row inherits default off: got %q, %v", url, err)
	}
}

func TestClearUseFlareSolverr(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://flaresolverr.example.com")
	if err := settings.SetDomainDefault(d, 30, 8, 1, "10M", "1", true); err != nil {
		t.Fatal(err)
	}
	if err := domains.UpdateHostOverrides(d, "example.com", "", "", "", "", "", "on"); err != nil {
		t.Fatal(err)
	}

	t.Setenv(settings.EnvFlareSolverrURL, "")
	if err := settings.ClearUseFlareSolverr(d); err != nil {
		t.Fatal(err)
	}
	def, err := settings.DefaultLimits(d)
	if err != nil {
		t.Fatal(err)
	}
	if def.UseFlareSolverr {
		t.Fatal("default UseFlareSolverr still on after clear")
	}
	url, err := domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "" {
		t.Fatalf("host after clear: got %q, %v", url, err)
	}
	err = domains.UpdateHostOverrides(d, "example.com", "", "", "", "", "", "on")
	if err == nil || !strings.Contains(err.Error(), "CREATORR_FLARESOLVERR_URL") {
		t.Fatalf("re-enable without URL: got %v", err)
	}
}
