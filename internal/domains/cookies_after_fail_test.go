package domains_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
)

func TestCookiesAfterFailDefaultOff(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "caf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()

	if err := domains.EnsureHost(database, "example.com"); err != nil {
		t.Fatal(err)
	}
	on, err := domains.CookiesAfterFail(database, "example.com")
	if err != nil || on {
		t.Fatalf("default cookies_after_fail: on=%v err=%v", on, err)
	}
	if err := domains.SetCookiesAfterFail(database, "example.com", true); err != nil {
		t.Fatal(err)
	}
	on, err = domains.CookiesAfterFail(database, "example.com")
	if err != nil || !on {
		t.Fatalf("after set: on=%v err=%v", on, err)
	}
	if domains.AllowStoredJar(true, false) {
		t.Fatal("after-fail non-retry must deny stored jar")
	}
	if !domains.AllowStoredJar(true, true) {
		t.Fatal("after-fail retry must allow stored jar")
	}
	if !domains.AllowStoredJar(false, false) {
		t.Fatal("flag off must allow stored jar")
	}

	if err := domains.SetCookies(database, "example.com", "# Netscape\nx=1\n"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path, err := domains.TempJarForNonDownload(database, dir, "https://www.example.com/v")
	if err != nil || path != "" {
		t.Fatalf("non-download with after-fail must omit jar, got %q err=%v", path, err)
	}
	path, had, err := domains.StoredJarForURL(database, dir, "https://www.example.com/v", false)
	if err != nil || path != "" || !had {
		t.Fatalf("StoredJar omit: path=%q had=%v err=%v", path, had, err)
	}
	path, had, err = domains.StoredJarForURL(database, dir, "https://www.example.com/v", true)
	if err != nil || path == "" || !had {
		t.Fatalf("StoredJar allow: path=%q had=%v err=%v", path, had, err)
	}
}

func TestCookieAttachStageMessage(t *testing.T) {
	st := domains.CookieAttachStatus{State: domains.CookieAttachRetried, RetryReason: "AgeRestricted"}
	if got := st.StageMessage(); got != "Retried with account cookies (AgeRestricted)" {
		t.Fatalf("got %q", got)
	}
	if !st.ShowStage() {
		t.Fatal("expected show stage")
	}
	off := domains.CookieAttachStatus{State: domains.CookieAttachOff}
	if off.ShowStage() {
		t.Fatal("off must not show stage")
	}
}
