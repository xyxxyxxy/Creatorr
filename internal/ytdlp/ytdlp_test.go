package ytdlp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendCookiesAndAuth(t *testing.T) {
	args := appendCookiesAndAuth(nil, "/tmp/cookies.txt", options{username: "u@example.com", password: "secret"})
	if len(args) != 4 || args[0] != "--cookies" || args[2] != "--username" || args[3] != "u@example.com" {
		t.Fatalf("cookies+user: %v", args)
	}
	args = appendCookiesAndAuth(nil, "", options{username: "u@example.com", password: "secret"})
	if len(args) != 3 || args[0] != "--username" || args[2] != "secret" {
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

func TestNormalizeStreamFormat(t *testing.T) {
	want := "best[protocol^=http][protocol!*=m3u8]/best[protocol^=http]/b"
	if got := normalizeStreamFormat(""); got != want {
		t.Fatalf("empty = %q", got)
	}
	if got := normalizeStreamFormat("best"); got != want {
		t.Fatalf("best = %q", got)
	}
	if got := normalizeStreamFormat("bestvideo+bestaudio"); got != want {
		t.Fatalf("plus format = %q", got)
	}
	if got := normalizeStreamFormat("18"); got != "18/best" {
		t.Fatalf("progressive id = %q", got)
	}
}

func TestNormalizeHDFormat(t *testing.T) {
	if got := normalizeHDFormat(""); got != "bv*+ba/b" {
		t.Fatalf("empty = %q", got)
	}
	if got := normalizeHDFormat("best"); got != "bv*+ba/b" {
		t.Fatalf("best = %q", got)
	}
	if got := normalizeHDFormat("bv*[height<=1080]+ba/b"); got != "bv*[height<=1080]+ba/b" {
		t.Fatalf("1080p = %q", got)
	}
	got := normalizeStreamPlayFormat("bv*+ba")
	if !strings.Contains(got, "avc") || !strings.Contains(got, "bv*+ba") {
		t.Fatalf("stream play format: %q", got)
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

func TestStreamKindFromInfo(t *testing.T) {
	progressive := map[string]any{
		"url":          "https://cdn.example.com/v.mp4",
		"http_headers": map[string]any{"User-Agent": "ua"},
	}
	got, ok := streamKindFromInfo(progressive)
	if !ok || got["kind"] != "progressive" {
		t.Fatalf("progressive = %+v ok=%v", got, ok)
	}

	hls := map[string]any{
		"url":          "https://cdn.example.com/master.m3u8",
		"http_headers": map[string]any{"Referer": "https://example.com/"},
	}
	got, ok = streamKindFromInfo(hls)
	if !ok || got["kind"] != "hls" || got["url"] != hls["url"] {
		t.Fatalf("hls = %+v ok=%v", got, ok)
	}

	pipe := map[string]any{
		"requested_formats": []any{
			map[string]any{"url": "https://cdn.example.com/v.webm", "http_headers": map[string]any{"User-Agent": "v"}},
			map[string]any{"url": "https://cdn.example.com/a.m4a", "http_headers": map[string]any{"User-Agent": "a"}},
		},
	}
	got, ok = streamKindFromInfo(pipe)
	if !ok || got["kind"] != "pipe" {
		t.Fatalf("pipe = %+v ok=%v", got, ok)
	}
	if got["video_url"] != "https://cdn.example.com/v.webm" || got["audio_url"] != "https://cdn.example.com/a.m4a" {
		t.Fatalf("pipe urls = %+v", got)
	}

	// Separate A+V HLS without shared master must be pipe (not hls): otherwise CDN-first
	// would proxy video-only.
	hlsAV := map[string]any{
		"format_id": "hls-7130+hls-audio-audio",
		"requested_formats": []any{
			map[string]any{"url": "https://cdn.example.com/v.m3u8", "protocol": "m3u8_native"},
			map[string]any{"url": "https://cdn.example.com/a.m3u8", "protocol": "m3u8_native"},
		},
	}
	got, ok = streamKindFromInfo(hlsAV)
	if !ok || got["kind"] != "pipe" {
		t.Fatalf("hls A+V = %+v ok=%v", got, ok)
	}

	// Same master on both legs → CDN HLS (full VOD duration; no pipe EVENT growth).
	hlsShared := map[string]any{
		"format_id": "hls-7130+hls-audio-audio",
		"requested_formats": []any{
			map[string]any{
				"url": "https://cdn.example.com/v.m3u8", "protocol": "m3u8_native",
				"manifest_url": "https://cdn.example.com/master.m3u8",
				"http_headers": map[string]any{"User-Agent": "v"},
			},
			map[string]any{
				"url": "https://cdn.example.com/a.m3u8", "protocol": "m3u8_native",
				"manifest_url": "https://cdn.example.com/master.m3u8",
			},
		},
	}
	got, ok = streamKindFromInfo(hlsShared)
	if !ok || got["kind"] != "hls" || got["url"] != "https://cdn.example.com/master.m3u8" {
		t.Fatalf("shared HLS master = %+v ok=%v", got, ok)
	}

	// Top-level manifest_url wins even when format_id has '+'.
	hlsTop := map[string]any{
		"format_id":    "hls-7130+hls-audio-audio",
		"manifest_url": "https://cdn.example.com/from-top.m3u8",
		"requested_formats": []any{
			map[string]any{"url": "https://cdn.example.com/v.m3u8"},
			map[string]any{"url": "https://cdn.example.com/a.m3u8"},
		},
	}
	got, ok = streamKindFromInfo(hlsTop)
	if !ok || got["kind"] != "hls" || got["url"] != "https://cdn.example.com/from-top.m3u8" {
		t.Fatalf("top manifest_url = %+v ok=%v", got, ok)
	}

	mergedID := map[string]any{
		"format_id": "399+251",
		"url":       "https://cdn.example.com/should-not-use.mp4",
	}
	got, ok = streamKindFromInfo(mergedID)
	if !ok || got["kind"] != "pipe" {
		t.Fatalf("format_id plus = %+v ok=%v", got, ok)
	}
}

func TestHLSMasterFromInfo(t *testing.T) {
	info := map[string]any{
		"manifest_url": "https://cdn.example.com/index.m3u8",
		"http_headers": map[string]any{"User-Agent": "ua"},
	}
	u, h, ok := hlsMasterFromInfo(info)
	if !ok || u != "https://cdn.example.com/index.m3u8" || h["User-Agent"] != "ua" {
		t.Fatalf("manifest_url = %q headers=%+v ok=%v", u, h, ok)
	}

	info = map[string]any{
		"formats": []any{
			map[string]any{
				"protocol": "m3u8_native",
				"url":      "https://cdn.example.com/variant.m3u8",
			},
		},
	}
	u, _, ok = hlsMasterFromInfo(info)
	if !ok || u != "https://cdn.example.com/variant.m3u8" {
		t.Fatalf("formats scan = %q ok=%v", u, ok)
	}
}

func TestNeedsPipeStream(t *testing.T) {
	if !needsPipeStream(map[string]any{"requested_formats": []any{map[string]any{}, map[string]any{}}}) {
		t.Fatal("expected pipe for 2 requested_formats")
	}
	if needsPipeStream(map[string]any{"url": "https://cdn.example.com/v.mp4"}) {
		t.Fatal("expected no pipe for single progressive url")
	}
}

func TestAppendAVInputsSeekAfterInputs(t *testing.T) {
	args := appendAVInputs(nil, "https://cdn.example.com/v.mp4", "https://cdn.example.com/a.m4a",
		map[string]string{"User-Agent": "ua"}, map[string]string{"User-Agent": "ua"}, 20)
	var iVideo, iAudio, iSS = -1, -1, -1
	for i, a := range args {
		switch a {
		case "-i":
			if iVideo < 0 {
				iVideo = i
			} else {
				iAudio = i
			}
		case "-ss":
			iSS = i
		}
	}
	if iVideo < 0 || iAudio < 0 || iSS < 0 {
		t.Fatalf("missing markers in %v", args)
	}
	if iSS < iAudio {
		t.Fatalf("-ss must follow all -i (output seek); args=%v", args)
	}
	if args[iSS+1] != "20" {
		t.Fatalf("seek value %q", args[iSS+1])
	}
}

func TestProgressiveURLFromInfoSeparateStreams(t *testing.T) {
	info := map[string]any{
		"requested_formats": []any{
			map[string]any{"url": "https://cdn.example.com/v.webm"},
			map[string]any{"url": "https://cdn.example.com/a.m4a"},
		},
	}
	_, _, err := progressiveURLFromInfo(info)
	if !errors.Is(err, errSeparateStreams) {
		t.Fatalf("err = %v, want errSeparateStreams", err)
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
