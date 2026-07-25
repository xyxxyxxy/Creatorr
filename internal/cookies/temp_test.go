package cookies_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/cookies"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestTempJarFallsBackToDefault(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	dir := t.TempDir()
	path, err := cookies.TempJarForURL(database, dir, "https://www.example.com/v")
	if err != nil || path != "" {
		t.Fatalf("expected empty jar, got %q err=%v", path, err)
	}

	if err := cookies.Upsert(database, settings.DomainDefault, "# Netscape\ndefault=1\n"); err != nil {
		t.Fatal(err)
	}
	path, err = cookies.TempJarForURL(database, dir, "https://www.example.com/v")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected default jar path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "# Netscape\ndefault=1\n" {
		t.Fatalf("content=%q", b)
	}

	if err := cookies.Upsert(database, "example.com", "# Netscape\nhost=1\n"); err != nil {
		t.Fatal(err)
	}
	path, err = cookies.TempJarForURL(database, dir, "https://www.example.com/v")
	if err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "# Netscape\nhost=1\n" {
		t.Fatalf("host jar should win, got %q", b)
	}

	ok, tip, err := cookies.Applies(database, "example.com")
	if err != nil || !ok || tip != "Cookies" {
		t.Fatalf("Applies host: ok=%v tip=%q err=%v", ok, tip, err)
	}
	_ = cookies.Delete(database, "example.com")
	ok, tip, err = cookies.Applies(database, "example.com")
	if err != nil || !ok || tip != "Default cookies" {
		t.Fatalf("Applies default: ok=%v tip=%q err=%v", ok, tip, err)
	}
}
