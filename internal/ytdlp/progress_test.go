package ytdlp

import "testing"

func TestParseProgressPercent(t *testing.T) {
	msg, frac := parseProgress("[download]  42.5% of 10.00MiB at 1.00MiB/s ETA 00:05")
	if frac == nil {
		t.Fatal("expected a fraction, got nil")
	}
	if *frac < 0.424 || *frac > 0.426 {
		t.Fatalf("fraction = %v, want ~0.425", *frac)
	}
	if msg != "Downloading 42% · 1.00MiB/s" {
		t.Fatalf("message = %q, want speed suffix", msg)
	}
}

func TestParseProgressDestinationNoFraction(t *testing.T) {
	msg, frac := parseProgress("[download] Destination: video.mkv")
	if frac != nil {
		t.Fatalf("expected nil fraction, got %v", *frac)
	}
	if msg == "" {
		t.Fatal("expected a non-empty message")
	}
}

func TestParseProgressMerging(t *testing.T) {
	msg, frac := parseProgress("[Merger] Merging formats into \"video.mkv\"")
	if frac != nil {
		t.Fatalf("expected nil fraction, got %v", *frac)
	}
	if msg != "Merging…" {
		t.Fatalf("message = %q, want Merging…", msg)
	}
}

func TestParseProgressIgnoresNoise(t *testing.T) {
	msg, frac := parseProgress("some unrelated log line")
	if msg != "" || frac != nil {
		t.Fatalf("expected empty result, got msg=%q frac=%v", msg, frac)
	}
}

func TestParseProgressClampsFraction(t *testing.T) {
	msg, frac := parseProgress("[download] 100.0% of 5.00MiB")
	if frac == nil || *frac != 1.0 {
		t.Fatalf("expected fraction 1.0, got %v", frac)
	}
	if msg != "Downloading 100%" {
		t.Fatalf("message = %q, want no speed when yt-dlp omits at", msg)
	}
}

func TestParseDownloadSpeed(t *testing.T) {
	if got := parseDownloadSpeed("[download]  42.0% of 10.00MiB at 1.00MiB/s ETA 00:05"); got != "1.00MiB/s" {
		t.Fatalf("got %q", got)
	}
	if got := parseDownloadSpeed("[download]  50.0% of 1.00MiB at 500.00KiB/s ETA 00:01"); got != "500.00KiB/s" {
		t.Fatalf("got %q", got)
	}
	if got := parseDownloadSpeed("[download] 100.0% of 5.00MiB"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
