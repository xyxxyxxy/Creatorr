package web

import (
	"net/http"
	"net/url"
	"strconv"
)

// PageSize is the default page length for admin list tables.
const PageSize = 50

// VideoPageSize is the page length for series/source video lists (taller rows with thumbs).
const VideoPageSize = 20

// SeriesPageSize is the page length for the /series media list.
const SeriesPageSize = 20

// HistoryPageSize is the page length for /history Notifications and Tasks tables.
const HistoryPageSize = 20

// TaskPageSize is the page length for open tasks in each /tasks domain lane.
const TaskPageSize = 20

// PageInfo drives the pagination partial under a list table.
type PageInfo struct {
	Page       int
	PageSize   int
	Total      int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	From       int
	To         int
	PrevHref   string
	NextHref   string
	Show       bool // true when more than one page (pager UI visible)
	// LiveTarget: when set (element id without #), pager links HTMX-get the
	// same href, select/swap that id, and push the URL - no full reload.
	LiveTarget string
}

// ParsePage reads a 1-based page number from query (default 1).
func ParsePage(r *http.Request, param string) int {
	if param == "" {
		param = "page"
	}
	n, err := strconv.Atoi(r.URL.Query().Get(param))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// NewPageInfo builds pager state and prev/next hrefs from the request URL.
func NewPageInfo(r *http.Request, param string, page, total int) PageInfo {
	return NewPageInfoSize(r, param, page, total, PageSize)
}

// NewPageInfoSize is NewPageInfo with an explicit page length.
func NewPageInfoSize(r *http.Request, param string, page, total, pageSize int) PageInfo {
	if param == "" {
		param = "page"
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = PageSize
	}
	info := PageInfo{
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}
	if total <= 0 {
		info.Page = 1
		info.TotalPages = 0
		return info
	}
	info.TotalPages = (total + pageSize - 1) / pageSize
	if page > info.TotalPages {
		page = info.TotalPages
		info.Page = page
	}
	info.From = (page-1)*pageSize + 1
	info.To = page * pageSize
	if info.To > total {
		info.To = total
	}
	info.Show = info.TotalPages > 1
	info.HasPrev = page > 1
	info.HasNext = page < info.TotalPages
	if info.HasPrev {
		info.PrevHref = pageHref(r, param, page-1)
	}
	if info.HasNext {
		info.NextHref = pageHref(r, param, page+1)
	}
	return info
}

func pageHref(r *http.Request, param string, page int) string {
	q := r.URL.Query()
	if q == nil {
		q = url.Values{}
	}
	if page <= 1 {
		q.Del(param)
	} else {
		q.Set(param, strconv.Itoa(page))
	}
	u := *r.URL
	u.RawQuery = q.Encode()
	return u.String()
}

// Offset returns SQL OFFSET for a 1-based page using PageSize.
func Offset(page int) int {
	return OffsetSize(page, PageSize)
}

// OffsetSize returns SQL OFFSET for a 1-based page and page length.
func OffsetSize(page, pageSize int) int {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = PageSize
	}
	return (page - 1) * pageSize
}

// SlicePage returns one page of items and matching PageInfo.
func SlicePage[T any](r *http.Request, param string, items []T) ([]T, PageInfo) {
	return SlicePageSize(r, param, items, PageSize)
}

// SlicePageSize is SlicePage with an explicit page length.
func SlicePageSize[T any](r *http.Request, param string, items []T, pageSize int) ([]T, PageInfo) {
	page := ParsePage(r, param)
	total := len(items)
	info := NewPageInfoSize(r, param, page, total, pageSize)
	if total == 0 {
		return nil, info
	}
	start := OffsetSize(info.Page, info.PageSize)
	if start >= total {
		return nil, info
	}
	end := start + info.PageSize
	if end > total {
		end = total
	}
	return items[start:end], info
}
