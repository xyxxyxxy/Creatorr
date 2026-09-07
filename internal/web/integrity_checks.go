package web

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// Keys rendered by the Results panel (suppressed from generic DetailFields).
var integrityDetailSuppressKeys = map[string]struct{}{
	"outcome":                 {},
	"checks":                  {},
	"integrity_checked":       {},
	"partial":                 {},
	"skipped":                 {},
	"failed":                  {},
	"skipped_busy":            {},
	"skipped_profile_off":     {},
	"skipped_no_media":        {},
	"skipped_busy_ids":        {},
	"skipped_profile_off_ids": {},
	"skipped_no_media_ids":    {},
	"verified":                {}, // legacy
}

var integrityHistorySkipMerge = map[string]struct{}{
	library.VideoHistIntegrityChecked: {},
	library.VideoHistVerifyFailed:     {},
	"sidecar_externally_changed":      {},
}

type integrityStatView struct {
	Title      string
	Value      string
	Desc       string
	ValueClass string // e.g. text-success / text-error / text-warning
}

type integrityVideoNoteView struct {
	ID       int64
	Title    string
	SeriesID int64
	Missing  bool
	Note     string
}

// integrityChecksView is the Results panel for integrity_check / integrity_check_initial.
type integrityChecksView struct {
	Bulk      bool
	Outcome   string
	Stats     []integrityStatView
	Failed    []integrityVideoNoteView
	Truncated bool
}

func integrityCheckLabel(key string) string {
	switch key {
	case library.IntegrityCheckNullDecode:
		return "Null-decode"
	case library.IntegrityCheckMediaChecksum:
		return "Media checksum"
	case library.IntegrityCheckSidecarChecksum:
		return "Sidecar checksum"
	case library.IntegrityCheckNFO:
		return "Episode NFO"
	default:
		return key
	}
}

func integrityResultValueClass(result string) string {
	switch result {
	case library.IntegrityResultOK, library.IntegrityResultFilled:
		return "text-success"
	case library.IntegrityResultFailed:
		return "text-error"
	case library.IntegrityResultPartial:
		return "text-warning"
	default:
		return ""
	}
}

func integrityCountValueClass(n int, kind string) string {
	if n <= 0 {
		return ""
	}
	switch kind {
	case "ok":
		return "text-success"
	case "partial":
		return "text-warning"
	case "failed":
		return "text-error"
	default:
		return ""
	}
}

func parseIntegrityReportFromDetail(detail string) *library.IntegrityCheckReport {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	var raw struct {
		Outcome string `json:"outcome"`
		Checks  []struct {
			Key    string `json:"key"`
			Result string `json:"result"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return nil
	}
	if raw.Outcome == "" && len(raw.Checks) == 0 {
		return nil
	}
	rep := &library.IntegrityCheckReport{Outcome: raw.Outcome}
	for _, c := range raw.Checks {
		rep.Checks = append(rep.Checks, library.IntegrityCheckItem{
			Key: c.Key, Result: c.Result, Detail: c.Detail,
		})
	}
	return rep
}

func parseVerifyAllResultJSON(s string) *library.VerifyAllMediaResult {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var raw library.VerifyAllMediaResult
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil
	}
	// Accept legacy-only count objects without outcome/checks.
	if raw.IntegrityChecked == 0 && raw.Partial == 0 && raw.Skipped == 0 && raw.Failed == 0 &&
		raw.Outcome == "" && len(raw.Checks) == 0 &&
		raw.SkippedBusy == 0 && raw.SkippedProfileOff == 0 && raw.SkippedNoMedia == 0 {
		// Might still be all-zero finished run; treat as present if any known key exists.
		var m map[string]any
		if json.Unmarshal([]byte(s), &m) != nil {
			return nil
		}
		_, hasChecked := m["integrity_checked"]
		_, hasSkipped := m["skipped"]
		_, hasFailed := m["failed"]
		if !hasChecked && !hasSkipped && !hasFailed {
			return nil
		}
	}
	return &raw
}

func (h *Handler) buildIntegrityChecks(t *queue.Task, events []library.VideoHistoryEvent) *integrityChecksView {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case queue.KindIntegrityCheckInitial:
		return h.buildIntegrityChecksInitial(t, events)
	case queue.KindIntegrityCheck:
		return h.buildIntegrityChecksBulk(t, events)
	default:
		return nil
	}
}

func (h *Handler) buildIntegrityChecksInitial(t *queue.Task, events []library.VideoHistoryEvent) *integrityChecksView {
	rep := parseIntegrityReportFromDetail(t.Detail)
	if rep == nil {
		// Prefer terminal history row with report.
	histLoop:
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			switch e.Event {
			case library.VideoHistIntegrityChecked, library.VideoHistVerifyFailed:
				if r := parseIntegrityReportFromDetail(e.Detail); r != nil {
					rep = r
					break histLoop
				}
			}
		}
	}
	if rep == nil || (rep.Outcome == "" && len(rep.Checks) == 0) {
		return nil
	}
	stats := make([]integrityStatView, 0, len(rep.Checks))
	for _, c := range rep.Checks {
		stats = append(stats, integrityStatView{
			Title:      integrityCheckLabel(c.Key),
			Value:      c.Result,
			Desc:       c.Detail,
			ValueClass: integrityResultValueClass(c.Result),
		})
	}
	view := &integrityChecksView{
		Bulk:    false,
		Outcome: rep.Outcome,
		Stats:   stats,
	}
	if rep.Outcome == library.IntegrityOutcomeFailed {
		vid := int64(0)
		if t.VideoID.Valid {
			vid = t.VideoID.Int64
		}
		if vid == 0 {
			for _, e := range events {
				if e.Event == library.VideoHistVerifyFailed {
					vid = e.VideoID
					break
				}
			}
		}
		if vid > 0 {
			notes := map[int64]string{vid: integrityFailingNote(rep)}
			view.Failed = h.integrityVideoNotes(map[int64]struct{}{vid: {}}, notes, 1)
		}
	}
	return view
}

func (h *Handler) buildIntegrityChecksBulk(t *queue.Task, events []library.VideoHistoryEvent) *integrityChecksView {
	src := strings.TrimSpace(t.Detail)
	if src == "" || !strings.Contains(src, "integrity_checked") {
		// Live run: counters live on payload until finish SetDetail.
		if p := strings.TrimSpace(t.Payload); p != "" {
			src = p
		}
	}
	res := parseVerifyAllResultJSON(src)
	if res == nil {
		return nil
	}
	res.FinalizeOutcome()
	view := &integrityChecksView{
		Bulk:    true,
		Outcome: res.Outcome,
		Stats: []integrityStatView{
			{
				Title:      "Ok",
				Value:      strconv.Itoa(res.IntegrityChecked),
				Desc:       "Clean integrity check",
				ValueClass: integrityCountValueClass(res.IntegrityChecked, "ok"),
			},
			{
				Title:      "Partial",
				Value:      strconv.Itoa(res.Partial),
				Desc:       "Sidecar/NFO issues",
				ValueClass: integrityCountValueClass(res.Partial, "partial"),
			},
			{
				Title: "Skipped",
				Value: strconv.Itoa(res.Skipped),
				Desc:  skipReasonsDesc(res),
			},
			{
				Title:      "Failed",
				Value:      strconv.Itoa(res.Failed),
				Desc:       "Null-decode or media hash",
				ValueClass: integrityCountValueClass(res.Failed, "failed"),
			},
		},
	}

	failedIDs := map[int64]struct{}{}
	notes := map[int64]string{}
	for _, e := range events {
		switch e.Event {
		case library.VideoHistVerifyFailed:
			failedIDs[e.VideoID] = struct{}{}
			if rep := parseIntegrityReportFromDetail(e.Detail); rep != nil {
				notes[e.VideoID] = integrityFailingNote(rep)
			} else if d := strings.TrimSpace(e.Message); d != "" {
				notes[e.VideoID] = d
			}
		}
	}
	view.Failed = h.integrityVideoNotes(failedIDs, notes, library.IntegrityCheckDetailCap)
	if len(failedIDs) > library.IntegrityCheckDetailCap {
		view.Truncated = true
	}
	return view
}

func skipReasonsDesc(res *library.VerifyAllMediaResult) string {
	if res == nil || res.Skipped <= 0 {
		return "Busy, integrity check disabled, or no media"
	}
	var parts []string
	if res.SkippedBusy > 0 {
		parts = append(parts, "busy")
	}
	if res.SkippedProfileOff > 0 {
		parts = append(parts, "integrity check disabled")
	}
	if res.SkippedNoMedia > 0 {
		parts = append(parts, "no media")
	}
	if len(parts) == 0 {
		return "Busy, integrity check disabled, or no media"
	}
	return strings.Join(parts, ", ")
}

func integrityFailingNote(rep *library.IntegrityCheckReport) string {
	if rep == nil {
		return ""
	}
	var parts []string
	for _, c := range rep.Checks {
		if c.Result == library.IntegrityResultFailed || c.Result == library.IntegrityResultPartial {
			label := integrityCheckLabel(c.Key)
			if d := strings.TrimSpace(c.Detail); d != "" {
				parts = append(parts, label+": "+d)
			} else {
				parts = append(parts, label+" "+c.Result)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func (h *Handler) integrityVideoNotes(ids map[int64]struct{}, notes map[int64]string, capN int) []integrityVideoNoteView {
	if len(ids) == 0 {
		return nil
	}
	sorted := make([]int64, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if capN > 0 && len(sorted) > capN {
		sorted = sorted[:capN]
	}
	out := make([]integrityVideoNoteView, 0, len(sorted))
	for _, id := range sorted {
		ref := integrityVideoNoteView{ID: id, Title: fmt.Sprintf("#%d", id), Missing: true}
		if h.Library != nil {
			if vv, err := h.Library.GetVideo(id); err == nil && vv != nil {
				ref.Title = vv.Title
				ref.SeriesID = vv.SeriesID
				ref.Missing = false
			}
		}
		if notes != nil {
			ref.Note = notes[id]
		}
		out = append(out, ref)
	}
	return out
}

func filterIntegrityHistoryRows(rows []taskDetailHistRow) []taskDetailHistRow {
	if len(rows) == 0 {
		return rows
	}
	out := make([]taskDetailHistRow, 0, len(rows))
	for _, r := range rows {
		if _, skip := integrityHistorySkipMerge[r.Event]; skip {
			continue
		}
		out = append(out, r)
	}
	return out
}

func filterSuppressedDetailFields(fields []detailField, suppress map[string]struct{}) []detailField {
	if len(fields) == 0 || len(suppress) == 0 {
		return fields
	}
	out := make([]detailField, 0, len(fields))
	for _, f := range fields {
		if _, skip := suppress[f.Key]; skip {
			continue
		}
		out = append(out, f)
	}
	return out
}
