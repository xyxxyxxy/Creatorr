package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func TestLibrarySettingsInUseProfileDeleteDisabled(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := lib.CreateProfile("in-use-profile", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: p.ID, Monitored: false,
	}); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/library", nil))
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(body, 400))
	}
	if !strings.Contains(body, `data-tip="Used by 1 series"`) {
		t.Fatalf("missing Used by tooltip: %s", truncate(body, 800))
	}
	if strings.Contains(body, "modal-delete-profile-"+strconv.FormatInt(p.ID, 10)) {
		t.Fatalf("in-use profile should not get delete modal")
	}
}

func TestActionDeleteProfileConflict(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
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
	p, err := lib.CreateProfile("used", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: p.ID, Monitored: false,
	}); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	form := url.Values{"id": {strconv.FormatInt(p.ID, 10)}, "redirect": {"/settings/library"}}
	req := httptest.NewRequest(http.MethodPost, "/actions/delete-profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	errMsg := u.Query().Get("err")
	if !strings.Contains(errMsg, "used by") {
		t.Fatalf("want conflict flash redirect, got %q", loc)
	}
	if _, err := lib.GetProfile(p.ID); err != nil {
		t.Fatalf("profile should remain: %v", err)
	}
}
