package web

import (
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

type videoDetailRow struct {
	Label  string
	Value  string
	IsURL  bool
	IsPath bool
}

// videoDetailRows builds labeled rows from dedicated video columns (+ derived import mode).
func videoDetailRows(store *library.Store, v *library.Video) []videoDetailRow {
	if v == nil {
		return nil
	}
	var out []videoDetailRow
	add := func(label, value string, isURL, isPath bool) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		out = append(out, videoDetailRow{Label: label, Value: value, IsURL: isURL, IsPath: isPath})
	}

	if v.DurationSeconds.Valid && v.DurationSeconds.Int64 > 0 {
		add("Duration", formatDetailDuration(float64(v.DurationSeconds.Int64)), false, false)
	}
	if v.Width.Valid && v.Height.Valid && v.Width.Int64 > 0 && v.Height.Int64 > 0 {
		add("Resolution", fmt.Sprintf("%dx%d", v.Width.Int64, v.Height.Int64), false, false)
	}
	if v.FPS.Valid && v.FPS.Float64 > 0 {
		add("FPS", fmt.Sprintf("%g", v.FPS.Float64), false, false)
	}
	if v.DownloadFormatSelector.Valid {
		add("Download format", v.DownloadFormatSelector.String, false, false)
	}
	if v.DownloadRemuxContainer.Valid {
		add("Remux", v.DownloadRemuxContainer.String, false, false)
	}
	if v.Tool.Valid {
		add("Tool", v.Tool.String, false, false)
	}
	if v.ImportSrc.Valid && strings.TrimSpace(v.ImportSrc.String) != "" {
		add("Import path", v.ImportSrc.String, false, true)
		if store != nil && store.ImportInPlace(v.ImportSrc.String) {
			add("Import mode", "bound in place", false, false)
		} else {
			add("Import mode", "packed from inbox", false, false)
		}
	}
	return out
}

// formatDetailDuration turns seconds into e.g. "10min 2s", "1h 3min".
func formatDetailDuration(sec float64) string {
	if sec < 0 {
		sec = -sec
	}
	d := time.Duration(sec*float64(time.Second) + 0.5)
	if d < time.Second {
		return "<1s"
	}
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	var parts []string
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dmin", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
}

// formatDurationClock formats seconds as M:SS or H:MM:SS for thumb badges.
func formatDurationClock(sec int) string {
	if sec <= 0 {
		return ""
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
