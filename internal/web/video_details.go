package web

import (
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

type videoDetailRow struct {
	Label       string
	Value       string
	IsURL       bool
	IsPath      bool
	ProgressPct int    // 0–100; -1 = no % (unknown duration); only set for Stream cache
	ProgressOn  bool   // show daisyUI progress
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
	if v.StreamURLsKind.Valid {
		if label := library.StreamTypeLabel(v.StreamURLsKind.String); label != "" {
			add("Stream kind", label, false, false)
		} else {
			add("Stream kind", v.StreamURLsKind.String, false, false)
		}
	}
	if v.Status == "streamable" && !library.StreamCDNDirect(v.StreamKind()) {
		beginningSec := 0
		if store != nil {
			if n, err := settings.CacheBeginningSeconds(store.DB); err == nil {
				beginningSec = n
			}
		}
		eff := float64(0)
		if store != nil {
			eff = store.EffectiveStreamCacheSeconds(v.ID, v.StreamBeginningCached, v.StreamPlaybackCachedSeconds, beginningSec)
		} else if v.StreamPlaybackCachedSeconds > 0 {
			eff = v.StreamPlaybackCachedSeconds
		} else if v.StreamBeginningCached && beginningSec > 0 {
			eff = float64(beginningSec)
		}
		dur := 0.0
		if v.DurationSeconds.Valid && v.DurationSeconds.Int64 > 0 {
			dur = float64(v.DurationSeconds.Int64)
		}
		pct := library.StreamCachePercent(eff, dur, v.StreamPlaybackCacheComplete)
		var label string
		if pct >= 0 {
			if dur > 0 {
				label = fmt.Sprintf("%d%% · %s / %s", pct, formatDetailDuration(eff), formatDetailDuration(dur))
			} else {
				label = fmt.Sprintf("%d%% · %s", pct, formatDetailDuration(eff))
			}
		} else if eff > 0 {
			label = formatDetailDuration(eff) + " cached"
		} else {
			label = "none"
		}
		out = append(out, videoDetailRow{
			Label:       "Stream cache",
			Value:       label,
			ProgressOn:  pct >= 0 || eff > 0,
			ProgressPct: pct,
		})
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
