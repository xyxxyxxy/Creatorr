package library

import (
	"testing"
)

func TestNormalizeAutoIgnoreMediaTypes(t *testing.T) {
	got := NormalizeAutoIgnoreMediaTypes([]string{" short ", "unknown", "", "livestream", "short", "UNKNOWN"})
	want := []string{"livestream", "short"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestMediaTypeExcluded(t *testing.T) {
	ex := []string{"short", "livestream"}
	if MediaTypeExcluded(ex, "") {
		t.Fatal("empty type must never be excluded")
	}
	if MediaTypeExcluded(ex, "video") {
		t.Fatal("video should pass")
	}
	if !MediaTypeExcluded(ex, "short") {
		t.Fatal("short should be excluded")
	}
}

func TestMediaTypeMatchFilter(t *testing.T) {
	if MediaTypeMatchFilter(nil) != "" {
		t.Fatal("empty exclude → no filter")
	}
	got := MediaTypeMatchFilter([]string{"short", "livestream"})
	// Alpha sort: livestream before short
	want := "media_type!=livestream & media_type!=short"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildDownloadMatchFilter(t *testing.T) {
	if got := BuildDownloadMatchFilter(nil); got != LiveBroadcastMatchFilter {
		t.Fatalf("empty exclude: got %q want %q", got, LiveBroadcastMatchFilter)
	}
	got := BuildDownloadMatchFilter([]string{"short"})
	want := "media_type!=short & " + LiveBroadcastMatchFilter
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMergeMediaTypeSuggestions(t *testing.T) {
	got := MergeMediaTypeSuggestions([]string{"episode", "short", "unknown"})
	seen := map[string]bool{}
	for _, v := range got {
		if v == "unknown" {
			t.Fatal("unknown must not appear in suggestions")
		}
		if seen[v] {
			t.Fatalf("duplicate %q", v)
		}
		seen[v] = true
	}
	for _, seed := range YouTubeMediaTypeSeed {
		if !seen[seed] {
			t.Fatalf("missing seed %q in %v", seed, got)
		}
	}
	if !seen["episode"] {
		t.Fatalf("missing custom episode in %v", got)
	}
}

func TestVideoListFilterMediaTypeActive(t *testing.T) {
	if !(VideoListFilter{MediaType: "short"}).Active() {
		t.Fatal("media_type should be active")
	}
}
