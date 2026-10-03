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
		t.Fatal("status cell must pin icon in a fixed track for list alignment")
	}
	row, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "sources_list_live.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(row), `1fr 11rem`) {
		t.Fatal("sources list row must use fixed-width status track")
	}
}
