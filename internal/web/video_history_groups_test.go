package web

import "testing"

func TestGroupVideoHistoryByTask(t *testing.T) {
	rows := []videoHistoryView{
		{Event: "packed", Message: "Packed to library", TaskID: 19, TaskKind: "download", HasTask: true, HistoryID: 19, CreatedAgo: "1m"},
		{Event: "remuxed", Message: "Remuxed to mkv", TaskID: 19, TaskKind: "download", HasTask: true, HistoryID: 19, CreatedAgo: "1m"},
		{Event: "downloaded", Message: "Download finished", TaskID: 19, TaskKind: "download", HasTask: true, HistoryID: 19, CreatedAgo: "2m"},
		{Event: "discovered", Message: "Indexed from scan", HasTask: false, CreatedAgo: "1h"},
		{Event: "verified", Message: "OK", TaskID: 20, HasTask: true, HistoryID: 20, CreatedAgo: "3h"},
		{Event: "download_failed", Message: "fail", TaskID: 21, HasTask: true, HistoryID: 21, CreatedAgo: "4h"},
	}
	got := groupVideoHistoryByTask(rows)
	if len(got) != 4 {
		t.Fatalf("groups=%d want 4", len(got))
	}
	g0 := got[0]
	if !g0.Grouped || g0.TaskID != 19 || g0.Event != "download" || len(g0.Stages) != 3 {
		t.Fatalf("group0: %+v stages=%d", g0, len(g0.Stages))
	}
	if g0.Stages[0].Event != "packed" || g0.Stages[1].Event != "remuxed" || g0.Stages[2].Event != "downloaded" {
		t.Fatalf("stage order (latest top): %q %q %q", g0.Stages[0].Event, g0.Stages[1].Event, g0.Stages[2].Event)
	}
	if got[1].Grouped || got[1].Event != "discovered" {
		t.Fatalf("group1: %+v", got[1])
	}
	if got[2].Grouped || got[2].Event != "verified" || got[2].TaskID != 20 {
		t.Fatalf("group2: %+v", got[2])
	}
	if !got[3].HasError || got[3].Event != "download_failed" {
		t.Fatalf("group3: %+v", got[3])
	}
}

func TestGroupVideoHistoryByTaskEmpty(t *testing.T) {
	if got := groupVideoHistoryByTask(nil); got != nil {
		t.Fatalf("nil: %v", got)
	}
}

func TestGroupVideoHistoryByTaskKindFallback(t *testing.T) {
	rows := []videoHistoryView{
		{Event: "packed", TaskID: 7, HasTask: true, HistoryID: 7},
		{Event: "downloaded", TaskID: 7, HasTask: true, HistoryID: 7},
	}
	got := groupVideoHistoryByTask(rows)
	if len(got) != 1 || got[0].Event != "Task #7" {
		t.Fatalf("fallback: %+v", got)
	}
}

func TestVideoHistoryGroupsToTimeline(t *testing.T) {
	groups := groupVideoHistoryByTask([]videoHistoryView{
		{Event: "packed", Message: "Packed", TaskID: 19, TaskKind: "download", HasTask: true, HistoryID: 19, CreatedAgo: "1m", CreatedAt: "t1"},
		{Event: "downloaded", Message: "Done", TaskID: 19, TaskKind: "download", HasTask: true, HistoryID: 19, CreatedAgo: "2m", CreatedAt: "t0"},
		{Event: "discovered", Message: "Indexed", HasTask: false, CreatedAgo: "1h", CreatedAt: "td"},
	})
	got := videoHistoryGroupsToTimeline(groups)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if !got[0].IsFirst || got[0].Event != "download" || got[0].HistoryID != 19 || len(got[0].Substages) != 2 {
		t.Fatalf("group0: %+v", got[0])
	}
	if got[0].Substages[0].Event != "packed" || got[0].Substages[1].Event != "downloaded" {
		t.Fatalf("substages (latest top): %+v", got[0].Substages)
	}
	if got[0].Message != "" {
		t.Fatalf("grouped message should be empty: %q", got[0].Message)
	}
	if !got[1].IsLast || got[1].Event != "discovered" || got[1].Message != "Indexed" || len(got[1].Substages) != 0 {
		t.Fatalf("group1: %+v", got[1])
	}
}
