package queue

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

func TestCommandFingerprint(t *testing.T) {
	a := `ffprobe -v quiet -i "/library/A/ep.mkv"`
	b := `ffprobe -v quiet -i "/library/B/other.mkv"`
	if exectrace.Fingerprint(a) != exectrace.Fingerprint(b) {
		t.Fatalf("path templates should match: %q vs %q", exectrace.Fingerprint(a), exectrace.Fingerprint(b))
	}
	if !strings.Contains(exectrace.Fingerprint(a), "$PATH") {
		t.Fatalf("want $PATH in %q", exectrace.Fingerprint(a))
	}
}

func TestTaskCommandsDedupeAndFfprobeCap(t *testing.T) {
	c := newTaskCommands()
	c.Append(1, "ffprobe", `ffprobe -i "/a/1.mkv"`)
	c.Append(1, "ffprobe", `ffprobe -i "/a/2.mkv"`) // same template → count
	c.Append(1, "ffprobe", `ffprobe -show_format "/a/1.mkv"`)
	c.Append(1, "ffprobe", `ffprobe -show_streams "/a/1.mkv"`)
	c.Append(1, "ffprobe", `ffprobe -show_chapters "/a/1.mkv"`) // over cap → omitted
	got := c.Snapshot(1)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "×2 similar") {
		t.Fatalf("want dedupe count: %v", got)
	}
	if !strings.Contains(joined, "omitted") {
		t.Fatalf("want omitted marker: %v", got)
	}
	if len(got) != 4 {
		t.Fatalf("len=%d got=%v", len(got), got)
	}
}

func TestTaskCommandsHeadTail(t *testing.T) {
	c := newTaskCommands()
	for i := 0; i < taskCommandsHead+taskCommandsTail+5; i++ {
		c.Append(1, "yt-dlp", fmt.Sprintf("yt-dlp -J https://example.com/v%d", i))
	}
	got := c.Snapshot(1)
	if len(got) != taskCommandsHead+taskCommandsTail+1 { // + omitted line
		t.Fatalf("len=%d want %d+omitted got=%v", len(got), taskCommandsHead+taskCommandsTail, got)
	}
	if !strings.Contains(got[len(got)-1], "omitted") {
		t.Fatalf("last should be omitted: %v", got)
	}
}

func TestPersistCommandsOnStatus(t *testing.T) {
	if !PersistCommandsOnStatus(KindDownload, StatusDone) {
		t.Fatal("download done should persist")
	}
	if PersistCommandsOnStatus(KindRenameEpisodes, StatusDone) {
		t.Fatal("rename done should not persist")
	}
	if !PersistCommandsOnStatus(KindRenameEpisodes, StatusFailed) {
		t.Fatal("rename failed should persist")
	}
	if PersistCommandsOnStatus(KindSyncFiles, StatusCancelled) {
		t.Fatal("sync cancel should not persist")
	}
}
