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

func TestBrowserListViewsAreInfinite(t *testing.T) {
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

	cases := []struct {
		path       string
		liveID     string
		infiniteID string
	}{
		{"/explorer/browse?type=files&at=browser", "files-list-live", "files-list-infinite"},
		{"/explorer/browse?type=tasks&at=browser", "tasks-list-live", "tasks-list-infinite"},
		{"/explorer/browse?type=notifications&at=browser", "notifications-list-live", "notifications-list-infinite"},
		{"/explorer/browse?type=sources&at=browser", "sources-list-live", "sources-list-infinite"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", tc.path, rec.Code, truncate(rec.Body.String(), 300))
		}
		body := rec.Body.String()
		if !strings.Contains(body, `id="`+tc.liveID+`"`) {
			t.Fatalf("%s missing live root", tc.path)
		}
		if !strings.Contains(body, `data-list-mode="infinite"`) {
			t.Fatalf("%s list view must be infinite: %s", tc.path, truncate(body, 400))
		}
		tableRec := httptest.NewRecorder()
		r.ServeHTTP(tableRec, httptest.NewRequest(http.MethodGet, tc.path+"&view=table", nil))
		if tableRec.Code != 200 {
			t.Fatalf("%s table status %d", tc.path, tableRec.Code)
		}
		tableBody := tableRec.Body.String()
		if !strings.Contains(tableBody, `data-list-mode="paginated"`) {
			t.Fatalf("%s table must stay paginated: %s", tc.path, truncate(tableBody, 400))
		}
		if strings.Contains(tableBody, `id="`+tc.infiniteID+`"`) {
			t.Fatalf("%s table must not render infinite sentinel", tc.path)
		}
	}
}
