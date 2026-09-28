package exectrace

import (
	"context"
	"strings"
	"testing"
)

func TestFingerprint(t *testing.T) {
	a := Fingerprint(`ffmpeg -i "/library/Series/S2019/a.mkv" -f null -`)
	b := Fingerprint(`ffmpeg -i "/library/Series/S2019/b.mkv" -f null -`)
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
	if !strings.Contains(a, "$PATH") {
		t.Fatalf("%q", a)
	}
	c := Fingerprint(`yt-dlp -J https://example.com/v`)
	if strings.Contains(c, "$PATH") {
		t.Fatalf("url should not become path: %q", c)
	}
}

func TestFormat(t *testing.T) {
	got := Format("yt-dlp", "-f", "bv*+ba/b", "https://example.com/v")
	want := `yt-dlp -f "bv*+ba/b" https://example.com/v`
	if got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}

	got = Format("ffmpeg", "-i", "/tmp/a b.mkv", "out.mkv")
	want = `ffmpeg -i "/tmp/a b.mkv" out.mkv`
	if got != want {
		t.Fatalf("Format spaces = %q, want %q", got, want)
	}

	if Format("") != "" {
		t.Fatal("empty bin should yield empty")
	}
	if Format("ffmpeg", "") != `ffmpeg ""` {
		t.Fatalf("empty arg: %q", Format("ffmpeg", ""))
	}
}

func TestFormatRedactsPassword(t *testing.T) {
	got := Format("/data/bin/yt-dlp", "--username", "a@example.com", "--password", "s3cret!", "-f", "b", "https://example.com/v")
	if strings.Contains(got, "s3cret") {
		t.Fatalf("password leaked: %q", got)
	}
	if !strings.Contains(got, "--password") || !strings.Contains(got, RedactedSecret) {
		t.Fatalf("want redacted password flag: %q", got)
	}
	got = Format("yt-dlp", "--password=s3cret!", "https://example.com/v")
	if strings.Contains(got, "s3cret") || !strings.Contains(got, "--password="+RedactedSecret) {
		t.Fatalf("equals form: %q", got)
	}
	got = Format("yt-dlp", "-p", "s3cret!", "https://example.com/v")
	if strings.Contains(got, "s3cret") || !strings.Contains(got, RedactedSecret) {
		t.Fatalf("short -p: %q", got)
	}
	// ffmpeg -p must not be treated as yt-dlp password.
	got = Format("ffmpeg", "-p", "not-a-secret", "out.mkv")
	if !strings.Contains(got, "not-a-secret") {
		t.Fatalf("ffmpeg -p should stay: %q", got)
	}
}

func TestRedactLine(t *testing.T) {
	in := `$ /data/bin/yt-dlp --username a@example.com --password "s3cret!" -f b https://example.com/v`
	got := RedactLine(in)
	if strings.Contains(got, "s3cret") {
		t.Fatalf("leaked: %q", got)
	}
	if !strings.Contains(got, "--password "+RedactedSecret) {
		t.Fatalf("got %q", got)
	}
	// Idempotent.
	if again := RedactLine(got); again != got {
		t.Fatalf("idempotent: %q vs %q", again, got)
	}
	plain := "Downloading 22%"
	if RedactLine(plain) != plain {
		t.Fatalf("plain progress changed")
	}
}

func TestRecordNoopWithoutRecorder(t *testing.T) {
	Record(context.Background(), "ffmpeg", "-version")
	Record(context.TODO(), "ffmpeg", "-version")
}

func TestRecordWithRecorder(t *testing.T) {
	var got []string
	ctx := With(context.Background(), func(bin, line string) {
		got = append(got, bin+"|"+line)
	})
	Record(ctx, "ffprobe", "-v", "quiet", "x.mkv")
	if len(got) != 1 {
		t.Fatalf("got %d lines", len(got))
	}
	if got[0] != "ffprobe|ffprobe -v quiet x.mkv" {
		t.Fatalf("line = %q", got[0])
	}
}

func TestFormatPretty(t *testing.T) {
	got := FormatPretty("/usr/local/bin/yt-dlp", "--plugin-dirs", "/data/p", "--no-mtime", "-f", "best", "https://example.com/v")
	want := "/usr/local/bin/yt-dlp\n  --plugin-dirs /data/p\n  --no-mtime\n  -f best https://example.com/v"
	if got != want {
		t.Fatalf("FormatPretty = %q, want %q", got, want)
	}
	got = FormatPretty("ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "in.mkv", "out.mkv")
	want = "ffmpeg\n  -hide_banner\n  -loglevel error\n  -i in.mkv out.mkv"
	if got != want {
		t.Fatalf("FormatPretty short = %q, want %q", got, want)
	}
}

func TestPretty(t *testing.T) {
	in := `/usr/local/bin/yt-dlp --plugin-dirs /data/p --no-mtime -f "bv*" https://example.com/v`
	want := "/usr/local/bin/yt-dlp\n  --plugin-dirs /data/p\n  --no-mtime\n  -f \"bv*\" https://example.com/v"
	if got := Pretty(in); got != want {
		t.Fatalf("Pretty = %q, want %q", got, want)
	}
	in = `ffmpeg -hide_banner -loglevel error -user_agent "Mozilla/5.0 (x)" -i url out.mkv`
	want = "ffmpeg\n  -hide_banner\n  -loglevel error\n  -user_agent \"Mozilla/5.0 (x)\"\n  -i url out.mkv"
	if got := Pretty(in); got != want {
		t.Fatalf("Pretty short = %q, want %q", got, want)
	}
	// Do not split inside double quotes.
	in = `yt-dlp -f "a --b -c" url`
	want = "yt-dlp\n  -f \"a --b -c\" url"
	if got := Pretty(in); got != want {
		t.Fatalf("Pretty quoted = %q, want %q", got, want)
	}
}
