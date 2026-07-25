package ytdlp

import (
	"testing"
	"time"
)

func TestEntriesFromInfoSingleVideo(t *testing.T) {
	info := map[string]any{
		"id": "abc123", "title": "My Video", "webpage_url": "https://example.com/watch?v=abc123",
		"upload_date": "20240102", "description": "hello",
		"thumbnails": []any{
			map[string]any{"url": "https://example.com/small.jpg"},
			map[string]any{"url": "https://example.com/large.jpg"},
		},
	}
	entries := entriesFromInfo(info)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.ID != "abc123" || e.Title != "My Video" || e.UploadDate != "2024-01-02T00:00:00Z" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.ThumbnailURL != "https://example.com/large.jpg" {
		t.Fatalf("thumbnail = %q, want the last (highest-res) entry", e.ThumbnailURL)
	}
}

func TestEntriesFromInfoFlatPlaylist(t *testing.T) {
	info := map[string]any{
		"_type": "playlist",
		"entries": []any{
			map[string]any{"id": "v1", "title": "Video 1", "webpage_url": "https://example.com/watch?v=v1"},
			map[string]any{"id": "v2", "title": "Video 2", "webpage_url": "https://example.com/watch?v=v2"},
			map[string]any{"_type": "playlist", "id": "nested"},
			map[string]any{"title": "no id, skipped"},
		},
	}
	entries := entriesFromInfo(info)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].ID != "v1" || entries[1].ID != "v2" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestEntriesFromInfoChannelTabsPreferVideos(t *testing.T) {
	info := map[string]any{
		"_type": "playlist",
		"title": "Creator",
		"entries": []any{
			map[string]any{
				"_type":       "playlist",
				"id":          "UC1",
				"title":       "Creator - Videos",
				"webpage_url": "https://www.youtube.com/@creator/videos",
				"entries": []any{
					map[string]any{"_type": "url", "id": "vid1", "title": "Upload 1"},
					map[string]any{"_type": "url", "id": "vid2", "title": "Upload 2"},
				},
			},
			map[string]any{
				"_type":       "playlist",
				"id":          "UC1",
				"title":       "Creator - Live",
				"webpage_url": "https://www.youtube.com/@creator/streams",
				"entries": []any{
					map[string]any{"_type": "url", "id": "live1", "title": "Stream"},
				},
			},
		},
	}
	entries := entriesFromInfo(info)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (Videos tab only)", len(entries))
	}
	if entries[0].ID != "vid1" || entries[1].ID != "vid2" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestEntriesFromInfoChannelTabsFallbackAll(t *testing.T) {
	info := map[string]any{
		"_type": "playlist",
		"entries": []any{
			map[string]any{
				"_type": "playlist",
				"id":    "t1",
				"title": "Tab A",
				"entries": []any{
					map[string]any{"id": "a1", "title": "A"},
				},
			},
			map[string]any{
				"_type": "playlist",
				"id":    "t2",
				"title": "Tab B",
				"entries": []any{
					map[string]any{"id": "b1", "title": "B"},
				},
			},
		},
	}
	entries := entriesFromInfo(info)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
}

func TestEntryFromMapPrefersTimestamp(t *testing.T) {
	e := entryFromMap(map[string]any{
		"id": "v1", "upload_date": "20240101", "timestamp": float64(1705329000),
	})
	if e.UploadDate != "2024-01-15T14:30:00Z" {
		// 1705329000 = 2024-01-15T14:30:00Z
		want := time.Unix(1705329000, 0).UTC().Format(time.RFC3339)
		if e.UploadDate != want {
			t.Fatalf("upload_date = %q, want %q (timestamp over upload_date)", e.UploadDate, want)
		}
	}
}

func TestEntryFromMapDropsNonHTTPURL(t *testing.T) {
	e := entryFromMap(map[string]any{"id": "v1", "webpage_url": "/relative/path"})
	if e.WebpageURL != "" {
		t.Fatalf("webpage_url = %q, want empty for a relative URL", e.WebpageURL)
	}
}

func TestEntriesFromInfoNil(t *testing.T) {
	if entriesFromInfo(nil) != nil {
		t.Fatal("expected nil entries for nil info")
	}
}
