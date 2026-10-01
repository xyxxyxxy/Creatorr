package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type importRemoteHit struct {
	VideoID     int64
	SeriesID    int64
	Title       string
	SeriesTitle string
	HasMedia    bool
}

// importSuggestCatalog is loaded once per ScanImport so title/id matching is O(files×catalog)
// in memory instead of re-querying SQLite for every media file.
type importSuggestCatalog struct {
	videos   []VideoSuggestion
	series   []SeriesSuggestion
	byRemote map[string]importRemoteHit
}

func (s *Store) loadImportSuggestCatalog() (*importSuggestCatalog, error) {
	cat := &importSuggestCatalog{byRemote: map[string]importRemoteHit{}}
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, v.title, v.remote_id, s.title,
		  EXISTS(SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'video') AS has_media
		FROM videos v
		JOIN series s ON s.id = v.series_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sug VideoSuggestion
		var hasMedia bool
		if err := rows.Scan(&sug.VideoID, &sug.SeriesID, &sug.Title, &sug.RemoteID, &sug.SeriesTitle, &hasMedia); err != nil {
			return nil, err
		}
		cat.videos = append(cat.videos, sug)
		rid := strings.TrimSpace(sug.RemoteID)
		if rid == "" {
			continue
		}
		hit := importRemoteHit{
			VideoID: sug.VideoID, SeriesID: sug.SeriesID,
			Title: sug.Title, SeriesTitle: sug.SeriesTitle, HasMedia: hasMedia,
		}
		prev, ok := cat.byRemote[rid]
		if !ok || (!hit.HasMedia && prev.HasMedia) || (hit.HasMedia == prev.HasMedia && hit.VideoID < prev.VideoID) {
			cat.byRemote[rid] = hit
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	srows, err := s.DB.SQL.Query(`SELECT id, title FROM series ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = srows.Close() }()
	for srows.Next() {
		var sug SeriesSuggestion
		if err := srows.Scan(&sug.SeriesID, &sug.Title); err != nil {
			return nil, err
		}
		cat.series = append(cat.series, sug)
	}
	return cat, srows.Err()
}

func titleSuggestionsFrom(videos []VideoSuggestion, stem string, limit int) []VideoSuggestion {
	clean := cleanStem(stem)
	type scored struct {
		score float64
		v     VideoSuggestion
	}
	var hits []scored
	for _, sug := range videos {
		ratio := seqRatio(clean, sug.Title)
		if ratio >= 0.45 {
			sug.Score = round3(ratio)
			hits = append(hits, scored{ratio, sug})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]VideoSuggestion, len(hits))
	for i, h := range hits {
		out[i] = h.v
	}
	return out
}

func seriesSuggestionsFrom(series []SeriesSuggestion, mediaPath string, limit int) []SeriesSuggestion {
	needles := []string{
		filepath.Base(filepath.Dir(mediaPath)),
		cleanStem(strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))),
	}
	type scored struct {
		score float64
		s     SeriesSuggestion
	}
	var hits []scored
	for _, sug := range series {
		best := 0.0
		for _, needle := range needles {
			if needle == "" || needle == "." || needle == ".." {
				continue
			}
			if r := seqRatio(needle, sug.Title); r > best {
				best = r
			}
		}
		if best >= 0.5 {
			copySug := sug
			copySug.Score = round3(best)
			hits = append(hits, scored{best, copySug})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]SeriesSuggestion, len(hits))
	for i, h := range hits {
		out[i] = h.s
	}
	return out
}

func extractImportIDs(path string) []ImportIDHint {
	// Priority for ID match: NFO uniqueid, then filename [id], then info.json id.
	var found []ImportIDHint
	nfo := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	if b, err := os.ReadFile(nfo); err == nil {
		text := string(b)
		for _, m := range uniqueIDTyped.FindAllStringSubmatch(text, -1) {
			found = append(found, ImportIDHint{HandlerID: strings.ToLower(m[1]), RemoteID: strings.TrimSpace(m[2])})
		}
		for _, m := range uniqueIDAny.FindAllStringSubmatch(text, -1) {
			found = append(found, ImportIDHint{HandlerID: "unknown", RemoteID: strings.TrimSpace(m[1])})
		}
	}
	for _, m := range bracketID.FindAllStringSubmatch(filepath.Base(path), -1) {
		found = append(found, ImportIDHint{HandlerID: "yt-dlp", RemoteID: strings.TrimSpace(m[1])})
	}
	for _, cand := range []string{
		strings.TrimSuffix(path, filepath.Ext(path)) + ".info.json",
		path + ".info.json",
	} {
		b, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		var data map[string]any
		if json.Unmarshal(b, &data) != nil {
			continue
		}
		id := ""
		switch v := data["id"].(type) {
		case string:
			id = v
		case float64:
			id = fmt.Sprintf("%.0f", v)
		}
		if id == "" {
			continue
		}
		extractor := ""
		if v, ok := data["extractor_key"].(string); ok && v != "" {
			extractor = strings.ToLower(v)
		} else if v, ok := data["extractor"].(string); ok && v != "" {
			extractor = strings.ToLower(v)
		}
		handler := normalizeImportHandler(extractor)
		if extractor == "" {
			// yt-dlp info.json without extractor_key - treat as catch-all id.
			handler = "yt-dlp"
		}
		found = append(found, ImportIDHint{HandlerID: handler, RemoteID: id})
	}
	seen := map[string]bool{}
	var out []ImportIDHint
	for _, h := range found {
		if h.RemoteID == "" {
			continue
		}
		key := h.RemoteID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, h)
	}
	return out
}

func cleanStem(stem string) string {
	clean := stripBracket.ReplaceAllString(stem, "")
	clean = stripSE.ReplaceAllString(clean, "")
	return strings.TrimSpace(clean)
}

func seqRatio(a, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		if a == "" {
			return 0
		}
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	l := lcsLen(ra, rb)
	return 2 * float64(l) / float64(len(ra)+len(rb))
}

func lcsLen(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
		for j := range cur {
			cur[j] = 0
		}
	}
	return prev[len(b)]
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}
