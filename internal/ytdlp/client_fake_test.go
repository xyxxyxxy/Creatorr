package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testdataFakeYtdlp(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	bin := filepath.Join(filepath.Dir(file), "testdata", "fake-yt-dlp")
	return bin
}

func TestClientListFakeYtdlp(t *testing.T) {
	c := &Client{Bin: testdataFakeYtdlp(t)}
	entries, err := c.List(context.Background(), ListOpts{
		URL: "https://example.com/playlist/PL_example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].ID != "vid1" || entries[0].Title != "First Video" {
		t.Fatalf("entry[0] = %+v", entries[0])
	}
	if entries[1].ID != "vid2" || entries[1].Title != "Second Video" {
		t.Fatalf("entry[1] = %+v", entries[1])
	}
	if entries[0].WebpageURL != "https://example.com/watch?v=vid1" {
		t.Fatalf("webpage_url = %q", entries[0].WebpageURL)
	}
}

func TestClientResolveFakeYtdlp(t *testing.T) {
	c := &Client{Bin: testdataFakeYtdlp(t)}
	e, err := c.Resolve(context.Background(), ResolveOpts{
		URL: "https://example.com/watch?v=vid1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "vid1" || e.Title != "First Video" {
		t.Fatalf("entry = %+v", e)
	}
	if e.Description != "Local fixture video" {
		t.Fatalf("description = %q", e.Description)
	}
	if e.Duration != 120 {
		t.Fatalf("duration = %v, want 120", e.Duration)
	}
	if e.ThumbnailURL != "https://example.com/thumb.jpg" {
		t.Fatalf("thumbnail = %q", e.ThumbnailURL)
	}
	if e.UploadDate != "2024-01-01T00:00:00Z" {
		t.Fatalf("upload_date = %q", e.UploadDate)
	}
}

func TestClientDownloadFakeYtdlp(t *testing.T) {
	c := &Client{Bin: testdataFakeYtdlp(t)}
	out := t.TempDir()
	path, err := c.Download(context.Background(), DownloadOpts{
		URL:    "https://example.com/watch?v=vid1",
		OutDir: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("empty media path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestFakeYtdlpVerifyBinary(t *testing.T) {
	ver, err := VerifyBinary(testdataFakeYtdlp(t))
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2024.01.01-fake" {
		t.Fatalf("version %q", ver)
	}
}
