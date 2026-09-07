package api_test

import (
	"bytes"
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

func TestEnqueueTaskForcesManualOrigin(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := mountAPI(t, lib, q)

	// Extra JSON keys (origin/parent) must be ignored; server always stores manual.
	body := []byte(`{"kind":"scan","domain":"example.com","origin":"scheduled","parent_task_id":99}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp gen.EnqueueTaskResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	got, err := q.GetTask(resp.Id)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Origin != queue.OriginManual {
		t.Fatalf("origin=%q want manual", got.Origin)
	}
	if got.ParentTaskID.Valid {
		t.Fatalf("parent_task_id set: %v", got.ParentTaskID.Int64)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", listRec.Code, listRec.Body.String())
	}
	var tasks []gen.Task
	if err := json.NewDecoder(listRec.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Origin != gen.TaskOriginManual {
		t.Fatalf("list origin: %+v", tasks)
	}
}
