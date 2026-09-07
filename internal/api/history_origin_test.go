package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestListHistoryOriginAndFilters(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "history-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := mountAPI(t, lib, q)

	manualID, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindScan, Domain: "a.example", Message: "manual scan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CancelWithReason(manualID, queue.CancelReasonManual); err != nil {
		t.Fatal(err)
	}

	var schedID int64
	err = d.SQL.QueryRow(`
		INSERT INTO tasks (kind, status, domain, message, origin, created_at, finished_at)
		VALUES (?, ?, ?, 'sync done', ?, datetime('now'), datetime('now'))
		RETURNING id
	`, queue.KindSyncFiles, queue.StatusDone, queue.SystemDomain, queue.OriginScheduled).Scan(&schedID)
	if err != nil {
		t.Fatal(err)
	}

	parent, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "b.example", Message: "parent",
	})
	if err != nil {
		t.Fatal(err)
	}
	var child int64
	err = d.SQL.QueryRow(`
		INSERT INTO tasks (kind, status, domain, message, origin, parent_task_id, created_at, finished_at)
		VALUES (?, ?, ?, 'child verify', ?, ?, datetime('now'), datetime('now'))
		RETURNING id
	`, queue.KindIntegrityCheckInitial, queue.StatusDone, queue.SystemDomain, queue.OriginTask, parent).Scan(&child)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CancelWithReason(parent, queue.CancelReasonManual); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d body=%s", rec.Code, rec.Body.String())
	}
	var all []gen.HistoryItem
	if err := json.NewDecoder(rec.Body).Decode(&all); err != nil {
		t.Fatal(err)
	}
	if len(all) < 3 {
		t.Fatalf("got %d history items, want >= 3", len(all))
	}
	byID := map[int64]gen.HistoryItem{}
	for _, it := range all {
		byID[it.Id] = it
		if it.Origin == "" {
			t.Fatalf("item %d missing origin", it.Id)
		}
	}
	if byID[manualID].Origin != gen.HistoryItemOriginManual {
		t.Fatalf("manual origin=%q", byID[manualID].Origin)
	}
	if byID[schedID].Origin != gen.HistoryItemOriginScheduled {
		t.Fatalf("scheduled origin=%q", byID[schedID].Origin)
	}
	if byID[child].Origin != gen.HistoryItemOriginTask {
		t.Fatalf("child origin=%q", byID[child].Origin)
	}
	if byID[child].ParentTaskId == nil || *byID[child].ParentTaskId != parent {
		t.Fatalf("child parent_task_id=%v want %d", byID[child].ParentTaskId, parent)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history?origin=scheduled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("origin filter: status %d body=%s", rec.Code, rec.Body.String())
	}
	var scheduled []gen.HistoryItem
	if err := json.NewDecoder(rec.Body).Decode(&scheduled); err != nil {
		t.Fatal(err)
	}
	if len(scheduled) != 1 || scheduled[0].Id != schedID {
		t.Fatalf("origin=scheduled: %+v", scheduled)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history?domain=a.example&kind=scan", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("domain/kind filter: status %d body=%s", rec.Code, rec.Body.String())
	}
	var filtered []gen.HistoryItem
	if err := json.NewDecoder(rec.Body).Decode(&filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Id != manualID {
		t.Fatalf("domain+kind: %+v", filtered)
	}
}
