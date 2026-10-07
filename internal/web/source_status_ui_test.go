package web

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSourceStatusIconUnscheduledDistinct(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "source_status_icon.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `eq .Kind "unscheduled"`) || !strings.Contains(s, `calendar-minus`) {
		t.Fatal("unscheduled must use calendar-minus (distinct from scheduled calendar-clock)")
	}
	if !strings.Contains(s, `eq .Kind "scheduled"`) || !strings.Contains(s, `calendar-clock`) {
		t.Fatal("scheduled must keep calendar-clock")
	}
	cell, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "source_status_cell.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cell), `grid-cols-[1.25rem_minmax(0,1fr)]`) {
		t.Fatal("status cell must pin icon in a fixed track for table alignment")
	}
	row, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "sources_list_live.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(row), `1fr 11rem`) {
		t.Fatal("sources list row must not reserve a Status column; scan+discovered live on the second line")
	}
	if !strings.Contains(string(row), `source_list_meta`) {
		t.Fatal("sources list row must render source_list_meta second line")
	}
	meta, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "source_list_meta.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), `text-xs opacity-60`) || !strings.Contains(string(meta), `sourceListMeta`) {
		t.Fatal("source_list_meta must be a muted second-line sentence")
	}
	bulkJS, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "ui", "src", "js", "sources_bulk.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bulkJS), `11rem`) {
		t.Fatal("sources_bulk.js must not reinject the removed Status 11rem track")
	}
	scanActs, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "source_scan_actions.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(scanActs), `action="/actions/scan-source" class="contents"`) {
		t.Fatal("scan actions must not nest join-items in form.contents (breaks daisyUI join seam)")
	}
	if !strings.Contains(string(scanActs), `form="form-scan-source-{{.ID}}"`) {
		t.Fatal("enabled Scan must submit via form= so join-items stay siblings")
	}
}
