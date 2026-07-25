package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSeriesNFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvshow.nfo")
	err := WriteSeriesNFO(path, SeriesNFO{
		Title:     "Demo Show",
		SortTitle: "Show, Demo",
		Plot:      "About the show",
		Studio:    "Example",
		Genres:    []string{"Tech"},
		Premiered: "2020-01-15T12:00:00Z",
		Monitored: true,
		Actors:    []SeriesActor{{Name: "Creator", Role: "Host"}},
		UniqueIDType:  "site",
		UniqueIDValue: "UC123",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"<title>Demo Show</title>",
		"<sorttitle>Show, Demo</sorttitle>",
		"<premiered>2020-01-15</premiered>",
		"<year>2020</year>",
		"<status>Continuing</status>",
		"<genre>Tech</genre>",
		"<name>Creator</name>",
		`type="site"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestWriteSeriesNFOEnded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvshow.nfo")
	if err := WriteSeriesNFO(path, SeriesNFO{Title: "X", Monitored: false}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "<status>Ended</status>") {
		t.Fatalf("want Ended: %s", b)
	}
}

func TestBuildPrefetchDraftPlaylistNoArt(t *testing.T) {
	info := map[string]any{
		"extractor_key": "generic:playlist",
		"title":         "My Playlist",
		"description":   "Desc",
		"thumbnails": []any{
			map[string]any{"url": "https://example.com/t.jpg", "id": "avatar_uncropped", "width": 100.0, "height": 100.0},
		},
	}
	d := BuildPrefetchDraftFromInfo(info, t.TempDir())
	if !d.PlaylistOnly {
		t.Fatal("expected playlist only")
	}
	if d.Plot != "Desc" || d.OriginalTitle != "My Playlist" {
		t.Fatalf("draft=%+v", d)
	}
	if len(d.ArtFiles) != 0 {
		t.Fatalf("playlist should skip art: %+v", d.ArtFiles)
	}
	if got := SeriesTitleFromDraft(d); got != "My Playlist" {
		t.Fatalf("title=%q", got)
	}
}

func TestSeriesTitleFromDraft(t *testing.T) {
	if got := SeriesTitleFromDraft(PrefetchDraft{Studio: "Creator"}); got != "Creator" {
		t.Fatalf("studio fallback=%q", got)
	}
	if got := SeriesTitleFromDraft(PrefetchDraft{}); got != "" {
		t.Fatalf("empty=%q", got)
	}
}

// Regression: add-series fetch must keep art files alive until WriteAddSeriesDraft copies them.
func TestWriteAddSeriesDraftPersistsArt(t *testing.T) {
	s := &Store{CacheDir: t.TempDir()}
	tmp := t.TempDir()
	poster := filepath.Join(tmp, "poster.jpg")
	if err := os.WriteFile(poster, []byte("poster-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	const token = "abcd1234abcd1234abcd1234abcd1234"
	if err := s.WriteAddSeriesDraft(token, PrefetchDraft{
		OriginalTitle: "Channel",
		ArtFiles:      map[string]string{ArtPoster: poster},
	}); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(tmp) // simulate work-dir cleanup after persist
	got, err := s.ReadAddSeriesDraft(token)
	if err != nil {
		t.Fatal(err)
	}
	path, ok := got.ArtFiles[ArtPoster]
	if !ok || path == "" {
		t.Fatalf("poster not persisted: %+v", got.ArtFiles)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "poster-bytes" {
		t.Fatalf("poster content=%q", b)
	}
}

func TestClearPrefetchDraftRemovesArt(t *testing.T) {
	s := &Store{CacheDir: t.TempDir()}
	const seriesID, taskID int64 = 7, 42
	artDir := filepath.Join(s.CacheDir, "series-meta", "7", "art-42")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(artDir, "poster.jpg")
	if err := os.WriteFile(poster, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.WritePrefetchDraft(seriesID, taskID, PrefetchDraft{
		ArtFiles: map[string]string{ArtPoster: poster},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearPrefetchDraft(seriesID, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(poster); !os.IsNotExist(err) {
		t.Fatalf("poster still in cache: %v", err)
	}
	if _, err := os.Stat(s.prefetchDraftPath(seriesID, taskID)); !os.IsNotExist(err) {
		t.Fatalf("draft json still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.CacheDir, "series-meta", "7")); !os.IsNotExist(err) {
		t.Fatalf("empty series-meta dir should be pruned: %v", err)
	}
}

func TestClearAddSeriesDraftRemovesDir(t *testing.T) {
	s := &Store{CacheDir: t.TempDir()}
	const token = "abcd1234abcd1234abcd1234abcd1234"
	tmp := t.TempDir()
	poster := filepath.Join(tmp, "poster.jpg")
	if err := os.WriteFile(poster, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAddSeriesDraft(token, PrefetchDraft{
		ArtFiles: map[string]string{ArtPoster: poster},
	}); err != nil {
		t.Fatal(err)
	}
	dir := s.addSeriesDraftDir(token)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearAddSeriesDraft(token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("draft dir still present: %v", err)
	}
}

func TestWriteAddSeriesDraftSkipsMissingArt(t *testing.T) {
	s := &Store{CacheDir: t.TempDir()}
	const token = "deadbeefdeadbeefdeadbeefdeadbeef"
	if err := s.WriteAddSeriesDraft(token, PrefetchDraft{
		OriginalTitle: "Channel",
		ArtFiles:      map[string]string{ArtPoster: filepath.Join(t.TempDir(), "gone.jpg")},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadAddSeriesDraft(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ArtFiles) != 0 {
		t.Fatalf("missing art should be dropped: %+v", got.ArtFiles)
	}
}

func TestPickChannelArtURLs(t *testing.T) {
	poster, banner := pickChannelArtURLs(map[string]any{
		"thumbnails": []any{
			map[string]any{"url": "https://example.com/banner.jpg", "id": "banner_uncropped", "width": 1000.0, "height": 200.0},
			map[string]any{"url": "https://example.com/avatar.jpg", "id": "avatar_uncropped", "width": 200.0, "height": 200.0},
		},
	})
	if poster != "https://example.com/avatar.jpg" || banner != "https://example.com/banner.jpg" {
		t.Fatalf("poster=%q banner=%q", poster, banner)
	}
}

func TestParseActorsForm(t *testing.T) {
	got := ParseActorsForm("Alice\nBob|Host\n")
	if len(got) != 2 || got[0].Name != "Alice" || got[1].Role != "Host" {
		t.Fatalf("%+v", got)
	}
}

func TestParseActorsFromFields(t *testing.T) {
	got := ParseActorsFromFields([]string{"Alice", "", "Bob"}, []string{"Host", "x", "Guest"})
	if len(got) != 2 || got[0].Name != "Alice" || got[0].Role != "Host" || got[1].Name != "Bob" || got[1].Role != "Guest" {
		t.Fatalf("%+v", got)
	}
	if got[0].Order != 0 || got[1].Order != 1 {
		t.Fatalf("order %+v", got)
	}
	dup := ParseActorsFromFields([]string{"Alice", "alice", "Bob"}, []string{"Host", "Other", "Guest"})
	if len(dup) != 2 || dup[0].Name != "Alice" || dup[0].Role != "Host" || dup[1].Name != "Bob" {
		t.Fatalf("dedupe %+v", dup)
	}
}

func TestParseStringListFields(t *testing.T) {
	got := ParseStringListFields([]string{" Tech ", "tech", "News", ""})
	if len(got) != 2 || got[0] != "Tech" || got[1] != "News" {
		t.Fatalf("%+v", got)
	}
}

func TestListSeriesMetaFiles(t *testing.T) {
	dir := t.TempDir()
	if got := ListSeriesMetaFiles(dir); len(got) != 0 {
		t.Fatalf("empty dir: %+v", got)
	}
	nfo := filepath.Join(dir, "tvshow.nfo")
	if err := os.WriteFile(nfo, []byte("<tvshow/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(dir, "poster.jpg")
	if err := os.WriteFile(poster, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ListSeriesMetaFiles(dir)
	if len(got) != 2 {
		t.Fatalf("got %d files: %+v", len(got), got)
	}
	if got[0].Role != SeriesMetaFileRoleNFO || got[0].Path != nfo {
		t.Fatalf("nfo row: %+v", got[0])
	}
	if got[1].Role != ArtPoster || got[1].Path != poster {
		t.Fatalf("poster row: %+v", got[1])
	}
	if ResolveSeriesMetaFile(dir, "nfo") != nfo {
		t.Fatal("resolve nfo")
	}
	if ResolveSeriesMetaFile(dir, "poster") != poster {
		t.Fatal("resolve poster")
	}
	if ResolveSeriesMetaFile(dir, "banner") != "" {
		t.Fatal("missing banner should be empty")
	}
}
