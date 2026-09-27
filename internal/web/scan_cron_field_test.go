package web_test

import (
	"net/http"
	"net/http/httptest"
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

func TestEditSourceScanCronOffDefaultsWeekly(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "scan-cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	roots, err := lib.ListRoots()
	if err != nil || len(roots) == 0 {
		t.Fatalf("roots: %v len=%d", err, len(roots))
	}
	prof, err := lib.CreateProfile("p-scan", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: roots[0].ID, QualityProfileID: prof.ID,
		SourceURL: "https://example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`UPDATE sources SET scan_cron = 'never' WHERE series_id = ?`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var srcID int64
	if err := d.SQL.QueryRow(`SELECT id FROM sources WHERE series_id = ?`, ser.ID).Scan(&srcID); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest(http.MethodGet, "/series/"+strconv.FormatInt(ser.ID, 10), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	id := `id="edit-source-scan-cron-` + strconv.FormatInt(srcID, 10) + `"`
	i := strings.Index(body, id)
	if i < 0 {
		t.Fatalf("missing edit scan cron field: %s", truncate(body, 500))
	}
	start := strings.LastIndex(body[:i], "<input")
	if start < 0 {
		t.Fatal("edit scan cron input tag not found")
	}
	tag := body[start:]
	if j := strings.Index(tag, ">"); j >= 0 {
		tag = tag[:j]
	}
	for _, want := range []string{`value="never"`, `disabled`, `data-cron-default="@weekly"`} {
		if !strings.Contains(tag, want) {
			t.Fatalf("edit scan cron tag missing %s: %s", want, tag)
		}
	}
	if strings.Contains(tag, ` name="scan_cron"`) {
		t.Fatalf("disabled scan cron must not submit the visible never value: %s", tag)
	}
	if !strings.Contains(body, `id="add-source-scan-cron"`) || !strings.Contains(body, `value="@weekly"`) {
		t.Fatal("add source scan cron should still default to @weekly")
	}
}
