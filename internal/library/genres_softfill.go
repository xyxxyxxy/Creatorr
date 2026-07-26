package library

import (
	"encoding/json"
	"os"
	"strings"
)

// SoftFillVideoGenresFromCategories sets videos.genres from categories when genres are empty.
// No-op when videoID invalid, genres already non-empty, or categories empty after normalize.
// Returns true when the row was updated.
func (s *Store) SoftFillVideoGenresFromCategories(videoID int64, categories []string) (bool, error) {
	if videoID <= 0 {
		return false, nil
	}
	genres := ParseStringListFields(categories)
	if len(genres) == 0 {
		return false, nil
	}
	var raw string
	err := s.DB.SQL.QueryRow(`SELECT COALESCE(genres, '[]') FROM videos WHERE id = ?`, videoID).Scan(&raw)
	if err != nil {
		return false, err
	}
	if len(decodeStringSlice(raw)) > 0 {
		return false, nil
	}
	res, err := s.DB.SQL.Exec(`UPDATE videos SET genres = ? WHERE id = ?`, encodeStringSlice(genres), videoID)
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

// SoftFillVideoGenresFromInfoJSON soft-fills genres from a packed info.json path.
func (s *Store) SoftFillVideoGenresFromInfoJSON(videoID int64, infoPath string) (bool, error) {
	return s.SoftFillVideoGenresFromCategories(videoID, CategoriesFromInfoJSON(infoPath))
}

// SoftFillVideoGenresFromPackedInfo soft-fills genres from the registered kind=json file when present.
func (s *Store) SoftFillVideoGenresFromPackedInfo(videoID int64) (bool, error) {
	var path string
	err := s.DB.SQL.QueryRow(`
		SELECT path FROM files WHERE video_id = ? AND kind = 'json' LIMIT 1
	`, videoID).Scan(&path)
	if err != nil || strings.TrimSpace(path) == "" {
		return false, nil
	}
	return s.SoftFillVideoGenresFromInfoJSON(videoID, path)
}

// softFillGenresOntoVideo soft-fills empty genres from packed info.json onto v in-memory.
func (s *Store) softFillGenresOntoVideo(v *Video) {
	if v == nil || len(v.Genres) > 0 {
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
