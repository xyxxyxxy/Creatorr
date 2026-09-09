package library

import (
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueYtDlpUpdate queues a system-lane yt-dlp GitHub update pass.
// origin must be manual|scheduled|boot (API/UI force manual; cron scheduled; boot boot).
// Boot callers should MoveToFront the returned id so it runs ahead of other pending system work.
func (s *Store) EnqueueYtDlpUpdate(origin string) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	if origin == "" {
		origin = queue.OriginManual
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:  origin,
		Kind:    queue.KindYtDlpUpdate,
		Domain:  queue.SystemDomain,
		Message: "yt-dlp update",
		Payload: map[string]any{},
	})
}
