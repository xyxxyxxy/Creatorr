package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/api"
	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/testutil"
)

func TestDeleteRootConflict(t *testing.T) {
	_, q, lib := testutil.OpenStores(t)
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	}); err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Library: lib, Queue: q}
	r := chi.NewRouter()
	gen.HandlerFromMux(srv, r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/roots/"+strconv.FormatInt(root.ID, 10), nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody["code"] != "Conflict" {
		t.Fatalf("code=%v body=%v", errBody["code"], errBody)
	}
}

func TestDeleteRootOK(t *testing.T) {
	_, q, lib := testutil.OpenStores(t)
	root, err := lib.CreateRoot("temp", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Library: lib, Queue: q}
	r := chi.NewRouter()
	gen.HandlerFromMux(srv, r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/roots/"+strconv.FormatInt(root.ID, 10), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}
