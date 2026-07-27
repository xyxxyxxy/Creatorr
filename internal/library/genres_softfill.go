package library

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// MergeCategoryGenres ensures category genres are present and listed first.
func MergeCategoryGenres(genres, categories []string) []string {
	managed := ParseStringListFields(categories)
	if len(managed) == 0 {
		return ParseStringListFields(genres)
	}
	managedFold := make(map[string]string, len(managed))
	for _, m := range managed {
		managedFold[strings.ToLower(m)] = m
	}
	var tail []string
	for _, g := range ParseStringListFields(genres) {
		if _, ok := managedFold[strings.ToLower(g)]; ok {
			continue
		}
		tail = append(tail, g)
	}
	return append(managed, tail...)
}

// SoftFillVideoGenresFromCategories merges yt-dlp categories into videos.genres when the setting is on.
// Returns true when the row was updated.
func (s *Store) SoftFillVideoGenresFromCategories(videoID int64, categories []string) (bool, error) {
	return s.EnsureVideoGenresFromCategories(videoID, categories)
}

// EnsureVideoGenresFromCategories merges category genres into videos.genres when the setting is on.
func (s *Store) EnsureVideoGenresFromCategories(videoID int64, categories []string) (bool, error) {
	if videoID <= 0 {
		return false, nil
	}
	enabled, err := settings.MetadataGenresFromCategoriesEnabled(s.DB)
	if err != nil || !enabled {
		return false, err
	}
	genres := ParseStringListFields(categories)
	if len(genres) == 0 {
		return false, nil
	}
	var raw string
	err = s.DB.SQL.QueryRow(`SELECT COALESCE(genres, '[]') FROM videos WHERE id = ?`, videoID).Scan(&raw)
	if err != nil {
		return false, err
	}
	merged := MergeCategoryGenres(decodeStringSlice(raw), categories)
	encoded := encodeStringSlice(merged)
	if encoded == raw {
		return false, nil
	}
	res, err := s.DB.SQL.Exec(`UPDATE videos SET genres = ? WHERE id = ?`, encoded, videoID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CategoriesFromInfoJSON reads yt-dlp categories from a packed info.json (best-effort).
func CategoriesFromInfoJSON(path string) []string {
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
	raw, ok := data["categories"]
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

// SoftFillVideoGenresFromInfoJSON merges genres from a packed info.json path.
func (s *Store) SoftFillVideoGenresFromInfoJSON(videoID int64, infoPath string) (bool, error) {
	return s.EnsureVideoGenresFromCategories(videoID, CategoriesFromInfoJSON(infoPath))
}

// CategoriesForPackedVideo returns categories from the registered info.json sidecar when present.
func (s *Store) CategoriesForPackedVideo(videoID int64) []string {
	var path string
	err := s.DB.SQL.QueryRow(`
		SELECT path FROM files WHERE video_id = ? AND kind = 'json' LIMIT 1
	`, videoID).Scan(&path)
	if err != nil || strings.TrimSpace(path) == "" {
		return nil
	}
	return CategoriesFromInfoJSON(path)
}

// SoftFillVideoGenresFromPackedInfo merges genres from the registered kind=json file when present.
func (s *Store) SoftFillVideoGenresFromPackedInfo(videoID int64) (bool, error) {
	return s.EnsureVideoGenresFromCategories(videoID, s.CategoriesForPackedVideo(videoID))
}

// softFillGenresOntoVideo merges category genres from packed info.json onto v in-memory when enabled.
func (s *Store) softFillGenresOntoVideo(v *Video) {
	if v == nil {
		return
	}
	ok, err := s.SoftFillVideoGenresFromPackedInfo(v.ID)
	if err != nil || !ok {
		return
	}
	fresh, err := s.GetVideo(v.ID)
	if err != nil {
		return
	}
	v.Genres = fresh.Genres
}
