package web_test

import (
	"net/http"
	"net/http/httptest"
	"os"
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

func TestLibraryTreeJSNoAutoExpandOrPathRestore(t *testing.T) {
	b, err := os.ReadFile("ui/src/js/library_tree.js")
	if err != nil {
		b, err = os.ReadFile("internal/web/ui/src/js/library_tree.js")
	}
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, "creatorr_browser_tree_path") || strings.Contains(src, "localStorage") {
		t.Fatal("library tree must not persist expand path in localStorage")
	}
	if strings.Contains(src, "expandFirstRoot") {
		t.Fatal("library tree must not auto-expand on mount")
	}
}

func TestBrowserLibraryTreeToggleAndChildren(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "tree.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	rootDir := t.TempDir()
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: rootDir})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Tree Show", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@tree",
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	up, err := lib.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ep1", Title: "Episode One", SourceID: srcID,
		WebpageURL: "https://www.example.com/watch?v=ep1",
	}, 0)
	if err != nil || up.VideoID <= 0 {
		t.Fatalf("upsert video: %#v err=%v", up, err)
	}
	vidID := up.VideoID
	mediaPath := filepath.Join(rootDir, "ep1.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO files (series_id, video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, ?, ?, 'video', datetime('now'), 1)
	`, ser.ID, vidID, mediaPath); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	get := func(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// Toggle on redirects and sets cookie.
	on := get("/browser?type=series&tree=1")
	if on.Code != http.StatusSeeOther {
		t.Fatalf("tree=1 want redirect, got %d", on.Code)
	}
	var treeCookie *http.Cookie
	for _, c := range on.Result().Cookies() {
		if c.Name == "creatorr_browser_tree" && c.Value == "1" {
			treeCookie = c
		}
	}
	if treeCookie == nil {
		t.Fatalf("missing tree cookie: %v", on.Result().Cookies())
	}

	page := get("/browser?type=series", treeCookie)
	if page.Code != 200 {
		t.Fatalf("tree page %d: %s", page.Code, truncate(page.Body.String(), 300))
	}
	body := page.Body.String()
	if !strings.Contains(body, `id="library-tree-live"`) {
		t.Fatalf("missing library tree: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `data-lucide="folder-tree"`) {
		t.Fatalf("missing Tree control icon: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `aria-pressed="true"`) {
		t.Fatalf("Tree control should be pressed: %s", truncate(body, 400))
	}
	// Type chips not accent-selected while Tree on; chip click must exit Tree.
	if strings.Contains(body, `href="/browser?type=series" class="btn join-item btn-accent"`) {
		t.Fatalf("series type chip should not be accent while Tree on")
	}
	if !strings.Contains(body, `href="/browser?type=notifications&tree=0" class="btn join-item"`) {
		t.Fatalf("type chips must pass tree=0 while Tree on so a click exits Tree")
	}
	if strings.Contains(body, `id="series-list-live"`) {
		t.Fatalf("Explorer series live should not render in Tree mode")
	}
	if !strings.Contains(body, `data-lucide="folder"`) {
		t.Fatalf("root row should use folder icon")
	}

	// Children: root → series (one level only).
	rootKids := get("/explorer/tree-children?parent=root&parent_id=1")
	if rootKids.Code != 200 {
		t.Fatalf("root children %d: %s", rootKids.Code, rootKids.Body.String())
	}
	rk := rootKids.Body.String()
	if !strings.Contains(rk, "Tree Show") || !strings.Contains(rk, `/series/`+strconv.FormatInt(ser.ID, 10)) {
		t.Fatalf("root children want series link: %s", truncate(rk, 500))
	}
	if strings.Contains(rk, "Episode One") {
		t.Fatalf("root children must not nest videos: %s", truncate(rk, 400))
	}

	seriesKids := get("/explorer/tree-children?parent=series&parent_id=" + strconv.FormatInt(ser.ID, 10))
	if seriesKids.Code != 200 {
		t.Fatalf("series children %d", seriesKids.Code)
	}
	sk := seriesKids.Body.String()
	if !strings.Contains(sk, `/series/`+strconv.FormatInt(ser.ID, 10)+`/sources/`+strconv.FormatInt(srcID, 10)) {
		t.Fatalf("series children want source link: %s", truncate(sk, 500))
	}
	if strings.Contains(sk, "Episode One") {
		t.Fatalf("series children must not nest videos")
	}

	srcKids := get("/explorer/tree-children?parent=source&parent_id=" + strconv.FormatInt(srcID, 10))
	if srcKids.Code != 200 {
		t.Fatalf("source children %d", srcKids.Code)
	}
	vk := srcKids.Body.String()
	if !strings.Contains(vk, "Episode One") || !strings.Contains(vk, `/videos/`+strconv.FormatInt(vidID, 10)) {
		t.Fatalf("source children want video: %s", truncate(vk, 500))
	}
	if strings.Contains(vk, "ep1.mkv") {
		t.Fatalf("source children must not nest files")
	}

	fileKids := get("/explorer/tree-children?parent=video&parent_id=" + strconv.FormatInt(vidID, 10))
	if fileKids.Code != 200 {
		t.Fatalf("video children %d", fileKids.Code)
	}
	fk := fileKids.Body.String()
	if !strings.Contains(fk, "ep1.mkv") || !strings.Contains(fk, `/files/`) {
		t.Fatalf("video children want file link: %s", truncate(fk, 500))
	}
	if strings.Contains(fk, `data-tree-expand`) {
		t.Fatalf("file leaves must not be expandable")
	}

	// Exit tree via type chip (same as Notifications join link).
	off := get("/browser?type=notifications&tree=0", treeCookie)
	if off.Code != http.StatusSeeOther {
		t.Fatalf("tree=0 want redirect, got %d", off.Code)
	}
	var offCookie *http.Cookie
	for _, c := range off.Result().Cookies() {
		if c.Name == "creatorr_browser_tree" {
			offCookie = c
		}
	}
	if offCookie == nil || offCookie.Value == "1" {
		t.Fatalf("tree=0 should clear tree cookie, got %#v", offCookie)
	}
	notif := get("/browser?type=notifications", offCookie)
	if notif.Code != 200 {
		t.Fatalf("notifications explorer %d", notif.Code)
	}
	nb := notif.Body.String()
	if strings.Contains(nb, `id="library-tree-live"`) {
		t.Fatalf("tree should be off after type chip exit")
	}
	if !strings.Contains(nb, `id="notifications-list-live"`) {
		t.Fatalf("want notifications explorer after type chip exit")
	}
}
