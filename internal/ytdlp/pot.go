package ytdlp

import (
	"context"
	"strings"
	"sync"
)

// PO token outcome for task detail / UI.
const (
	POTOff        = "off"        // provider URL unset
	POTSkipped    = "skipped"    // never, or auto/always with no mint attempt
	POTGenerating = "generating" // mint started (Generating a … PO Token); not yet Retrieved
	POTIssued     = "issued"     // Retrieved a PO Token
	POTFailed     = "failed"     // provider/plugin error
)

// POTStatus is stored under task detail JSON key "po-token".
type POTStatus struct {
	State    string      `json:"state"`              // off|skipped|generating|issued|failed
	Detail   string      `json:"detail,omitempty"`   // short operator note
	Fetch    string      `json:"fetch,omitempty"`    // auto|always|never
	Attempts []POTStatus `json:"attempts,omitempty"` // per yt-dlp invoke (cookie retry); no nested Attempts
}

// POTStage is one Stages substage line under downloaded / download_failed.
type POTStage struct {
	Message   string
	HasError  bool
	Icon      string // lucide name (cookie / shield-* Stages substages)
	IconClass string // optional Tailwind color/opacity on the icon
}

// StageEntries returns download-nested Stages lines for this PO token outcome.
// Always emits for known states: used (issued), skipped (skipped/off), plus failed/generating.
func (s POTStatus) StageEntries() []POTStage {
	switch s.State {
	case POTIssued:
		return []POTStage{{Message: "PO used", Icon: "shield-check", IconClass: "text-success"}}
	case POTFailed:
		return []POTStage{{Message: "PO failed", HasError: true, Icon: "triangle-alert", IconClass: "text-warning"}}
	case POTGenerating:
		return []POTStage{{Message: "PO generating", Icon: "loader-circle", IconClass: "text-info animate-spin"}}
	case POTSkipped, POTOff:
		return []POTStage{{Message: "PO skipped", Icon: "shield-off", IconClass: "opacity-70"}}
	default:
		return nil
	}
}

// ShowStage reports whether Stages should nest a PO token line under download.
func (s POTStatus) ShowStage() bool {
	return s.State != "" && len(s.StageEntries()) > 0
}

// AttemptAt returns the i-th yt-dlp pass snapshot when Attempts is set; otherwise s itself for i==0.
func (s POTStatus) AttemptAt(i int) (POTStatus, bool) {
	if i < 0 {
		return POTStatus{}, false
	}
	if len(s.Attempts) > 0 {
		if i >= len(s.Attempts) {
			return POTStatus{}, false
		}
		a := s.Attempts[i]
		a.Attempts = nil
		return a, a.State != ""
	}
	if i == 0 && s.State != "" {
		out := s
		out.Attempts = nil
		return out, true
	}
	return POTStatus{}, false
}

// DetailKeyPOToken is the tasks.detail JSON object key for POTStatus.
const DetailKeyPOToken = "po-token"

type potIssueKey struct{}

type potTracker struct {
	mu       sync.Mutex
	status   POTStatus   // current yt-dlp pass
	attempts []POTStatus // completed passes (TakePOTAttempt)
	onFail   func(detail string)
	failed   sync.Once
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

func stripPOTAttempts(st POTStatus) POTStatus {
	st.Attempts = nil
	return st
}

func (t *potTracker) aggregateLocked() POTStatus {
	if len(t.attempts) == 0 {
		return t.status
	}
	atts := make([]POTStatus, len(t.attempts), len(t.attempts)+1)
	copy(atts, t.attempts)
	cur := stripPOTAttempts(t.status)
	if cur.State != "" {
		atts = append(atts, cur)
	}
	last := atts[len(atts)-1]
	return POTStatus{
		State:    last.State,
		Detail:   last.Detail,
		Fetch:    last.Fetch,
		Attempts: atts,
	}
}

// POTStatusFromContext returns the latest classified POT status (may include Attempts).
func POTStatusFromContext(ctx context.Context) POTStatus {
	t := potTrackerFrom(ctx)
	if t == nil {
		return POTStatus{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.aggregateLocked()
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
	next.Attempts = nil
	t.status = next
	agg := t.aggregateLocked()
	changed := next.State != prev.State || next.Detail != prev.Detail
	failFn := t.onFail
	updFn := t.onUpdate
	t.mu.Unlock()

	if next.State == POTFailed && failFn != nil {
		t.failed.Do(func() { failFn(next.Detail) })
	}
	if changed && updFn != nil {
		updFn(agg)
	}
}

func potRank(state string) int {
	switch state {
	case POTFailed:
		return 4
	case POTIssued:
		return 3
	case POTGenerating:
		return 2
	case POTSkipped:
		return 1
	case POTOff:
		return 0
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
// Complete output never stays at generating: mint-start without Retrieved → skipped.
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
	if generating, detail := detectPOTGenerating(output); generating {
		d := "PO token mint started but not retrieved"
		if detail != "" {
			d = detail
		}
		return POTStatus{State: POTSkipped, Fetch: fetch, Detail: d}
	}
	// auto/always with no mint attempt: extractor skipped attestation.
	detail := "No PO token requested for this extract"
	if fetch == "always" {
		detail = "No PO token minted (extractor did not request one)"
	}
	return POTStatus{State: POTSkipped, Fetch: fetch, Detail: detail}
}

// FinalizePOT resolves mid-flight generating to a terminal skipped state when the
// task/yt-dlp invoke ends without Issued/Failed. Live ObservePOT may leave
// generating; rank would otherwise block ClassifyPOT skipped from winning.
// When Attempts are present, the returned status includes them (State = latest).
func FinalizePOT(ctx context.Context) POTStatus {
	t := potTrackerFrom(ctx)
	if t == nil {
		return POTStatus{}
	}
	t.mu.Lock()
	st := t.status
	updFn := t.onUpdate
	if st.State == POTGenerating {
		next := POTStatus{
			State:  POTSkipped,
			Fetch:  st.Fetch,
			Detail: "PO token mint started but not retrieved",
		}
		if st.Detail != "" {
			next.Detail = st.Detail
		}
		t.status = next
	}
	agg := t.aggregateLocked()
	t.mu.Unlock()
	if agg.State != "" && updFn != nil {
		updFn(agg)
	}
	return agg
}

// TakePOTAttempt finalizes the current yt-dlp pass, appends it to Attempts, and
// clears live status so the next download invoke (cookie retry) starts clean.
func TakePOTAttempt(ctx context.Context) POTStatus {
	t := potTrackerFrom(ctx)
	if t == nil {
		return POTStatus{}
	}
	t.mu.Lock()
	st := t.status
	if st.State == POTGenerating {
		st = POTStatus{
			State:  POTSkipped,
			Fetch:  st.Fetch,
			Detail: "PO token mint started but not retrieved",
		}
		if t.status.Detail != "" {
			st.Detail = t.status.Detail
		}
	}
	st = stripPOTAttempts(st)
	if st.State != "" {
		t.attempts = append(t.attempts, st)
	}
	t.status = POTStatus{}
	agg := t.aggregateLocked()
	updFn := t.onUpdate
	t.mu.Unlock()
	if agg.State != "" && updFn != nil {
		updFn(agg)
	}
	return st
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

func detectPOTGenerating(output string) (bool, string) {
	for _, line := range strings.Split(output, "\n") {
		if d := potGeneratingFromLine(line); d != "" {
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

// potGeneratingFromLine matches bgutil/yt-dlp mint-start lines (not yet Retrieved).
func potGeneratingFromLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	low := strings.ToLower(line)
	if strings.Contains(low, "generating a") && strings.Contains(low, "po token") {
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
		return
	}
	if d := potGeneratingFromLine(line); d != "" {
		ObservePOT(ctx, POTStatus{State: POTGenerating, Fetch: o.potFetch, Detail: d})
	}
}
