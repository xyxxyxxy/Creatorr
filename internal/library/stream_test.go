package library_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestStreamURL(t *testing.T) {
	got, err := library.StreamURL("http://creatorr.example.com:8787", 42, "sectok")
	if err != nil {
		t.Fatal(err)
	}
	want := "http://creatorr.example.com:8787/stream/videos/42/master.m3u8?token=sectok"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	_, err = library.StreamURL("", 1, "t")
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("empty base: %v", err)
	}
	_, err = library.StreamURL("http://creatorr.example.com:8787", 1, "")
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("empty token: %v", err)
	}
}

func TestStreamTokenRoundtrip(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	tok1, err := library.EnsureStreamToken(d)
	if err != nil || tok1 == "" {
		t.Fatalf("ensure: %q %v", tok1, err)
	}
	tok2, err := library.EnsureStreamToken(d)
	if err != nil || tok2 != tok1 {
		t.Fatalf("stable: %q %q %v", tok1, tok2, err)
	}
	if !library.ValidStreamToken(d, tok1) {
		t.Fatal("valid token rejected")
	}
	if library.ValidStreamToken(d, "wrong") {
		t.Fatal("invalid token accepted")
	}
	if library.ValidStreamToken(nil, tok1) {
		t.Fatal("nil db accepted")
	}
	tok3, err := library.RegenerateStreamToken(d)
	if err != nil || tok3 == "" || tok3 == tok1 {
		t.Fatalf("regen: %q %v (old %q)", tok3, err, tok1)
	}
	if library.ValidStreamToken(d, tok1) {
		t.Fatal("old token still valid")
	}
	if !library.ValidStreamToken(d, tok3) {
		t.Fatal("new token rejected")
	}
}

func TestPackStreamWritesStrmAndNFO(t *testing.T) {
	root := t.TempDir()
	proxyURL := "http://creatorr.example.com:8787/stream/videos/7?token=abc"
	strmPath, nfoPath, thumbPath, subPaths, err := library.PackStream(proxyURL, root, library.EpisodeNFO{
		SeriesTitle:    "Show",
		Title:          "Episode",
		Season:         1,
		Episode:        3,
		Plot:           "plot",
		Aired:          "2024-03-01T12:00:00Z",
		UniqueID:       "rid7",
		SourceSite:     "creatorr",
		RuntimeSeconds: 125,
	}, library.NamingConfig{EpisodeFormat: library.DefaultEpisodeFormat}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if thumbPath != "" || len(subPaths) != 0 {
		t.Fatalf("unexpected thumb=%q subs=%v", thumbPath, subPaths)
	}
	strmBody, err := os.ReadFile(strmPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(strmBody)) != proxyURL {
		t.Fatalf("strm=%q", strmBody)
	}
	nfoBody, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(nfoBody)
	for _, want := range []string{"<showtitle>Show</showtitle>", "<title>Episode</title>", "rid7", "<runtime>3</runtime>", "<durationinseconds>125</durationinseconds>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("nfo missing %q in %s", want, body)
		}
	}
	if !strings.HasSuffix(strmPath, ".strm") || !strings.HasSuffix(nfoPath, ".nfo") {
		t.Fatalf("paths strm=%q nfo=%q", strmPath, nfoPath)
	}
}

func TestPackStreamCopiesSubtitleSidecars(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	src := filepath.Join(work, "meta.en.vtt")
	if err := os.WriteFile(src, []byte("WEBVTT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proxyURL := "http://creatorr.example.com:8787/stream/videos/9/master.m3u8?token=tok"
	strmPath, _, _, subPaths, err := library.PackStream(proxyURL, root, library.EpisodeNFO{
		SeriesTitle: "Show", Title: "Ep", Season: 2024, Episode: 1, UniqueID: "rid9", SourceSite: "creatorr",
	}, library.NamingConfig{EpisodeFormat: library.DefaultEpisodeFormat}, "", []string{src}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(subPaths) != 1 {
		t.Fatalf("subs=%v", subPaths)
	}
	if !strings.HasSuffix(subPaths[0], ".en.vtt") {
		t.Fatalf("sub path=%q", subPaths[0])
	}
	stem := strings.TrimSuffix(strmPath, ".strm")
	if subPaths[0] != stem+".en.vtt" {
		t.Fatalf("want beside strm: %q vs %q", stem+".en.vtt", subPaths[0])
	}
	b, err := os.ReadFile(subPaths[0])
	if err != nil || string(b) != "WEBVTT\n" {
		t.Fatalf("sub content=%q err=%v", b, err)
	}
}
