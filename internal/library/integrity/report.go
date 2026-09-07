package integrity

import (
	"strings"
)

// Integrity check step keys (task detail Results panel + video_history.detail).
const (
	IntegrityCheckNullDecode      = "null_decode"
	IntegrityCheckMediaChecksum   = "media_checksum"
	IntegrityCheckSidecarChecksum = "sidecar_checksum"
	IntegrityCheckNFO             = "nfo"
)

// Per-check and video/task outcome results.
const (
	IntegrityResultOK      = "ok"
	IntegrityResultFilled  = "filled"
	IntegrityResultFailed  = "failed"
	IntegrityResultPartial = "partial"
	IntegrityResultSkipped = "skipped"
)

const (
	IntegrityOutcomeOK      = "ok"
	IntegrityOutcomePartial = "partial"
	IntegrityOutcomeFailed  = "failed"
	IntegrityOutcomeSkipped = "skipped"
)

// IntegrityCheckDetailCap is max video IDs kept per skipped/fail/partial list in task detail.
const IntegrityCheckDetailCap = 100

// IntegrityCheckItem is one step in an IntegrityCheckReport.
type IntegrityCheckItem struct {
	Key    string `json:"key"`
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
}

// IntegrityCheckReport is the structured result of RunIntegrityCheckVideo.
type IntegrityCheckReport struct {
	Outcome string               `json:"outcome"`
	Checks  []IntegrityCheckItem `json:"checks"`
}

// IntegrityCheckReportDetailMap is the JSON object stored on video_history / tasks.detail.
func (r *IntegrityCheckReport) DetailMap() map[string]any {
	if r == nil {
		return nil
	}
	checks := make([]map[string]any, 0, len(r.Checks))
	for _, c := range r.Checks {
		m := map[string]any{"key": c.Key, "result": c.Result}
		if d := strings.TrimSpace(c.Detail); d != "" {
			m["detail"] = d
		}
		checks = append(checks, m)
	}
	out := map[string]any{
		"outcome": r.Outcome,
		"checks":  checks,
	}
	return out
}

func (r *IntegrityCheckReport) SetCheck(key, result, detail string) {
	if r == nil {
		return
	}
	item := IntegrityCheckItem{Key: key, Result: result, Detail: strings.TrimSpace(detail)}
	for i := range r.Checks {
		if r.Checks[i].Key == key {
			r.Checks[i] = item
			return
		}
	}
	r.Checks = append(r.Checks, item)
}

func (r *IntegrityCheckReport) FinalizeCheckOutcome() {
	if r == nil {
		return
	}
	hardFail := false
	softPartial := false
	for _, c := range r.Checks {
		switch c.Key {
		case IntegrityCheckNullDecode, IntegrityCheckMediaChecksum:
			if c.Result == IntegrityResultFailed {
				hardFail = true
			}
		case IntegrityCheckSidecarChecksum, IntegrityCheckNFO:
			if c.Result == IntegrityResultFailed || c.Result == IntegrityResultPartial {
				softPartial = true
			}
		}
	}
	switch {
	case hardFail:
		r.Outcome = IntegrityOutcomeFailed
	case softPartial:
		r.Outcome = IntegrityOutcomePartial
	default:
		r.Outcome = IntegrityOutcomeOK
	}
}

// VerifyAllMediaResult is the finish summary for a bulk integrity_check task.
type VerifyAllMediaResult struct {
	IntegrityChecked  int                       `json:"integrity_checked"`
	Partial           int                       `json:"partial"`
	Skipped           int                       `json:"skipped"`
	Failed            int                       `json:"failed"`
	SkippedBusy       int                       `json:"skipped_busy"`
	SkippedProfileOff int                       `json:"skipped_profile_off"`
	SkippedNoMedia    int                       `json:"skipped_no_media"`
	Outcome           string                    `json:"outcome"`
	Checks            map[string]map[string]int `json:"checks"`
	SkippedBusyIDs    []int64                   `json:"skipped_busy_ids,omitempty"`
	SkippedProfileIDs []int64                   `json:"skipped_profile_off_ids,omitempty"`
	SkippedNoMediaIDs []int64                   `json:"skipped_no_media_ids,omitempty"`
}

// FinalizeOutcome sets Outcome from aggregate counters.
func (r *VerifyAllMediaResult) FinalizeOutcome() {
	if r == nil {
		return
	}
	switch {
	case r.Failed > 0 && (r.IntegrityChecked > 0 || r.Partial > 0):
		r.Outcome = IntegrityOutcomePartial
	case r.Partial > 0 && r.Failed == 0:
		r.Outcome = IntegrityOutcomePartial
	case r.Failed > 0 && r.IntegrityChecked == 0 && r.Partial == 0:
		r.Outcome = IntegrityOutcomeFailed
	case r.IntegrityChecked > 0:
		r.Outcome = IntegrityOutcomeOK
	default:
		r.Outcome = IntegrityOutcomeOK
	}
}

func (r *VerifyAllMediaResult) DetailMap() map[string]any {
	if r == nil {
		return nil
	}
	r.FinalizeOutcome()
	m := map[string]any{
		"outcome":             r.Outcome,
		"integrity_checked":   r.IntegrityChecked,
		"partial":             r.Partial,
		"skipped":             r.Skipped,
		"failed":              r.Failed,
		"skipped_busy":        r.SkippedBusy,
		"skipped_profile_off": r.SkippedProfileOff,
		"skipped_no_media":    r.SkippedNoMedia,
	}
	if len(r.Checks) > 0 {
		m["checks"] = r.Checks
	}
	if len(r.SkippedBusyIDs) > 0 {
		m["skipped_busy_ids"] = r.SkippedBusyIDs
	}
	if len(r.SkippedProfileIDs) > 0 {
		m["skipped_profile_off_ids"] = r.SkippedProfileIDs
	}
	if len(r.SkippedNoMediaIDs) > 0 {
		m["skipped_no_media_ids"] = r.SkippedNoMediaIDs
	}
	return m
}

func BumpCheckAgg(agg map[string]map[string]int, report *IntegrityCheckReport) {
	if report == nil {
		return
	}
	if agg == nil {
		return
	}
	for _, c := range report.Checks {
		if agg[c.Key] == nil {
			agg[c.Key] = map[string]int{}
		}
		agg[c.Key][c.Result]++
	}
}

func AppendCappedID(dst []int64, id int64) []int64 {
	if id <= 0 || len(dst) >= IntegrityCheckDetailCap {
		return dst
	}
	return append(dst, id)
}

func FailDetail(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
