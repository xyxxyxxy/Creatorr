package library

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
)

// EpisodeNFO is Kodi-style episode metadata written beside the media file.
type EpisodeNFO struct {
	SeriesTitle    string
	Title          string
	SortTitle      string
	OriginalTitle  string
	Season         int
	Episode        int
	Plot           string
	Tagline        string
	Studio         string
	Genres         []string
	Tags           []string
	Actors         []SeriesActor
	Country        string
	MPAA           string
	Aired          string // YYYY-MM-DD or RFC3339
	UniqueIDType   string
	UniqueID       string
	SourceSite     string // legacy fallback when UniqueIDType empty
	Domain         string // source hostname for {domain}; empty when unknown
	RuntimeSeconds int    // 0 = omit; Emby/Kodi use this for episode duration
}

// WriteEpisodeNFO writes episodedetails XML at path.
func WriteEpisodeNFO(path string, meta EpisodeNFO) error {
	return os.WriteFile(path, FormatEpisodeNFO(meta), 0o644)
}

// FormatEpisodeNFO returns episodedetails XML bytes.
// sorttitle / originaltitle are omitted when empty or equal to title (no redundant tags).
func FormatEpisodeNFO(meta EpisodeNFO) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString("<episodedetails>\n")
	b.WriteString("  <title>" + xmlEscape(meta.Title) + "</title>\n")
	if st := omitWhenEqualTitle(meta.SortTitle, meta.Title); st != "" {
		b.WriteString("  <sorttitle>" + xmlEscape(st) + "</sorttitle>\n")
	}
	if ot := omitWhenEqualTitle(meta.OriginalTitle, meta.Title); ot != "" {
		b.WriteString("  <originaltitle>" + xmlEscape(ot) + "</originaltitle>\n")
	}
	if meta.SeriesTitle != "" {
		b.WriteString("  <showtitle>" + xmlEscape(meta.SeriesTitle) + "</showtitle>\n")
	}
	b.WriteString(fmt.Sprintf("  <season>%d</season>\n", meta.Season))
	b.WriteString(fmt.Sprintf("  <episode>%d</episode>\n", meta.Episode))
	plot := meta.Plot
	if plot == "" {
		plot = meta.Title
	}
	b.WriteString("  <plot>" + xmlEscape(plot) + "</plot>\n")
	if meta.Tagline != "" {
		b.WriteString("  <tagline>" + xmlEscape(meta.Tagline) + "</tagline>\n")
	}
	if meta.Studio != "" {
		b.WriteString("  <studio>" + xmlEscape(meta.Studio) + "</studio>\n")
	}
	for _, g := range meta.Genres {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		b.WriteString("  <genre>" + xmlEscape(g) + "</genre>\n")
	}
	for _, t := range meta.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		b.WriteString("  <tag>" + xmlEscape(t) + "</tag>\n")
	}
	if meta.Aired != "" {
		day := UploadCalendarDate(meta.Aired)
		if day == "" {
			day = meta.Aired
		}
		b.WriteString("  <aired>" + xmlEscape(day) + "</aired>\n")
	}
	if meta.RuntimeSeconds > 0 {
		b.WriteString(fmt.Sprintf("  <runtime>%d</runtime>\n", (meta.RuntimeSeconds+59)/60))
	}
	if meta.Country != "" {
		b.WriteString("  <country>" + xmlEscape(meta.Country) + "</country>\n")
	}
	if meta.MPAA != "" {
		b.WriteString("  <mpaa>" + xmlEscape(meta.MPAA) + "</mpaa>\n")
	}
	uidType := strings.TrimSpace(meta.UniqueIDType)
	uidVal := strings.TrimSpace(meta.UniqueID)
	if uidType == "" {
		uidType = strings.TrimSpace(meta.SourceSite)
	}
	if uidType == "" {
		uidType = "creatorr"
	}
	if uidVal != "" {
		b.WriteString(fmt.Sprintf(`  <uniqueid type="%s" default="true">%s</uniqueid>`+"\n",
			xmlEscape(uidType), xmlEscape(uidVal)))
	}
	for i, a := range meta.Actors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		order := a.Order
		if order == 0 {
			order = i
		}
		b.WriteString("  <actor>\n")
		b.WriteString("    <name>" + xmlEscape(name) + "</name>\n")
		if role := strings.TrimSpace(a.Role); role != "" {
			b.WriteString("    <role>" + xmlEscape(role) + "</role>\n")
		}
		b.WriteString(fmt.Sprintf("    <order>%d</order>\n", order))
		b.WriteString("  </actor>\n")
	}
	if meta.RuntimeSeconds > 0 {
		b.WriteString(fmt.Sprintf(`  <fileinfo>
    <streamdetails>
      <video>
        <durationinseconds>%d</durationinseconds>
      </video>
    </streamdetails>
  </fileinfo>
`, meta.RuntimeSeconds))
	}
	b.WriteString("</episodedetails>\n")
	return []byte(b.String())
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// omitWhenEqualTitle returns trimmed s, or "" when empty or equal to title.
func omitWhenEqualTitle(s, title string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == strings.TrimSpace(title) {
		return ""
	}
	return s
}

// SidecarBundle is on-disk metadata next to a video (never the video itself).
// InfoJSON is ignored by RefreshDiskSidecars: info.json is download-time provenance only.
type SidecarBundle struct {
	InfoJSON []byte // ignored on refresh; kept for call-site compatibility
	ThumbSrc string // optional path to thumbnail image to copy
	SubSrcs  []string
}

// RefreshDiskSidecars rewrites NFO/thumb/subs beside an existing packed file.
// Leaves the media file and info.json untouched (info.json updates only when media changes).
// No-op (nil) when no video pack anchor on disk.
// Removes prior nfo/thumb/sub files from disk before rewrite (orphan cleanup).
func (s *Store) RefreshDiskSidecars(videoID int64, bundle SidecarBundle, taskID int64) error {
	_ = bundle.InfoJSON // never write info.json on independent sidecar refresh
	mediaPath, ok, err := s.HasPackAnchor(videoID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}

	// Drop previous nfo/thumb/sub only (preserve kind=json / on-disk info.json).
	oldRows, _ := s.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ? AND kind IN ('nfo','thumb','sub')`, videoID)
	if oldRows != nil {
		var oldPaths []string
		for oldRows.Next() {
			var p string
			if oldRows.Scan(&p) == nil && p != "" {
				oldPaths = append(oldPaths, p)
			}
		}
		_ = oldRows.Close()
		for _, p := range oldPaths {
			_ = os.Remove(p)
		}
	}

	nfoPath, err := s.writeEpisodeNFOBeside(v, mediaPath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(mediaPath)
	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	stemBase := filepath.Base(stem)
	thumbPath := ""
	thumbURL := ""
	if v.ThumbnailURL.Valid {
		thumbURL = v.ThumbnailURL.String
	}
	thumbSrc, cleanupThumb := MaterializeThumbSrc(bundle.ThumbSrc, thumbURL)
	defer cleanupThumb()
	if thumbSrc != "" {
		ext := strings.ToLower(filepath.Ext(thumbSrc))
		if ext == "" {
			ext = ".jpg"
		}
		thumbPath = filepath.Join(dir, stemBase+"-thumb"+ext)
		if err := copyFile(thumbSrc, thumbPath); err != nil {
			return fmt.Errorf("copy thumb: %w", err)
		}
	}

	var subPaths []string
	for _, src := range bundle.SubSrcs {
		if !fileExists(src) {
			continue
		}
		suffix := SubtitleLangAndExt(src, guessSubtitleWorkStem(src))
		if suffix == "" {
			continue
		}
		dest := stem + suffix
		if err := copyFile(src, dest); err != nil {
			continue
		}
		subPaths = append(subPaths, dest)
	}

	// Sidecar refresh must remap via frozen applied-cut plan (not live profile).
	if plan, ok, _ := sponsorblock.ReadPlan(mediaPath); ok && plan.HasCuts() {
		sponsorblock.RemapSubtitleFiles(subPaths, plan.Cuts(), plan.CardDurationSec, plan.InfoCards)
	}

	acquired := nowRFC3339()
	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ? AND kind IN ('nfo','thumb','sub')`, videoID); err != nil {
		return err
	}
	insert := func(kind, path string) error {
		if path == "" || !fileExists(path) {
			return nil
		}
		_, err := tx.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, ?, ?)
		`, videoID, path, kind, acquired)
		return err
	}
	if err := insert("nfo", nfoPath); err != nil {
		return err
	}
	if err := insert("thumb", thumbPath); err != nil {
		return err
	}
	for _, p := range subPaths {
		if err := insert("sub", p); err != nil {
			return err
		}
	}
	detail, _ := json.Marshal(map[string]any{
		"nfo": nfoPath, "thumb": thumbPath, "subs": len(subPaths),
	})
	if taskID > 0 {
		if _, err := tx.Exec(`
			INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
			VALUES (?, ?, 'refresh_sidecars', 'Rewrote NFO/thumb/sub sidecars', ?, ?)
		`, videoID, acquired, string(detail), taskID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// guessSubtitleWorkStem returns the yt-dlp output stem for a subtitle path
// (e.g. meta.en.vtt → meta, Show [id].en-US.srt → Show [id],
// meta.en.auto.srt → meta).
func guessSubtitleWorkStem(srcPath string) string {
	base := filepath.Base(srcPath)
	lower := strings.ToLower(base)
	for ext := range subtitleExts {
		if strings.HasSuffix(lower, ext) {
			base = base[:len(base)-len(ext)]
			lower = strings.ToLower(base)
			break
		}
	}
	// Strip optional .auto marker (Creatorr auto-caption naming).
	if strings.HasSuffix(lower, "."+autoSubtitleMarker) {
		base = base[:len(base)-len("."+autoSubtitleMarker)]
		lower = strings.ToLower(base)
	}
	// Strip trailing .lang or .lang-REGION (e.g. .en, .en-US)
	if i := strings.LastIndex(base, "."); i > 0 {
		tag := base[i+1:]
		if looksLikeLangTag(tag) {
			return base[:i]
		}
	}
	return base
}

func looksLikeLangTag(tag string) bool {
	if tag == "" || tag == "info" {
		return false
	}
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '*' {
			continue
		}
		return false
	}
	return true
}

// RewriteVideoNFO rewrites the episode NFO beside an on-disk video from current DB metadata.
// Returns changed=false when there is no pack anchor or on-disk bytes already match.
// When taskID > 0 and bytes change, appends video_history (nfo_regenerated).
func (s *Store) RewriteVideoNFO(videoID, taskID int64) (changed bool, err error) {
	mediaPath, ok, err := s.HasPackAnchor(videoID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return false, err
	}
	meta, nfoPath, err := s.episodeNFOBeside(v, mediaPath)
	if err != nil {
		return false, err
	}
	want := FormatEpisodeNFO(meta)
	if existing, rerr := os.ReadFile(nfoPath); rerr == nil && bytes.Equal(existing, want) {
		return false, nil
	}
	if err := os.WriteFile(nfoPath, want, 0o644); err != nil {
		return false, fmt.Errorf("write nfo: %w", err)
	}
	acquired := nowRFC3339()
	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ? AND kind = 'nfo'`, videoID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'nfo', ?)
	`, videoID, nfoPath, acquired); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if taskID > 0 {
		if err := s.AddVideoHistory(videoID, "nfo_regenerated", "Episode NFO regenerated", map[string]any{
			"path": nfoPath,
		}, taskID); err != nil {
			return true, err
		}
	}
	return true, nil
}

// RegenerateAllNFOs is removed for production - use EnqueueRegenerateNFO.
// Tests should enqueue a task and call NFORegeneratePass.

// writeEpisodeNFOBeside builds EpisodeNFO from the video row and writes it next to mediaPath.
func (s *Store) writeEpisodeNFOBeside(v *Video, mediaPath string) (nfoPath string, err error) {
	meta, nfoPath, err := s.episodeNFOBeside(v, mediaPath)
	if err != nil {
		return "", err
	}
	if err := WriteEpisodeNFO(nfoPath, meta); err != nil {
		return "", fmt.Errorf("write nfo: %w", err)
	}
	return nfoPath, nil
}

func (s *Store) episodeNFOBeside(v *Video, mediaPath string) (EpisodeNFO, string, error) {
	var seriesTitle string
	_ = s.DB.SQL.QueryRow(`SELECT title FROM series WHERE id = ?`, v.SeriesID).Scan(&seriesTitle)

	season, episode := 0, 0
	if v.Season.Valid {
		season = int(v.Season.Int64)
	}
	if v.Episode.Valid {
		episode = int(v.Episode.Int64)
	}
	if season == 0 || episode == 0 {
		upload := ""
		if v.UploadDate.Valid {
			upload = v.UploadDate.String
		}
		s2, e2, aerr := s.AssignSeasonEpisode(v.SeriesID, upload, 0, v.ID)
		if aerr != nil {
			return EpisodeNFO{}, "", aerr
		}
		if season == 0 {
			season = s2
		}
		if episode == 0 {
			episode = e2
		}
	}

	stem := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	nfoPath := stem + ".nfo"
	aired := ""
	if v.UploadDate.Valid {
		aired = v.UploadDate.String
	}
	runtime := 0
	if v.DurationSeconds.Valid && v.DurationSeconds.Int64 > 0 {
		runtime = int(v.DurationSeconds.Int64)
	}
	return episodeMetaFromVideo(v, seriesTitle, season, episode, aired, runtime), nfoPath, nil
}

// EpisodeMetaFromVideo builds EpisodeNFO from a video row (shared by pack / rewrite / stream).
func EpisodeMetaFromVideo(v *Video, seriesTitle string, season, episode int, aired string, runtime int) EpisodeNFO {
	return episodeMetaFromVideo(v, seriesTitle, season, episode, aired, runtime)
}

// episodeMetaFromVideo builds EpisodeNFO from a video row (shared by pack / rewrite / stream).
func episodeMetaFromVideo(v *Video, seriesTitle string, season, episode int, aired string, runtime int) EpisodeNFO {
	if v == nil {
		return EpisodeNFO{}
	}
	uidType := strings.TrimSpace(v.UniqueIDType)
	uidVal := strings.TrimSpace(v.UniqueIDValue)
	sourceSite := "yt-dlp"
	if uidType != "" {
		sourceSite = uidType
	}
	if uidVal == "" {
		uidVal = v.RemoteID
	}
	domain := ""
	if v.SourceURL.Valid {
		domain = namingDomain(v.SourceURL.String)
	}
	return EpisodeNFO{
		SeriesTitle:    seriesTitle,
		Title:          v.Title,
		SortTitle:      v.SortTitle,
		OriginalTitle:  v.OriginalTitle,
		Season:         season,
		Episode:        episode,
		Plot:           v.Description,
		Tagline:        v.Tagline,
		Studio:         v.Studio,
		Genres:         v.Genres,
		Tags:           v.Tags,
		Actors:         v.Actors,
		Country:        v.Country,
		MPAA:           v.MPAA,
		Aired:          aired,
		UniqueIDType:   uidType,
		UniqueID:       uidVal,
		SourceSite:     sourceSite,
		Domain:         domain,
		RuntimeSeconds: runtime,
	}
}

// MaterializeThumbSrc returns an on-disk thumbnail path for packing/refresh.
// Prefer an existing thumbSrc file; otherwise soft-download thumbnailURL to a temp .jpg.
// Caller must invoke cleanup (no-op when nothing was downloaded). Soft-ok on HTTP failure.
func MaterializeThumbSrc(thumbSrc, thumbnailURL string) (path string, cleanup func()) {
	cleanup = func() {}
	if thumbSrc != "" && fileExists(thumbSrc) {
		return thumbSrc, cleanup
	}
	url := strings.TrimSpace(thumbnailURL)
	if url == "" {
		return "", cleanup
	}
	tmp, err := os.CreateTemp("", "creatorr-thumb-*.jpg")
	if err != nil {
		return "", cleanup
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	if err := downloadURLToFile(url, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", cleanup
	}
	return tmpPath, func() { _ = os.Remove(tmpPath) }
}

func downloadURLToFile(rawURL, dest string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("thumb http %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
