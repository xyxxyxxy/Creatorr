package web

import (
	"encoding/json"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// breadcrumb is one daisyUI breadcrumbs <li> (empty Href = current page).
type breadcrumb struct {
	Href  string
	Label string
	Icon  string
}

func crumb(href, label, icon string) breadcrumb {
	return breadcrumb{Href: href, Label: label, Icon: icon}
}

// historySourceID picks a source for breadcrumbs from task detail/payload or video.
func historySourceID(t *queue.Task, video *library.Video) int64 {
	if t == nil {
		return 0
	}
	if t.Detail != "" {
		var d struct {
			SourceID int64 `json:"source_id"`
		}
		if json.Unmarshal([]byte(t.Detail), &d) == nil && d.SourceID > 0 {
			return d.SourceID
		}
	}
	if sid := queue.SourceIDFromPayload(t.Payload); sid > 0 {
		return sid
	}
	if video != nil && video.SourceID.Valid && video.SourceID.Int64 > 0 {
		return video.SourceID.Int64
	}
	return 0
}

func sourceBreadcrumbLabel(src *library.Source) string {
	if src == nil {
		return "Source"
	}
	if src.Label.Valid && src.Label.String != "" {
		return src.Label.String
	}
	u := DisplayURL(src.URL)
	if len(u) > 48 {
		return u[:45] + "…"
	}
	if u != "" {
		return u
	}
	return fmt.Sprintf("Source #%d", src.ID)
}

// historyBreadcrumbs builds Series→Video|Source→History or History→item.
func historyBreadcrumbs(series *seriesLink, source *sourceLink, video *videoLink, kind string) []breadcrumb {
	return taskBreadcrumbs(series, source, video, kind, false)
}

// taskBreadcrumbs builds crumbs for /task/{id}. Live tasks use Tasks as list parent when no series path.
func taskBreadcrumbs(series *seriesLink, source *sourceLink, video *videoLink, kind string, live bool) []breadcrumb {
	cur := crumb("", kind, "scroll-text")
	if series == nil {
		if live {
			return []breadcrumb{
				crumb("/tasks", "Tasks", "list-todo"),
				cur,
			}
		}
		return []breadcrumb{
			crumb("/history", "History", "history"),
			cur,
		}
	}
	out := []breadcrumb{
		crumb("/series", "Series", "tv"),
		crumb(fmt.Sprintf("/series/%d", series.ID), series.Title, "clapperboard"),
	}
	// Prefer video path when both exist (series > video > task).
	if video != nil {
		sid := video.SeriesID
		if sid == 0 {
			sid = series.ID
		}
		out = append(out, crumb(fmt.Sprintf("/series/%d/videos/%d", sid, video.ID), video.Title, "film"))
	} else if source != nil {
		out = append(out, crumb(fmt.Sprintf("/series/%d/sources/%d", series.ID, source.ID), source.Title, "rss"))
	}
	return append(out, cur)
}

// seriesLink / sourceLink / videoLink are local to history detail; declared here for crumbs.
type seriesLink struct {
	ID    int64
	Title string
}

type sourceLink struct {
	ID    int64
	Title string
}

type videoLink struct {
	ID       int64
	SeriesID int64
	Title    string
}
