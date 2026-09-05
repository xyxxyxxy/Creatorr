package web_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func TestSeriesMetadataBodyDiscardClearsDraft(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	lib.CacheDir = filepath.Join(t.TempDir(), "cache")
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SaveSeriesMetadata(ser.ID, library.SaveSeriesMetadataParams{
		Plot: "saved plot",
	}); err != nil {
		t.Fatal(err)
	}
	tid, err := q.Enqueue(queue.EnqueueParams{
		Kind: queue.KindPrefetchSeriesMeta, Domain: "example.com", SeriesID: ser.ID,
		Payload: map[string]any{"url": "https://example.com/c"}, Message: "fetch",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.WritePrefetchDraft(ser.ID, tid, library.PrefetchDraft{
		Plot: "draft plot from fetch",
	}); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/series/"+strconv.FormatInt(ser.ID, 10)+"/metadata/body?discard="+strconv.FormatInt(tid, 10), nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "saved plot") {
		t.Fatalf("want saved plot in body: %s", truncate(body, 600))
	}
	if strings.Contains(body, "draft plot from fetch") {
		t.Fatalf("draft plot should not appear after discard: %s", truncate(body, 600))
	}
	if strings.Contains(body, `name="prefetch_task_id"`) {
		t.Fatalf("prefetch_task_id should be gone after discard")
	}
	if _, err := lib.ReadPrefetchDraft(ser.ID, tid); err == nil {
		t.Fatal("draft file should be cleared")
	}
	task, err := q.GetTask(tid)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Status != queue.StatusCancelled {
		t.Fatalf("want cancelled pending prefetch, got %s", task.Status)
	}
}
