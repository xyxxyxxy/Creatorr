package web

import (
	"net/http"
	"strings"
)

const (
	cookieColsSeries       = "creatorr_cols_series"
	cookieColsSeriesVideos = "creatorr_cols_series_videos"
	cookieColsVideos       = "creatorr_cols_videos"
)

// tableCol is one configurable list-table column.
type tableCol struct {
	Key     string
	Label   string
	Visible bool
}

type tableColDef struct {
	Key     string
	Label   string
	Default bool
}

func seriesTableColDefs() []tableColDef {
	return []tableColDef{
		{Key: "title", Label: "Title", Default: true},
		{Key: "monitored", Label: "Monitored", Default: true},
		{Key: "progress", Label: "Progress", Default: true},
		{Key: "status", Label: "Status", Default: true},
		{Key: "root", Label: "Root", Default: false},
		{Key: "quality", Label: "Quality", Default: false},
		{Key: "downloaded", Label: "Downloaded", Default: false},
		{Key: "wanted", Label: "Wanted", Default: false},
		{Key: "errors", Label: "Errors", Default: false},
		{Key: "delivery", Label: "Delivery", Default: false},
	}
}

func videoTableColDefs(showSeries bool) []tableColDef {
	defs := []tableColDef{
		{Key: "title", Label: "Title", Default: true},
	}
	if showSeries {
		defs = append(defs, tableColDef{Key: "series", Label: "Series", Default: true})
	}
	defs = append(defs,
		tableColDef{Key: "status", Label: "Status", Default: true},
		tableColDef{Key: "upload", Label: "Upload", Default: true},
		tableColDef{Key: "duration", Label: "Duration", Default: true},
		tableColDef{Key: "resolution", Label: "Resolution", Default: true},
		tableColDef{Key: "size", Label: "Size", Default: false},
		tableColDef{Key: "media", Label: "Media", Default: false},
		tableColDef{Key: "kind", Label: "Special kind", Default: false},
	)
	return defs
}

func defaultTableColKeys(defs []tableColDef) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		if d.Default {
			out = append(out, d.Key)
		}
	}
	if len(out) == 0 && len(defs) > 0 {
		out = append(out, defs[0].Key)
	}
	return out
}

func parseTableColsCookie(r *http.Request, cookieName string, defs []tableColDef) []tableCol {
	allowed := map[string]tableColDef{}
	for _, d := range defs {
		allowed[d.Key] = d
	}
	var keys []string
	if c, err := r.Cookie(cookieName); err == nil {
		for _, part := range strings.Split(c.Value, ",") {
			k := strings.ToLower(strings.TrimSpace(part))
			if k == "" {
				continue
			}
			if _, ok := allowed[k]; ok {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		keys = defaultTableColKeys(defs)
	}
	seen := map[string]struct{}{}
	visible := map[string]bool{}
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		visible[k] = true
	}
	out := make([]tableCol, 0, len(defs))
	for _, d := range defs {
		out = append(out, tableCol{
			Key:     d.Key,
			Label:   d.Label,
			Visible: visible[d.Key],
		})
	}
	// At least one column must stay visible.
	any := false
	for _, c := range out {
		if c.Visible {
			any = true
			break
		}
	}
	if !any && len(out) > 0 {
		out[0].Visible = true
	}
	return out
}

func tableColVisible(cols []tableCol, key string) bool {
	for _, c := range cols {
		if c.Key == key {
			return c.Visible
		}
	}
	return false
}
