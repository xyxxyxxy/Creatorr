package api_test

import (
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

func TestGetImportPicker(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
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
		Title: "PickerShow", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = lib.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'pick1', 'Ep One', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}

	srv := &api.Server{Library: lib, Queue: q}
	r := chi.NewRouter()
	gen.HandlerFromMux(srv, r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/picker", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out gen.ImportPickerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Series) != 1 || out.Series[0].Title != "PickerShow" {
		t.Fatalf("series=%v", out.Series)
	}
	if out.Series[0].PosterUrl == nil || *out.Series[0].PosterUrl == "" {
		t.Fatal("want poster_url set without disk check")
	}
	if len(out.Videos) != 1 || out.Videos[0].Title != "Ep One" || out.Videos[0].HasMedia {
		t.Fatalf("videos=%v", out.Videos)
	}
}
