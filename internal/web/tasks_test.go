package web_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestTasksLanePagesAtTwenty(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	const n = 25
	for i := 1; i <= n; i++ {
		if _, err := q.Enqueue(queue.EnqueueParams{
			Kind:    queue.KindScan,
			Domain:  "example.com",
			Payload: map[string]any{"source_id": int64(i), "mode": "scan"},
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := strings.Count(body, `data-task-row-status=`); got != web.TaskPageSize {
		t.Fatalf("page 1 rows=%d want %d", got, web.TaskPageSize)
	}
	if !strings.Contains(body, "1 / 2") {
		t.Fatalf("missing pager 1 / 2")
	}
	if !strings.Contains(body, "p_example_com=2") {
		t.Fatalf("missing per-lane next href")
	}
	if !strings.Contains(body, `hx-target="#tasks-live"`) {
		t.Fatalf("pager missing live target")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/tasks?p_example_com=2", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("page2 status %d: %s", rec2.Code, rec2.Body.String())
	}
	body2 := rec2.Body.String()
	if got := strings.Count(body2, `data-task-row-status=`); got != n-web.TaskPageSize {
		t.Fatalf("page 2 rows=%d want %d", got, n-web.TaskPageSize)
	}
	if !strings.Contains(body2, `data-scheduled-task`) {
		t.Fatalf("system scheduled rows missing on host page 2")
	}
}
