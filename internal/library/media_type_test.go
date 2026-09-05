package library

import "testing"

func TestNormalizeMediaType(t *testing.T) {
	if got := NormalizeMediaType("  short  "); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeMediaType(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildDownloadMatchFilter(t *testing.T) {
	if got := BuildDownloadMatchFilter(); got != LiveBroadcastMatchFilter {
		t.Fatalf("got %q want %q", got, LiveBroadcastMatchFilter)
	}
}

func TestVideoListFilterMediaTypeActive(t *testing.T) {
	if !(VideoListFilter{MediaType: "short"}).Active() {
		t.Fatal("media_type should be active")
	}
}
