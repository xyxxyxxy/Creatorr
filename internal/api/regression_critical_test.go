package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/api"
	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/health"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func mountAPIWithHealth(t *testing.T, lib *library.Store, q *queue.Store, h *health.Checker) http.Handler {
	t.Helper()
	srv := &api.Server{Library: lib, Queue: q, Health: h}
	r := chi.NewRouter()
	gen.HandlerFromMux(srv, r)
	return r
}

func TestGetHealthAPI(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)

	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 2024.01.01-fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	checker := &health.Checker{
		DB: d,
		Cfg: config.Config{
			DBPath:            filepath.Join(dir, "api.db"),
			InitialRootFolder: filepath.Join(dir, "library"),
			ImportRoot:        filepath.Join(dir, "import"),
			YtDlpBin:          bin,
			// FlareSolverrURL / PotProviderURL empty → skipped (no live probes)
		},
	}
	_ = os.MkdirAll(checker.Cfg.InitialRootFolder, 0o755)
	_ = os.MkdirAll(checker.Cfg.ImportRoot, 0o755)

	h := mountAPIWithHealth(t, lib, q, checker)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp gen.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != gen.Ok && resp.Status != gen.Degraded {
		t.Fatalf("status=%q", resp.Status)
	}
}

func TestRegressionCriticalLibraryAndTasksAPI(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
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
		Title: "API Show", SourceURL: "https://example.com/@api",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := lib.ListSources(ser.ID)
	if err != nil || len(srcs) == 0 {
		t.Fatalf("sources: %v len=%d", err, len(srcs))
	}
	src := srcs[0]
	_, _ = q.CancelAll()
	vres, err := lib.UpsertListed(ser.ID, library.ListedVideo{
		SourceID: src.ID, RemoteID: "v1", Title: "Ep",
		WebpageURL: "https://example.com/watch?v=v1",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = q.CancelAll()
	_, _ = d.SQL.Exec(`UPDATE videos SET status = 'ignored' WHERE id = ?`, vres.VideoID)

	h := mountAPI(t, lib, q)

	// GET /api/series/{id}
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/series/%d", ser.ID), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get series %d: %s", rec.Code, rec.Body.String())
	}

	// GET /api/videos/{id}
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/videos/%d", vres.VideoID), nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get video %d: %s", rec.Code, rec.Body.String())
	}

	// POST /api/videos/{id}/want
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/videos/%d/want", vres.VideoID), nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want video %d: %s", rec.Code, rec.Body.String())
	}

	// POST /api/series/{id}/scan
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/series/%d/scan", ser.ID), nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("scan series %d: %s", rec.Code, rec.Body.String())
	}

	// POST /api/tasks/cancel-all
	req = httptest.NewRequest(http.MethodPost, "/api/tasks/cancel-all", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel-all %d: %s", rec.Code, rec.Body.String())
	}

	// GET /api/notifications/unread-count
	req = httptest.NewRequest(http.MethodGet, "/api/notifications/unread-count", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unread-count %d: %s", rec.Code, rec.Body.String())
	}
	var uc gen.NotificationUnreadCount
	if err := json.NewDecoder(rec.Body).Decode(&uc); err != nil {
		t.Fatal(err)
	}

	// PUT /api/domains/{domain}/paused
	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(gen.SetPausedRequest{Paused: true})
	req = httptest.NewRequest(http.MethodPut, "/api/domains/example.com/paused", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("set paused %d: %s", rec.Code, rec.Body.String())
	}
}
