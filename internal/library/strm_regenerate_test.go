package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestRewriteStreamFileSkipAndRewrite(t *testing.T) {
	s := openLib(t)
	if err := settings.Set(s.DB, settings.KeyExternalBaseURL, "http://creatorr.example.com:8787"); err != nil {
		t.Fatal(err)
	}
	tok, err := library.EnsureStreamToken(s.DB)
	if err != nil {
		t.Fatal(err)
	}

	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		DeliveryMode: "stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@s"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "T", WebpageURL: "https://www.example.com/watch?v=vid1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable' WHERE id = ?`, vid); err != nil {
		t.Fatal(err)
	}

	want, err := library.StreamURL("http://creatorr.example.com:8787", vid, tok)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	strmPath := filepath.Join(dir, "ep.strm")
	if err := os.WriteFile(strmPath, []byte(want+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'strm', ?, datetime('now'))`, vid, strmPath); err != nil {
		t.Fatal(err)
	}

	changed, err := s.RewriteStreamFile(vid)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected skip when matching")
	}

	newTok, err := library.RegenerateStreamToken(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = s.RewriteStreamFile(vid)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rewrite after token rotate")
	}
	body, err := os.ReadFile(strmPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), newTok) {
		t.Fatalf("strm missing new token: %s", body)
	}
	if strings.Contains(string(body), tok) {
		t.Fatal("old token still in strm")
	}
}

func TestRewriteStreamFileProgressiveKind(t *testing.T) {
	s := openLib(t)
	if err := settings.Set(s.DB, settings.KeyExternalBaseURL, "http://creatorr.example.com:8787"); err != nil {
		t.Fatal(err)
	}
	tok, err := library.EnsureStreamToken(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		DeliveryMode: "stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@s"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid2", Title: "T", WebpageURL: "https://www.example.com/watch?v=vid2",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable', stream_urls_kind = 'progressive' WHERE id = ?`, vid); err != nil {
		t.Fatal(err)
	}
	want, err := library.StreamURLForKind("http://creatorr.example.com:8787", vid, tok, "progressive")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	strmPath := filepath.Join(dir, "ep.strm")
	if err := os.WriteFile(strmPath, []byte("http://old.example.com/stream/videos/1/master.m3u8?token=old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'strm', ?, datetime('now'))`, vid, strmPath); err != nil {
		t.Fatal(err)
	}
	changed, err := s.RewriteStreamFile(vid)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rewrite to progressive URL")
	}
	body, err := os.ReadFile(strmPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != want {
		t.Fatalf("strm=%q want %q", body, want)
	}
}
