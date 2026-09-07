package library_test

import (
	"encoding/json"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestIntegrityCheckReportDetailMapAndFinalize(t *testing.T) {
	rep := &library.IntegrityCheckReport{
		Checks: []library.IntegrityCheckItem{
			{Key: library.IntegrityCheckNullDecode, Result: library.IntegrityResultOK},
			{Key: library.IntegrityCheckMediaChecksum, Result: library.IntegrityResultFilled},
			{Key: library.IntegrityCheckSidecarChecksum, Result: library.IntegrityResultPartial, Detail: "thumb hash mismatch"},
			{Key: library.IntegrityCheckNFO, Result: library.IntegrityResultOK},
		},
	}
	// finalize via MarkVerified path uses finalizeOutcome - call through DetailMap after setting
	rep2 := &library.IntegrityCheckReport{Checks: rep.Checks}
	_ = rep2.DetailMap() // does not finalize; MarkVerified does
	// Use failed hard check
	fail := &library.IntegrityCheckReport{
		Checks: []library.IntegrityCheckItem{
			{Key: library.IntegrityCheckNullDecode, Result: library.IntegrityResultFailed, Detail: "ffmpeg boom"},
			{Key: library.IntegrityCheckMediaChecksum, Result: library.IntegrityResultSkipped},
		},
	}
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "IR", SourceURL: "https://www.example.com/@ir", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ir1", Title: "One", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tid := seedTaskID(t, s)
	if err := s.MarkVerifyFailed(res.VideoID, tid, "Integrity check failed", fail); err != nil {
		t.Fatal(err)
	}
	hist, err := s.ListVideoHistory(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	var detail string
	for _, e := range hist {
		if e.Event == library.VideoHistVerifyFailed {
			detail = e.Detail
			break
		}
	}
	if detail == "" {
		t.Fatal("missing fail history")
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["outcome"] != library.IntegrityOutcomeFailed {
		t.Fatalf("outcome=%v", raw["outcome"])
	}
	if raw["check"] != library.IntegrityCheckNullDecode {
		t.Fatalf("check=%v", raw["check"])
	}
}

func TestMarkVerifiedPartialMessage(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "IP", SourceURL: "https://www.example.com/@ip", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ip1", Title: "One", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tid := seedTaskID(t, s)
	rep := &library.IntegrityCheckReport{
		Outcome: library.IntegrityOutcomePartial,
		Checks: []library.IntegrityCheckItem{
			{Key: library.IntegrityCheckNullDecode, Result: library.IntegrityResultOK},
			{Key: library.IntegrityCheckMediaChecksum, Result: library.IntegrityResultOK},
			{Key: library.IntegrityCheckSidecarChecksum, Result: library.IntegrityResultPartial, Detail: "thumb"},
			{Key: library.IntegrityCheckNFO, Result: library.IntegrityResultOK},
		},
	}
	if err := s.MarkVerified(res.VideoID, tid, rep); err != nil {
		t.Fatal(err)
	}
	hist, err := s.ListVideoHistory(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range hist {
		if e.Event == library.VideoHistIntegrityChecked {
			found = true
			if e.Message != "Integrity check ok (sidecar/NFO issues)" {
				t.Fatalf("message=%q", e.Message)
			}
			var raw map[string]any
			if err := json.Unmarshal([]byte(e.Detail), &raw); err != nil {
				t.Fatal(err)
			}
			if raw["outcome"] != library.IntegrityOutcomePartial {
				t.Fatalf("outcome=%v", raw["outcome"])
			}
		}
	}
	if !found {
		t.Fatal("missing integrity_checked")
	}
}

func TestVerifyAllMediaMessageIncludesPartial(t *testing.T) {
	got := library.VerifyAllMediaMessage(10, 2, 3, 1)
	want := "Integrity checked 10, partial 2, skipped 3, failed 1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestVerifyAllMediaResultFinalizeOutcome(t *testing.T) {
	cases := []struct {
		name string
		r    library.VerifyAllMediaResult
		want string
	}{
		{"ok", library.VerifyAllMediaResult{IntegrityChecked: 5}, library.IntegrityOutcomeOK},
		{"partial soft", library.VerifyAllMediaResult{IntegrityChecked: 4, Partial: 1}, library.IntegrityOutcomePartial},
		{"partial mix", library.VerifyAllMediaResult{IntegrityChecked: 2, Failed: 1}, library.IntegrityOutcomePartial},
		{"failed", library.VerifyAllMediaResult{Failed: 3}, library.IntegrityOutcomeFailed},
	}
	for _, tc := range cases {
		r := tc.r
		r.FinalizeOutcome()
		if r.Outcome != tc.want {
			t.Fatalf("%s: outcome=%s want %s", tc.name, r.Outcome, tc.want)
		}
	}
}
