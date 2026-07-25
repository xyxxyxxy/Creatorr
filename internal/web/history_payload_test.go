package web

import (
	"strings"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestHistoryEventError(t *testing.T) {
	cases := []struct {
		event string
		want  bool
	}{
		{"download_failed", true},
		{"source_failed", true},
		{"wanted_download_error", true}, // legacy
		{"wanted_source_error", true},  // legacy
		{library.SourceHistScanError, true},
		{library.SourceHistCancelled, false},
		{"cancelled", false},
		{"download", false}, // legacy pack event
		{"downloaded", false},
		{"packed", false},
		{"stream_packed", false},
		{"beginning_cached", false},
		{"imported", false},
		{"import_created", false},
		{"nfo_regenerated", false},
		{"maturity_repacked", false},
		{"maturity_sidecars_refreshed", false},
		{"sponsorblock_cut", false},
		{"scanned", false},
		{"file_missing", false},
	}
	for _, tc := range cases {
		if got := historyEventError(tc.event); got != tc.want {
			t.Fatalf("historyEventError(%q)=%v want %v", tc.event, got, tc.want)
		}
	}
}

func TestIsEmptyJSONPayload(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"null", true},
		{"{}", true},
		{"{ }", true},
		{"[]", true},
		{`{"a":1}`, false},
		{`[1]`, false},
		{"not-json", false},
	}
	for _, tc := range cases {
		if got := isEmptyJSONPayload(tc.in); got != tc.want {
			t.Fatalf("isEmptyJSONPayload(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseHistoryTimeRangeValues(t *testing.T) {
	tr := parseHistoryTimeRangeValues("2026-07-25T10:30", "2026-07-25T11:00")
	if tr.FromUI != "2026-07-25T10:30" || tr.ToUI != "2026-07-25T11:00" {
		t.Fatalf("ui=%+v", tr)
	}
	from, err := time.Parse(time.RFC3339Nano, tr.From)
	if err != nil || !from.Equal(time.Date(2026, 7, 25, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("from=%q err=%v", tr.From, err)
	}
	to, err := time.Parse(time.RFC3339Nano, tr.To)
	if err != nil {
		t.Fatal(err)
	}
	wantTo := time.Date(2026, 7, 25, 11, 0, 59, 999999999, time.UTC)
	if !to.Equal(wantTo) {
		t.Fatalf("to=%v want %v", to, wantTo)
	}

	swapped := parseHistoryTimeRangeValues("2026-07-25T12:00", "2026-07-25T10:00")
	if swapped.FromUI != "2026-07-25T10:00" || swapped.ToUI != "2026-07-25T12:00" {
		t.Fatalf("swap ui=%+v", swapped)
	}
	if !strings.HasPrefix(swapped.From, "2026-07-25T10:00:00") {
		t.Fatalf("swap from=%q", swapped.From)
	}

	empty := parseHistoryTimeRangeValues("", "nope")
	if empty.FromUI != "" || empty.ToUI != "" || empty.From != "" || empty.To != "" {
		t.Fatalf("empty=%+v", empty)
	}
}
