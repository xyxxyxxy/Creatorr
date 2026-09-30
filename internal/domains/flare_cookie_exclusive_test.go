package domains_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestRejectFlareWithCookies(t *testing.T) {
	if err := domains.RejectFlareWithCookies(false, "# Netscape\nx=1\n"); err != nil {
		t.Fatal(err)
	}
	if err := domains.RejectFlareWithCookies(true, ""); err != nil {
		t.Fatal(err)
	}
	if err := domains.RejectFlareWithCookies(true, "  "); err != nil {
		t.Fatal(err)
	}
	err := domains.RejectFlareWithCookies(true, "# Netscape\nx=1\n")
	if !errors.Is(err, domains.ErrFlareCookieExclusive) {
		t.Fatalf("got %v", err)
	}
}

func TestSetCookiesRejectsWhileFlareOn(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	t.Setenv(settings.EnvFlareSolverrURL, "http://flaresolverr.example.com")
	if err := domains.UpdateHostOverrides(database, "example.com", "", "", "", "", "", "on"); err != nil {
		t.Fatal(err)
	}
	err = domains.SetCookies(database, "example.com", "# Netscape\nhost=1\n")
	if !errors.Is(err, domains.ErrFlareCookieExclusive) {
		t.Fatalf("got %v", err)
	}
}

func TestUpdateHostOverridesRejectsFlareWithJar(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	t.Setenv(settings.EnvFlareSolverrURL, "http://flaresolverr.example.com")
	if err := domains.SetCookies(database, "example.com", "# Netscape\nhost=1\n"); err != nil {
		t.Fatal(err)
	}
	err = domains.UpdateHostOverrides(database, "example.com", "", "", "", "", "", "on")
	if !errors.Is(err, domains.ErrFlareCookieExclusive) {
		t.Fatalf("got %v", err)
	}
}
