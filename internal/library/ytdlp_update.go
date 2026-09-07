package library

import (
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueYtDlpUpdate queues a system-lane yt-dlp GitHub update pass.
// origin must be manual|scheduled|boot (API/UI force manual; cron scheduled; boot boot).
func (s *Store) EnqueueYtDlpUpdate(priority int, origin string) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	if origin == "" {
		origin = queue.OriginManual
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:   origin,
		Kind:     queue.KindYtDlpUpdate,
		Domain:   queue.SystemDomain,
		Priority: priority,
		Message:  "yt-dlp update",
		Payload:  map[string]any{},
	})
}
