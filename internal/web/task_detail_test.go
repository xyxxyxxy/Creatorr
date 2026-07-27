package web

import (
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestParsePOTDetail(t *testing.T) {
	got := parsePOTDetail(`{"po-token":{"state":"issued","detail":"Retrieved a gvs PO Token","fetch":"auto"}}`)
	if got == nil || got.State != "issued" || got.Label != "Issued" || got.Fetch != "auto" {
		t.Fatalf("got %#v", got)
	}
	if parsePOTDetail(`{"created_ids":[1]}`) != nil {
		t.Fatal("expected nil without pot")
	}
	if parsePOTDetail("not-json") != nil {
		t.Fatal("expected nil for non-json")
	}
}

func TestTaskDetailFieldsCreatedState(t *testing.T) {
	h := &Handler{}
	detail := `{
		"created": 3,
		"created_ids": [10, 12, 13],
		"updated_ids": [20],
		"skipped_title_regexp_include": [
			{"remote_id": "r1", "title": "Skipped Vlog"},
			{"remote_id": "r2", "title": "Another Skip"}
		],
		"skipped_title_regexp_exclude": [
			{"remote_id": "r3", "title": "Trailer Cut"}
		],
		"ignored_media_type_ids": [13],
		"ignored_index_as_ignored_ids": [12]
	}`
	fields := h.taskDetailFields(detail)
	byKey := map[string]detailField{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	created, ok := byKey["created_ids"]
	if !ok || !created.IsVideoList || len(created.Videos) != 3 {
		t.Fatalf("created_ids: %+v", created)
	}
	want := map[int64]struct{ state, reason string }{
		10: {"wanted", ""},
		12: {"ignored", library.IgnoreReasonIndexAsIgnored},
		13: {"ignored", library.IgnoreReasonMediaType},
	}
	for _, v := range created.Videos {
		exp, ok := want[v.ID]
		if !ok {
			t.Fatalf("unexpected id %d", v.ID)
		}
		if !v.HasState || v.State != exp.state || v.IgnoredReason != exp.reason {
			t.Fatalf("id %d: HasState=%v State=%q Reason=%q want %q/%q",
				v.ID, v.HasState, v.State, v.IgnoredReason, exp.state, exp.reason)
		}
	}
	updated := byKey["updated_ids"]
	if !updated.IsVideoList || len(updated.Videos) != 1 || updated.Videos[0].HasState {
		t.Fatalf("updated_ids should not carry state: %+v", updated)
	}
	skippedIn := byKey["skipped_title_regexp_include"]
	if !skippedIn.IsSkippedTitleList || skippedIn.Text != "2" || len(skippedIn.SkippedTitles) != 2 {
		t.Fatalf("skipped_title_regexp_include: %+v", skippedIn)
	}
	if skippedIn.SkippedTitles[0].Title != "Skipped Vlog" || skippedIn.SkippedTitles[0].RemoteID != "r1" {
		t.Fatalf("first include skip: %+v", skippedIn.SkippedTitles[0])
	}
	skippedEx := byKey["skipped_title_regexp_exclude"]
	if !skippedEx.IsSkippedTitleList || skippedEx.Text != "1" || skippedEx.SkippedTitles[0].Title != "Trailer Cut" {
		t.Fatalf("skipped_title_regexp_exclude: %+v", skippedEx)
	}
	if byKey["ignored_media_type_ids"].Text != "1" {
		t.Fatalf("media_type count: %+v", byKey["ignored_media_type_ids"])
	}
	if byKey["ignored_index_as_ignored_ids"].Text != "1" {
		t.Fatalf("index_as_ignored count: %+v", byKey["ignored_index_as_ignored_ids"])
	}
}

func TestTaskDetailFieldsCreatedAllWanted(t *testing.T) {
	h := &Handler{}
	fields := h.taskDetailFields(`{"created_ids":[1],"skipped_title_regexp_include":[],"skipped_title_regexp_exclude":[],"ignored_media_type_ids":[],"ignored_index_as_ignored_ids":[]}`)
	var created detailField
	for _, f := range fields {
		if f.Key == "created_ids" {
			created = f
			break
		}
	}
	if len(created.Videos) != 1 || !created.Videos[0].HasState || created.Videos[0].State != "wanted" {
		t.Fatalf("wanted row: %+v", created.Videos)
	}
	if strings.TrimSpace(created.Videos[0].IgnoredReason) != "" {
		t.Fatalf("wanted must not set reason: %q", created.Videos[0].IgnoredReason)
	}
}

func TestMergeVideoHistoryDetailFields(t *testing.T) {
	rows := []taskDetailHistRow{
		{Event: "sidecar_refreshed", VideoID: 10, VideoTitle: "Alpha", SeriesID: 1},
		{Event: "sidecar_refreshed", VideoID: 10, VideoTitle: "Alpha", SeriesID: 1}, // dup
		{Event: "downloaded", VideoID: 11, VideoTitle: "Beta", SeriesID: 1},
		{Event: "sidecar_refreshed", VideoID: 12, VideoTitle: "Gamma", SeriesID: 2},
		{Event: "cancelled", Detail: `{"kind":"media_verify"}`, VideoID: 13, VideoTitle: "Delta", SeriesID: 1},
	}
	got := mergeVideoHistoryDetailFields(nil, rows)
	if len(got) != 3 {
		t.Fatalf("want 3 event keys, got %d: %+v", len(got), got)
	}
	if got[0].Key != "sidecar_refreshed" || !got[0].IsVideoList || len(got[0].Videos) != 2 {
		t.Fatalf("sidecar_refreshed: %+v", got[0])
	}
	if got[0].Videos[0].Title != "Alpha" || got[0].Videos[1].ID != 12 {
		t.Fatalf("sidecar_refreshed videos: %+v", got[0].Videos)
	}
	if got[1].Key != "downloaded" || len(got[1].Videos) != 1 {
		t.Fatalf("downloaded: %+v", got[1])
	}
	if got[2].Key != "media_verify" || len(got[2].Videos) != 1 || got[2].Videos[0].ID != 13 {
		t.Fatalf("cancelled→media_verify: %+v", got[2])
	}
	// Existing JSON key wins; do not duplicate as history event list.
	existing := []detailField{{Key: "sidecar_refreshed", Text: "already"}}
	got = mergeVideoHistoryDetailFields(existing, rows)
	if len(got) != 3 || got[0].Text != "already" || got[1].Key != "downloaded" || got[2].Key != "media_verify" {
		t.Fatalf("skip existing key: %+v", got)
	}
}

func TestHistoryEventLabel(t *testing.T) {
	cases := []struct {
		event, detail, want string
	}{
		{"sidecar_refreshed", "", "sidecar_refreshed"},
		{"cancelled", `{"kind":"media_verify"}`, "media_verify"},
		{"cancelled", `{"kind":"sponsorblock_cut"}`, "sponsorblock_cut"},
		{"cancelled", `{}`, "cancelled"},
		{"cancelled", "", "cancelled"},
		{library.SourceHistCancelled, `{"mode":"scan"}`, "scan"},
	}
	for _, tc := range cases {
		if got := historyEventLabel(tc.event, tc.detail); got != tc.want {
			t.Fatalf("historyEventLabel(%q,%q)=%q want %q", tc.event, tc.detail, got, tc.want)
		}
	}
}
