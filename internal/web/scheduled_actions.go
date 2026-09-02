package web

import (
	"sort"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

type scheduledTaskView struct {
	Key      string // settings key (stable row id)
	Kind     string // task-style kind label
	Schedule string // human cron summary
	EndsAt   string // RFC3339Nano
	AbsTip   string // absolute UTC
	WaitTip  string // initial countdown tip
	Busy     bool   // matching system task already pending/running
}

func buildScheduledTasks(lib *library.Store, now time.Time) ([]scheduledTaskView, error) {
	if lib == nil {
		return nil, nil
	}
	entries, err := settings.Scheduler(lib.DB)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	hasRetention, err := lib.AnyRootRetentionTTL()
	if err != nil {
		return nil, err
	}
	type row struct {
		view scheduledTaskView
		next time.Time
	}
	var rows []row
	for _, e := range entries {
		cron := strings.TrimSpace(e.Value)
		if cron == "" {
			continue
		}
		if e.Key == settings.KeyRetentionDeleteCron && !hasRetention {
			continue
		}
		next, err := cronexpr.Next(cron, now)
		if err != nil || next.IsZero() {
			continue
		}
		rem := int(next.Sub(now).Round(time.Second) / time.Second)
		if rem < 1 {
			rem = 1
		}
		rows = append(rows, row{
			next: next,
			view: scheduledTaskView{
				Key:      e.Key,
				Kind:     scheduledTaskKind(e.Key),
				Schedule: cronexpr.Describe(cron),
				EndsAt:   next.UTC().Format(time.RFC3339Nano),
				AbsTip:   formatAbsoluteTip(next),
				WaitTip:  scheduledTaskWaitTip(rem),
			},
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		// Furthest next run first; soonest last (just above real tasks).
		return rows[i].next.After(rows[j].next)
	})
	out := make([]scheduledTaskView, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.view)
	}
	return out, nil
}

func scheduledTaskKind(key string) string {
	switch key {
	case settings.KeyDownloadWantedCron:
		return "download_wanted"
	case settings.KeySyncFilesCron:
		return queue.KindSyncFiles
	case settings.KeyRetentionDeleteCron:
		return queue.KindRetentionDelete
	case settings.KeyYtDlpUpdateCron:
		return queue.KindYtDlpUpdate
	default:
		return key
	}
}

// markScheduledBusy sets Busy when a matching system-lane task is already open.
func markScheduledBusy(q *queue.Store, rows []scheduledTaskView) {
	if q == nil {
		return
	}
	for i := range rows {
		kind := rows[i].Kind
		if kind == "download_wanted" {
			continue
		}
		busy, err := q.HasPendingOrRunningKind(kind, queue.SystemDomain)
		if err == nil && busy {
			rows[i].Busy = true
		}
	}
}
