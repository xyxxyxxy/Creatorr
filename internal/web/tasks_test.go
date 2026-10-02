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

func TestTasksLanePagesAtTen(t *testing.T) {
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
			Origin:  queue.OriginManual,
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
	if !strings.Contains(body, `id="tasks-list-live"`) {
		t.Fatalf("missing tasks-list-live")
	}
	if strings.Contains(body, `aria-label="Notification filters"`) || strings.Contains(body, `aria-label="Task filters"`) {
		t.Fatalf("/tasks must not show Explorer filters")
	}
	if !strings.Contains(body, "example.com") {
		t.Fatalf("missing example.com lane")
	}
	if got := strings.Count(body, `id="task-row-`); got != web.TaskPageSize {
		t.Fatalf("lane page 1 rows=%d want %d", got, web.TaskPageSize)
	}
	if !strings.Contains(body, "1 / 3") {
		t.Fatalf("missing pager 1 / 3")
	}
	if !strings.Contains(body, `hx-target="#tasks-list-live"`) {
		t.Fatalf("pager missing live target")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/tasks?p_example_com=2", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("page2 status %d: %s", rec2.Code, rec2.Body.String())
	}
	body2 := rec2.Body.String()
	if got := strings.Count(body2, `id="task-row-`); got != web.TaskPageSize {
		t.Fatalf("lane page 2 rows=%d want %d", got, web.TaskPageSize)
	}
}

func TestBrowserTasksAndNotificationsTypes(t *testing.T) {
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

	failID, err := q.Enqueue(queue.EnqueueParams{
		Origin:  queue.OriginManual,
		Kind:    queue.KindScan,
		Domain:  "example.com",
		Message: "scan start",
		Payload: map[string]any{"source_id": int64(1), "mode": "scan"},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := q.ClaimNext()
	if err != nil || claimed == nil || claimed.ID != failID {
		t.Fatalf("claim: err=%v task=%v", err, claimed)
	}
	if err := q.Finish(failID, queue.StatusFailed, "Download failed", "DownloadFailed", "ERROR: unable to download"); err != nil {
		t.Fatal(err)
	}

	for _, typ := range []string{"tasks", "notifications"} {
		req := httptest.NewRequest(http.MethodGet, "/browser?type="+typ, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", typ, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `aria-current="page"`) {
			t.Fatalf("%s missing active type tab", typ)
		}
		if typ == "tasks" && !strings.Contains(body, `id="tasks-list-live"`) {
			t.Fatalf("tasks browser missing live panel")
		}
		if typ == "tasks" && !strings.Contains(body, `placeholder="Search"`) {
			t.Fatalf("tasks browser missing Search placeholder")
		}
		if typ == "tasks" && !strings.Contains(body, `text-error`) {
			t.Fatalf("failed task message should use text-error")
		}
		if typ == "tasks" && strings.Contains(body, `aria-label="Status: Queued, Running"`) {
			t.Fatalf("Browser Tasks must not default Status to Queued+Running")
		}
		if typ == "tasks" && strings.Contains(body, "All statuses") {
			t.Fatalf("Filter must not offer All statuses clear row")
		}
		if typ == "notifications" && !strings.Contains(body, `id="notifications-list-live"`) {
			t.Fatalf("notifications browser missing live panel")
		}
		if typ == "notifications" && strings.Contains(body, `js-list-filters`) && !strings.Contains(body, `placeholder="Search"`) {
			t.Fatalf("notifications browser missing Search placeholder")
		}
	}

	reqTasks := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	recTasks := httptest.NewRecorder()
	r.ServeHTTP(recTasks, reqTasks)
	if recTasks.Code != 200 {
		t.Fatalf("/tasks status %d", recTasks.Code)
	}
	tasksBody := recTasks.Body.String()
	if strings.Contains(tasksBody, `aria-label="Status: Queued, Running"`) {
		t.Fatalf("/tasks must not show Status filter chip")
	}
	if !strings.Contains(tasksBody, "system") {
		t.Fatalf("/tasks missing system lane")
	}

	req := httptest.NewRequest(http.MethodGet, "/history", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("/history status %d want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "type=tasks") {
		t.Fatalf("/history redirect = %q", loc)
	}
}
