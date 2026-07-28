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

func TestLibrarySettingsSponsorBlock(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/library", nil))
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(body, 400))
	}
	if !strings.Contains(body, "SponsorBlock") {
		t.Fatalf("missing SponsorBlock UI: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "/settings/general") {
		t.Fatalf("missing settings nav (template likely errored before foot): %s", truncate(body, 800))
	}
	if !strings.Contains(body, "sponsorblock_mark") || !strings.Contains(body, "sponsor.ajay.app") {
		t.Fatalf("missing SB fields/attribution: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "sponsorblock_reencode_cut") || !strings.Contains(body, "Re-encode on cut") {
		t.Fatalf("missing reencode_cut UI: %s", truncate(body, 500))
	}
}
