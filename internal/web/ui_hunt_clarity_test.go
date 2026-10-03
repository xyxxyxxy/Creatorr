package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func uiHuntHandler(t *testing.T) (*library.Store, *queue.Store, chi.Router, func()) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "ui-hunt.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	return lib, q, r, func() { _ = d.Close() }
}

func TestVideoStatusLabelOnListAndDetail(t *testing.T) {
	lib, q, r, close := uiHuntHandler(t)
	defer close()

	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "ErrSeries", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'err1', 'Broken Ep', 'wanted')
	`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var vid int64
	if err := lib.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'err1'`).Scan(&vid); err != nil {
		t.Fatal(err)
	}
	tid, err := q.InsertRunning(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com",
		VideoID: vid, Message: "download",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.MarkDownloadFailed(vid, tid, "DownloadFailed", "boom"); err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/series/"+itoa(ser.ID)+"?status=wanted_download_error", nil)
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	if listRec.Code != 200 {
		t.Fatalf("list status %d: %s", listRec.Code, truncate(listRec.Body.String(), 400))
	}
	listBody := listRec.Body.String()
	if !strings.Contains(listBody, "wanted (download error)") {
		t.Fatalf("video list missing status label: %s", truncate(listBody, 800))
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/series/"+itoa(ser.ID)+"/videos/"+itoa(vid), nil)
	detailRec := httptest.NewRecorder()
	r.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != 200 {
		t.Fatalf("detail status %d: %s", detailRec.Code, truncate(detailRec.Body.String(), 400))
	}
	detailBody := detailRec.Body.String()
	if !strings.Contains(detailBody, "wanted (download error)") {
		t.Fatalf("video detail missing status label: %s", truncate(detailBody, 800))
	}
}

func TestSeriesListProgressLabeledCounts(t *testing.T) {
	lib, _, r, close := uiHuntHandler(t)
	defer close()

	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "ProgressQA", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://example.com/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(remote, status string) {
		t.Helper()
		if _, err := lib.DB.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, status)
			VALUES (?, ?, ?, ?)
		`, ser.ID, remote, remote, status); err != nil {
			t.Fatal(err)
		}
	}
	insert("d1", "downloaded")
	insert("d2", "downloaded")
	insert("e1", "wanted_download_error")
	insert("w1", "wanted")

	req := httptest.NewRequest(http.MethodGet, "/series", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "2 downloaded") || !strings.Contains(body, "1 errors") || !strings.Contains(body, "1 wanted") {
		t.Fatalf("series progress labels missing: %s", truncate(body, 1200))
	}
	if strings.Contains(body, "2 | 1 | 1") {
		t.Fatalf("bare pipe counts still present: %s", truncate(body, 800))
	}
	const progressWrap = `flex flex-col gap-0.5 w-full min-w-0`
	if !strings.Contains(body, progressWrap+`"`) {
		t.Fatalf("monitored progress should not be muted: %s", truncate(body, 800))
	}
	if _, err := lib.DB.SQL.Exec(`UPDATE series SET monitored = 0 WHERE id = ?`, ser.ID); err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/series", nil))
	if rec2.Code != 200 {
		t.Fatalf("unmonitored status %d: %s", rec2.Code, truncate(rec2.Body.String(), 400))
	}
	body2 := rec2.Body.String()
	if !strings.Contains(body2, progressWrap+` opacity-40"`) {
		t.Fatalf("unmonitored progress should mute bar+labels: %s", truncate(body2, 800))
	}
}

func TestBulkVideoMetadataModalStartsAtNoChange(t *testing.T) {
	lib, _, r, close := uiHuntHandler(t)
	defer close()

	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "BulkQA", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://example.com/b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID: ser.ID, Title: "Ep", UploadDate: "2024-01-02T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/series/"+itoa(ser.ID), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	sel := `data-bulk-special-feature`
	i := strings.Index(body, sel)
	if i < 0 {
		t.Fatalf("missing bulk special kind select: %s", truncate(body, 600))
	}
	chunk := body[i:]
	if end := strings.Index(chunk, "</select>"); end > 0 {
		chunk = chunk[:end]
	}
	if !strings.Contains(chunk, `<option value="">No change</option>`) {
		t.Fatalf("bulk Special kind missing No change: %s", truncate(chunk, 400))
	}
	if !strings.Contains(chunk, `aria-label="Special kind"`) {
		t.Fatalf("bulk Special kind missing aria-label: %s", truncate(chunk, 400))
	}
}

func TestHydrateVideoBulkMetadataSkipsSpecialFeature(t *testing.T) {
	// JS has no test runner; pin the hydrate guard so Special kind stays No change.
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	web.StaticHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("app.js status %d", rec.Code)
	}
	js := rec.Body.String()
	start := strings.Index(js, "async function hydrateVideoBulkMetadataForm")
	if start < 0 {
		t.Fatal("hydrateVideoBulkMetadataForm missing")
	}
	rest := js[start:]
	end := strings.Index(rest, "\n  function runVideoBulkAction")
	if end < 0 {
		t.Fatal("runVideoBulkAction boundary missing")
	}
	fn := rest[:end]
	if strings.Contains(fn, "special_feature") {
		t.Fatalf("hydrate must not touch special_feature:\n%s", truncate(fn, 800))
	}
	if !strings.Contains(fn, "Special kind stays No change") {
		t.Fatal("hydrate missing Special kind No-change comment guard")
	}
}

func TestBulkThumbCardClickSelectsNotNavigates(t *testing.T) {
	// Multi-select must match thumb cards (li.card) as well as list-row.
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	web.StaticHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("app.js status %d", rec.Code)
	}
	js := rec.Body.String()
	for _, pin := range []string{
		`#series-list-rows > [data-series-id]`,
		`#series-videos-rows > [data-video-id], #videos-list-rows > [data-video-id]`,
		`thumb+list bulk click #series-list-rows > [data-series-id]`,
		`thumb+list bulk click #series-videos-rows > [data-video-id], #videos-list-rows > [data-video-id]`,
	} {
		if !strings.Contains(js, pin) {
			t.Fatalf("app.js missing bulk thumb click pin %q", pin)
		}
	}
	if strings.Contains(js, `#series-list-rows > .list-row[data-series-id]`) {
		t.Fatal("series bulk click still list-row-only; cards/gallery would navigate")
	}
	if strings.Contains(js, `#series-videos-rows > .list-row[data-video-id]`) {
		t.Fatal("video bulk click still list-row-only; cards/gallery would navigate")
	}
	for _, tip := range []string{
		`Use the multi-select bar`,
		`VIDEO_BULK_CONFIRM_AFTER = 5`,
		`SERIES_BULK_CONFIRM_AFTER = 5`,
	} {
		if !strings.Contains(js, tip) {
			t.Fatalf("app.js missing bulk contract %q", tip)
		}
	}
}

func TestTasksBadgeContractOpenCountNotSoftPause(t *testing.T) {
	_, q, _, close := uiHuntHandler(t)
	defer close()

	// Soft-pause alone must not invent open tasks (nav badge uses GET /api/tasks length).
	if err := domains.SetPaused(q.DB, "example.com", true); err != nil {
		t.Fatal(err)
	}
	active, err := q.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("soft-pause alone ListActive=%d want 0", len(active))
	}

	if _, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindScan, Domain: "example.com",
		Payload: map[string]any{"source_id": int64(1), "mode": "scan"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.InsertRunning(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "other.example",
		Message: "dl",
	}); err != nil {
		t.Fatal(err)
	}
	active, err = q.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("ListActive=%d want pending+running", len(active))
	}

	// refreshBadge always hides the warning paused badge; count uses list length.
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	web.StaticHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("app.js status %d", rec.Code)
	}
	js := rec.Body.String()
	start := strings.Index(js, "async function refreshBadge")
	if start < 0 {
		t.Fatal("refreshBadge missing")
	}
	rest := js[start:]
	end := strings.Index(rest, "\n  function setNotifyBadge")
	if end < 0 {
		t.Fatal("setNotifyBadge boundary missing")
	}
	fn := rest[:end]
	if !strings.Contains(fn, `pb.classList.add("hidden")`) {
		t.Fatalf("refreshBadge must hide paused badge: %s", truncate(fn, 600))
	}
	if !strings.Contains(fn, "Soft-pause alone must not bump") {
		t.Fatal("refreshBadge missing soft-pause comment guard")
	}
	if !strings.Contains(fn, "const n = list.length") {
		t.Fatalf("refreshBadge must count all open tasks: %s", truncate(fn, 600))
	}
}

func TestVideoBulkMetadataCommonStillReportsSpecialFeature(t *testing.T) {
	// Server may still report shared kind; client hydrate must ignore it (see skip test).
	lib, _, r, close := uiHuntHandler(t)
	defer close()

	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "CommonQA", RootID: 1, QualityProfileID: 1, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, remote := range []string{"a", "b"} {
		res, err := lib.DB.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, status, special_feature, studio)
			VALUES (?, ?, ?, 'wanted', 'episode', 'SameStudio')
		`, ser.ID, remote, remote)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}
	body, _ := json.Marshal(map[string]any{"ids": ids})
	req := httptest.NewRequest(http.MethodPost, "/videos/bulk-metadata-common", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 400))
	}
	var meta map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	sf, _ := meta["special_feature"].(map[string]any)
	if sf == nil || sf["same"] != true {
		t.Fatalf("special_feature=%v", meta["special_feature"])
	}
}

func TestListLiveSearchPinsViewportAnchor(t *testing.T) {
	// Live filter search must not jump the page: pin panel top across outerHTML swap.
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()
	web.StaticHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("app.js status %d", rec.Code)
	}
	js := rec.Body.String()
	for _, pin := range []string{
		"listLiveAnchorTop",
		"sources-list-live",
		"files-list-live",
		"tasks-list-live",
		"notifications-list-live",
		"getBoundingClientRect().top",
	} {
		if !strings.Contains(js, pin) {
			t.Fatalf("app.js missing live-scroll pin %q", pin)
		}
	}
}
