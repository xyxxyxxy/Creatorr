package web

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

func TestFileSyncNotifySectionsFromDetail(t *testing.T) {
	resolve := func(id int64) notifyFileSyncIssueRef {
		switch id {
		case 10:
			return notifyFileSyncIssueRef{SeriesID: 1, Series: "Coop", Title: "Ep One", Missing: false}
		case 11:
			return notifyFileSyncIssueRef{SeriesID: 1, Series: "Coop", Title: "Ep Two", Missing: false}
		case 12:
			return notifyFileSyncIssueRef{SeriesID: 2, Series: "Other", Title: "Size Drift", Missing: false}
		default:
			return notifyFileSyncIssueRef{Missing: true}
		}
	}
	detail, err := json.Marshal(map[string]any{
		"missing_ids":            []int64{10},
		"externally_changed_ids": []int64{12},
		"sidecar_missing": []map[string]any{
			{"VideoID": 11, "FileID": 99, "Kind": "json", "Path": "/lib/s2026e01.info.json"},
		},
		"sidecar_changed": []map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	secs := fileSyncNotifySectionsFromDetail(string(detail), resolve)
	if len(secs) != 2 {
		t.Fatalf("sections=%d want 2: %+v", len(secs), secs)
	}
	miss := secs[0]
	if miss.Heading != "Missing" || miss.Total != 2 || len(miss.Items) != 2 {
		t.Fatalf("missing: %+v", miss)
	}
	if miss.Items[0].ID != 10 || miss.Items[0].Title != "Ep One" || miss.Items[0].Detail != "" {
		t.Fatalf("media missing: %+v", miss.Items[0])
	}
	if miss.Items[1].ID != 11 || miss.Items[1].Detail != "json: s2026e01.info.json" {
		t.Fatalf("sidecar missing: %+v", miss.Items[1])
	}
	chg := secs[1]
	if chg.Heading != "Size changed" || chg.Total != 1 || chg.Items[0].ID != 12 {
		t.Fatalf("changed: %+v", chg)
	}
}

func TestFileSyncNotifySectionsCap(t *testing.T) {
	n := notify.FileSyncIssueListCap + 5
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	detail, err := json.Marshal(map[string]any{"missing_ids": ids})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(id int64) notifyFileSyncIssueRef {
		return notifyFileSyncIssueRef{
			SeriesID: 1,
			Series:   "S",
			Title:    fmt.Sprintf("V%d", id),
			Missing:  false,
		}
	}
	secs := fileSyncNotifySectionsFromDetail(string(detail), resolve)
	if len(secs) != 1 {
		t.Fatalf("sections=%d", len(secs))
	}
	if secs[0].Total != n || secs[0].Extra != 5 || len(secs[0].Items) != notify.FileSyncIssueListCap {
		t.Fatalf("cap: total=%d extra=%d items=%d", secs[0].Total, secs[0].Extra, len(secs[0].Items))
	}
}

func TestFileSyncNotifySectionsEmpty(t *testing.T) {
	if secs := fileSyncNotifySectionsFromDetail("", func(int64) notifyFileSyncIssueRef { return notifyFileSyncIssueRef{} }); secs != nil {
		t.Fatalf("empty detail: %+v", secs)
	}
	if secs := fileSyncNotifySectionsFromDetail(`{"restored_ids":[1]}`, func(int64) notifyFileSyncIssueRef { return notifyFileSyncIssueRef{} }); secs != nil {
		t.Fatalf("no issues: %+v", secs)
	}
}

func TestSidecarIssueDetailLabel(t *testing.T) {
	if got := sidecarIssueDetailLabel("json", "/a/b/ep.info.json"); got != "json: ep.info.json" {
		t.Fatalf("got %q", got)
	}
	if got := sidecarIssueDetailLabel("nfo", ""); got != "nfo" {
		t.Fatalf("got %q", got)
	}
	if got := sidecarIssueDetailLabel("", "/x/y.nfo"); got != "y.nfo" {
		t.Fatalf("got %q", got)
	}
}
