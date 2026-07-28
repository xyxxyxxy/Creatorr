package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendCookiesAndAuth(t *testing.T) {
	args := appendCookiesAndAuth(nil, "/tmp/cookies.txt", options{username: "u@example.com", password: "secret"})
	if len(args) != 6 || args[0] != "--cookies" || args[2] != "--username" || args[3] != "u@example.com" || args[4] != "--password" || args[5] != "secret" {
		t.Fatalf("cookies+user: %v", args)
	}
	args = appendCookiesAndAuth(nil, "", options{username: "u@example.com", password: "secret"})
	if len(args) != 4 || args[0] != "--username" || args[3] != "secret" {
		t.Fatalf("auth only: %v", args)
	}
	args = appendCookiesAndAuth(nil, "", options{username: "  ", password: "x"})
	if len(args) != 0 {
		t.Fatalf("blank username: %v", args)
	}
}

func TestRateOff(t *testing.T) {
	cases := map[string]bool{
		"": true, "0": true, "off": true, "None": true, "UNLIMITED": true,
		"1M": false, "500K": false,
	}
	for in, want := range cases {
		if got := rateOff(in); got != want {
			t.Errorf("rateOff(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSecondsOff(t *testing.T) {
	cases := map[string]bool{
		"": true, "0": true, "0.0": true, "-1": true,
		"1": false, "0.5": false, "2": false,
	}
	for in, want := range cases {
		if got := secondsOff(in); got != want {
			t.Errorf("secondsOff(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAppendPaceFlags(t *testing.T) {
	args := appendPaceFlags(nil, "1M", "2")
	want := []string{
		"--limit-rate", "1M",
		"--sleep-requests", "2",
		"--sleep-subtitles", "2",
		"--sleep-interval", "2",
	}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
	}
}

func TestAppendPaceFlagsAllOff(t *testing.T) {
	args := appendPaceFlags(nil, "off", "0")
	if len(args) != 0 {
		t.Fatalf("args = %v, want empty", args)
	}
}

func TestAppendSubtitleFlags(t *testing.T) {
	if got := appendSubtitleFlags(nil, nil, true); len(got) != 0 {
		t.Fatalf("empty langs = %v", got)
	}
	got := appendSubtitleFlags(nil, []string{"en", "de"}, true)
	want := []string{"--write-subs", "--sub-langs", "en,de", "--convert-subs", "srt", "--write-auto-subs"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v want %v", got, want)
	}
	got = appendSubtitleFlags(nil, []string{"en"}, false)
	want = []string{"--write-subs", "--sub-langs", "en", "--convert-subs", "srt"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNormalizeFormat(t *testing.T) {
	if got := normalizeFormat(""); got != "bv*+ba/b" {
		t.Fatalf("normalizeFormat(\"\") = %q", got)
	}
	if got := normalizeFormat("bestvideo+bestaudio"); got != "bestvideo+bestaudio" {
		t.Fatalf("normalizeFormat = %q", got)
	}
	if got := normalizeFormat("bv*+ba/b"); got != "bv*+ba/b" {
		t.Fatalf("normalizeFormat should preserve profile fallbacks, got %q", got)
	}
}

func TestAppendPOTArgs(t *testing.T) {
	got := appendPOTArgs(nil, options{potFetch: "auto", potProviderURL: "http://creatorr-po-token:4416"})
	want := []string{
		"--extractor-args", "youtube:fetch_pot=auto,pot_trace=true",
		"--extractor-args", "youtubepot-bgutilhttp:base_url=http://creatorr-po-token:4416",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v want %v", got, want)
	}
	got = appendPOTArgs(nil, options{})
	if len(got) != 2 || got[1] != "youtube:fetch_pot=never" {
		t.Fatalf("empty URL should force never, got %v", got)
	}
	got = appendPOTArgs(nil, options{potFetch: "never", potProviderURL: "http://creatorr-po-token:4416"})
	if len(got) != 2 || got[1] != "youtube:fetch_pot=never" {
		t.Fatalf("never should omit pot_trace and base_url, got %v", got)
	}
}

func TestFindMediaSkipsHLSPlaylist(t *testing.T) {
	dir := t.TempDir()
	playlist := filepath.Join(dir, "clip [id].mp4")
	real := filepath.Join(dir, "other [id2].mp4")
	if err := os.WriteFile(playlist, []byte("#EXTM3U\n#EXT-X-VERSION:6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("not-a-playlist"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := findMedia(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != real {
		t.Fatalf("got %q want %q", got, real)
	}
}
