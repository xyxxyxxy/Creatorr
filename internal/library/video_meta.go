package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
)

// MediaCompleteMeta is Creatorr-owned state written on download/import complete.
type MediaCompleteMeta struct {
	Tool                   string // yt-dlp | import
	AcquiredVia            string // source | archive | import
	DownloadFormatSelector string // archive download only
	DownloadRemuxContainer string // "mkv" only when remux ran; empty when skipped
	ImportSrc              string // original path at import
	InPlace                bool   // transient: history message only (not a column)
	DurationSeconds        int    // optional; 0 → try info.json
	Width                  int
	Height                 int
	FPS                    float64
}

// InfoJSONMediaMeta is soft-filled from packed info.json (read-only; never edit the file).
type InfoJSONMediaMeta struct {
	DurationSeconds int
	Width           int
	Height          int
	FPS             float64
	MediaType       string
	Description     string
	Title           string
	ThumbnailURL    string
	UploadDate      string
}

// MediaMetaFromInfoJSON reads duration/resolution/fps/media_type from a packed info.json.
func MediaMetaFromInfoJSON(path string) InfoJSONMediaMeta {
	var out InfoJSONMediaMeta
	if path == "" || !fileExists(path) {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var data map[string]any
	if json.Unmarshal(b, &data) != nil {
		return out
	}
	out.DurationSeconds = positiveIntFromAny(data["duration"])
	out.Width = positiveIntFromAny(data["width"])
	out.Height = positiveIntFromAny(data["height"])
	out.FPS = positiveFloatFromAny(data["fps"])
	if s, ok := data["media_type"].(string); ok {
		out.MediaType = NormalizeMediaType(s)
	}
	if s, ok := data["description"].(string); ok {
		if len(s) > 4000 {
			s = s[:4000]
		}
		out.Description = s
	}
	if s, ok := data["title"].(string); ok {
		out.Title = strings.TrimSpace(s)
	}
	if s, ok := data["thumbnail"].(string); ok {
		out.ThumbnailURL = strings.TrimSpace(s)
	}
	out.UploadDate = UploadDateFromInfoJSON(path)
	return out
}

func positiveIntFromAny(v any) int {
	switch x := v.(type) {
	case float64:
		if x > 0 {
			return int(x + 0.5)
		}
	case json.Number:
		f, err := x.Float64()
		if err == nil && f > 0 {
			return int(f + 0.5)
		}
	case int:
		if x > 0 {
			return x
		}
	case int64:
		if x > 0 {
			return int(x)
		}
	}
	return 0
}

func positiveFloatFromAny(v any) float64 {
	switch x := v.(type) {
	case float64:
		if x > 0 {
			return x
		}
	case json.Number:
		f, err := x.Float64()
		if err == nil && f > 0 {
			return f
		}
	}
	return 0
}

// SetDurationSeconds sets duration_seconds when sec > 0.
func (s *Store) SetDurationSeconds(videoID int64, sec int) error {
	if sec <= 0 {
		return nil
	}
	_, err := s.DB.SQL.Exec(`UPDATE videos SET duration_seconds = ? WHERE id = ?`, sec, videoID)
	return err
}

// SetMediaType sets videos.media_type when non-empty.
func (s *Store) SetMediaType(videoID int64, mediaType string) error {
	mediaType = NormalizeMediaType(mediaType)
	if mediaType == "" {
		return nil
	}
	_, err := s.DB.SQL.Exec(`UPDATE videos SET media_type = ? WHERE id = ?`, mediaType, videoID)
	return err
}

// SetDurationSecondsIfEmpty fills duration_seconds only when currently NULL/0.
func (s *Store) SetDurationSecondsIfEmpty(videoID int64, sec int) error {
	if sec <= 0 {
		return nil
	}
	_, err := s.DB.SQL.Exec(`
		UPDATE videos SET duration_seconds = ?
		WHERE id = ? AND (duration_seconds IS NULL OR duration_seconds <= 0)
	`, sec, videoID)
	return err
}

// SoftFillDurationFromMedia probes media with ffprobe and soft-fills duration_seconds
// when the column is still NULL/0. Probe errors are ignored (import must not fail).
func (s *Store) SoftFillDurationFromMedia(ctx context.Context, videoID int64, mediaPath string) error {
	mediaPath = strings.TrimSpace(mediaPath)
	if videoID < 1 || mediaPath == "" || !fileExists(mediaPath) {
		return nil
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	if v.DurationSeconds.Valid && v.DurationSeconds.Int64 > 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p, err := sponsorblock.ProbeMedia(ctx, mediaPath)
	if err != nil || p.Duration <= 0 {
		return nil
	}
	return s.SetDurationSecondsIfEmpty(videoID, int(p.Duration+0.5))
}

// FillMediaColumnsFromInfoJSON soft-fills NULL duration/width/height/fps and empty media_type from info.json.
func (s *Store) FillMediaColumnsFromInfoJSON(videoID int64, infoPath string) error {
	m := MediaMetaFromInfoJSON(infoPath)
	if m.DurationSeconds <= 0 && m.Width <= 0 && m.Height <= 0 && m.FPS <= 0 && m.MediaType == "" {
		return nil
	}
	_, err := s.DB.SQL.Exec(`
		UPDATE videos SET
		  duration_seconds = CASE WHEN (duration_seconds IS NULL OR duration_seconds <= 0) AND ? > 0 THEN ? ELSE duration_seconds END,
		  width = CASE WHEN (width IS NULL OR width <= 0) AND ? > 0 THEN ? ELSE width END,
		  height = CASE WHEN (height IS NULL OR height <= 0) AND ? > 0 THEN ? ELSE height END,
		  fps = CASE WHEN (fps IS NULL OR fps <= 0) AND ? > 0 THEN ? ELSE fps END,
		  media_type = CASE WHEN (media_type IS NULL OR media_type = '') AND ? != '' THEN ? ELSE media_type END
		WHERE id = ?
	`, m.DurationSeconds, m.DurationSeconds, m.Width, m.Width, m.Height, m.Height, m.FPS, m.FPS, m.MediaType, m.MediaType, videoID)
	return err
}

// ResolveDurationSeconds returns duration from the column, else packed info.json.
// When found only in info.json, backfills duration_seconds.
func (s *Store) ResolveDurationSeconds(videoID int64, durationCol sql.NullInt64, infoJSONPath string) int {
	if durationCol.Valid && durationCol.Int64 > 0 {
		return int(durationCol.Int64)
	}
	sec := DurationSecondsFromInfoJSON(infoJSONPath)
	if sec <= 0 || videoID < 1 {
		return sec
	}
	_ = s.SetDurationSecondsIfEmpty(videoID, sec)
	return sec
}

// ResolveResolutionLabel returns the resolution bucket from columns, else packed info.json.
// Soft-fills empty media columns (dims + media_type) from info.json when present.
func (s *Store) ResolveResolutionLabel(videoID int64, width, height sql.NullInt64, infoJSONPath string) string {
	if infoJSONPath != "" && videoID >= 1 {
		_ = s.FillMediaColumnsFromInfoJSON(videoID, infoJSONPath)
	}
	if label := ResolutionLabelFromCols(width, height); label != "" {
		return label
	}
	if infoJSONPath == "" || videoID < 1 {
		return ""
	}
	m := MediaMetaFromInfoJSON(infoJSONPath)
	if m.Width <= 0 || m.Height <= 0 {
		return ""
	}
	return ResolutionLabel(m.Width, m.Height)
}

// ResolutionLabelFromCols returns the bucket when both dimensions are positive.
func ResolutionLabelFromCols(width, height sql.NullInt64) string {
	if !width.Valid || !height.Valid {
		return ""
	}
	return ResolutionLabel(int(width.Int64), int(height.Int64))
}

// ImportInPlace reports whether import_src sits under a library root (bound in place).
func (s *Store) ImportInPlace(importSrc string) bool {
	src := strings.TrimSpace(importSrc)
	if src == "" {
		return false
	}
	roots, err := s.ListRoots()
	if err != nil {
		return false
	}
	for _, r := range roots {
		root := strings.TrimSpace(r.Path)
		if root == "" {
			continue
		}
		if pathUnderRoot(src, root) {
			return true
		}
	}
	return false
}

func pathUnderRoot(path, root string) bool {
	path = strings.TrimRight(filepath.Clean(path), string(filepath.Separator))
	root = strings.TrimRight(filepath.Clean(root), string(filepath.Separator))
	if root == "" || path == "" {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// ResolutionLabel returns a rough bucket from pixel size: 240p, 360p, 480p,
// 720p, 1080p, or 4K. Uses the short side (min of width/height) so landscape and
// portrait share the same label. 1440-class short side maps into 1080p.
// Empty when width or height is unknown/non-positive.
func ResolutionLabel(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	p := width
	if height < width {
		p = height
	}
	switch {
	case p < 300:
		return "240p"
	case p < 420:
		return "360p"
	case p < 600:
		return "480p"
	case p < 900:
		return "720p"
	case p < 1800:
		return "1080p"
	default:
		return "4K"
	}
}

// ResolutionLabel returns the rough resolution bucket from stored width/height columns.
func (v Video) ResolutionLabel() string {
	return ResolutionLabelFromCols(v.Width, v.Height)
}
