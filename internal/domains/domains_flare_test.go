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
	defer d.Close()
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
	err = settings.SetDomainDefault(d, 30, 8, 1, "10M", "off", "1", true)
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

	if err := domains.UpdateLimits(d, "example.com", "", "", "", "off"); err != nil {
		t.Fatal(err)
	}
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "" {
		t.Fatalf("explicit off: got %q, %v", url, err)
	}

	// Default on + host inherit (NULL) → on
	if err := settings.SetDomainDefault(d, 30, 8, 1, "10M", "off", "1", true); err != nil {
		t.Fatal(err)
	}
	if err := domains.UpdateLimits(d, "example.com", "", "", "", "default"); err != nil {
		t.Fatal(err)
	}
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "http://flaresolverr.example.com" {
		t.Fatalf("inherit default on: got %q, %v", url, err)
	}

	// Explicit off still wins over default on
	if err := domains.UpdateLimits(d, "example.com", "", "", "", "off"); err != nil {
		t.Fatal(err)
	}
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "" {
		t.Fatalf("override off vs default on: got %q, %v", url, err)
	}

	// Missing host row uses default
	_ = domains.Delete(d, "example.com")
	url, err = domains.FlareSolverrURL(d, "example.com")
	if err != nil || url != "http://flaresolverr.example.com" {
		t.Fatalf("missing row inherits default on: got %q, %v", url, err)
	}
}

func TestClearUseFlareSolverr(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	t.Setenv(settings.EnvFlareSolverrURL, "http://flaresolverr.example.com")
	if err := settings.SetDomainDefault(d, 30, 8, 1, "10M", "off", "1", true); err != nil {
		t.Fatal(err)
	}
	if err := domains.UpdateHostOverrides(d, "example.com", "", "", "", "", "", "", "on"); err != nil {
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
	err = domains.UpdateHostOverrides(d, "example.com", "", "", "", "", "", "", "on")
	if err == nil || !strings.Contains(err.Error(), "CREATORR_FLARESOLVERR_URL") {
		t.Fatalf("re-enable without URL: got %v", err)
	}
}
