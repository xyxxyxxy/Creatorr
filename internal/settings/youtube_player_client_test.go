package settings

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

func TestNormalizeYoutubePlayerClient(t *testing.T) {
	if got := NormalizeYoutubePlayerClient(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := NormalizeYoutubePlayerClient("  mweb , tv , default "); got != "mweb,tv,default" {
		t.Fatalf("collapse = %q", got)
	}
}

func TestValidateYoutubePlayerClient(t *testing.T) {
	if err := validateYoutubePlayerClient("tv,default"); err != nil {
		t.Fatal(err)
	}
	if err := validateYoutubePlayerClient("mweb;evil"); err == nil {
		t.Fatal("expected reject ;")
	}
	if err := validateYoutubePlayerClient("web=1"); err == nil {
		t.Fatal("expected reject =")
	}
	if err := validateYoutubePlayerClient("bad-name"); err == nil {
		t.Fatal("expected reject hyphen")
	}
}

func TestEffectiveYoutubePlayerClient(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	got, err := EffectiveYoutubePlayerClient(d)
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultYoutubePlayerClient {
		t.Fatalf("seed = %q", got)
	}
	if err := Set(d, KeyYoutubePlayerClient, "mweb,tv,default"); err != nil {
		t.Fatal(err)
	}
	got, err = EffectiveYoutubePlayerClient(d)
	if err != nil {
		t.Fatal(err)
	}
	if got != "mweb,tv,default" {
		t.Fatalf("set = %q", got)
	}
}
