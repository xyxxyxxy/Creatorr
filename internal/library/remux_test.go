package library_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestRemuxIfNeededSameExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, remuxed, err := library.RemuxIfNeeded(context.Background(), path)
	if err != nil || got != path || remuxed {
		t.Fatalf("got=%q remuxed=%v err=%v", got, remuxed, err)
	}
}

func TestRemuxIfNeededRecordsCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(path, []byte("not-media"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines []string
	ctx := exectrace.With(context.Background(), func(line string) {
		lines = append(lines, line)
	})
	_, _, _ = library.RemuxIfNeeded(ctx, path)
	if len(lines) != 1 {
		t.Fatalf("recorded %v", lines)
	}
	if !strings.HasPrefix(lines[0], "ffmpeg ") || !strings.Contains(lines[0], path) {
		t.Fatalf("line=%q", lines[0])
	}
	if !strings.Contains(lines[0], "-map 0:V") || !strings.Contains(lines[0], "-dn") {
		t.Fatalf("want video/audio map and -dn, got %q", lines[0])
	}
}
