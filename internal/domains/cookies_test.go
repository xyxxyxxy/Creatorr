package domains_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestCookieJarHostOnlyNoDefault(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()

	dir := t.TempDir()
	path, err := domains.TempJarForURL(database, dir, "https://www.example.com/v")
	if err != nil || path != "" {
		t.Fatalf("expected empty jar, got %q err=%v", path, err)
	}

	if err := domains.SetCookies(database, settings.DomainDefault, "# Netscape\ndefault=1\n"); err == nil {
		t.Fatal("default jar must be rejected")
	}
	_ = domains.ClearCookies(database, settings.DomainDefault)

	path, err = domains.TempJarForURL(database, dir, "https://www.example.com/v")
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("default jar must not apply, got %q", path)
	}

	if err := domains.SetCookies(database, "example.com", "# Netscape\nhost=1\n"); err != nil {
		t.Fatal(err)
	}
	path, err = domains.TempJarForURL(database, dir, "https://www.example.com/v")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected host jar path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "# Netscape\nhost=1\n" {
		t.Fatalf("host jar should win, got %q", b)
	}

	ok, tip, err := domains.CookiesApply(database, "example.com")
	if err != nil || !ok || tip != "Cookies" {
		t.Fatalf("CookiesApply host: ok=%v tip=%q err=%v", ok, tip, err)
	}
	_ = domains.ClearCookies(database, "example.com")
	ok, tip, err = domains.CookiesApply(database, "example.com")
	if err != nil || ok || tip != "" {
		t.Fatalf("CookiesApply after clear: ok=%v tip=%q err=%v", ok, tip, err)
	}
}
