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

func testNotesRouter(t *testing.T) (*library.Store, http.Handler) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	return lib, r
}

func TestSaveVideoNotes(t *testing.T) {
	lib, h := testNotesRouter(t)
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title:            "Notes",
		RootID:           root.ID,
		QualityProfileID: prof.ID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'n1', 'One', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vidID, _ := res.LastInsertId()

	form := url.Values{}
	form.Set("video_id", strconv.FormatInt(vidID, 10))
	form.Set("notes", "video note")
	req := httptest.NewRequest(http.MethodPost, "/actions/save-video-notes", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	v, err := lib.GetVideo(vidID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Notes != "video note" {
		t.Fatalf("notes = %q", v.Notes)
	}

	form = url.Values{}
	form.Set("video_id", "999999")
	form.Set("notes", "x")
	req = httptest.NewRequest(http.MethodPost, "/actions/save-video-notes", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status %d", rec.Code)
	}

	form = url.Values{}
	form.Set("video_id", strconv.FormatInt(vidID, 10))
	form.Set("notes", strings.Repeat("a", library.NotesMaxBytes+1))
	req = httptest.NewRequest(http.MethodPost, "/actions/save-video-notes", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("too long status %d", rec.Code)
	}
}

func TestSaveSeriesNotes(t *testing.T) {
	lib, h := testNotesRouter(t)
	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title:            "Notes",
		RootID:           root.ID,
		QualityProfileID: prof.ID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("series_id", strconv.FormatInt(ser.ID, 10))
	form.Set("notes", "series note")
	req := httptest.NewRequest(http.MethodPost, "/actions/save-series-notes", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	got, err := lib.GetSeries(ser.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "series note" {
		t.Fatalf("notes = %q", got.Notes)
	}

	form = url.Values{}
	form.Set("series_id", "999999")
	form.Set("notes", "x")
	req = httptest.NewRequest(http.MethodPost, "/actions/save-series-notes", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status %d", rec.Code)
	}
}
