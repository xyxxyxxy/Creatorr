package worker_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/testutil/fakemedia"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func fakeYtdlpBin(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// worker_test -> ../../ytdlp/testdata/fake-yt-dlp
	return filepath.Join(filepath.Dir(file), "..", "ytdlp", "testdata", "fake-yt-dlp")
}

func TestScanHandlerFakeYtdlp(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "ScanShow", SourceURL: "https://example.com/playlist/PL_example",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := lib.ListSources(ser.ID)
	if err != nil || len(srcs) == 0 {
		t.Fatalf("sources: %v len=%d", err, len(srcs))
	}
	src := &srcs[0]
	_, _ = q.CancelAll()

	payload := map[string]any{"source_id": src.ID, "series_id": ser.ID, "mode": "full"}
	taskID, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindScan, Domain: "example.com",
		SeriesID: ser.ID, Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := q.GetTask(taskID)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}

	h := worker.ScanHandler(worker.Deps{
		Library: lib,
		TmpRoot: t.TempDir(),
		YtDlp:   &ytdlp.Client{Bin: fakeYtdlpBin(t)},
	})
	if err := h(context.Background(), task, func(string, *float64) {}); err != nil {
		t.Fatal(err)
	}
	videos, err := lib.ListVideos(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) < 2 {
		t.Fatalf("want ≥2 videos from fake list, got %d", len(videos))
	}
}

func TestDownloadHandlerFakeYtdlp(t *testing.T) {
	fakemedia.PrependPATH(t)
	d, err := db.Open(filepath.Join(t.TempDir(), "dl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "DLShow", SourceURL: "https://example.com/@dl",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := lib.ListSources(ser.ID)
	if err != nil || len(srcs) == 0 {
		t.Fatalf("sources: %v len=%d", err, len(srcs))
	}
	src := &srcs[0]
	_, _ = q.CancelAll()
	res, err := lib.UpsertListed(ser.ID, library.ListedVideo{
		SourceID: src.ID, RemoteID: "vid1", Title: "First Video",
		WebpageURL: "https://example.com/watch?v=vid1", UploadDate: "2024-01-01T00:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = q.CancelAll()
	_, _ = d.SQL.Exec(`UPDATE videos SET status = 'wanted', source_url = ? WHERE id = ?`,
		"https://example.com/watch?v=vid1", res.VideoID)

	taskID, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com",
		SeriesID: ser.ID, VideoID: res.VideoID,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := q.GetTask(taskID)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}

	h := worker.DownloadHandler(worker.Deps{
		Library: lib,
		TmpRoot: t.TempDir(),
		YtDlp:   &ytdlp.Client{Bin: fakeYtdlpBin(t)},
	})
	if err := h(context.Background(), task, func(string, *float64) {}); err != nil {
		t.Fatal(err)
	}
	v, err := lib.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "downloaded" {
		t.Fatalf("status=%q want downloaded", v.Status)
	}
	path, ok, err := lib.HasVideoFile(res.VideoID)
	if err != nil || !ok || path == "" {
		t.Fatalf("packed media missing: ok=%v path=%q err=%v", ok, path, err)
	}
}
