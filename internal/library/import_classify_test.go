package library_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestNormalizeImportGroupStem(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"S2026E031700 [RjKoxm9643E1]", "s2026e031700"},
		{"s2026e031700", "s2026e031700"},
		{"  Foo  [bar]  Baz  ", "foo baz"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := library.NormalizeImportGroupStem(tc.in); got != tc.want {
			t.Fatalf("NormalizeImportGroupStem(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	// NFO + thumb stems from ClassifyImportFile must group together.
	_, nfoStem := library.ClassifyImportFile("S2026E031700 [RjKoxm9643E1].nfo")
	_, thumbStem := library.ClassifyImportFile("s2026e031700-thumb.jpg")
	if library.NormalizeImportGroupStem(nfoStem) != library.NormalizeImportGroupStem(thumbStem) {
		t.Fatalf("nfo stem %q and thumb stem %q should normalize equal", nfoStem, thumbStem)
	}
}

func TestImportSidecarStemMatchesMedia(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Ep One [id1].mkv")
	same := filepath.Join(dir, "Ep One [id1].nfo")
	bracketDiff := filepath.Join(dir, "Ep One.nfo") // same after normalize
	other := filepath.Join(dir, "Other Ep.nfo")
	otherDir := filepath.Join(t.TempDir(), "Ep One [id1].nfo")
	if !library.ImportSidecarStemMatchesMedia(same, media) {
		t.Fatal("exact stem should match")
	}
	if !library.ImportSidecarStemMatchesMedia(bracketDiff, media) {
		t.Fatal("normalized stem should match")
	}
	if library.ImportSidecarStemMatchesMedia(other, media) {
		t.Fatal("different basename must not match")
	}
	if library.ImportSidecarStemMatchesMedia(otherDir, media) {
		t.Fatal("different directory must not match")
	}
}
