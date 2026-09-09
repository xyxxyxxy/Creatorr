package library

import "testing"

func TestDownloadURLArchiveOrg(t *testing.T) {
	const details = "https://archive.org/details/example-item"
	const remote = "example-item/episode.mp4"
	want := "https://archive.org/download/" + remote
	if got := DownloadURL(details, remote); got != want {
		t.Fatalf("DownloadURL = %q, want %q", got, want)
	}
}

func TestDownloadURLArchiveOrgWebHost(t *testing.T) {
	got := DownloadURL("https://web.archive.org/details/foo", "foo/bar.mp4")
	want := "https://archive.org/download/foo/bar.mp4"
	if got != want {
		t.Fatalf("DownloadURL = %q, want %q", got, want)
	}
}

func TestDownloadURLYouTubeUnchanged(t *testing.T) {
	const u = "https://www.youtube.com/watch?v=abc123"
	if got := DownloadURL(u, "abc123"); got != u {
		t.Fatalf("DownloadURL = %q, want %q", got, u)
	}
}

func TestDownloadURLEmptyRemoteID(t *testing.T) {
	const u = "https://archive.org/details/foo"
	if got := DownloadURL(u, ""); got != u {
		t.Fatalf("DownloadURL = %q, want %q", got, u)
	}
}

func TestDownloadURLBareArchiveIdentifier(t *testing.T) {
	const u = "https://archive.org/details/foo"
	if got := DownloadURL(u, "foo"); got != u {
		t.Fatalf("DownloadURL = %q, want unchanged %q", got, u)
	}
}

func TestDownloadTaskURL(t *testing.T) {
	const details = "https://archive.org/details/example-item"
	const remote = "example-item/episode.mp4"
	wantHTTP := "https://archive.org/download/" + remote
	if got := DownloadTaskURL(details, remote, false); got != wantHTTP {
		t.Fatalf("native IA = %q, want %q", got, wantHTTP)
	}
	const yt = "https://www.youtube.com/watch?v=abc123XYZ01"
	if got := DownloadTaskURL(yt, "abc123XYZ01", true); got != "ytarchive:abc123XYZ01" {
		t.Fatalf("fallback = %q, want ytarchive:abc123XYZ01", got)
	}
	if got := DownloadTaskURL(yt, "", true); got != "" {
		t.Fatalf("fallback empty remote = %q, want empty", got)
	}
	if got := DownloadTaskURL(yt, "abc123XYZ01", false); got != yt {
		t.Fatalf("non-IA no fallback = %q, want %q", got, yt)
	}
}
