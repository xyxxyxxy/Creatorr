package worker

import (
	"context"
	"encoding/json"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// SeriesMoveHandler updates series title/root, moves the folder, and applies naming.
func SeriesMoveHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "series move deps missing")
		}
		renamed, skipped, failed, err := d.Library.SeriesMovePass(ctx, t, progress)
		if err != nil {
			return err
		}
		progress("Series folder moved. "+library.ApplyNamingMessage(renamed, skipped, failed), ptrFloat(1))
		detail, _ := json.Marshal(map[string]any{
			"renamed": renamed, "skipped_busy": skipped, "failed": failed,
		})
		_ = d.Library.Queue.SetDetail(t.ID, string(detail))
		return nil
	}
}
