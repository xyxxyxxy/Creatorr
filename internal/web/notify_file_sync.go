package web

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

// notifyFileSyncIssueRef is one row in a file_sync_issues body section.
type notifyFileSyncIssueRef struct {
	ID       int64
	SeriesID int64
	Series   string
	Title    string
	Detail   string // optional; e.g. "json: ep.info.json"
	Missing  bool   // video no longer in library
}

// notifyFileSyncIssueSection is Missing or Size changed for the notification Related to UI.
type notifyFileSyncIssueSection struct {
	Heading string
	Total   int
	Items   []notifyFileSyncIssueRef
	Extra   int // items beyond FileSyncIssueListCap
}

// fileSyncNotifySectionsFromDetail builds Related to video sections from sync_files
// tasks.detail JSON. Returns nil when detail has no issue lists.
func fileSyncNotifySectionsFromDetail(detail string, resolve func(videoID int64) notifyFileSyncIssueRef) []notifyFileSyncIssueSection {
	detail = strings.TrimSpace(detail)
	if detail == "" || resolve == nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return nil
	}

	missing := append(refsFromVideoIDs(raw["missing_ids"], resolve),
		refsFromSidecarIssues(raw["sidecar_missing"], resolve)...)
	changed := append(refsFromVideoIDs(raw["externally_changed_ids"], resolve),
		refsFromSidecarIssues(raw["sidecar_changed"], resolve)...)

	var out []notifyFileSyncIssueSection
	if sec := capFileSyncNotifySection("Missing", missing); sec != nil {
		out = append(out, *sec)
	}
	if sec := capFileSyncNotifySection("Size changed", changed); sec != nil {
		out = append(out, *sec)
	}
	return out
}

func capFileSyncNotifySection(heading string, items []notifyFileSyncIssueRef) *notifyFileSyncIssueSection {
	if len(items) == 0 {
		return nil
	}
	sec := &notifyFileSyncIssueSection{Heading: heading, Total: len(items), Items: items}
	if len(items) > notify.FileSyncIssueListCap {
		sec.Extra = len(items) - notify.FileSyncIssueListCap
		sec.Items = items[:notify.FileSyncIssueListCap]
	}
	return sec
}

func refsFromVideoIDs(v any, resolve func(videoID int64) notifyFileSyncIssueRef) []notifyFileSyncIssueRef {
	ids, ok := jsonNumberIDs(v)
	if !ok || len(ids) == 0 {
		return nil
	}
	out := make([]notifyFileSyncIssueRef, 0, len(ids))
	for _, id := range ids {
		ref := resolve(id)
		ref.ID = id
		out = append(out, ref)
	}
	return out
}

func refsFromSidecarIssues(v any, resolve func(videoID int64) notifyFileSyncIssueRef) []notifyFileSyncIssueRef {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]notifyFileSyncIssueRef, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		vid, ok := jsonInt64(m["VideoID"])
		if !ok || vid <= 0 {
			continue
		}
		kind, _ := m["Kind"].(string)
		path, _ := m["Path"].(string)
		ref := resolve(vid)
		ref.ID = vid
		ref.Detail = sidecarIssueDetailLabel(kind, path)
		out = append(out, ref)
	}
	return out
}

func jsonInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func sidecarIssueDetailLabel(kind, path string) string {
	kind = strings.TrimSpace(kind)
	base := filepath.Base(strings.TrimSpace(path))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return kind
	}
	if kind == "" {
		return base
	}
	return kind + ": " + base
}

func (h *Handler) resolveFileSyncNotifyRef(videoID int64) notifyFileSyncIssueRef {
	ref := notifyFileSyncIssueRef{
		ID:      videoID,
		Title:   "",
		Missing: true,
	}
	if h.Library == nil {
		return ref
	}
	v, err := h.Library.GetVideo(videoID)
	if err != nil || v == nil {
		return ref
	}
	ref.Title = v.Title
	ref.SeriesID = v.SeriesID
	ref.Missing = false
	if ser, serr := h.Library.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
		ref.Series = ser.Title
	}
	return ref
}
