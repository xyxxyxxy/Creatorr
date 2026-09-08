package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestCancelTaskRequiresManualReason(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "cancel-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := mountAPI(t, lib, q)

	id, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindScan, Domain: "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/tasks/" + strconv.FormatInt(id, 10) + "/cancel"

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no body: status %d want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty reason: status %d want 400 body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"reason":"shutdown"}`)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"Validation"`) {
		t.Fatalf("non-manual: status %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"reason":"manual"}`)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("manual: status %d want 204 body=%s", rec.Code, rec.Body.String())
	}
	got, err := q.GetTask(id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusCancelled || got.Message != "Cancelled (manual)" {
		t.Fatalf("task status=%q message=%q", got.Status, got.Message)
	}
}
