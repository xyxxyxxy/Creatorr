package settings

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

func TestNormalizePotFetch(t *testing.T) {
	if got := NormalizePotFetch(""); got != PotFetchAuto {
		t.Fatalf("empty = %q", got)
	}
	if got := NormalizePotFetch("always"); got != PotFetchAlways {
		t.Fatalf("always = %q", got)
	}
	if got := NormalizePotFetch("bogus"); got != PotFetchAuto {
		t.Fatalf("bogus = %q", got)
	}
}

func TestEffectivePotFetch(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	mode, err := EffectivePotFetch(d, "")
	if err != nil || mode != PotFetchNever {
		t.Fatalf("empty URL: mode=%q err=%v", mode, err)
	}
	if err := Set(d, KeyPotFetch, PotFetchAlways); err != nil {
		t.Fatal(err)
	}
	mode, err = EffectivePotFetch(d, "http://creatorr-po-token:4416")
	if err != nil || mode != PotFetchAlways {
		t.Fatalf("with URL: mode=%q err=%v", mode, err)
	}
}
