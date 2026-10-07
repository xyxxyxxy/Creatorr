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

	reqLegacy := httptest.NewRequest(http.MethodGet, "/tasks?p_example_com=1", nil)
	recLegacy := httptest.NewRecorder()
	r.ServeHTTP(recLegacy, reqLegacy)
	if recLegacy.Code != http.StatusMovedPermanently {
		t.Fatalf("legacy /tasks status %d want %d", recLegacy.Code, http.StatusMovedPermanently)
	}
	if loc := recLegacy.Header().Get("Location"); loc != "/queues?p_example_com=1" {
		t.Fatalf("legacy /tasks Location=%q", loc)
	}

	req := httptest.NewRequest(http.MethodGet, "/queues", nil)
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
		t.Fatalf("/queues must not show Explorer filters")
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

	req2 := httptest.NewRequest(http.MethodGet, "/queues?p_example_com=2", nil)
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
			t.Fatalf("Browser Queues must not default Status to Queued+Running")
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

	reqTasks := httptest.NewRequest(http.MethodGet, "/queues", nil)
	recTasks := httptest.NewRecorder()
	r.ServeHTTP(recTasks, reqTasks)
	if recTasks.Code != 200 {
		t.Fatalf("/queues status %d", recTasks.Code)
	}
	tasksBody := recTasks.Body.String()
	if strings.Contains(tasksBody, `aria-label="Status: Queued, Running"`) {
		t.Fatalf("/queues must not show Status filter chip")
	}
	if !strings.Contains(tasksBody, "system") {
		t.Fatalf("/queues missing system lane")
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

func TestBrowserTasksSearchKeepsQuery(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "tasks-q.db"))
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

	keep, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindScan, Domain: "example.com",
		Message: "unique-alpha-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindScan, Domain: "other.example",
		Message: "beta-other",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=tasks&at=browser&q=unique-alpha", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="q" value="unique-alpha"`) {
		t.Fatalf("search input must keep q value: %s", body[:min(800, len(body))])
	}
	if !strings.Contains(body, `data-filter-total="1"`) {
		t.Fatalf("want one matching task, got: %s", body[:min(800, len(body))])
	}
	if !strings.Contains(body, strconv.FormatInt(keep, 10)) {
		t.Fatalf("want matching task id %d in body", keep)
	}
}

func TestTasksStatusJoinFilterChips(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "status-join.db"))
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

	get := func(path string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", path, rec.Code, truncate(rec.Body.String(), 400))
		}
		return rec.Body.String()
	}

	multi := get("/explorer/browse?type=tasks&at=browser&status=pending&status=running")
	if strings.Contains(multi, `aria-label="Status: Queued, Running"`) {
		t.Fatalf("must not use mega-chip Status: Queued, Running: %s", truncate(multi, 800))
	}
	if !strings.Contains(multi, `role="group" aria-label="Status"`) ||
		!strings.Contains(multi, `aria-label="Clear Status"`) {
		t.Fatalf("Status join prefix missing clear control: %s", truncate(multi, 800))
	}
	clearIdx := strings.Index(multi, `aria-label="Clear Status"`)
	clearSnip := multi[clearIdx:]
	if end := strings.Index(clearSnip, "</a>"); end > 0 {
		clearSnip = clearSnip[:end]
	}
	if !strings.Contains(clearSnip, "status=") || strings.Contains(clearSnip, "status=pending") ||
		strings.Contains(clearSnip, "status=running") {
		t.Fatalf("Clear Status should drop all status values: %s", clearSnip)
	}
	if !strings.Contains(multi, `aria-label="Queued"`) || !strings.Contains(multi, `aria-label="Running"`) {
		t.Fatalf("want per-value Status chips Queued + Running: %s", truncate(multi, 800))
	}
	if !strings.Contains(multi, `aria-label="2 selected"`) || !strings.Contains(multi, `>2</span>`) {
		t.Fatalf("Filter Status accordion summary must show selected count: %s", truncate(multi, 800))
	}
	if !strings.Contains(multi, `class="btn btn-accent join-item"`) {
		t.Fatalf("Status clear prefix must use solid accent join-item: %s", truncate(multi, 800))
	}
	if !strings.Contains(multi, `class="btn btn-soft btn-accent join-item"`) {
		t.Fatalf("Status value chips must use soft accent join-item: %s", truncate(multi, 800))
	}
	if !strings.Contains(multi, `class="btn btn-accent btn-square"`) {
		t.Fatalf("Clear all must use solid accent square: %s", truncate(multi, 800))
	}
	// Join must be a flex-row sibling, not nested inside daisyUI .filter.
	if strings.Contains(multi, `class="filter"`) &&
		strings.Index(multi, `class="filter"`) < strings.Index(multi, `role="group" aria-label="Status"`) &&
		!strings.Contains(multi[strings.Index(multi, `class="filter"`):strings.Index(multi, `role="group" aria-label="Status"`)], `</div>`) {
		t.Fatalf("Status join must not nest inside .filter: %s", truncate(multi, 900))
	}
	if !strings.Contains(multi, `aria-label="Active filters"`) ||
		!strings.Contains(multi, `flex flex-wrap items-center gap-2 mb-3`) {
		t.Fatalf("active filters row must be flex wrap (joins as siblings): %s", truncate(multi, 600))
	}
	// Drop Queued keeps running; drop Running keeps pending.
	queuedIdx := strings.Index(multi, `aria-label="Queued"`)
	runningIdx := strings.Index(multi, `aria-label="Running"`)
	if queuedIdx < 0 || runningIdx < 0 {
		t.Fatal("missing status chip markers")
	}
	queuedChip := multi[queuedIdx:]
	if end := strings.Index(queuedChip, "/>"); end > 0 {
		queuedChip = queuedChip[:end]
	}
	runningChip := multi[runningIdx:]
	if end := strings.Index(runningChip, "/>"); end > 0 {
		runningChip = runningChip[:end]
	}
	if !strings.Contains(queuedChip, "status=running") || strings.Contains(queuedChip, "status=pending") {
		t.Fatalf("Queued chip should drop pending only: %s", queuedChip)
	}
	if !strings.Contains(runningChip, "status=pending") || strings.Contains(runningChip, "status=running") {
		t.Fatalf("Running chip should drop running only: %s", runningChip)
	}

	single := get("/explorer/browse?type=tasks&at=browser&status=pending")
	if !strings.Contains(single, `role="group" aria-label="Status"`) ||
		!strings.Contains(single, `aria-label="Queued"`) {
		t.Fatalf("single Status must still use join layout: %s", truncate(single, 800))
	}
	// Selected Filter value rows match accordion label color (text-accent).
	i := strings.Index(single, ">Queued</a>")
	if i < 0 {
		t.Fatal("missing Queued Filter option")
	}
	start := strings.LastIndex(single[:i], "<a ")
	if start < 0 || !strings.Contains(single[start:i], `class="text-accent"`) {
		t.Fatalf("selected Filter value must use text-accent: %s", single[start:i+len(">Queued</a>")])
	}
}
