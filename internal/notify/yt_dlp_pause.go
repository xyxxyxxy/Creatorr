package notify

import (
	"context"
	"log/slog"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// SoftPauseAndAlert soft-pauses the hostname when code is a yt-dlp pause code,
// then records the matching alert (cookie_invalid / rate_limited / ytdlp_failed).
// Used by the worker and stream proxy so behavior stays aligned.
func SoftPauseAndAlert(ctx context.Context, database *db.DB, log *slog.Logger, taskID int64, domain, code, detail string) {
	if database == nil || domain == "" || domain == "unknown" || domain == "system" {
		return
	}
	if apperrors.IsYtDlpPauseCode(code) {
		if err := domains.SetPaused(database, domain, true); err != nil {
			if log != nil {
				log.Warn("auto-pause domain", "domain", domain, "code", code, "err", err)
			}
		}
	}
	if code == apperrors.CodeCookieInvalid {
		ytdlp.InvalidateFlareHost(ctx, domain)
	}
	var nerr error
	switch code {
	case apperrors.CodeCookieInvalid:
		nerr = CookieInvalid(ctx, database, taskID, domain, detail)
	case apperrors.CodeRateLimited:
		nerr = RateLimited(ctx, database, taskID, domain, detail)
	default:
		nerr = YtDlpFailed(ctx, database, taskID, domain, detail)
	}
	if nerr != nil && log != nil {
		log.Warn("notify", "domain", domain, "code", code, "err", nerr)
	}
}
