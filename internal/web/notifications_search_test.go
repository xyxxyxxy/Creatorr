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
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func TestNotificationsExplorerSearchKeepsQ(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "nsearch.db"))
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

	keep, err := notify.InsertNotification(d, notify.EventCookieInvalid, "unique-alpha-title", "body-a", 0, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := notify.InsertNotification(d, notify.EventRateLimited, "other", "beta-body", 0, false, ""); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=notifications&at=browser&q=unique-alpha", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="q" value="unique-alpha"`) {
		t.Fatalf("search input must keep q value: %s", truncate(body, 800))
	}
	if !strings.Contains(body, `data-filter-total="1"`) {
		t.Fatalf("want one matching notification, got: %s", truncate(body, 800))
	}
	if !strings.Contains(body, strconv.FormatInt(keep, 10)) {
		t.Fatalf("want matching notification id %d in body", keep)
	}
}
