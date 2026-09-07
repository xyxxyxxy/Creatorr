package web

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestBuildIntegrityChecksInitial(t *testing.T) {
	h := &Handler{}
	rep := library.IntegrityCheckReport{
		Outcome: library.IntegrityOutcomePartial,
		Checks: []library.IntegrityCheckItem{
			{Key: library.IntegrityCheckNullDecode, Result: library.IntegrityResultOK},
			{Key: library.IntegrityCheckMediaChecksum, Result: library.IntegrityResultOK},
			{Key: library.IntegrityCheckSidecarChecksum, Result: library.IntegrityResultPartial, Detail: "thumb"},
			{Key: library.IntegrityCheckNFO, Result: library.IntegrityResultFailed, Detail: "NFO mismatch"},
		},
	}
	detail, _ := json.Marshal(rep.DetailMap())
	task := &queue.Task{Kind: queue.KindIntegrityCheckInitial, Status: queue.StatusDone, Detail: string(detail)}
	view := h.buildIntegrityChecks(task, nil)
	if view == nil || view.Bulk || view.Outcome != library.IntegrityOutcomePartial {
		t.Fatalf("%+v", view)
	}
	if len(view.Stats) != 4 {
		t.Fatalf("stats=%d", len(view.Stats))
	}
	if view.Stats[2].Value != library.IntegrityResultPartial || view.Stats[2].Desc != "thumb" {
		t.Fatalf("sidecar stat=%+v", view.Stats[2])
	}
	if len(view.Failed) != 0 {
		t.Fatalf("partial should not list failed videos: %+v", view.Failed)
	}
}

func TestBuildIntegrityChecksInitialFailedListsVideo(t *testing.T) {
	h := &Handler{}
	rep := library.IntegrityCheckReport{
		Outcome: library.IntegrityOutcomeFailed,
		Checks: []library.IntegrityCheckItem{
			{Key: library.IntegrityCheckNullDecode, Result: library.IntegrityResultFailed, Detail: "ffmpeg boom"},
			{Key: library.IntegrityCheckMediaChecksum, Result: library.IntegrityResultSkipped},
		},
	}
	detail, _ := json.Marshal(rep.DetailMap())
	task := &queue.Task{
		Kind:    queue.KindIntegrityCheckInitial,
		Status:  queue.StatusDone,
		Detail:  string(detail),
		VideoID: sql.NullInt64{Int64: 42, Valid: true},
	}
	view := h.buildIntegrityChecks(task, nil)
	if view == nil || len(view.Failed) != 1 || view.Failed[0].ID != 42 {
		t.Fatalf("%+v", view)
	}
	if view.Failed[0].Note == "" || !strings.Contains(view.Failed[0].Note, "Null-decode") {
		t.Fatalf("note=%q", view.Failed[0].Note)
	}
}

func TestBuildIntegrityChecksBulkStatsAndFailedOnly(t *testing.T) {
	h := &Handler{}
	res := library.VerifyAllMediaResult{
		IntegrityChecked:  2,
		Partial:           1,
		Skipped:           3,
		Failed:            1,
		SkippedBusy:       1,
		SkippedProfileOff: 1,
		SkippedNoMedia:    1,
		Checks: map[string]map[string]int{
			library.IntegrityCheckNullDecode: {library.IntegrityResultOK: 3, library.IntegrityResultFailed: 1},
		},
	}
	detail, _ := json.Marshal(res.DetailMap())
	task := &queue.Task{Kind: queue.KindIntegrityCheck, Status: queue.StatusRunning, Detail: string(detail)}
	events := []library.VideoHistoryEvent{
		{VideoID: 1, Event: library.VideoHistIntegrityChecked, Detail: `{"outcome":"ok","checks":[]}`},
		{VideoID: 2, Event: library.VideoHistIntegrityChecked, Detail: `{"outcome":"partial","checks":[{"key":"nfo","result":"failed","detail":"bad nfo"}]}`},
		{VideoID: 3, Event: library.VideoHistVerifyFailed, Detail: `{"outcome":"failed","check":"null_decode","checks":[{"key":"null_decode","result":"failed","detail":"boom"}]}`},
	}
	view := h.buildIntegrityChecks(task, events)
	if view == nil || !view.Bulk {
		t.Fatalf("%+v", view)
	}
	if len(view.Stats) != 4 {
		t.Fatalf("stats=%+v", view.Stats)
	}
	if view.Stats[0].Value != "2" || view.Stats[1].Value != "1" || view.Stats[2].Value != "3" || view.Stats[3].Value != "1" {
		t.Fatalf("stat values=%+v", view.Stats)
	}
	if len(view.Failed) != 1 || view.Failed[0].ID != 3 {
		t.Fatalf("failed=%+v", view.Failed)
	}
	if !strings.Contains(view.Failed[0].Note, "Null-decode") {
		t.Fatalf("note=%q", view.Failed[0].Note)
	}
}

func TestFilterIntegrityHistoryRows(t *testing.T) {
	rows := []taskDetailHistRow{
		{Event: library.VideoHistIntegrityChecked, VideoID: 1},
		{Event: "sidecar_externally_changed", VideoID: 1},
		{Event: "renamed", VideoID: 2},
	}
	got := filterIntegrityHistoryRows(rows)
	if len(got) != 1 || got[0].Event != "renamed" {
		t.Fatalf("%+v", got)
	}
}

func TestTaskDetailFieldsSuppressesIntegrityKeys(t *testing.T) {
	h := &Handler{}
	detail := `{
		"outcome":"partial",
		"integrity_checked":2,
		"partial":1,
		"skipped":0,
		"failed":1,
		"checks":{"null_decode":{"ok":3}},
		"other_key":"keep"
	}`
	fields := filterSuppressedDetailFields(h.taskDetailFields(detail), integrityDetailSuppressKeys)
	byKey := map[string]detailField{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for _, k := range []string{"outcome", "integrity_checked", "partial", "skipped", "failed", "checks"} {
		if _, ok := byKey[k]; ok {
			t.Fatalf("suppressed key still present: %s", k)
		}
	}
	if byKey["other_key"].Text != "keep" {
		t.Fatalf("%+v", fields)
	}
}
