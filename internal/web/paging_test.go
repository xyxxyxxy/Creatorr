package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewPageInfo(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/history?status=failed&page=2", nil)
	info := NewPageInfo(r, "page", 2, 120)
	if info.TotalPages != 3 || info.From != 51 || info.To != 100 {
		t.Fatalf("info=%+v", info)
	}
	if !info.HasPrev || !info.HasNext {
		t.Fatalf("expected both prev/next: %+v", info)
	}
	if info.PrevHref != "/history?status=failed" && info.PrevHref != "/history?page=1&status=failed" {
		// page=1 stripped
		if info.PrevHref != "/history?status=failed" {
			t.Fatalf("PrevHref=%q", info.PrevHref)
		}
	}
	if info.NextHref != "/history?page=3&status=failed" && info.NextHref != "/history?status=failed&page=3" {
		t.Fatalf("NextHref=%q", info.NextHref)
	}
}

func TestNewPageInfoSizeHistory(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/history?page=2", nil)
	info := NewPageInfoSize(r, "page", 2, 45, HistoryPageSize)
	if info.PageSize != 20 || info.TotalPages != 3 || info.From != 21 || info.To != 40 {
		t.Fatalf("info=%+v", info)
	}
	if OffsetSize(2, HistoryPageSize) != 20 {
		t.Fatalf("offset=%d", OffsetSize(2, HistoryPageSize))
	}
}

func TestSlicePage(t *testing.T) {
	items := make([]int, 55)
	for i := range items {
		items[i] = i + 1
	}
	r := httptest.NewRequest(http.MethodGet, "/?page=2", nil)
	page, info := SlicePage(r, "page", items)
	if len(page) != 5 || page[0] != 51 || info.Total != 55 {
		t.Fatalf("page=%v info=%+v", page, info)
	}
}

func TestParsePage(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x?sources_page=3", nil)
	if got := ParsePage(r, "sources_page"); got != 3 {
		t.Fatalf("got %d", got)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := ParsePage(r2, "page"); got != 1 {
		t.Fatalf("got %d", got)
	}
}
