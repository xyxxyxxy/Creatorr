package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/api"
	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func mountAPI(t *testing.T, lib *library.Store, q *queue.Store) http.Handler {
	t.Helper()
	srv := &api.Server{Library: lib, Queue: q}
	r := chi.NewRouter()
	gen.HandlerFromMux(srv, r)
	return r
}

func TestBulkEditSeriesAPI(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := mountAPI(t, lib, q)

	mode := gen.BulkEditSeriesRequestDeliveryModeAudio
	body, _ := json.Marshal(gen.BulkEditSeriesRequest{
		SeriesIds:    []int64{ser.ID},
		DeliveryMode: &mode,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/series/bulk-edit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/series/bulk-edit", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", rec2.Code, rec2.Body.String())
	}

	bad, _ := json.Marshal(gen.BulkEditSeriesRequest{SeriesIds: []int64{ser.ID}})
	req3 := httptest.NewRequest(http.MethodPost, "/api/series/bulk-edit", bytes.NewReader(bad))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("want 400 empty fields, got %d: %s", rec3.Code, rec3.Body.String())
	}
}

func TestBulkSetSeriesMonitoredAPI(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := mountAPI(t, lib, q)
	monitored := false
	body, _ := json.Marshal(gen.BulkSetSeriesMonitoredRequest{
		SeriesIds: []int64{ser.ID},
		Monitored: monitored,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/series/bulk-monitored", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	got, err := lib.GetSeries(ser.ID, false)
	if err != nil || got.Monitored {
		t.Fatalf("monitored=%v err=%v", got.Monitored, err)
	}
}
