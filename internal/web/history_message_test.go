package web

import (
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestHistoryListMessagePrefersShortError(t *testing.T) {
	t.Parallel()
	got := historyListMessage(queue.Task{
		Status:       queue.StatusFailed,
		Message:      "Download failed",
		ErrorMessage: "ERROR: Unable to download webpage: [Errno -3] Temporary failure in name resolution",
	})
	if got != "ERROR: Unable to download webpage: [Errno -3] Temporary failure in name resolution" {
		t.Fatalf("got %q", got)
	}
}

func TestHistoryListMessageKeepsSpecificMessage(t *testing.T) {
	t.Parallel()
	got := historyListMessage(queue.Task{
		Status:       queue.StatusFailed,
		Message:      "Cookie / auth failure",
		ErrorMessage: "detail only",
	})
	if got != "Cookie / auth failure: detail only" {
		t.Fatalf("got %q", got)
	}
}

func TestHistoryListMessageDoneUnchanged(t *testing.T) {
	t.Parallel()
	got := historyListMessage(queue.Task{Status: queue.StatusDone, Message: "Scan: indexed 0 videos (0 new)"})
	if got != "Scan: indexed 0 videos (0 new)" {
		t.Fatalf("got %q", got)
	}
}

func TestHistoryListMessageOneLine(t *testing.T) {
	t.Parallel()
	got := historyListMessage(queue.Task{
		Status:       queue.StatusFailed,
		Message:      "yt-dlp download failed",
		ErrorMessage: "WARNING: [youtube] Failed to resolve\nERROR: second line that should not appear",
	})
	if strings.Contains(got, "\n") {
		t.Fatalf("newline in %q", got)
	}
	if strings.Contains(got, "second line") {
		t.Fatalf("kept second line: %q", got)
	}
	if !strings.HasPrefix(got, "yt-dlp download failed: WARNING:") {
		t.Fatalf("got %q", got)
	}
}
