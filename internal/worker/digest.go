package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

var digestDebounce = 2 * time.Second

// SetDigestDebounceForTest overrides digest coalesce delay; returns previous value.
func SetDigestDebounceForTest(d time.Duration) time.Duration {
	prev := digestDebounce
	digestDebounce = d
	return prev
}

// digestState buffers successful media completions until the global media queue drains.
type digestState struct {
	mu    sync.Mutex
	items map[int64]notify.DigestItem // keyed by video id
	timer *time.Timer
}

func (r *Runner) digest() *digestState {
	r.digestOnce.Do(func() {
		r.digestBuf = &digestState{items: map[int64]notify.DigestItem{}}
	})
	return r.digestBuf
}

func mediaKind(kind string) bool {
	switch kind {
	case queue.KindDownload, queue.KindPackStream, queue.KindCacheBeginning:
		return true
	default:
		return false
	}
}

func (r *Runner) noteDigestSuccess(task *queue.Task) {
	if r.Library == nil || !task.VideoID.Valid {
		return
	}
	vid := task.VideoID.Int64
	seriesTitle, videoTitle := "", ""
	if v, err := r.Library.GetVideo(vid); err == nil && v != nil {
		videoTitle = v.Title
		if ser, err := r.Library.GetSeries(v.SeriesID, false); err == nil && ser != nil {
			seriesTitle = ser.Title
		}
	}
	d := r.digest()
	d.mu.Lock()
	defer d.mu.Unlock()
	it := d.items[vid]
	it.Domain = task.Domain
	if seriesTitle != "" {
		it.Series = seriesTitle
	}
	if videoTitle != "" {
		it.Title = videoTitle
	}
	switch task.Kind {
	case queue.KindDownload:
		it.Kind = "archive"
		it.Beginning = false
	case queue.KindPackStream:
		if it.Kind == "" {
			it.Kind = "stream"
		}
		// Keep Beginning if cache_beginning already completed out of order.
	case queue.KindCacheBeginning:
		it.Kind = "stream"
		it.Beginning = true
	}
	d.items[vid] = it
}

func (r *Runner) maybeScheduleDigest(ctx context.Context, log *slog.Logger) {
	if r.Queue == nil {
		return
	}
	d := r.digest()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.items) == 0 {
		return
	}
	if !r.mediaBacklogClear() {
		return
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(digestDebounce, func() {
		r.flushDigest(ctx, log)
	})
}

func (r *Runner) mediaBacklogClear() bool {
	n, err := r.Queue.CountMediaActive()
	if err != nil || n > 0 {
		return false
	}
	if r.Library == nil {
		return true
	}
	has, err := r.Library.HasEligibleWantedMedia()
	if err != nil || has {
		return false
	}
	return true
}

func (r *Runner) flushDigest(ctx context.Context, log *slog.Logger) {
	d := r.digest()
	d.mu.Lock()
	d.timer = nil
	if len(d.items) == 0 {
		d.mu.Unlock()
		return
	}
	if !r.mediaBacklogClear() {
		d.mu.Unlock()
		return
	}
	items := make([]notify.DigestItem, 0, len(d.items))
	for _, it := range d.items {
		items = append(items, it)
	}
	d.items = map[int64]notify.DigestItem{}
	d.mu.Unlock()
	if err := notify.DownloadDigest(ctx, r.Queue.DB, items); err != nil && log != nil {
		log.Warn("download_digest notify", "err", err)
	}
}
