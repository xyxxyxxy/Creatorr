package library

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// MediaCompleteMeta is Creatorr-owned state written on download/import complete.
type MediaCompleteMeta struct {
	Tool                   string  // yt-dlp | import
	DownloadFormatSelector string  // archive download only
	DownloadRemuxContainer string  // "mkv" only when remux ran; empty when skipped
	ImportSrc              string  // original path at import
	InPlace                bool    // transient: history message only (not a column)
	DurationSeconds        int     // optional; 0 → try info.json
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

// SetStreamURLsKind stores progressive|pipe|hls (empty clears).
func (s *Store) SetStreamURLsKind(videoID int64, kind string) error {
	kind = strings.TrimSpace(strings.ToLower(kind))
	var v any
	if kind != "" {
		v = kind
	}
	_, err := s.DB.SQL.Exec(`UPDATE videos SET stream_urls_kind = ? WHERE id = ?`, v, videoID)
	return err
}

// SetStreamBeginningCached sets the stream beginning cache flag.
func (s *Store) SetStreamBeginningCached(videoID int64, cached bool) error {
	flag := 0
	if cached {
		flag = 1
	}
	_, err := s.DB.SQL.Exec(`UPDATE videos SET stream_beginning_cached = ? WHERE id = ?`, flag, videoID)
	return err
}

// SetStreamMeta updates stream_urls_kind and optional duration/resolution from urls/resolve.
func (s *Store) SetStreamMeta(videoID int64, kind string, durationSec, width, height int, fps float64) error {
	kind = strings.TrimSpace(strings.ToLower(kind))
	var kindVal any
	if kind != "" {
		kindVal = kind
	}
	_, err := s.DB.SQL.Exec(`
		UPDATE videos SET
		  stream_urls_kind = COALESCE(?, stream_urls_kind),
		  duration_seconds = CASE WHEN ? > 0 THEN ? ELSE duration_seconds END,
		  width = CASE WHEN ? > 0 THEN ? ELSE width END,
		  height = CASE WHEN ? > 0 THEN ? ELSE height END,
		  fps = CASE WHEN ? > 0 THEN ? ELSE fps END
		WHERE id = ?
	`, kindVal, durationSec, durationSec, width, width, height, height, fps, fps, videoID)
	return err
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

// StreamURLsKind returns last known urls kind, or "".
func (v Video) StreamKind() string {
	if !v.StreamURLsKind.Valid {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(v.StreamURLsKind.String))
}

// StreamNeedsBeginning reports whether cache_beginning applies (pipe mux only).
func StreamNeedsBeginning(kind string) bool {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case ytdlp.UrlsKindHLS, ytdlp.UrlsKindProgressive:
		return false
	default:
		return true
	}
}

// StreamCDNDirect reports CDN HLS/progressive - no beginning cache needed.
func StreamCDNDirect(kind string) bool {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case ytdlp.UrlsKindHLS, ytdlp.UrlsKindProgressive:
		return true
	default:
		return false
	}
}

// StreamTypeLabel is a short UI label for stream_urls_kind (empty when unknown).
func StreamTypeLabel(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case ytdlp.UrlsKindPipe:
		return "piped"
	case ytdlp.UrlsKindHLS:
		return "CDN HLS"
	case ytdlp.UrlsKindProgressive:
		return "CDN progressive"
	default:
		return ""
	}
}

// StreamTypeListLabel is the video-list meta label; pipe + beginning → "piped - beginning cached".
func StreamTypeListLabel(kind string, beginningCached bool) string {
	label := StreamTypeLabel(kind)
	if label == "piped" && beginningCached {
		return "piped - beginning cached"
	}
	return label
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

