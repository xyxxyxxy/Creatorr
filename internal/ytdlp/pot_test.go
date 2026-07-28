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

func TestPOTTrackerRank(t *testing.T) {
	ctx := ContextWithPOTTracker(t.Context(), nil, nil)
	ObservePOT(ctx, POTStatus{State: POTSkipped, Fetch: "auto"})
	ObservePOT(ctx, POTStatus{State: POTIssued, Detail: "Retrieved a gvs PO Token"})
	ObservePOT(ctx, POTStatus{State: POTSkipped, Detail: "should not win"})
	st := POTStatusFromContext(ctx)
	if st.State != POTIssued {
		t.Fatalf("got %#v", st)
	}
	ObservePOT(ctx, POTStatus{State: POTFailed, Detail: "boom"})
	st = POTStatusFromContext(ctx)
	if st.State != POTFailed {
		t.Fatalf("failed should win, got %#v", st)
	}
}
