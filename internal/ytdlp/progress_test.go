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
	if msg == "" {
		t.Fatal("expected a non-empty message")
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
	_, frac := parseProgress("[download] 100.0% of 5.00MiB")
	if frac == nil || *frac != 1.0 {
		t.Fatalf("expected fraction 1.0, got %v", frac)
	}
}
