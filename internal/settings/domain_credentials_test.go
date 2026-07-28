package settings_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestCredentialsHostOverrideOnly(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}

	// Legacy default credentials must not apply to hosts.
	if err := settings.SaveDefaultCredentials(d, "def@example.com", "def-pass", false); err != nil {
		t.Fatal(err)
	}
	creds, err := settings.CredentialsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "" || creds.FromHost {
		t.Fatalf("no host row: %+v", creds)
	}

	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := settings.SaveHostCredentials(d, "example.com", "host@example.com", "host-pass", false, false); err != nil {
		t.Fatal(err)
	}
	creds, err = settings.CredentialsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "host@example.com" || creds.Password != "host-pass" || !creds.FromHost {
		t.Fatalf("host override: %+v", creds)
	}

	if err := settings.SaveHostCredentials(d, "example.com", "", "", true, false); err != nil {
		t.Fatal(err)
	}
	creds, err = settings.CredentialsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "" || creds.FromHost {
		t.Fatalf("after clear inherit: %+v", creds)
	}

	if err := settings.SaveHostCredentials(d, "example.com", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	creds, err = settings.CredentialsForDomain(d, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "" || !creds.FromHost {
		t.Fatalf("explicit none on host: %+v", creds)
	}
}

func TestSaveDefaultCredentialsKeepPassword(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := settings.SaveDefaultCredentials(d, "a@example.com", "secret", false); err != nil {
		t.Fatal(err)
	}
	if err := settings.SaveDefaultCredentials(d, "a@example.com", "", true); err != nil {
		t.Fatal(err)
	}
	_, pass, err := settings.DefaultCredentials(d)
	if err != nil {
		t.Fatal(err)
	}
	if pass != "secret" {
		t.Fatalf("password = %q", pass)
	}
}

func TestCredentialsForURL(t *testing.T) {
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
	if err := settings.SaveHostCredentials(d, "example.com", "u@example.com", "p", false, false); err != nil {
		t.Fatal(err)
	}
	creds, err := settings.CredentialsForURL(d, "https://example.com/watch/1")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "u@example.com" || creds.Password != "p" {
		t.Fatalf("got %+v", creds)
	}
}
