package library_test

import (
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSourceHistoryScannedAndStatus(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "SrcHist", SourceURL: "https://www.example.com/@srchist",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	tid := seedTaskID(t, s)

	if err := s.AddSourceHistory(srcID, library.SourceHistScanError, "boom", map[string]any{
		"mode": library.SourceHistModeScan, "code": "ScanFailed",
	}, tid); err != nil {
		t.Fatal(err)
	}
	st, err := s.LatestSourceScanStatus(srcID)
	if err != nil || st.Event != library.SourceHistScanError || st.LastErrorCode != "ScanFailed" {
		t.Fatalf("status after error: %+v err=%v", st, err)
	}
	warn, err := s.SeriesWarnLevels([]int64{ser.ID})
	if err != nil || warn[ser.ID] != library.SeriesWarnError {
		t.Fatalf("warn=%v err=%v", warn, err)
	}

	created, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "One", SourceID: srcID,
	}, tid)
	if err != nil || !created.Created {
		t.Fatalf("upsert: %+v %v", created, err)
	}
	if err := s.AddSourceHistory(srcID, library.SourceHistScanned, "ok", map[string]any{
		"mode": library.SourceHistModeScan, "created": 1, "updated": 0,
		"created_ids": []int64{created.VideoID}, "updated_ids": []int64{},
	}, tid); err != nil {
		t.Fatal(err)
	}
	st, err = s.LatestSourceScanStatus(srcID)
	if err != nil || st.Event != library.SourceHistScanned || !st.HasCreatedCount || st.CreatedCount != 1 {
		t.Fatalf("status after scanned: %+v err=%v", st, err)
	}
	warn, err = s.SeriesWarnLevels([]int64{ser.ID})
	if err != nil || warn[ser.ID] == library.SeriesWarnError {
		t.Fatalf("error warn should clear after scanned, warn=%v err=%v", warn, err)
	}

	tl, err := s.ListVideoTimelinePage(created.VideoID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range tl {
		if e.Event == "discovered" {
			found = true
		}
		if e.Event == "discovered" || e.Event == "updated" {
			// ok projected
		}
	}
	if !found {
		t.Fatalf("expected projected discovered on timeline, got %+v", tl)
	}

	vh, err := s.ListVideoHistory(created.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range vh {
		if e.Event == "discovered" {
			t.Fatal("discovered must not be in video_history")
		}
	}
}

func TestSourceHistoryTipDueIgnoresFullAndMeta(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "TipDue", SourceURL: "https://www.example.com/@tipdue",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	tid := seedTaskID(t, s)
	now := time.Now().UTC()

	if err := s.AddSourceHistory(srcID, library.SourceHistScanned, "full", map[string]any{
		"mode": library.SourceHistModeFull, "created": 0, "updated": 0,
		"created_ids": []int64{}, "updated_ids": []int64{},
	}, tid); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE source_history SET created_at = ? WHERE source_id = ?`, now.Format(time.RFC3339Nano), srcID)
	last, err := s.LatestTipScannedAt(srcID)
	if err != nil || !last.IsZero() {
		t.Fatalf("full mode must not set tip scanned at, last=%v err=%v", last, err)
	}

	if err := s.AddSourceHistory(srcID, library.SourceHistScanned, "tip", map[string]any{
		"mode": library.SourceHistModeScan, "created": 0, "updated": 0,
		"created_ids": []int64{}, "updated_ids": []int64{},
	}, tid); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE source_history SET created_at = ? WHERE id = (SELECT MAX(id) FROM source_history WHERE source_id = ?)`,
		now.Format(time.RFC3339Nano), srcID)
	last, err = s.LatestTipScannedAt(srcID)
	if err != nil || last.IsZero() {
		t.Fatalf("tip mode should set tip scanned at, last=%v err=%v", last, err)
	}
}

func TestAddSourceHistoryRequiresTaskID(t *testing.T) {
	s := openLib(t)
	err := s.AddSourceHistory(1, library.SourceHistScanned, "x", nil, 0)
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}
