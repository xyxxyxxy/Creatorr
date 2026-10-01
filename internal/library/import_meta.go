package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type importMeta struct {
	Title       string
	RemoteID    string
	HandlerID   string
	WebpageURL  string
	UploadDate  string
	Description string
}

func readImportMeta(path string, hints []ImportIDHint) importMeta {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	m := importMeta{
		Title: cleanStem(stem),
	}
	if m.Title == "" {
		m.Title = stem
	}
	if len(hints) > 0 {
		m.RemoteID = hints[0].RemoteID
		if hints[0].HandlerID != "" && hints[0].HandlerID != "unknown" {
			m.HandlerID = hints[0].HandlerID
		}
	}
	// info.json: provenance URL / handler / interim title-desc-date (NFO overrides editable below).
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
		if t, ok := data["title"].(string); ok && strings.TrimSpace(t) != "" {
			m.Title = strings.TrimSpace(t)
		}
		if d, ok := data["description"].(string); ok {
			m.Description = d
		}
		if u, ok := data["webpage_url"].(string); ok && u != "" {
			m.WebpageURL = u
		} else if u, ok := data["original_url"].(string); ok && u != "" {
			m.WebpageURL = u
		}
		switch v := data["upload_date"].(type) {
		case string:
			m.UploadDate = sidecarUploadTime(v)
		}
		if id, ok := data["id"].(string); ok && id != "" && m.RemoteID == "" {
			m.RemoteID = id
		}
		if ek, ok := data["extractor_key"].(string); ok && ek != "" {
			m.HandlerID = normalizeImportHandler(ek)
		} else if ek, ok := data["extractor"].(string); ok && ek != "" {
			m.HandlerID = normalizeImportHandler(ek)
		}
		break
	}
	// Episode NFO is operator-editable catalog: non-empty title / plot / aired win over info.json.
	nfo := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	if p, aired, _, err := ParseEpisodeNFOFile(nfo); err == nil {
		if t := strings.TrimSpace(p.Title); t != "" {
			m.Title = t
		}
		if plot := strings.TrimSpace(p.Plot); plot != "" {
			m.Description = plot
		}
		if t := sidecarUploadTime(aired); t != "" {
			m.UploadDate = t
		}
	}
	return m
}

func normalizeImportHandler(extractor string) string {
	e := strings.ToLower(strings.TrimSpace(extractor))
	switch e {
	case "youtube", "youtu", "ytdlp", "yt-dlp":
		return "yt-dlp"
	default:
		return e
	}
}

// sidecarUploadTime adapts yt-dlp info.json / NFO date fields to RFC3339 UTC for storage.
// Handler protocol is RFC3339-only; this is only for on-disk sidecar formats.
func sidecarUploadTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, ok := ParseUploadTime(s); ok {
		return t.UTC().Format(time.RFC3339)
	}
	// Form / datetime-local style (UTC, no zone suffix).
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	compact := strings.ReplaceAll(s, "-", "")
	if len(compact) >= 8 {
		if t, err := time.ParseInLocation("20060102", compact[:8], time.UTC); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ""
}

// uploadFormHasTime reports whether a Metadata upload_date form value includes a clock time.
// Date-only YYYY-MM-DD / YYYYMMDD → false; datetime-local / RFC3339 → true.
func uploadFormHasTime(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if len(raw) == 10 && raw[4] == '-' && raw[7] == '-' {
		return false
	}
	compact := strings.ReplaceAll(raw, "-", "")
	if len(compact) == 8 && !strings.ContainsAny(raw, "T :") {
		return false
	}
	return true
}

// UploadFormParts splits a stored upload_date for the Metadata date+time join (UTC).
// Midnight → day only with empty clock (time optional). Non-midnight → HH:MM.
func UploadFormParts(raw string) (day, clock string) {
	t, ok := ParseUploadTime(raw)
	if !ok {
		return "", ""
	}
	t = t.UTC()
	day = t.Format("2006-01-02")
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return day, ""
	}
	return day, t.Format("15:04")
}

// CombineUploadFormDateTime joins Metadata date + optional time fields into a value
// for SaveVideoMetadata (YYYY-MM-DD or YYYY-MM-DDTHH:MM). Empty day clears.
func CombineUploadFormDateTime(day, clock string) string {
	day = strings.TrimSpace(day)
	clock = strings.TrimSpace(clock)
	if day == "" {
		return ""
	}
	if clock == "" {
		return day
	}
	return day + "T" + clock
}

// UploadFormValue formats a stored upload_date for display/tests (UTC).
// Midnight → YYYY-MM-DD; otherwise YYYY-MM-DDTHH:MM.
func UploadFormValue(raw string) string {
	day, clock := UploadFormParts(raw)
	return CombineUploadFormDateTime(day, clock)
}

// fileModTimeUploadDate returns the file's modification time as RFC3339 UTC (creation
// time is not portable across filesystems; mtime is the fallback when sidecars omit date).
func fileModTimeUploadDate(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return st.ModTime().UTC().Format(time.RFC3339)
}

// applyDetectedPackRole sets special_feature from series-relative path when a Specials/extras layout is detected.
func (s *Store) applyDetectedPackRole(v *Video, mediaPath string) error {
	if v == nil {
		return nil
	}
	ser, err := s.GetSeries(v.SeriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	role := DetectPackRoleFromPath(SeriesDir(root.Path, ser.Title), mediaPath)
	if role == PackRoleRegular {
		return nil
	}
	if NormalizePackRole(v.PackRole) == role {
		return nil
	}
	return s.SetVideoPackRole(v.ID, role)
}

// suggestPackRoleForPath returns DetectPackRoleFromPath under the series folder when seriesID is known.
func (s *Store) suggestPackRoleForPath(mediaPath string, seriesID int64) string {
	if seriesID <= 0 {
		return PackRoleRegular
	}
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return PackRoleRegular
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return PackRoleRegular
	}
	return DetectPackRoleFromPath(SeriesDir(root.Path, ser.Title), mediaPath)
}
