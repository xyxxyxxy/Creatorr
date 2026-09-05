package library_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestEnqueueMaturityDueSkipsWhenProfileOff(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	// Profile delays stay 0 = off.
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "NoMat", SourceURL: "https://www.example.com/@nomat",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "T", WebpageURL: "https://www.example.com/watch?v=v1",
		UploadDate: time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339),
		SourceID:   ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	acquired := time.Now().UTC().Add(-47 * time.Hour).Format(time.RFC3339)
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status='downloaded', acquired_at=?, sidecars_acquired_at=? WHERE id=?`,
		acquired, acquired, res.VideoID); err != nil {
		t.Fatal(err)
	}
	mn, sn, err := s.EnqueueMaturityDue()
	if err != nil {
		t.Fatal(err)
	}
	if mn != 0 || sn != 0 {
		t.Fatalf("expected 0 enqueues, got media=%d sidecars=%d", mn, sn)
	}
}

func TestEnqueueMaturityDueMediaYoungAtAcquire(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	hours := 12
	if _, err := s.UpdateProfileParams(profileID, library.UpdateProfileParams{MaturityRedownloadHours: &hours}); err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MatYoung", SourceURL: "https://www.example.com/@matyoung",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := time.Now().UTC().Add(-48 * time.Hour)
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "T", WebpageURL: "https://www.example.com/watch?v=v1",
		UploadDate: upload.Format(time.RFC3339),
		SourceID:   ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	acquired := upload.Add(1 * time.Hour).Format(time.RFC3339)
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status='downloaded', acquired_at=?, sidecars_acquired_at=? WHERE id=?`,
		acquired, acquired, res.VideoID); err != nil {
		t.Fatal(err)
	}
	mn, sn, err := s.EnqueueMaturityDue()
	if err != nil {
		t.Fatal(err)
	}
	if mn != 1 || sn != 0 {
		t.Fatalf("expected media=1 sidecars=0, got media=%d sidecars=%d", mn, sn)
	}
	var kind, payload string
	if err := s.DB.SQL.QueryRow(`SELECT kind, payload FROM tasks WHERE video_id=? AND status=?`,
		res.VideoID, queue.StatusPending).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	if kind != queue.KindDownload || !library.TaskPayloadMaturity(payload) {
		t.Fatalf("kind=%q payload=%q", kind, payload)
	}
}

func TestEnqueueMaturityDueSkipsOldAtAcquire(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	hours := 12
	if _, err := s.UpdateProfileParams(profileID, library.UpdateProfileParams{MaturityRedownloadHours: &hours}); err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MatOld", SourceURL: "https://www.example.com/@matold",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := time.Now().UTC().Add(-48 * time.Hour)
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "T", WebpageURL: "https://www.example.com/watch?v=v1",
		UploadDate: upload.Format(time.RFC3339),
		SourceID:   ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	acquired := upload.Add(24 * time.Hour).Format(time.RFC3339)
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status='downloaded', acquired_at=?, sidecars_acquired_at=? WHERE id=?`,
		acquired, acquired, res.VideoID); err != nil {
		t.Fatal(err)
	}
	mn, sn, err := s.EnqueueMaturityDue()
	if err != nil {
		t.Fatal(err)
	}
	if mn != 0 || sn != 0 {
		t.Fatalf("expected skip, got media=%d sidecars=%d", mn, sn)
	}
}

func TestEnqueueMaturitySidecarUsesPackAnchor(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	sidecarHours := library.MaturitySidecarDaysToHours(7)
	if _, err := s.UpdateProfileParams(profileID, library.UpdateProfileParams{MaturitySidecarHours: &sidecarHours}); err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MatSide", SourceURL: "https://www.example.com/@matside",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := time.Now().UTC().Add(-10 * 24 * time.Hour)
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=v1",
		UploadDate: upload.Format(time.RFC3339),
		SourceID:   ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	acquired := upload.Add(2 * time.Hour).Format(time.RFC3339)
	dir := filepath.Join(root.Path, "MatSide")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	if err := os.WriteFile(media, []byte("VIDEO"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status='downloaded', acquired_at=?, sidecars_acquired_at=? WHERE id=?`,
		acquired, acquired, res.VideoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', ?)`,
		res.VideoID, media, acquired); err != nil {
		t.Fatal(err)
	}
	mn, sn, err := s.EnqueueMaturityDue()
	if err != nil {
		t.Fatal(err)
	}
	if mn != 0 || sn != 1 {
		t.Fatalf("expected sidecars=1, got media=%d sidecars=%d", mn, sn)
	}
	var kind string
	if err := s.DB.SQL.QueryRow(`SELECT kind FROM tasks WHERE video_id=? AND status=?`,
		res.VideoID, queue.StatusPending).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != queue.KindRefreshSidecars {
		t.Fatalf("kind=%q", kind)
	}
}

func TestClampMaturityLimits(t *testing.T) {
	if got := library.ClampMaturityRedownloadHours(-1); got != 0 {
		t.Fatalf("hours clamp low: %d", got)
	}
	if got := library.ClampMaturityRedownloadHours(999); got != library.MaxMaturityRedownloadHours {
		t.Fatalf("hours clamp high: %d", got)
	}
	if got := library.MaturitySidecarDaysToHours(7); got != 7*24 {
		t.Fatalf("days to hours: %d", got)
	}
	if got := library.MaturitySidecarHoursToDays(168); got != 7 {
		t.Fatalf("hours to days: %d", got)
	}
	if got := library.MaturitySidecarDaysForPreset(3); got != 30 {
		t.Fatalf("preset 3 days: %d", got)
	}
	if got := library.MaturitySidecarPresetIndex(14); got != 2 {
		t.Fatalf("preset index 14d: %d", got)
	}
	if got := library.MaturitySidecarLabel(365); got != "1 year" {
		t.Fatalf("label: %q", got)
	}
	if got := library.MaturityMediaHoursForPreset(5); got != 24 {
		t.Fatalf("media preset 5: %d", got)
	}
	if got := library.MaturityMediaLabel(48); got != "2 days" {
		t.Fatalf("media label: %q", got)
	}
}

func TestEnqueueMaturityDueSkipsArchiveAcquired(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	hours := 12
	if _, err := s.UpdateProfileParams(profileID, library.UpdateProfileParams{MaturityRedownloadHours: &hours}); err != nil {
		t.Fatal(err)
	}
	sidecarHours := library.MaturitySidecarDaysToHours(7)
	if _, err := s.UpdateProfileParams(profileID, library.UpdateProfileParams{MaturitySidecarHours: &sidecarHours}); err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MatArch", SourceURL: "https://www.example.com/@matarch",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := time.Now().UTC().Add(-48 * time.Hour)
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "T", WebpageURL: "https://www.example.com/watch?v=v1",
		UploadDate: upload.Format(time.RFC3339),
		SourceID:   ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	acquired := upload.Add(1 * time.Hour).Format(time.RFC3339)
	if _, err := s.DB.SQL.Exec(`
		UPDATE videos SET status='downloaded', acquired_at=?, sidecars_acquired_at=?, acquired_via=? WHERE id=?
	`, acquired, acquired, library.AcquiredViaArchive, res.VideoID); err != nil {
		t.Fatal(err)
	}
	mn, sn, err := s.EnqueueMaturityDue()
	if err != nil {
		t.Fatal(err)
	}
	if mn != 0 || sn != 0 {
		t.Fatalf("expected skip archive-acquired, got media=%d sidecars=%d", mn, sn)
	}
}
