package settings

import (
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const (
	YtDlpChannelStable  = "stable"
	YtDlpChannelNightly = "nightly"
)

// YtDlpChannelOption is one yt-dlp update channel choice.
type YtDlpChannelOption struct {
	Value string
	Label string
}

// YtDlpUpdateChannelOptions is the closed set for ytdlp_update_channel.
func YtDlpUpdateChannelOptions() []YtDlpChannelOption {
	return []YtDlpChannelOption{
		{Value: YtDlpChannelStable, Label: "Stable"},
		{Value: YtDlpChannelNightly, Label: "Nightly"},
	}
}

// NormalizeYtDlpUpdateChannel returns stable or nightly.
func NormalizeYtDlpUpdateChannel(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == YtDlpChannelNightly {
		return YtDlpChannelNightly
	}
	return YtDlpChannelStable
}

func validateYtDlpUpdateChannel(v string) error {
	v = strings.TrimSpace(strings.ToLower(v))
	if v != YtDlpChannelStable && v != YtDlpChannelNightly {
		return fmt.Errorf("yt-dlp update channel must be stable or nightly")
	}
	return nil
}

// YtDlpUpdatesEnabled reports whether automatic GitHub yt-dlp updates are on (non-empty cron).
func YtDlpUpdatesEnabled(database *db.DB) (bool, error) {
	v, err := Get(database, KeyYtDlpUpdateCron)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(v) != "", nil
}

// SyncYtDlpInstalledVersion writes ytdlp_installed_version (boot --version and ytdlp_update).
func SyncYtDlpInstalledVersion(database *db.DB, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("yt-dlp version empty")
	}
	if _, err := database.SQL.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, KeyYtDlpInstalledVersion, version); err != nil {
		return fmt.Errorf("record %s: %w", KeyYtDlpInstalledVersion, err)
	}
	return nil
}

// RecordYtDlpInstall persists installed version after a successful update task.
func RecordYtDlpInstall(database *db.DB, version string) error {
	return SyncYtDlpInstalledVersion(database, version)
}
