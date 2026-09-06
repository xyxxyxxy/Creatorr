package library_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestEnqueueVerifyAllMediaScopedMutualExclusive(t *testing.T) {
	s := openLib(t)
	_, err := s.EnqueueVerifyAllMediaScoped([]int64{1}, []int64{2})
	if err == nil {
		t.Fatal("want mutual exclusive error")
	}
}

func TestEnqueueVerifyAllMediaScopedSeriesPayload(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	serA, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "A", SourceURL: "https://www.example.com/@a", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	serB, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "B", SourceURL: "https://www.example.com/@b", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resA, err := s.UpsertListed(serA.ID, library.ListedVideo{
		RemoteID: "a1", Title: "One", SourceID: serA.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	resB, err := s.UpsertListed(serB.ID, library.ListedVideo{
		RemoteID: "b1", Title: "Two", SourceID: serB.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mediaA := filepath.Join(dir, "a.mkv")
	mediaB := filepath.Join(dir, "b.mkv")
	_ = os.WriteFile(mediaA, []byte("A"), 0o644)
	_ = os.WriteFile(mediaB, []byte("B"), 0o644)
	tid := seedTaskID(t, s)
	if err := s.CompleteImport(resA.VideoID, mediaA, "", "", "", nil, library.MediaCompleteMeta{}, tid); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(resB.VideoID, mediaB, "", "", "", nil, library.MediaCompleteMeta{}, tid); err != nil {
		t.Fatal(err)
	}

	id, err := s.EnqueueVerifyAllMediaScoped([]int64{serA.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SeriesIDs []int64 `json:"series_ids"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.SeriesIDs) != 1 || payload.SeriesIDs[0] != serA.ID {
		t.Fatalf("series_ids=%v", payload.SeriesIDs)
	}

	rows, err := s.DB.SQL.Query(`
		SELECT v.id FROM videos v
		WHERE v.status IN ('downloaded', 'verify_failed')
		  AND v.series_id IN (?)
		  AND EXISTS (SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'video')
		ORDER BY v.id
	`, serA.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var vid int64
		if err := rows.Scan(&vid); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, vid)
	}
	_ = rows.Close()
	if len(ids) != 1 || ids[0] != resA.VideoID {
		t.Fatalf("scoped ids=%v want [%d]", ids, resA.VideoID)
	}

	_, err = s.EnqueueVerifyAllMediaScoped(nil, []int64{resB.VideoID})
	if err == nil {
		t.Fatal("want duplicate while first pending")
	}
}

func TestEnqueueRegenerateNFOScopedPayload(t *testing.T) {
	s := openLib(t)
	id, err := s.EnqueueRegenerateNFOScoped(nil, []int64{9, 8})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		VideoIDs []int64 `json:"video_ids"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.VideoIDs) != 2 {
		t.Fatalf("video_ids=%v", payload.VideoIDs)
	}
	_, err = s.EnqueueRegenerateNFOScoped([]int64{1}, []int64{2})
	if err == nil {
		t.Fatal("want mutual exclusive")
	}
}

func TestEnqueueRenameEpisodesSeriesIDs(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	serA, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "RA", SourceURL: "https://www.example.com/@ra", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	serB, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "RB", SourceURL: "https://www.example.com/@rb", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueRenameEpisodesSeriesIDs([]int64{serA.ID, serB.ID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SeriesIDs []int64 `json:"series_ids"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.SeriesIDs) != 2 {
		t.Fatalf("series_ids=%v", payload.SeriesIDs)
	}
}
