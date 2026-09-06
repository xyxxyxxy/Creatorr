package web

import (
	"strings"
	"testing"
	"time"

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

func TestParseDomainAccessDetail(t *testing.T) {
	got := parseDomainAccessDetail(`{"domain-access":{"rate_limit":"10M","rate_override":true,"sleep_requests":1,"cookies":true,"cookies_override":true,"flare":false,"credentials":true,"credentials_override":true}}`)
	if got == nil || got.RateLimit != "10M" || !got.RateOverride || !got.Cookies || !got.Credentials || got.Flare {
		t.Fatalf("got %#v", got)
	}
	if !got.ShowRate() || !got.ShowSleep() {
		t.Fatal("expected show rate+sleep")
	}
	if parseDomainAccessDetail(`{"created_ids":[1]}`) != nil {
		t.Fatal("expected nil without domain-access")
	}
}

func TestTaskDetailFieldsCreatedState(t *testing.T) {
	h := &Handler{}
	detail := `{
		"created": 3,
		"updated": 2,
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
	if byKey["Videos indexed"].Text != "5" {
		t.Fatalf("Videos indexed: %+v", byKey["Videos indexed"])
	}
	if _, ok := byKey["created"]; ok {
		t.Fatal("raw created key should be hidden")
	}
	if _, ok := byKey["updated"]; ok {
		t.Fatal("raw updated key should be hidden")
	}
	created, ok := byKey["created_ids"]
	if !ok || !created.IsVideoList || len(created.Videos) != 3 {
		t.Fatalf("created_ids: %+v", created)
	}
	want := map[int64]struct{ state, reason, tip string }{
		10: {"wanted", "", "wanted"},
		12: {"ignored", library.IgnoreReasonIndexAsIgnored, "ignored (indexed as ignored)"},
		13: {"ignored", library.IgnoreReasonMediaType, "ignored (media type)"},
	}
	for _, v := range created.Videos {
		exp, ok := want[v.ID]
		if !ok {
			t.Fatalf("unexpected id %d", v.ID)
		}
		if !v.HasState || v.State != exp.state || v.IgnoredReason != exp.reason || v.StatusTip != exp.tip {
			t.Fatalf("id %d: HasState=%v State=%q Reason=%q Tip=%q want %q/%q/%q",
				v.ID, v.HasState, v.State, v.IgnoredReason, v.StatusTip, exp.state, exp.reason, exp.tip)
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

func TestTaskStages(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	created := "2026-09-06T11:56:00Z"
	started := "2026-09-06T11:57:00Z"
	finished := "2026-09-06T12:00:00Z"
	same := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "downloaded", Message: "Download finished", CreatedAt: "2026-09-06T11:58:00Z"},
		{VideoID: 7, Event: "remuxed", Message: "Remuxed to mkv", CreatedAt: "2026-09-06T11:59:00Z"},
		{VideoID: 7, Event: "packed", Message: "Packed", CreatedAt: "2026-09-06T11:59:30Z"},
	}
	got := taskStages(same, now, created, started, finished, "done")
	if len(got) != 5 || !got[0].IsFirst || !got[4].IsLast {
		t.Fatalf("stages: %+v", got)
	}
	if got[0].Event != "enqueued" || got[1].Event != "downloaded" || got[2].Event != "remuxed" || got[3].Event != "packed" || got[4].Event != "done" {
		t.Fatalf("order: %+v", got)
	}
	// previous row → this row (enqueued has no prior → empty); terminal done after pack.
	if got[0].Duration != "" || got[1].Duration != "2min" || got[2].Duration != "1min" || got[3].Duration != "30 sec" || got[4].Duration != "30 sec" {
		t.Fatalf("durations: %q %q %q %q %q", got[0].Duration, got[1].Duration, got[2].Duration, got[3].Duration, got[4].Duration)
	}
	if got[0].CreatedAgo == "" || got[1].CreatedAgo == "" {
		t.Fatalf("want times on enqueued+downloaded: %+v", got)
	}
	// 1s gap between rows → omit duration on the later stage.
	oneSec := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "downloaded", Message: "a", CreatedAt: "2026-09-06T11:56:01Z"},
	}
	got = taskStages(oneSec, now, created, started, "", "running")
	if len(got) != 2 || got[0].Duration != "" || got[1].Duration != "" {
		t.Fatalf("1sec stage omit duration: %+v", got)
	}
	ts := "2026-09-05T14:33:00Z"
	sameLabel := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "downloaded", Message: "a", CreatedAt: ts},
		{VideoID: 7, Event: "remuxed", Message: "b", CreatedAt: ts},
		{VideoID: 7, Event: "packed", Message: "c", CreatedAt: ts},
	}
	got = taskStages(sameLabel, now, "2026-09-05T14:30:00Z", "", "2026-09-05T14:33:00Z", "done")
	if len(got) != 5 || got[0].Event != "enqueued" || got[4].Event != "done" {
		t.Fatalf("want enqueued+3+done: %+v", got)
	}
	if got[0].CreatedAgo == "" || got[1].CreatedAgo == "" || got[2].CreatedAgo != "" || got[3].CreatedAgo != "" {
		t.Fatalf("duplicate compact time: %+v", got)
	}
	if got[0].Duration != "" || got[1].Duration != "3min" || got[2].Duration != "" || got[3].Duration != "" {
		t.Fatalf("row-gap durations: %q %q %q %q", got[0].Duration, got[1].Duration, got[2].Duration, got[3].Duration)
	}
	fail := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "download_failed", Message: "boom", CreatedAt: "2026-09-06T11:58:00Z"},
	}
	got = taskStages(fail, now, created, started, finished, "failed")
	if len(got) != 3 || got[0].Event != "enqueued" || !got[1].HasError || got[2].Event != "failed" || !got[2].HasError {
		t.Fatalf("fail stage: %+v", got)
	}
	if got[1].Duration != "2min" {
		t.Fatalf("fail duration: %q", got[1].Duration)
	}
	// Multi-video / no history: lifecycle enqueued → started → done.
	multi := []library.VideoHistoryEvent{
		{VideoID: 1, Event: "sidecar_missing", CreatedAt: "2026-09-06T11:58:00Z"},
		{VideoID: 2, Event: "sidecar_missing", CreatedAt: "2026-09-06T11:59:00Z"},
	}
	got = taskStages(multi, now, created, started, finished, "done")
	if len(got) != 3 || got[0].Event != "enqueued" || got[1].Event != "started" || got[2].Event != "done" {
		t.Fatalf("lifecycle: %+v", got)
	}
	if got[1].Duration != "1min" || got[2].Duration != "3min" {
		t.Fatalf("lifecycle durations: %q %q", got[1].Duration, got[2].Duration)
	}
	got = taskStages(nil, now, created, started, finished, "failed")
	if len(got) != 3 || got[2].Event != "failed" || !got[2].HasError {
		t.Fatalf("failed lifecycle: %+v", got)
	}
	got = taskStages(nil, now, created, "", "", "pending")
	if len(got) != 1 || got[0].Event != "enqueued" || !got[0].IsFirst || !got[0].IsLast {
		t.Fatalf("pending: %+v", got)
	}
	got = taskStages(nil, now, "", "", "", "pending")
	if len(got) != 1 || got[0].Event != "enqueued" {
		t.Fatalf("fallback enqueued: %+v", got)
	}
	got = taskStages(nil, now, "", "", "", "done")
	if len(got) != 2 || got[0].Event != "enqueued" || got[1].Event != "done" {
		t.Fatalf("done without timestamps: %+v", got)
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
