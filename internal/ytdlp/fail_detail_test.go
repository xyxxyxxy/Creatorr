package ytdlp

import (
	"strings"
	"testing"
)

func TestFormatFailDetailPrefersErrorLine(t *testing.T) {
	raw := strings.Repeat("[download] 12.3% of 11.84MiB at 1.92MiB/s ETA 00:06\n", 40) +
		"ERROR: [youtube] abc123: Unable to download video\n" +
		"Some extractor note\n" +
		"[download] 99.0% of 11.84MiB at 2.00MiB/s ETA 00:00\n"
	got := formatFailDetail(raw)
	if !strings.Contains(got, "ERROR: [youtube] abc123: Unable to download video") {
		t.Fatalf("want ERROR line, got %q", got)
	}
	if strings.Contains(got, "% of") || strings.Contains(got, "ETA") {
		t.Fatalf("progress noise should be dropped, got %q", got)
	}
	if !strings.Contains(got, "Some extractor note") {
		t.Fatalf("want short context after ERROR, got %q", got)
	}
}

func TestFormatFailDetailWarningOnly(t *testing.T) {
	raw := "[download] Destination: clip.mp4\nWARNING: Falling back to generic extractor\n"
	got := formatFailDetail(raw)
	if !strings.HasPrefix(got, "WARNING:") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatFailDetailLineBoundaryTruncate(t *testing.T) {
	// No ERROR: long non-progress text must truncate on a line boundary.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("line number ")
		b.WriteString(strings.Repeat("x", 20))
		b.WriteByte('\n')
	}
	got := formatFailDetail(b.String())
	if len(got) > maxFailDetailLen {
		t.Fatalf("len=%d want <= %d", len(got), maxFailDetailLen)
	}
	if strings.HasPrefix(got, "ine number") || strings.HasPrefix(got, "umber") {
		t.Fatalf("mid-line cut: %q", got[:40])
	}
	if !strings.HasPrefix(got, "line number") {
		t.Fatalf("want full line start, got %q", got[:40])
	}
}

func TestFormatFailDetailProgressOnlyFallsBack(t *testing.T) {
	raw := strings.Repeat("[download] 0.1% of 11.84MiB at 2.99MiB/s ETA 00:03\n", 5) +
		"exit status 1"
	got := formatFailDetail(raw)
	if got != "exit status 1" {
		t.Fatalf("got %q", got)
	}
}

func TestWrapYtdlpFailUsesFormattedDetail(t *testing.T) {
	stderr := []byte(strings.Repeat("[download] 0.0% of 11.84MiB at 1.92MiB/s ETA 00:06\n", 30) +
		"ERROR: [youtube] abc123: The downloaded file is empty\n")
	ae := wrapYtdlpFail("DownloadFailed", "yt-dlp download failed", stderr, nil, nil)
	if ae == nil {
		t.Fatal("nil")
	}
	if !strings.Contains(ae.Detail, "ERROR: [youtube] abc123: The downloaded file is empty") {
		t.Fatalf("detail=%q", ae.Detail)
	}
	if strings.Contains(ae.Detail, "ETA") {
		t.Fatalf("progress in detail: %q", ae.Detail)
	}
	if ae.Message != "yt-dlp download failed" {
		t.Fatalf("message=%q", ae.Message)
	}
	if ae.Code != "DownloadFailed" {
		t.Fatalf("code=%q", ae.Code)
	}
}
