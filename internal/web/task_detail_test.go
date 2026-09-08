package web

import (
	"strings"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func TestParsePOTDetail(t *testing.T) {
	got := parsePOTDetail(`{"po-token":{"state":"issued","detail":"Retrieved a gvs PO Token","fetch":"auto"}}`)
	if got == nil || got.State != "issued" || got.Label != "Issued" || got.Fetch != "auto" {
		t.Fatalf("got %#v", got)
	}
	got = parsePOTDetail(`{"po-token":{"state":"generating","detail":"Generating a player PO Token for mweb","fetch":"always"}}`)
	if got == nil || got.State != "generating" || got.Label != "Generating" {
		t.Fatalf("generating: %#v", got)
	}
	if parsePOTDetail(`{"created_ids":[1]}`) != nil {
		t.Fatal("expected nil without pot")
	}
	if parsePOTDetail("not-json") != nil {
		t.Fatal("expected nil for non-json")
	}
}

func TestSettlePOTForDisplay(t *testing.T) {
	gen := &ytdlp.POTStatus{State: ytdlp.POTGenerating, Fetch: "always", Detail: "Generating a player PO Token"}
	got := settlePOTForDisplay(gen, queue.StatusFailed)
	if got == nil || got.State != ytdlp.POTSkipped {
		t.Fatalf("finished must settle generating: %#v", got)
	}
	if settlePOTForDisplay(gen, queue.StatusRunning).State != ytdlp.POTGenerating {
		t.Fatal("live must keep generating")
	}
	issued := &ytdlp.POTStatus{State: ytdlp.POTIssued}
	if settlePOTForDisplay(issued, queue.StatusDone).State != ytdlp.POTIssued {
		t.Fatal("issued unchanged")
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

func TestTaskFailErrorText(t *testing.T) {
	if got := taskFailErrorText("ERROR: boom", `{"error":"other"}`); got != "ERROR: boom" {
		t.Fatalf("prefer error_message: %q", got)
	}
	if got := taskFailErrorText("", `{"error":"from json","created":1}`); got != "from json" {
		t.Fatalf("json error: %q", got)
	}
	if got := taskFailErrorText("", "plain legacy stderr"); got != "plain legacy stderr" {
		t.Fatalf("plain: %q", got)
	}
	if got := taskFailErrorText("", `{"created":1}`); got != "" {
		t.Fatalf("no error key: %q", got)
	}
}

func TestTaskDetailFieldsHideErrorKey(t *testing.T) {
	h := &Handler{}
	fields := h.taskDetailFieldsOpts(`{"error":"ERROR: boom","created":1}`, true)
	for _, f := range fields {
		if f.Key == "error" || f.Key == "" {
			t.Fatalf("error should be hidden: %+v", f)
		}
	}
	if len(fields) == 0 {
		t.Fatal("expected other fields")
	}
	plain := h.taskDetailFieldsOpts("legacy raw detail", true)
	if len(plain) != 0 {
		t.Fatalf("plain detail hidden when Error row shown: %+v", plain)
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
	if got[2].Key != "integrity_check_initial" || len(got[2].Videos) != 1 || got[2].Videos[0].ID != 13 {
		t.Fatalf("cancelled→integrity_check_initial: %+v", got[2])
	}
	// Existing JSON key wins; do not duplicate as history event list.
	existing := []detailField{{Key: "sidecar_refreshed", Text: "already"}}
	got = mergeVideoHistoryDetailFields(existing, rows)
	if len(got) != 3 || got[0].Text != "already" || got[1].Key != "downloaded" || got[2].Key != "integrity_check_initial" {
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
	got := taskStages(taskStagesInput{
		Events: same, Now: now, Created: created, Started: started, Finished: finished,
		Status: "done", Origin: queue.OriginManual,
	})
	if len(got) != 6 || !got[0].IsFirst || !got[5].IsLast {
		t.Fatalf("stages: %+v", got)
	}
	if got[0].Event != "done" || got[1].Event != "packed" || got[2].Event != "remuxed" || got[3].Event != "downloaded" || got[4].Event != "enqueued" || got[5].Event != "manual" {
		t.Fatalf("order (latest top): %+v", got)
	}
	if got[5].OriginIcon != "mouse-pointer-click" {
		t.Fatalf("origin icon: %+v", got[5])
	}
	// Durations stay on the stage that lasted that long; origin+enqueued same time → no duration on origin.
	if got[0].Duration != "" || got[1].Duration != "30 sec" || got[2].Duration != "30 sec" || got[3].Duration != "1min" || got[4].Duration != "2min" || got[5].Duration != "" {
		t.Fatalf("durations: %q %q %q %q %q %q", got[0].Duration, got[1].Duration, got[2].Duration, got[3].Duration, got[4].Duration, got[5].Duration)
	}
	if got[3].CreatedAgo == "" || got[4].CreatedAgo == "" {
		t.Fatalf("want times on downloaded+enqueued: %+v", got)
	}
	// 1s gap between rows → omit duration on the later stage.
	oneSec := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "downloaded", Message: "a", CreatedAt: "2026-09-06T11:56:01Z"},
	}
	got = taskStages(taskStagesInput{
		Events: oneSec, Now: now, Created: created, Started: started, Status: "running", Origin: queue.OriginManual,
	})
	if len(got) != 3 || got[0].Event != "downloaded" || got[1].Event != "enqueued" || got[2].Event != "manual" {
		t.Fatalf("running order: %+v", got)
	}
	if got[0].Duration != "" || got[1].Duration != "" {
		t.Fatalf("1sec stage omit duration: %+v", got)
	}
	ts := "2026-09-05T14:33:00Z"
	sameLabel := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "downloaded", Message: "a", CreatedAt: ts},
		{VideoID: 7, Event: "remuxed", Message: "b", CreatedAt: ts},
		{VideoID: 7, Event: "packed", Message: "c", CreatedAt: ts},
	}
	got = taskStages(taskStagesInput{
		Events: sameLabel, Now: now, Created: "2026-09-04T14:30:00Z", Finished: "2026-09-05T14:33:00Z",
		Status: "done", Origin: queue.OriginManual,
	})
	if len(got) != 6 || got[0].Event != "done" || got[5].Event != "manual" || got[4].Event != "enqueued" {
		t.Fatalf("want done…enqueued…manual: %+v", got)
	}
	// Display top→bottom: first compact time kept, later duplicates blanked; enqueued differs by day.
	if got[0].CreatedAgo == "" || got[1].CreatedAgo != "" || got[2].CreatedAgo != "" || got[3].CreatedAgo != "" || got[4].CreatedAgo == "" {
		t.Fatalf("duplicate compact time: %+v", got)
	}
	if got[0].Duration != "" || got[1].Duration != "" || got[2].Duration != "" || got[3].Duration != "" || got[4].Duration == "" {
		t.Fatalf("row-gap durations: %q %q %q %q %q", got[0].Duration, got[1].Duration, got[2].Duration, got[3].Duration, got[4].Duration)
	}
	fail := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "download_failed", Message: "boom", CreatedAt: "2026-09-06T11:58:00Z"},
	}
	got = taskStages(taskStagesInput{
		Events: fail, Now: now, Created: created, Started: started, Finished: finished,
		Status: "failed", Origin: queue.OriginManual,
	})
	if len(got) != 4 || got[0].Event != "failed" || !got[0].HasError || !got[1].HasError || got[2].Event != "enqueued" || got[3].Event != "manual" {
		t.Fatalf("fail stage: %+v", got)
	}
	if got[0].Duration != "" || got[1].Duration != "2min" || got[2].Duration != "2min" {
		t.Fatalf("fail durations: %q %q %q", got[0].Duration, got[1].Duration, got[2].Duration)
	}
	// Multi-video / no history: lifecycle done → started → enqueued → origin (latest top).
	multi := []library.VideoHistoryEvent{
		{VideoID: 1, Event: "sidecar_missing", CreatedAt: "2026-09-06T11:58:00Z"},
		{VideoID: 2, Event: "sidecar_missing", CreatedAt: "2026-09-06T11:59:00Z"},
	}
	got = taskStages(taskStagesInput{
		Events: multi, Now: now, Created: created, Started: started, Finished: finished,
		Status: "done", Origin: queue.OriginScheduled,
	})
	if len(got) != 4 || got[0].Event != "done" || got[1].Event != "started" || got[2].Event != "enqueued" || got[3].Event != "scheduled" {
		t.Fatalf("lifecycle: %+v", got)
	}
	if got[3].OriginIcon != "calendar-clock" {
		t.Fatalf("scheduled icon: %+v", got[3])
	}
	if got[0].Duration != "" || got[1].Duration != "3min" || got[2].Duration != "1min" {
		t.Fatalf("lifecycle durations: %q %q %q", got[0].Duration, got[1].Duration, got[2].Duration)
	}
	got = taskStages(taskStagesInput{
		Now: now, Created: created, Started: started, Finished: finished, Status: "failed", Origin: queue.OriginBoot,
	})
	if len(got) != 4 || got[0].Event != "failed" || !got[0].HasError || got[3].Event != "boot" {
		t.Fatalf("failed lifecycle: %+v", got)
	}
	got = taskStages(taskStagesInput{
		Now: now, Created: created, Started: started, Finished: finished, Status: "cancelled", Origin: queue.OriginManual,
	})
	if len(got) != 4 || got[0].Event != "cancelled" || got[0].HasError || !got[0].Neutral {
		t.Fatalf("cancelled lifecycle: %+v", got)
	}
	cancelHist := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "cancelled", Message: "Cancelled", Detail: `{"kind":"integrity_check"}`, CreatedAt: "2026-09-06T11:58:00Z"},
	}
	got = taskStages(taskStagesInput{
		Events: cancelHist, Now: now, Created: created, Started: started, Finished: finished,
		Status: "cancelled", Origin: queue.OriginManual,
	})
	// Latest top: cancelled terminal, remapped integrity_check (neutral), enqueued, origin
	if len(got) < 2 || got[0].Event != "cancelled" || got[0].HasError || !got[0].Neutral {
		t.Fatalf("cancelled terminal: %+v", got)
	}
	foundNeutralHist := false
	for _, s := range got {
		if s.Event == "integrity_check" && s.Neutral && !s.HasError {
			foundNeutralHist = true
		}
	}
	if !foundNeutralHist {
		t.Fatalf("cancelled integrity hist not neutral: %+v", got)
	}
	got = taskStages(taskStagesInput{Now: now, Created: created, Status: "pending", Origin: queue.OriginManual})
	if len(got) != 2 || got[0].Event != "enqueued" || got[1].Event != "manual" || !got[0].IsFirst || !got[1].IsLast {
		t.Fatalf("pending: %+v", got)
	}
	got = taskStages(taskStagesInput{Now: now, Status: "pending", Origin: queue.OriginManual})
	if len(got) != 2 || got[0].Event != "enqueued" || got[1].Event != "manual" {
		t.Fatalf("fallback enqueued: %+v", got)
	}
	got = taskStages(taskStagesInput{Now: now, Status: "done", Origin: queue.OriginManual})
	if len(got) != 3 || got[0].Event != "done" || got[1].Event != "enqueued" || got[2].Event != "manual" {
		t.Fatalf("done without timestamps: %+v", got)
	}

	// Origin=task + child nodes.
	childCreated := "2026-09-06T11:56:30Z"
	got = taskStages(taskStagesInput{
		Now: now, Created: created, Started: started, Finished: finished, Status: "done",
		Origin: queue.OriginTask, ParentTaskID: 9, ParentKind: "download",
		Children: []queue.Task{
			{ID: 11, Kind: queue.KindIntegrityCheckInitial, Status: queue.StatusPending, CreatedAt: childCreated},
			{ID: 12, Kind: queue.KindSponsorblockCut, Status: queue.StatusDone, CreatedAt: "2026-09-06T11:58:00Z"},
		},
	})
	if len(got) < 5 {
		t.Fatalf("task+children: %+v", got)
	}
	foundOrigin, foundPendingChild, foundDoneChild := false, false, false
	for _, s := range got {
		if s.Event == "task" && s.HistoryID == 9 && s.Message == "download #9" && s.OriginIcon == "git-branch" {
			foundOrigin = true
		}
		if s.Event == queue.KindIntegrityCheckInitial && s.HistoryID == 11 && s.Message == queue.StatusPending && s.OriginIcon == "git-branch" {
			foundPendingChild = true
		}
		if s.Event == queue.KindSponsorblockCut && s.HistoryID == 12 && s.Message == queue.StatusDone && s.OriginIcon == "" && !s.HasError {
			foundDoneChild = true
		}
	}
	if !foundOrigin || !foundPendingChild || !foundDoneChild {
		t.Fatalf("origin/children missing: origin=%v pending=%v done=%v got=%+v", foundOrigin, foundPendingChild, foundDoneChild, got)
	}
}

func TestTaskStagesCookieAttach(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	created := "2026-09-06T11:56:00Z"
	started := "2026-09-06T11:57:00Z"
	ca := &domains.CookieAttachStatus{
		State: domains.CookieAttachRetried, RetryReason: "AgeRestricted", AfterFail: true,
	}
	events := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "download_failed", Message: "Download failed", CreatedAt: "2026-09-06T11:58:00Z"},
	}
	got := taskStages(taskStagesInput{
		Events: events, Now: now, Created: created, Started: started, Status: "failed", Origin: queue.OriginManual,
		CookieAttach: ca,
		POT: &ytdlp.POTStatus{
			State: ytdlp.POTIssued, Fetch: "always",
			Attempts: []ytdlp.POTStatus{
				{State: ytdlp.POTSkipped, Fetch: "always"},
				{State: ytdlp.POTIssued, Fetch: "always"},
			},
		},
	})
	var fails []taskStageView
	for _, s := range got {
		if s.Event == "download_failed" {
			fails = append(fails, s)
		}
	}
	if len(fails) != 2 {
		t.Fatalf("want 2 download_failed nodes, got %d: %+v", len(fails), got)
	}
	// Newest-first: final attempt first, anonymous fail second.
	if fails[0].Message != "Download failed" {
		t.Fatalf("final attempt message: %q", fails[0].Message)
	}
	if len(fails[0].Substages) != 2 {
		t.Fatalf("final substages (cookies+PO): %+v", fails[0].Substages)
	}
	if fails[0].Substages[0].Event != "Cookies used" || fails[0].Substages[0].Icon != "cookie" {
		t.Fatalf("cookies note: %+v", fails[0].Substages[0])
	}
	if fails[0].Substages[1].Event != "PO used" {
		t.Fatalf("final PO note: %+v", fails[0].Substages[1])
	}
	if fails[1].Message != "AgeRestricted" || !fails[1].HasError {
		t.Fatalf("first attempt: %+v", fails[1])
	}
	if len(fails[1].Substages) != 1 || fails[1].Substages[0].Event != "PO skipped" {
		t.Fatalf("first attempt PO from pass 0: %+v", fails[1].Substages)
	}
	for _, s := range got {
		if s.Event == "cookies" {
			t.Fatal("cookies must not be a top-level stage")
		}
	}

	// Always-attach: cookies mentioned when used; PO when present.
	got = taskStages(taskStagesInput{
		Events: events, Now: now, Created: created, Status: "failed", Origin: queue.OriginManual,
		CookieAttach: &domains.CookieAttachStatus{State: domains.CookieAttachCookies, AfterFail: false},
		POT:          &ytdlp.POTStatus{State: ytdlp.POTSkipped},
	})
	var one *taskStageView
	for i := range got {
		if got[i].Event == "download_failed" {
			one = &got[i]
			break
		}
	}
	if one == nil || len(one.Substages) != 2 {
		t.Fatalf("always-attach want cookies+PO: %+v", one)
	}
	if one.Substages[0].Event != "Cookies used" || one.Substages[1].Event != "PO skipped" {
		t.Fatalf("always-attach substages: %+v", one.Substages)
	}

	got = taskStages(taskStagesInput{
		Now: now, Created: created, Status: "pending", Origin: queue.OriginManual,
		CookieAttach: &domains.CookieAttachStatus{State: domains.CookieAttachOff},
	})
	for _, s := range got {
		if s.Event == "cookies" || len(s.Substages) > 0 {
			t.Fatal("off must not inject cookie stages")
		}
	}
}

func TestTaskStagesPOTUnderDownload(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	created := "2026-09-06T11:56:00Z"
	events := []library.VideoHistoryEvent{
		{VideoID: 7, Event: "downloaded", Message: "Downloaded", CreatedAt: "2026-09-06T11:58:00Z"},
	}
	got := taskStages(taskStagesInput{
		Events: events, Now: now, Created: created, Status: "done", Origin: queue.OriginManual,
		CookieAttach: &domains.CookieAttachStatus{State: domains.CookieAttachCookies, AfterFail: false},
		POT:          &ytdlp.POTStatus{State: ytdlp.POTIssued},
	})
	var dl *taskStageView
	for i := range got {
		if got[i].Event == "downloaded" {
			dl = &got[i]
			break
		}
	}
	if dl == nil {
		t.Fatalf("missing downloaded: %+v", got)
	}
	if len(dl.Substages) != 2 {
		t.Fatalf("want cookies+PO used, got %+v", dl.Substages)
	}
	if dl.Substages[0].Event != "Cookies used" || dl.Substages[1].Event != "PO used" {
		t.Fatalf("substages: %+v", dl.Substages)
	}
	for _, s := range got {
		if s.Event == "PO used" || strings.HasPrefix(s.Event, "PO ") {
			t.Fatal("PO must not be a top-level stage")
		}
	}
	got = taskStages(taskStagesInput{
		Events: events, Now: now, Created: created, Status: "done", Origin: queue.OriginManual,
		POT: &ytdlp.POTStatus{State: ytdlp.POTOff},
	})
	for _, s := range got {
		if s.Event == "downloaded" {
			if len(s.Substages) != 1 || s.Substages[0].Event != "PO skipped" {
				t.Fatalf("off should nest PO skipped: %+v", s.Substages)
			}
		}
	}
}

func TestHistoryEventLabel(t *testing.T) {
	cases := []struct {
		event, detail, want string
	}{
		{"sidecar_refreshed", "", "sidecar_refreshed"},
		{"cancelled", `{"kind":"media_verify"}`, "integrity_check_initial"},
		{"cancelled", `{"kind":"integrity_check_initial"}`, "integrity_check_initial"},
		{"cancelled", `{"kind":"sponsorblock_cut"}`, "sponsorblock_cut"},
		{"cancelled", `{}`, "cancelled"},
		{"cancelled", "", "cancelled"},
		{library.SourceHistCancelled, `{"mode":"scan"}`, "scan"},
		{"verified", "", "integrity_checked"},
		{"integrity_checked", "", "integrity_checked"},
		{"integrity_check_failed", "", "integrity_check_failed"},
		{"verify_failed", "", "integrity_check_failed"},
	}
	for _, tc := range cases {
		if got := historyEventLabel(tc.event, tc.detail); got != tc.want {
			t.Fatalf("historyEventLabel(%q,%q)=%q want %q", tc.event, tc.detail, got, tc.want)
		}
	}
}

func TestEnrichVideoHistoryRenameMessages(t *testing.T) {
	views := []videoHistoryView{
		{Event: "renamed", Message: "Episode files renamed", VideoID: 2, HasTask: true, TaskID: 9, Detail: `{}`},
		{Event: "renamed", Message: "Episode files renamed (peer move after 'X')", VideoID: 3, HasTask: true, TaskID: 9},
		{Event: "packed", Message: "Packed", VideoID: 1},
	}
	// No queue/lib: only detail.trigger_video_id path works without GetTask.
	views[0].Detail = `{"trigger_video_id":1}`
	enrichVideoHistoryRenameMessages(nil, nil, views)
	if !strings.Contains(views[0].Message, "peer move after another video") {
		t.Fatalf("want peer message without title, got %q", views[0].Message)
	}
	if views[1].Message != "Episode files renamed (peer move after 'X')" {
		t.Fatalf("already enriched must stay: %q", views[1].Message)
	}
	if views[2].Message != "Packed" {
		t.Fatalf("non-renamed changed: %q", views[2].Message)
	}
}

func TestHistoryMessageWithDetail(t *testing.T) {
	got := historyMessageWithDetail("Media file size changed on disk", `{"old_size":100,"new_size":200}`)
	if !strings.Contains(got, "100") || !strings.Contains(got, "200") {
		t.Fatalf("size delta missing: %q", got)
	}
	got = historyMessageWithDetail("Sidecar integrity check failed", `{"old_hash":"aaaaaaaaaaaaaaaa","new_hash":"bbbbbbbbbbbbbbbb"}`)
	if !strings.Contains(got, "aaaaaaaaaaaa") || !strings.Contains(got, "bbbbbbbbbbbb") {
		t.Fatalf("hash delta missing: %q", got)
	}
}

func TestRenamePathsFromDetail(t *testing.T) {
	from, to := renamePathsFromDetail(`{"previous_path":"/lib/A/ep","new_path":"/lib/B/ep","previous":"ep","new":"ep2"}`)
	if from != "/lib/A/ep" || to != "/lib/B/ep" {
		t.Fatalf("got %q → %q", from, to)
	}
	from, to = renamePathsFromDetail(`{"previous":"oldstem","new":"newstem"}`)
	if from != "oldstem" || to != "newstem" {
		t.Fatalf("basename fallback: %q → %q", from, to)
	}
	from, to = renamePathsFromDetail(`not-json`)
	if from != "" || to != "" {
		t.Fatalf("bad json: %q → %q", from, to)
	}
}

func TestRenameScopeFromPayload(t *testing.T) {
	s, v := renameScopeFromPayload(`{"video_ids":[1,2],"series_id":9}`)
	if len(s) != 0 || len(v) != 2 || v[0] != 1 {
		t.Fatalf("video_ids win: series=%v videos=%v", s, v)
	}
	s, v = renameScopeFromPayload(`{"series_id":5}`)
	if len(s) != 1 || s[0] != 5 || len(v) != 0 {
		t.Fatalf("series_id: series=%v videos=%v", s, v)
	}
	s, v = renameScopeFromPayload(`{"series_ids":[7,8]}`)
	if len(s) != 2 || s[0] != 7 || len(v) != 0 {
		t.Fatalf("series_ids: series=%v videos=%v", s, v)
	}
	s, v = renameScopeFromPayload(`{}`)
	if len(s) != 0 || len(v) != 0 {
		t.Fatalf("empty: series=%v videos=%v", s, v)
	}
}
