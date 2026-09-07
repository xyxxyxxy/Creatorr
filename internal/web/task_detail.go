package web

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// detailVideoRef is one video id from task detail JSON.
type detailVideoRef struct {
	ID            int64
	Title         string
	SeriesID      int64
	Missing       bool
	State         string // wanted|ignored; set for created_ids rows
	IgnoredReason string // media_type|index_as_ignored; only when State=ignored
	StatusTip     string // badge tooltip when HasState (includes ignore reason)
	HasState      bool   // true when State is meaningful for this list
}

// detailSkippedTitle is a listing skipped by title filter (no video row).
type detailSkippedTitle struct {
	RemoteID string
	Title    string
}

// potDetailView is PO token state from task detail JSON for the Details table.
type potDetailView struct {
	State  string
	Label  string
	Detail string
	Fetch  string
}

func parsePOTDetail(detail string) *potDetailView {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return nil
	}
	potRaw, ok := raw[ytdlp.DetailKeyPOToken]
	if !ok || potRaw == nil {
		return nil
	}
	b, err := json.Marshal(potRaw)
	if err != nil {
		return nil
	}
	var pot struct {
		State  string `json:"state"`
		Detail string `json:"detail"`
		Fetch  string `json:"fetch"`
	}
	if err := json.Unmarshal(b, &pot); err != nil || pot.State == "" {
		return nil
	}
	label := pot.State
	switch pot.State {
	case "issued":
		label = "Issued"
	case "failed":
		label = "Failed"
	case "skipped":
		label = "Skipped"
	case "off":
		label = "Off"
	}
	return &potDetailView{State: pot.State, Label: label, Detail: pot.Detail, Fetch: pot.Fetch}
}

func parseDomainAccessDetail(detail string) *domains.DomainAccessSnapshot {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return nil
	}
	accRaw, ok := raw[domains.DetailKeyDomainAccess]
	if !ok || accRaw == nil {
		return nil
	}
	b, err := json.Marshal(accRaw)
	if err != nil {
		return nil
	}
	var snap domains.DomainAccessSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil
	}
	return &snap
}

// detailField is one top-level key from task detail JSON.
type detailField struct {
	Key                string
	Text               string // scalar / non-video value (pretty)
	Videos             []detailVideoRef
	IsVideoList        bool
	SkippedTitles      []detailSkippedTitle
	SkippedTitlesMore  int // titles beyond first 20
	IsSkippedTitleList bool
}

var detailVideoIDKeys = map[string]struct{}{
	"video_ids":              {},
	"missing_ids":            {},
	"restored_ids":           {},
	"beginning_missing_ids":  {},
	"beginning_restored_ids": {},
	"retention_ids":          {},
	"created_ids":            {},
	"updated_ids":            {},
}

func (h *Handler) resolveDetailVideos(ids []int64) []detailVideoRef {
	out := make([]detailVideoRef, 0, len(ids))
	for _, vid := range ids {
		ref := detailVideoRef{ID: vid, Title: fmt.Sprintf("#%d", vid), Missing: true}
		if h.Library != nil {
			if vv, err := h.Library.GetVideo(vid); err == nil && vv != nil {
				ref.Title = vv.Title
				ref.SeriesID = vv.SeriesID
				ref.Missing = false
			}
		}
		out = append(out, ref)
	}
	return out
}

func idSetFromJSON(v any) map[int64]struct{} {
	ids, ok := jsonNumberIDs(v)
	if !ok {
		return nil
	}
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func (h *Handler) resolveCreatedDetailVideos(ids []int64, mediaTypeIDs, indexAsIgnoredIDs map[int64]struct{}) []detailVideoRef {
	out := h.resolveDetailVideos(ids)
	for i := range out {
		out[i].HasState = true
		out[i].State = "wanted"
		out[i].StatusTip = "wanted"
		if _, ok := mediaTypeIDs[out[i].ID]; ok {
			out[i].State = "ignored"
			out[i].IgnoredReason = library.IgnoreReasonMediaType
			out[i].StatusTip = "ignored (media type)"
			continue
		}
		if _, ok := indexAsIgnoredIDs[out[i].ID]; ok {
			out[i].State = "ignored"
			out[i].IgnoredReason = library.IgnoreReasonIndexAsIgnored
			out[i].StatusTip = "ignored (indexed as ignored)"
		}
	}
	return out
}

func parseSkippedTitleRegexp(v any) ([]detailSkippedTitle, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]detailSkippedTitle, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		title, _ := m["title"].(string)
		remoteID, _ := m["remote_id"].(string)
		if title == "" && remoteID == "" {
			continue
		}
		out = append(out, detailSkippedTitle{RemoteID: remoteID, Title: title})
	}
	return out, true
}

func jsonNumberIDs(v any) ([]int64, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	ids := make([]int64, 0, len(arr))
	for _, el := range arr {
		switch n := el.(type) {
		case float64:
			ids = append(ids, int64(n))
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				return nil, false
			}
			ids = append(ids, i)
		default:
			return nil, false
		}
	}
	return ids, true
}

func formatDetailScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		b, err := json.MarshalIndent(x, "", "  ")
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

func detailJSONInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func (h *Handler) taskDetailFields(detail string) []detailField {
	return h.taskDetailFieldsOpts(detail, false)
}

func (h *Handler) taskDetailFieldsOpts(detail string, hideErrorKey bool) []detailField {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	var raw map[string]any
	dec := json.NewDecoder(strings.NewReader(detail))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil || len(raw) == 0 {
		if hideErrorKey {
			// Plain legacy detail shown as dedicated Error row.
			return nil
		}
		return []detailField{{Key: "", Text: detail}}
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	mediaTypeIDs := idSetFromJSON(raw["ignored_media_type_ids"])
	indexAsIgnoredIDs := idSetFromJSON(raw["ignored_index_as_ignored_ids"])
	out := make([]detailField, 0, len(keys))
	indexedShown := false
	for _, k := range keys {
		v := raw[k]
		switch k {
		case ytdlp.DetailKeyPOToken:
			// Shown as dedicated Details row (PO token).
			continue
		case domains.DetailKeyDomainAccess:
			// Shown as dedicated Details row (Domain access chips).
			continue
		case "error":
			if hideErrorKey {
				continue
			}
		case "created", "updated":
			// Scan counts: show one "Videos indexed" = created+updated (new + touched).
			if indexedShown {
				continue
			}
			indexedShown = true
			n := detailJSONInt(raw["created"]) + detailJSONInt(raw["updated"])
			out = append(out, detailField{Key: "Videos indexed", Text: strconv.Itoa(n)})
			continue
		case "ignored_media_type_ids", "ignored_index_as_ignored_ids":
			if ids, ok := jsonNumberIDs(v); ok {
				out = append(out, detailField{Key: k, Text: strconv.Itoa(len(ids))})
				continue
			}
		case "skipped_title_regexp_include", "skipped_title_regexp_exclude", "skipped_title_regexp":
			if titles, ok := parseSkippedTitleRegexp(v); ok {
				const maxShow = 20
				more := 0
				show := titles
				if len(titles) > maxShow {
					more = len(titles) - maxShow
					show = titles[:maxShow]
				}
				out = append(out, detailField{
					Key:                k,
					IsSkippedTitleList: true,
					SkippedTitles:      show,
					SkippedTitlesMore:  more,
					Text:               strconv.Itoa(len(titles)),
				})
				continue
			}
		case "ignored_title_regexp_ids":
			// Legacy scan detail: count only (title filter no longer creates rows).
			if ids, ok := jsonNumberIDs(v); ok {
				out = append(out, detailField{Key: k, Text: strconv.Itoa(len(ids))})
				continue
			}
		}
		if _, isVideoKey := detailVideoIDKeys[k]; isVideoKey {
			if ids, ok := jsonNumberIDs(v); ok {
				var videos []detailVideoRef
				if k == "created_ids" {
					videos = h.resolveCreatedDetailVideos(ids, mediaTypeIDs, indexAsIgnoredIDs)
				} else {
					videos = h.resolveDetailVideos(ids)
				}
				out = append(out, detailField{
					Key:         k,
					IsVideoList: true,
					Videos:      videos,
				})
				continue
			}
		}
		out = append(out, detailField{Key: k, Text: formatDetailScalar(v)})
	}
	return out
}

// taskFailErrorText returns operator-facing failure detail for the Details Error row.
// Prefer tasks.error_message, then JSON detail.error, then plain non-JSON detail.
func taskFailErrorText(errorMessage, detail string) string {
	if s := strings.TrimSpace(errorMessage); s != "" {
		return s
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err == nil && len(raw) > 0 {
		if v, ok := raw["error"]; ok {
			return strings.TrimSpace(formatDetailScalar(v))
		}
		return ""
	}
	return detail
}

// taskDetailHistRow is one video_history row used to build Detail video lists.
type taskDetailHistRow struct {
	Event      string
	Detail     string
	VideoID    int64
	VideoTitle string
	SeriesID   int64
}

// taskStageView is one daisyUI vertical-timeline node (task Stages + video History).
type taskStageView struct {
	Event      string
	Message    string
	CreatedAt  string
	CreatedAgo string
	Duration   string // how long this stage lasted until the next (omit ≤1s); kept after reverse for display
	HasError   bool
	Neutral    bool // cancelled / operator abort: muted, not success/fail
	IsFirst    bool
	IsLast     bool
	HistoryID  int64              // optional /task/{id} link on Event (0 = none)
	OriginIcon string             // lucide name when set (origin / pending child); muted middle icon
	Substages  []taskStageSubview // nested stages under a grouped video-history node
}

// taskStageSubview is one line inside a grouped timeline box (e.g. downloaded / remuxed / packed).
type taskStageSubview struct {
	Event    string
	Message  string
	HasError bool
	Neutral  bool
}

// taskStagesInput builds Stages for a task detail page.
type taskStagesInput struct {
	Events       []library.VideoHistoryEvent
	Now          time.Time
	Created      string
	Started      string
	Finished     string
	Status       string
	Origin       string
	ParentTaskID int64
	ParentKind   string
	Children     []queue.Task
}

type stageRank int

const (
	stageRankOrigin stageRank = iota
	stageRankEnqueued
	stageRankChild
	stageRankStarted
	stageRankHistory
	stageRankTerminal
)

type stagedItem struct {
	view taskStageView
	at   time.Time
	rank stageRank
}
