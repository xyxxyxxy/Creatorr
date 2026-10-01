package library

import (
	"encoding/json"
	"os"
	"strings"
)

// SoftFillVideoTagsFromInfo merges yt-dlp tags from info.json into videos.tags (always on).
func (s *Store) SoftFillVideoTagsFromInfoJSON(videoID int64, infoPath string) (bool, error) {
	return s.EnsureVideoTagsFromInfo(videoID, TagsFromInfoJSON(infoPath))
}

// EnsureVideoTagsFromInfo unions tags into videos.tags.
func (s *Store) EnsureVideoTagsFromInfo(videoID int64, tags []string) (bool, error) {
	if videoID <= 0 {
		return false, nil
	}
	incoming := ParseStringListFields(tags)
	if len(incoming) == 0 {
		return false, nil
	}
	var raw string
	err := s.DB.SQL.QueryRow(`SELECT COALESCE(tags, '[]') FROM videos WHERE id = ?`, videoID).Scan(&raw)
	if err != nil {
		return false, err
	}
	merged := MergeCategoryGenres(decodeStringSlice(raw), incoming)
	encoded := encodeStringSlice(merged)
	if encoded == raw {
		return false, nil
	}
	res, err := s.DB.SQL.Exec(`UPDATE videos SET tags = ? WHERE id = ?`, encoded, videoID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// TagsFromInfoJSON reads yt-dlp tags from a packed info.json (best-effort).
func TagsFromInfoJSON(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" || !fileExists(path) {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var data map[string]any
	if json.Unmarshal(b, &data) != nil {
		return nil
	}
	raw, ok := data["tags"]
	if !ok || raw == nil {
		return nil
	}
	var out []string
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range v {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
