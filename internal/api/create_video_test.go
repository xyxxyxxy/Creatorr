package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/api"
	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestCreateSeriesVideo(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
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
	srv := &api.Server{Library: lib, Queue: q}
	r := chi.NewRouter()
	gen.HandlerFromMux(srv, r)

	body, _ := json.Marshal(map[string]any{
		"title":       "Ep",
		"upload_date": "2024-05-01",
		"remote_id":   "r1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/series/"+strconv.FormatInt(ser.ID, 10)+"/videos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ignored" || out["remote_id"] != "r1" {
		t.Fatalf("%v", out)
	}

	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/api/series/"+strconv.FormatInt(ser.ID, 10)+"/videos", bytes.NewReader(body)))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("conflict status %d: %s", rec2.Code, rec2.Body.String())
	}
}
