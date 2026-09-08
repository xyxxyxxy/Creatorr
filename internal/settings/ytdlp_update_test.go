package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

func TestYtDlpUpdatesEnabled(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ytdlp-enabled.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	on, err := YtDlpUpdatesEnabled(d)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Fatal("expected enabled with default @weekly")
	}
	if err := Set(d, KeyYtDlpUpdateCron, ""); err != nil {
		t.Fatal(err)
	}
	on, err = YtDlpUpdatesEnabled(d)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Fatal("expected disabled with empty cron")
	}
}

func TestValidateYtDlpUpdateChannel(t *testing.T) {
	if err := validateYtDlpUpdateChannel("stable"); err != nil {
		t.Fatal(err)
	}
	if err := validateYtDlpUpdateChannel("nightly"); err != nil {
		t.Fatal(err)
	}
	if err := validateYtDlpUpdateChannel("beta"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSyncYtDlpInstalledVersion(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ytdlp-ver.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := SyncYtDlpInstalledVersion(d, "2026.09.01"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(d, KeyYtDlpInstalledVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026.09.01" {
		t.Fatalf("version = %q", got)
	}
	at, err := Get(d, KeyYtDlpInstalledAt)
	if err != nil {
		t.Fatal(err)
	}
	if at != "" {
		t.Fatalf("installed_at should stay empty on boot sync, got %q", at)
	}
	if err := RecordYtDlpInstall(d, "2026.09.02"); err != nil {
		t.Fatal(err)
	}
	at, err = Get(d, KeyYtDlpInstalledAt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(at) == "" {
		t.Fatal("installed_at should be set by RecordYtDlpInstall")
	}
	if err := SyncYtDlpInstalledVersion(d, "2026.09.03"); err != nil {
		t.Fatal(err)
	}
	at2, err := Get(d, KeyYtDlpInstalledAt)
	if err != nil {
		t.Fatal(err)
	}
	if at2 != at {
		t.Fatalf("boot sync must not change installed_at: %q -> %q", at, at2)
	}
}
