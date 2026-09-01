package library

import (
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueYtDlpUpdate queues a system-lane yt-dlp GitHub update pass.
func (s *Store) EnqueueYtDlpUpdate(priority int, trigger string) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	if trigger == "" {
		trigger = "manual"
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindYtDlpUpdate,
		Domain:   queue.SystemDomain,
		Priority: priority,
		Message:  "yt-dlp update",
		Payload: map[string]any{
			"trigger": trigger,
		},
	})
}
