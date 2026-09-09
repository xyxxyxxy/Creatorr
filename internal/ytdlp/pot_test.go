package ytdlp

import "testing"

func TestClassifyPOT(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		fetch  string
		url    string
		want   string
	}{
		{"off", "", "auto", "", POTOff},
		{"never", "", "never", "http://creatorr-po-token:4416", POTSkipped},
		{"auto skip", "[info] ok", "auto", "http://creatorr-po-token:4416", POTSkipped},
		{"generating", "[youtube] [pot:bgutil:http] Generating a player PO Token for mweb client via bgutil HTTP server", "always", "http://creatorr-po-token:4416", POTSkipped},
		{"issued beats generating", "[pot] Generating a player PO Token for mweb\nRetrieved a gvs PO Token for mweb client", "always", "http://x", POTIssued},
		{"issued", "[debug] Retrieved a gvs PO Token for web_safari client", "auto", "http://creatorr-po-token:4416", POTIssued},
		{"failed providers", "[debug] [youtube] [pot] PO Token Providers: none", "always", "http://creatorr-po-token:4416", POTFailed},
		{"failed ping", "WARNING: [youtube] [pot:bgutil:http] Error reaching GET http://127.0.0.1:4416/ping", "auto", "http://creatorr-po-token:4416", POTFailed},
		{"script unavailable ok", "[debug] [pot:bgutil:script-node] Script path doesn't exist\nRetrieved a gvs PO Token for web", "auto", "http://x", POTIssued},
	}
	for _, tc := range cases {
		got := ClassifyPOT(tc.out, tc.fetch, tc.url)
		if got.State != tc.want {
			t.Fatalf("%s: state=%q want %q detail=%q", tc.name, got.State, tc.want, got.Detail)
		}
	}
}

func TestDetectPOTIssue(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"[debug] [youtube] [pot] PO Token Providers: none", true},
		{"WARNING: [youtube] [pot:bgutil:http] Error reaching GET http://127.0.0.1:4416/ping (caused by TransportError).", true},
		{"[debug] [youtube] fKu: Retrieved a gvs PO Token for web_safari client", false},
		{"[debug] [youtube] [pot:bgutil:script-node] Script path doesn't exist: /app/x", false},
		{"[debug] [youtube] [pot] PO Token Providers: bgutil:http-1.3.1 (external), bgutil:script-node-1.3.1 (external, unavailable)", false},
		{"ERROR: [youtube] Failed to retrieve PO Token for web client", true},
	}
	for _, tc := range cases {
		got := DetectPOTIssue(tc.in)
		if tc.want && got == "" {
			t.Fatalf("want issue for %q", tc.in)
		}
		if !tc.want && got != "" {
			t.Fatalf("unexpected issue %q for %q", got, tc.in)
		}
	}
}

func TestPOTStatusStageEntries(t *testing.T) {
	off := POTStatus{State: POTOff}
	got := off.StageEntries()
	if !off.ShowStage() || len(got) != 1 || got[0].Message != "PO skipped" {
		t.Fatalf("off → skipped: %#v", got)
	}
	got = (POTStatus{State: POTIssued}).StageEntries()
	if len(got) != 1 || got[0].Message != "PO used" || got[0].HasError || got[0].Icon != "shield-check" {
		t.Fatalf("issued: %#v", got)
	}
	got = (POTStatus{State: POTFailed}).StageEntries()
	if len(got) != 1 || got[0].Message != "PO failed" || !got[0].HasError || got[0].Icon != "triangle-alert" {
		t.Fatalf("failed: %#v", got)
	}
	got = (POTStatus{State: POTSkipped}).StageEntries()
	if len(got) != 1 || got[0].Message != "PO skipped" || got[0].Icon != "shield-off" {
		t.Fatalf("skipped: %#v", got)
	}
}

func TestPOTTrackerRank(t *testing.T) {
	ctx := ContextWithPOTTracker(t.Context(), nil, nil)
	ObservePOT(ctx, POTStatus{State: POTSkipped, Fetch: "auto"})
	ObservePOT(ctx, POTStatus{State: POTGenerating, Detail: "Generating a player PO Token"})
	ObservePOT(ctx, POTStatus{State: POTSkipped, Detail: "should not win"})
	st := POTStatusFromContext(ctx)
	if st.State != POTGenerating {
		t.Fatalf("generating should beat skipped, got %#v", st)
	}
	ObservePOT(ctx, POTStatus{State: POTIssued, Detail: "Retrieved a gvs PO Token"})
	ObservePOT(ctx, POTStatus{State: POTGenerating, Detail: "should not downgrade"})
	st = POTStatusFromContext(ctx)
	if st.State != POTIssued {
		t.Fatalf("got %#v", st)
	}
	ObservePOT(ctx, POTStatus{State: POTFailed, Detail: "boom"})
	st = POTStatusFromContext(ctx)
	if st.State != POTFailed {
		t.Fatalf("failed should win, got %#v", st)
	}
}

func TestFinalizePOT(t *testing.T) {
	ctx := ContextWithPOTTracker(t.Context(), nil, nil)
	ObservePOT(ctx, POTStatus{State: POTGenerating, Fetch: "always", Detail: "Generating a player PO Token"})
	st := FinalizePOT(ctx)
	if st.State != POTSkipped || st.Fetch != "always" {
		t.Fatalf("finalize generating → skipped: %#v", st)
	}
	if POTStatusFromContext(ctx).State != POTSkipped {
		t.Fatalf("tracker %#v", POTStatusFromContext(ctx))
	}
	// Terminal states unchanged.
	ObservePOT(ctx, POTStatus{State: POTIssued, Detail: "Retrieved"})
	st = FinalizePOT(ctx)
	if st.State != POTIssued {
		t.Fatalf("issued must stay: %#v", st)
	}
}

func TestTakePOTAttemptPerPass(t *testing.T) {
	ctx := ContextWithPOTTracker(t.Context(), nil, nil)
	ObservePOT(ctx, POTStatus{State: POTGenerating, Fetch: "always", Detail: "pass1 generating"})
	a1 := TakePOTAttempt(ctx)
	if a1.State != POTSkipped {
		t.Fatalf("pass1: %#v", a1)
	}
	ObservePOT(ctx, POTStatus{State: POTIssued, Fetch: "always", Detail: "pass2 retrieved"})
	a2 := TakePOTAttempt(ctx)
	if a2.State != POTIssued {
		t.Fatalf("pass2: %#v", a2)
	}
	agg := POTStatusFromContext(ctx)
	if agg.State != POTIssued || len(agg.Attempts) != 2 {
		t.Fatalf("aggregate: %#v", agg)
	}
	if agg.Attempts[0].State != POTSkipped || agg.Attempts[1].State != POTIssued {
		t.Fatalf("attempts: %#v", agg.Attempts)
	}
	first, ok := agg.AttemptAt(0)
	if !ok || first.State != POTSkipped {
		t.Fatalf("AttemptAt0: %#v ok=%v", first, ok)
	}
	second, ok := agg.AttemptAt(1)
	if !ok || second.State != POTIssued {
		t.Fatalf("AttemptAt1: %#v ok=%v", second, ok)
	}
}
