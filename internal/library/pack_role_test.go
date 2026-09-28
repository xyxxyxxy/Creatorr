package library_test

import (
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestDetectPackRoleFromPath(t *testing.T) {
	series := "/library/Show"
	cases := []struct {
		path string
		want string
	}{
		{series + "/S2026/S2026E0001 [id].mkv", library.PackRoleRegular},
		{series + "/Specials/S00E0001 [id].mkv", library.PackRoleSpecialEpisode},
		{series + "/Season 00/S00E0002 [x].mkv", library.PackRoleSpecialEpisode},
		{series + "/Specials/something.mkv", library.PackRoleRegular}, // no S00E
		{series + "/trailers/01 Trailer.mkv", "trailers"},
		{series + "/behind the scenes/01 BTS.mkv", "behind the scenes"},
		{series + "/S2026/trailers/01 Trailer.mkv", library.PackRoleRegular}, // season-nested ignored
	}
	for _, tc := range cases {
		got := library.DetectPackRoleFromPath(series, tc.path)
		if got != tc.want {
			t.Errorf("DetectPackRoleFromPath(%q)=%q want %q", tc.path, got, tc.want)
		}
	}
}

func TestFormatEpisodeNFODisplayTags(t *testing.T) {
	b := library.FormatEpisodeNFO(library.EpisodeNFO{
		Title: "Special", Season: 0, Episode: 1,
		PackRole: library.PackRoleSpecialEpisode,
		DisplaySeason: 2026, DisplayEpisode: 3, DisplayEpisodeSet: true,
	})
	s := string(b)
	if !strings.Contains(s, "<displayseason>2026</displayseason>") {
		t.Fatalf("missing displayseason: %s", s)
	}
	if !strings.Contains(s, "<displayepisode>3</displayepisode>") {
		t.Fatalf("missing displayepisode: %s", s)
	}
	b2 := library.FormatEpisodeNFO(library.EpisodeNFO{
		Title: "Special", Season: 0, Episode: 2,
		PackRole: library.PackRoleSpecialEpisode,
		DisplaySeason: 2026, DisplayEpisodeSet: false,
	})
	s2 := string(b2)
	if !strings.Contains(s2, "<displayseason>2026</displayseason>") {
		t.Fatalf("missing displayseason-only: %s", s2)
	}
	if strings.Contains(s2, "<displayepisode>") {
		t.Fatalf("unexpected displayepisode: %s", s2)
	}
}

func TestBuildEpisodePathsSpecialAndFeature(t *testing.T) {
	root := t.TempDir()
	cfg := library.NamingConfig{
		EpisodeFormat:        settings.DefaultEpisodeFormat,
		SpecialEpisodeFormat: settings.DefaultSpecialEpisodeFormat,
		SpecialFeatureFormat: settings.DefaultSpecialFeatureFormat,
	}
	sp, err := library.BuildEpisodePaths(root, library.EpisodeNFO{
		SeriesTitle: "Show", Title: "Bonus", Season: 0, Episode: 1,
		UniqueID: "abc", PackRole: library.PackRoleSpecialEpisode,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sp.EpisodeDir, "Specials") {
		t.Fatalf("special dir: %s", sp.EpisodeDir)
	}
	if !strings.HasPrefix(sp.Stem, "S00E0001") {
		t.Fatalf("special stem: %s", sp.Stem)
	}
	ft, err := library.BuildEpisodePaths(root, library.EpisodeNFO{
		SeriesTitle: "Show", Title: "My Trailer", Season: 2026, Episode: 1,
		PackRole: "trailers",
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(ft.EpisodeDir, "trailers") {
		t.Fatalf("feature dir: %s", ft.EpisodeDir)
	}
	if !strings.Contains(ft.Stem, "My Trailer") {
		t.Fatalf("feature stem: %s", ft.Stem)
	}
	// {pack-role} in regular format is dropped when empty.
	reg, err := library.BuildEpisodePaths(root, library.EpisodeNFO{
		SeriesTitle: "Show", Title: "Ep", Season: 2026, Episode: 3,
		UniqueID: "id", PackRole: library.PackRoleRegular,
	}, library.NamingConfig{EpisodeFormat: "{pack-role}/S{year}/E{episode:02}"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reg.EpisodeDir, "Specials") || strings.Contains(reg.PrimaryBase, "//") {
		t.Fatalf("regular with empty pack-role: %s", reg.PrimaryBase)
	}
	if !strings.Contains(reg.PrimaryBase, "S2026") {
		t.Fatalf("regular path: %s", reg.PrimaryBase)
	}
}

func TestPackRoleKindFolder(t *testing.T) {
	if got := library.PackRoleKindFolder(""); got != "" {
		t.Fatalf("empty legacy: %q", got)
	}
	if got := library.PackRoleKindFolder(library.PackRoleRegular); got != "" {
		t.Fatalf("episode: %q", got)
	}
	if got := library.PackRoleKindFolder(library.PackRoleSpecialEpisode); got != "Specials" {
		t.Fatalf("special: %q", got)
	}
	if got := library.PackRoleKindFolder("trailers"); got != "trailers" {
		t.Fatalf("feature: %q", got)
	}
}

func TestValidateEpisodeFormatRejectsReserved(t *testing.T) {
	if err := settings.ValidateEpisodeFormat("trailers/{title}"); err == nil {
		t.Fatal("expected reject trailers/")
	}
	if err := settings.ValidateEpisodeFormat("Specials/S00E{episode:04}"); err == nil {
		t.Fatal("expected reject Specials/")
	}
	if err := settings.ValidateEpisodeFormat(settings.DefaultEpisodeFormat); err != nil {
		t.Fatal(err)
	}
}
