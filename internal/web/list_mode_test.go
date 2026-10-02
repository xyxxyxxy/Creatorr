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
	if libraryListMode(viewThumbs) != ListModeInfinite {
		t.Fatalf("thumbs → infinite")
	}
	if libraryListMode(viewTable) != ListModePaginated {
		t.Fatalf("table → paginated")
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
