package web

import (
	"net/http"
	"net/url"
	"strconv"
)

// ListMode is how a library list loads rows (orthogonal to view list|cards|gallery|table).
type ListMode string

const (
	ListModePaginated ListMode = "paginated"
	ListModeInfinite  ListMode = "infinite"
	ListModeFixed     ListMode = "fixed"
)

const (
	// VideoPageSize is the page length for paginated video lists (series detail, table).
	VideoPageSize = 10
	// SeriesPageSize is the page length for paginated /series table view.
	SeriesPageSize = 10
	// InfiniteChunkSize is rows per infinite-scroll chunk (/series and /videos list|cards|gallery).
	InfiniteChunkSize = 20
	// FixedDefault is the one-shot size for fixed lists (Overview Recent).
	FixedDefault = 10
	// MaxInfiniteRows caps auto-load append browsing.
	MaxInfiniteRows = 500
	// MaxRefreshRows caps rows returned on a full live outerHTML refresh (Want/Ignore, etc.).
	MaxRefreshRows = 100
)

// ListLoad drives list chrome (pager / sentinel / neither) and SQL limit/offset.
type ListLoad struct {
	Mode           ListMode
	PageSize       int
	Through        int  // infinite: pages loaded from start (1..)
	ThroughClamped bool // true when Through was reduced for MaxRefreshRows
	LoadedCount    int
	HasMore        bool
	NextHref       string // infinite sentinel; empty when no more / past browse cap
	NextPage       int    // page number for NextHref (0 if none)
	SkeletonCount  int    // placeholder rows in sentinel (min(chunk, remaining))
	Page           PageInfo
	Append         bool // HTMX append chunk (not full live root)
	BrowseCapped   bool // reached MaxInfiniteRows; show refine message, no sentinel
}

// libraryListMode returns infinite for /series and /videos list|cards|gallery; else paginated.
func libraryListMode(viewMode string) ListMode {
	if viewMode == viewTable {
		return ListModePaginated
	}
	return ListModeInfinite
}

// ParseThrough reads 1-based through from query (default 1).
func ParseThrough(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("through"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// maxThroughPages returns max through for a row budget at chunk size.
func maxThroughPages(chunk, maxRows int) int {
	if chunk < 1 {
		chunk = InfiniteChunkSize
	}
	if maxRows < chunk {
		return 1
	}
	return maxRows / chunk
}

// resolveInfiniteLoad builds ListLoad for infinite list|cards|gallery.
// appendTarget is the element id HTMX targeted for a chunk append (e.g. videos-list-infinite).
func resolveInfiniteLoad(r *http.Request, total int, liveTarget, appendTarget, pageParam string) ListLoad {
	chunk := InfiniteChunkSize
	if pageParam == "" {
		pageParam = "page"
	}
	hxTarget := r.Header.Get("HX-Target")
	append := appendTarget != "" && hxTarget == appendTarget

	out := ListLoad{
		Mode:     ListModeInfinite,
		PageSize: chunk,
		Append:   append,
	}

	browseMaxPages := maxThroughPages(chunk, MaxInfiniteRows)
	refreshMaxPages := maxThroughPages(chunk, MaxRefreshRows)

	if append {
		page := ParsePage(r, pageParam)
		if page < 1 {
			page = 1
		}
		if page > browseMaxPages {
			out.BrowseCapped = true
			out.Through = browseMaxPages
			out.LoadedCount = minInt(total, browseMaxPages*chunk)
			return out
		}
		out.Through = page
		start := OffsetSize(page, chunk)
		if start >= total {
			out.LoadedCount = total
			return out
		}
		end := start + chunk
		if end > total {
			end = total
		}
		out.LoadedCount = end - start
		out.HasMore = end < total && page < browseMaxPages
		if out.HasMore {
			out.NextHref = infiniteNextHref(r, pageParam, page+1)
			out.NextPage = page + 1
			out.SkeletonCount = skeletonCount(chunk, total-end)
		} else if end < total {
			out.BrowseCapped = true
		}
		// PageInfo for templates that need From/To of this chunk
		out.Page = NewPageInfoSize(r, pageParam, page, total, chunk)
		out.Page.LiveTarget = liveTarget
		return out
	}

	through := ParseThrough(r)
	if through > browseMaxPages {
		through = browseMaxPages
	}
	clamped := through
	if clamped > refreshMaxPages {
		clamped = refreshMaxPages
		out.ThroughClamped = true
	}
	out.Through = clamped
	limit := clamped * chunk
	if limit > total {
		limit = total
	}
	out.LoadedCount = limit
	out.HasMore = limit < total && clamped < browseMaxPages
	if out.HasMore {
		out.NextHref = infiniteNextHref(r, pageParam, clamped+1)
		out.NextPage = clamped + 1
		out.SkeletonCount = skeletonCount(chunk, total-limit)
	} else if limit < total {
		out.BrowseCapped = true
	}
	out.Page = NewPageInfoSize(r, pageParam, 1, total, chunk)
	out.Page.From = 1
	if total > 0 {
		out.Page.To = limit
		out.Page.PageSize = chunk
	}
	out.Page.LiveTarget = liveTarget
	out.Page.Show = false
	return out
}

// resolvePaginatedLoad builds ListLoad for classic pager lists.
func resolvePaginatedLoad(r *http.Request, total, pageSize int, liveTarget, pageParam string) ListLoad {
	if pageParam == "" {
		pageParam = "page"
	}
	page := ParsePage(r, pageParam)
	info := NewPageInfoSize(r, pageParam, page, total, pageSize)
	info.LiveTarget = liveTarget
	loaded := 0
	if total > 0 {
		loaded = info.To - info.From + 1
	}
	return ListLoad{
		Mode:        ListModePaginated,
		PageSize:    pageSize,
		Through:     1,
		LoadedCount: loaded,
		HasMore:     info.HasNext,
		Page:        info,
	}
}

// resolveFixedLoad builds ListLoad for a one-shot teaser (no pager/sentinel).
func resolveFixedLoad(total, size int) ListLoad {
	if size < 1 {
		size = FixedDefault
	}
	loaded := total
	if loaded > size {
		loaded = size
	}
	return ListLoad{
		Mode:        ListModeFixed,
		PageSize:    size,
		Through:     1,
		LoadedCount: loaded,
	}
}

// infiniteLimitOffset returns SQL limit/offset for an infinite ListLoad.
func infiniteLimitOffset(load ListLoad) (limit, offset int) {
	chunk := load.PageSize
	if chunk < 1 {
		chunk = InfiniteChunkSize
	}
	if load.Append {
		return chunk, OffsetSize(load.Through, chunk)
	}
	return load.Through * chunk, 0
}

func infiniteNextHref(r *http.Request, pageParam string, page int) string {
	q := r.URL.Query()
	if q == nil {
		q = url.Values{}
	}
	// Append requests use page=; keep through out of the sentinel href so a
	// mid-flight full refresh does not double-count. Client replaceState owns through.
	q.Del("through")
	if page <= 1 {
		q.Del(pageParam)
	} else {
		q.Set(pageParam, strconv.Itoa(page))
	}
	u := *r.URL
	u.RawQuery = q.Encode()
	return u.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// skeletonCount is how many placeholder rows the infinite sentinel should paint.
func skeletonCount(chunk, remaining int) int {
	if remaining < 1 {
		return 0
	}
	if chunk < 1 {
		chunk = InfiniteChunkSize
	}
	return minInt(chunk, remaining)
}
