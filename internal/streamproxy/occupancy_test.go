package streamproxy

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestTouchOccupancyCreatesStreamPlay(t *testing.T) {
	lib, h := openOccupancyHandler(t)
	vid, seriesID, tok := seedStreamVideo(t, lib)

	tid := h.touchOccupancy(vid, seriesID, "example.com", tok)
	if tid <= 0 {
		t.Fatal("expected task id")
	}
	st, err := h.Queue.TaskStatus(tid)
	if err != nil || st != queue.StatusRunning {
		t.Fatalf("status=%s err=%v", st, err)
	}

	again := h.touchOccupancy(vid, seriesID, "example.com", tok)
	if again != tid {
		t.Fatalf("reuse want %d got %d", tid, again)
	}

	var n int
	_ = h.Queue.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = ? AND status = ?`, queue.KindStreamPlay, queue.StatusRunning).Scan(&n)
	if n != 1 {
		t.Fatalf("running stream_play count=%d", n)
	}
}

func TestTouchOccupancyWhilePaused(t *testing.T) {
	lib, h := openOccupancyHandler(t)
	vid, seriesID, tok := seedStreamVideo(t, lib)
	if err := domains.SetPaused(lib.DB, "example.com", true); err != nil {
		t.Fatal(err)
	}
	tid := h.touchOccupancy(vid, seriesID, "example.com", tok)
	if tid <= 0 {
		t.Fatal("expected occupancy while paused")
	}
}

func TestOccupancyBlocksClaimNext(t *testing.T) {
	lib, h := openOccupancyHandler(t)
	vid, seriesID, tok := seedStreamVideo(t, lib)
	_ = h.touchOccupancy(vid, seriesID, "example.com", tok)

	_, err := h.Queue.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", VideoID: vid, Message: "dl",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.Queue.ClaimNext()
	if err != nil || task != nil {
		t.Fatalf("ClaimNext should be blocked: %v %+v", err, task)
	}
}

func TestFailOccupancySoftPauses(t *testing.T) {
	lib, h := openOccupancyHandler(t)
	vid, seriesID, tok := seedStreamVideo(t, lib)
	pc := playCtx{videoID: vid, seriesID: seriesID, domain: "example.com", token: tok}
	pc.taskID = h.touchOccupancy(vid, seriesID, "example.com", tok)

	h.handlePlayYtDlpFail(context.Background(), pc, apperrors.New(apperrors.CodeCookieInvalid, "cookies expired"))
	paused, err := domains.IsPaused(lib.DB, "example.com")
	if err != nil || !paused {
		t.Fatalf("paused=%v err=%v", paused, err)
	}
	st, _ := h.Queue.TaskStatus(pc.taskID)
	if st != queue.StatusFailed {
		t.Fatalf("status=%s want failed", st)
	}
}

func TestFinishOccupancyDone(t *testing.T) {
	lib, h := openOccupancyHandler(t)
	vid, seriesID, tok := seedStreamVideo(t, lib)
	tid := h.touchOccupancy(vid, seriesID, "example.com", tok)
	key := occupancyKey(vid, tok)
	occMu.Lock()
	o := occByKey[key]
	occMu.Unlock()
	if o == nil {
		t.Fatal("missing occupancy")
	}
	h.finishOccupancyDone(key, o)
	st, _ := h.Queue.TaskStatus(tid)
	if st != queue.StatusDone {
		t.Fatalf("status=%s", st)
	}
}

func TestClassifyPlayError(t *testing.T) {
	code, _ := classifyPlayError(apperrors.New(apperrors.CodeRateLimited, "429"))
	if code != apperrors.CodeRateLimited {
		t.Fatalf("code=%s", code)
	}
	code, _ = classifyPlayError(errors.New("HTTP Error 403: Sign in to confirm"))
	if code != apperrors.CodeCookieInvalid {
		t.Fatalf("detect code=%s", code)
	}
	code, _ = classifyPlayError(errors.New("something else"))
	if code != apperrors.CodeResolveFailed {
		t.Fatalf("unclassified want ResolveFailed got %s", code)
	}
}

func openOccupancyHandler(t *testing.T) (*library.Store, *Handler) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "occ.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	lib.PublicBaseURL = "http://creatorr.example.com:8787"
	_ = domains.EnsureHost(d, "example.com")
	h := &Handler{Library: lib, Queue: q}
	t.Cleanup(func() {
		rows, _ := d.SQL.Query(`SELECT id FROM tasks WHERE kind = ? AND status = ?`, queue.KindStreamPlay, queue.StatusRunning)
		if rows != nil {
			for rows.Next() {
				var id int64
				_ = rows.Scan(&id)
				_ = q.Finish(id, queue.StatusCancelled, "test cleanup", "Cancelled", "")
			}
			_ = rows.Close()
		}
		occMu.Lock()
		occByKey = map[string]*streamOccupancy{}
		occMu.Unlock()
	})
	return lib, h
}

func seedStreamVideo(t *testing.T, lib *library.Store) (videoID, seriesID int64, token string) {
	t.Helper()
	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: prof.ID, DeliveryMode: "stream", Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := lib.AddSource(ser.ID, library.AddSourceParams{URL: "https://example.com/@s"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "T", WebpageURL: "https://example.com/watch?v=vid1", SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.DB.SQL.Exec(`UPDATE videos SET status = 'streamable' WHERE id = ?`, res.VideoID); err != nil {
		t.Fatal(err)
	}
	tok, err := library.EnsureStreamToken(lib.DB)
	if err != nil {
		t.Fatal(err)
	}
	return res.VideoID, ser.ID, tok
}
