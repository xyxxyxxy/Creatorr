package settings

import (
	"path/filepath"
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
