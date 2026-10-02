package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLibraryListMode(t *testing.T) {
	if libraryListMode(viewList) != ListModeInfinite {
		t.Fatalf("list → infinite")
	}
	if libraryListMode(viewCards) != ListModeInfinite {
		t.Fatalf("cards → infinite")
	}
	if libraryListMode(viewGallery) != ListModeInfinite {
		t.Fatalf("gallery → infinite")
	}
	if libraryListMode(viewTable) != ListModePaginated {
		t.Fatalf("table → paginated")
	}
}

func TestCanonicalizeViewMode(t *testing.T) {
	if got := canonicalizeViewMode("thumbs"); got != viewCards {
		t.Fatalf("thumbs → cards, got %q", got)
	}
	if got := canonicalizeViewMode("cards"); got != viewCards {
		t.Fatalf("got %q", got)
	}
	if got := canonicalizeViewMode("gallery"); got != viewGallery {
		t.Fatalf("got %q", got)
	}
	if got := canonicalizeViewMode("nope"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveViewModeThumbsAlias(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/series?view=thumbs", nil)
	mode, write := resolveViewMode(r, cookieModeSeries, viewList)
	if mode != viewCards || !write {
		t.Fatalf("got mode=%q write=%v", mode, write)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/series", nil)
	r2.AddCookie(&http.Cookie{Name: cookieModeSeries, Value: "thumbs"})
	mode, write = resolveViewMode(r2, cookieModeSeries, viewList)
	if mode != viewCards || write {
		t.Fatalf("cookie thumbs → cards, got mode=%q write=%v", mode, write)
	}
}

func TestParseThrough(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/videos?through=3", nil)
	if got := ParseThrough(r); got != 3 {
		t.Fatalf("got %d", got)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/videos", nil)
	if got := ParseThrough(r2); got != 1 {
		t.Fatalf("got %d", got)
	}
}

func TestResolveInfiniteLoadFull(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/videos?view=list&through=2", nil)
	load := resolveInfiniteLoad(r, 100, "videos-list-live", "videos-list-infinite", "page")
	if load.Append || load.Through != 2 || load.LoadedCount != 40 {
		t.Fatalf("load=%+v", load)
	}
	if !load.HasMore || load.NextHref == "" {
		t.Fatalf("expected next: %+v", load)
	}
	if load.SkeletonCount != InfiniteChunkSize {
		// 100 total, 40 loaded → 60 rem → capped at chunk
		t.Fatalf("skeleton=%d want %d", load.SkeletonCount, InfiniteChunkSize)
	}
	limit, offset := infiniteLimitOffset(load)
	if limit != 40 || offset != 0 {
		t.Fatalf("limit=%d offset=%d", limit, offset)
	}
}

func TestResolveInfiniteLoadRefreshClamp(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/videos?through=20", nil)
	load := resolveInfiniteLoad(r, 1000, "videos-list-live", "videos-list-infinite", "page")
	if !load.ThroughClamped || load.Through != maxThroughPages(InfiniteChunkSize, MaxRefreshRows) {
		t.Fatalf("clamp=%+v", load)
	}
	if load.LoadedCount != MaxRefreshRows {
		t.Fatalf("loaded=%d", load.LoadedCount)
	}
}

func TestResolveInfiniteLoadAppend(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/videos?view=list&page=2", nil)
	r.Header.Set("HX-Target", "videos-list-infinite")
	load := resolveInfiniteLoad(r, 100, "videos-list-live", "videos-list-infinite", "page")
	if !load.Append || load.Through != 2 {
		t.Fatalf("load=%+v", load)
	}
	limit, offset := infiniteLimitOffset(load)
	if limit != InfiniteChunkSize || offset != InfiniteChunkSize {
		t.Fatalf("limit=%d offset=%d", limit, offset)
	}
	if !load.HasMore {
		t.Fatalf("expected has more")
	}
	if load.SkeletonCount != InfiniteChunkSize {
		// page 2 ends at 40 of 100 → 60 rem
		t.Fatalf("skeleton=%d", load.SkeletonCount)
	}
}

func TestSkeletonCountPartialRemainder(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/videos?view=list", nil)
	load := resolveInfiniteLoad(r, 25, "videos-list-live", "videos-list-infinite", "page")
	if load.SkeletonCount != 5 {
		t.Fatalf("skeleton=%d want 5 (25-20)", load.SkeletonCount)
	}
}

func TestResolvePaginatedAndFixed(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/videos?view=table&page=2", nil)
	pag := resolvePaginatedLoad(r, 45, VideoPageSize, "videos-list-live", "page")
	if pag.Mode != ListModePaginated || pag.Page.PageSize != 10 || pag.Page.Page != 2 {
		t.Fatalf("pag=%+v", pag)
	}
	fix := resolveFixedLoad(50, FixedDefault)
	if fix.Mode != ListModeFixed || fix.LoadedCount != 10 || fix.HasMore {
		t.Fatalf("fix=%+v", fix)
	}
}
