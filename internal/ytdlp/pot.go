package ytdlp

import (
	"context"
	"strings"
	"sync"
)

// PO token outcome for task detail / UI.
const (
	POTOff     = "off"     // provider URL unset
	POTSkipped = "skipped" // never, or auto/always with no mint attempt
	POTIssued  = "issued"  // Retrieved a PO Token
	POTFailed  = "failed"  // provider/plugin error
)

// POTStatus is stored under task detail JSON key "po-token".
type POTStatus struct {
	State  string `json:"state"`            // off|skipped|issued|failed
	Detail string `json:"detail,omitempty"` // short operator note
	Fetch  string `json:"fetch,omitempty"`  // auto|always|never
}

// DetailKeyPOToken is the tasks.detail JSON object key for POTStatus.
const DetailKeyPOToken = "po-token"

type potIssueKey struct{}

type potTracker struct {
	mu     sync.Mutex
	status POTStatus
	onFail func(detail string)
	failed sync.Once
	onUpdate func(POTStatus) // optional: persist mid-task
}

// ContextWithPOTTracker watches yt-dlp POT lines for this task.
// onFail is called once when state becomes failed (warning notify).
// onUpdate is called when the classified state changes (persist detail).
func ContextWithPOTTracker(ctx context.Context, onFail func(detail string), onUpdate func(POTStatus)) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, potIssueKey{}, &potTracker{onFail: onFail, onUpdate: onUpdate})
}

// ContextWithPOTIssue is kept for callers that only need failure notify.
func ContextWithPOTIssue(ctx context.Context, fn func(detail string)) context.Context {
	return ContextWithPOTTracker(ctx, fn, nil)
}

func potTrackerFrom(ctx context.Context) *potTracker {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(potIssueKey{}).(*potTracker)
	return t
}

// POTStatusFromContext returns the latest classified POT status (may be empty).
func POTStatusFromContext(ctx context.Context) POTStatus {
	t := potTrackerFrom(ctx)
	if t == nil {
		return POTStatus{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

func (t *potTracker) apply(next POTStatus) {
	t.mu.Lock()
	prev := t.status
	if potRank(next.State) < potRank(prev.State) {
		t.mu.Unlock()
		return
	}
	if next.State == prev.State && next.Detail == prev.Detail && next.Fetch == prev.Fetch {
		t.mu.Unlock()
		return
	}
	if next.Fetch == "" {
		next.Fetch = prev.Fetch
	}
	if next.Detail == "" && next.State == prev.State {
		next.Detail = prev.Detail
	}
	t.status = next
	st := t.status
	changed := st.State != prev.State || st.Detail != prev.Detail
	failFn := t.onFail
	updFn := t.onUpdate
	t.mu.Unlock()

	if st.State == POTFailed && failFn != nil {
		t.failed.Do(func() { failFn(st.Detail) })
	}
	if changed && updFn != nil {
		updFn(st)
	}
}

func potRank(state string) int {
	switch state {
	case POTFailed:
		return 4
	case POTIssued:
		return 3
	case POTSkipped:
		return 2
	case POTOff:
		return 1
	default:
		return 0
	}
}

// ObservePOT merges a classified status into the task tracker.
func ObservePOT(ctx context.Context, st POTStatus) {
	t := potTrackerFrom(ctx)
	if t == nil || st.State == "" {
		return
	}
	t.apply(st)
}

// ClassifyPOT derives PO token outcome from yt-dlp output and fetch settings.
func ClassifyPOT(output, fetch, providerURL string) POTStatus {
	fetch = strings.TrimSpace(fetch)
	if fetch == "" {
		fetch = "never"
	}
	urlSet := strings.TrimSpace(providerURL) != ""
	if !urlSet {
		return POTStatus{State: POTOff, Fetch: fetch, Detail: "PO token provider URL unset"}
	}
	if fetch == "never" {
		return POTStatus{State: POTSkipped, Fetch: fetch, Detail: "PO token fetch set to never"}
	}

	if d := DetectPOTIssue(output); d != "" {
		return POTStatus{State: POTFailed, Fetch: fetch, Detail: d}
	}
	if issued, detail := detectPOTIssued(output); issued {
		return POTStatus{State: POTIssued, Fetch: fetch, Detail: detail}
	}
	// auto/always with no mint attempt: extractor skipped attestation.
	detail := "No PO token requested for this extract"
	if fetch == "always" {
		detail = "No PO token minted (extractor did not request one)"
	}
	return POTStatus{State: POTSkipped, Fetch: fetch, Detail: detail}
}

// DetectPOTIssue scans yt-dlp output for PO token provider failures.
func DetectPOTIssue(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if d := potIssueFromLine(line); d != "" {
			return d
		}
	}
	return ""
}

func detectPOTIssued(output string) (bool, string) {
	for _, line := range strings.Split(output, "\n") {
		if d := potIssuedFromLine(line); d != "" {
			return true, d
		}
	}
	return false, ""
}

func potIssuedFromLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	low := strings.ToLower(line)
	if strings.Contains(low, "retrieved a") && strings.Contains(low, "po token") {
		return trimPOTLine(line)
	}
	return ""
}

func potIssueFromLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	low := strings.ToLower(line)
	if strings.Contains(line, "PO Token Providers: none") {
		return "PO Token Providers: none (plugin not loaded)"
	}
	if strings.Contains(low, "error reaching get") &&
		(strings.Contains(low, "[pot") || strings.Contains(low, "/ping") || strings.Contains(low, "4416")) {
		return trimPOTLine(line)
	}
	if strings.Contains(low, "[pot") && strings.Contains(low, "error") {
		return trimPOTLine(line)
	}
	if strings.Contains(low, "po token") &&
		(strings.Contains(low, "failed to retrieve") || strings.Contains(low, "unable to retrieve") ||
			strings.Contains(low, "failed to fetch") || strings.Contains(low, "unable to fetch")) {
		return trimPOTLine(line)
	}
	return ""
}

func isPOTTraceLine(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "[pot") || strings.Contains(line, "PO Token")
}

func trimPOTLine(line string) string {
	line = strings.TrimSpace(line)
	for _, p := range []string{"WARNING: ", "ERROR: ", "[debug] "} {
		line = strings.TrimPrefix(line, p)
	}
	if len(line) > 400 {
		line = line[:400]
	}
	return line
}

func notePOTOutput(ctx context.Context, o options, chunks ...[]byte) {
	var b strings.Builder
	for _, c := range chunks {
		b.Write(c)
	}
	st := ClassifyPOT(b.String(), o.potFetch, o.potProviderURL)
	ObservePOT(ctx, st)
}

// observePOTLine updates the tracker from one yt-dlp line (streaming path).
func observePOTLine(ctx context.Context, o options, line string) {
	if d := potIssueFromLine(line); d != "" {
		ObservePOT(ctx, POTStatus{State: POTFailed, Fetch: o.potFetch, Detail: d})
		return
	}
	if d := potIssuedFromLine(line); d != "" {
		ObservePOT(ctx, POTStatus{State: POTIssued, Fetch: o.potFetch, Detail: d})
	}
}
