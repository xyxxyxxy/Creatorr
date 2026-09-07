package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestPreviewApplyEpisodeNaming(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Prev", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@prev"})
	if err != nil {
		t.Fatal(err)
	}
	late, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "late", Title: "Late", SourceID: src.ID,
		UploadDate: "2024-03-15T18:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seasonDir := filepath.Join(root.Path, "Prev", "S2024")
	_ = os.MkdirAll(seasonDir, 0o755)
	oldMedia := filepath.Join(seasonDir, "S2024E0001 [late].mkv")
	if err := os.WriteFile(oldMedia, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 2024, episode = 1 WHERE id = ?`, late.VideoID)
	_, _ = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		late.VideoID, oldMedia, "2024-01-01T00:00:00Z")

	_, err = s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "early", Title: "Early", SourceID: src.ID,
		UploadDate: "2024-03-15T08:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = 'rename_episodes' AND status IN ('pending', 'running')`)

	prev, err := s.PreviewApplyEpisodeNaming([]int64{ser.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prev.TotalChanges != 1 || len(prev.Items) != 1 {
		t.Fatalf("want 1 change, got total=%d items=%d busy=%d unchanged=%d",
			prev.TotalChanges, len(prev.Items), prev.SkippedBusy, prev.Unchanged)
	}
	it := prev.Items[0]
	if it.VideoID != late.VideoID {
		t.Fatalf("video id %d", it.VideoID)
	}
	if !strings.Contains(it.From, "E0001") || !strings.Contains(it.To, "E0002") {
		t.Fatalf("from=%q to=%q", it.From, it.To)
	}
	var path string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, late.VideoID).Scan(&path)
	if path != oldMedia {
		t.Fatalf("preview must not rename disk: %q", path)
	}
}
