package queue

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

var (
	// ErrDuplicate means an equivalent pending/running task already exists.
	ErrDuplicate = errors.New("task already queued")
	// ErrQueueFull means the domain download queue is at max_download_queue.
	ErrQueueFull = errors.New("download queue full")
)

// EnqueueParams creates a pending task.
type EnqueueParams struct {
	Kind              string
	Domain            string
	SeriesID          int64
	VideoID           int64
	Payload           map[string]any
	Message           string
	Origin            string // required: manual|scheduled|boot|task
	ParentTaskID      int64  // required when Origin=task; forces OriginTask when >0
	BypassDownloadCap bool   // Download now: skip max_download_queue
	Immediate         bool   // Download now: ClaimImmediate (sets payload.download_now)
}

// normalizeEnqueueOrigin validates and normalizes origin/parent for insert.
func normalizeEnqueueOrigin(p *EnqueueParams) error {
	if p.ParentTaskID > 0 {
		p.Origin = OriginTask
	}
	p.Origin = strings.TrimSpace(p.Origin)
	if !ValidOrigin(p.Origin) {
		return fmt.Errorf("origin required: manual|scheduled|boot|task")
	}
	if p.Origin == OriginTask && p.ParentTaskID <= 0 {
		return fmt.Errorf("parent_task_id required when origin=task")
	}
	return nil
}

// Enqueue inserts a pending task after duplicate and download-cap checks.
func (s *Store) Enqueue(p EnqueueParams) (int64, error) {
	if p.Kind == "" {
		return 0, fmt.Errorf("kind required")
	}
	if err := normalizeEnqueueOrigin(&p); err != nil {
		return 0, err
	}
	domain := p.Domain
	if domain == "" {
		domain = "unknown"
	} else if domain != "unknown" && domain != SystemDomain {
		domain = settings.NormalizeDomain(domain)
	}
	if p.Immediate {
		if p.Payload == nil {
			p.Payload = map[string]any{}
		}
		p.Payload[PayloadKeyDownloadNow] = true
	}
	payload := "{}"
	if p.Payload != nil {
		b, err := json.Marshal(p.Payload)
		if err != nil {
			return 0, err
		}
		payload = string(b)
	}
	if err := s.rejectDuplicate(p, payload); err != nil {
		return 0, err
	}
	if p.Kind == KindDownload && !p.BypassDownloadCap {
		if err := s.rejectDownloadQueueFull(domain); err != nil {
			return 0, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var series any
	var video any
	var parent any
	if p.SeriesID > 0 {
		series = p.SeriesID
	}
	if p.VideoID > 0 {
		video = p.VideoID
	}
	if p.ParentTaskID > 0 {
		parent = p.ParentTaskID
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	seq, err := s.nextQueueSeqLocked(domain)
	if err != nil {
		return 0, err
	}
	res, err := s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, series_id, video_id, payload, message, domain, queue_seq, origin, parent_task_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.Kind, StatusPending, series, video, payload, nullStr(p.Message), domain, seq, p.Origin, parent, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertRunning inserts a running task without duplicate or download-cap checks.
// Used for sync bookkeeping (e.g. series folder move NFO rewrite) so video_history can link a task_id.
// Caller must pass origin (and real parent_task_id when origin=task); never invent a fake parent.
func (s *Store) InsertRunning(p EnqueueParams) (int64, error) {
	if p.Kind == "" {
		return 0, fmt.Errorf("kind required")
	}
	if err := normalizeEnqueueOrigin(&p); err != nil {
		return 0, err
	}
	domain := p.Domain
	if domain == "" {
		domain = "unknown"
	} else if domain != "unknown" && domain != SystemDomain {
		domain = settings.NormalizeDomain(domain)
	}
	if p.Immediate {
		if p.Payload == nil {
			p.Payload = map[string]any{}
		}
		p.Payload[PayloadKeyDownloadNow] = true
	}
	payload := "{}"
	if p.Payload != nil {
		b, err := json.Marshal(p.Payload)
		if err != nil {
			return 0, err
		}
		payload = string(b)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var series any
	var video any
	var parent any
	if p.SeriesID > 0 {
		series = p.SeriesID
	}
	if p.VideoID > 0 {
		video = p.VideoID
	}
	if p.ParentTaskID > 0 {
		parent = p.ParentTaskID
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	seq, err := s.nextQueueSeqLocked(domain)
	if err != nil {
		return 0, err
	}
	res, err := s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, series_id, video_id, payload, message, domain, queue_seq, origin, parent_task_id, created_at, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.Kind, StatusRunning, series, video, payload, nullStr(p.Message), domain, seq, p.Origin, parent, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) nextQueueSeqLocked(domain string) (int64, error) {
	var seq int64
	err := s.DB.SQL.QueryRow(`
		SELECT COALESCE(MAX(queue_seq), 0) + 1 FROM tasks
		WHERE domain = ? AND status IN (?, ?)
	`, domain, StatusPending, StatusRunning).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// MoveToFront sets a pending task's queue_seq ahead of all open tasks on its domain.
func (s *Store) MoveToFront(id int64) error {
	if id <= 0 {
		return fmt.Errorf("task id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var domain, status string
	err := s.DB.SQL.QueryRow(`SELECT domain, status FROM tasks WHERE id = ?`, id).Scan(&domain, &status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task not found")
	}
	if err != nil {
		return err
	}
	if status != StatusPending {
		return fmt.Errorf("only pending tasks can move to front")
	}
	var minSeq sql.NullInt64
	if err := s.DB.SQL.QueryRow(`
		SELECT MIN(queue_seq) FROM tasks
		WHERE domain = ? AND status IN (?, ?)
	`, domain, StatusPending, StatusRunning).Scan(&minSeq); err != nil {
		return err
	}
	front := int64(1)
	if minSeq.Valid {
		front = minSeq.Int64 - 1
	}
	res, err := s.DB.SQL.Exec(`UPDATE tasks SET queue_seq = ? WHERE id = ? AND status = ?`, front, id, StatusPending)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("task not pending")
	}
	return nil
}

func (s *Store) rejectDuplicate(p EnqueueParams, payloadJSON string) error {
	// Path-touching system kinds never overlap series_move; series_move needs them idle.
	switch p.Kind {
	case KindRenameEpisodes, KindRegenerateNFO, KindSyncFiles, KindRetentionDelete:
		if busy, err := s.PathTouchingSystemBusy(KindSeriesMove); err != nil {
			return err
		} else if busy {
			return ErrDuplicate
		}
	case KindSeriesMove:
		if busy, err := s.PathTouchingSystemBusy(KindRenameEpisodes, KindRegenerateNFO, KindSyncFiles, KindRetentionDelete); err != nil {
			return err
		} else if busy {
			return ErrDuplicate
		}
		// ponytail: one open move per series; different series may queue together (bulk root change).
		return s.rejectIfExists(`
			SELECT 1 FROM tasks WHERE kind = ? AND series_id = ? AND status IN (?, ?) LIMIT 1
		`, KindSeriesMove, p.SeriesID, StatusPending, StatusRunning)
	}
	// System lane: at most one pending/running task per kind (except import keeps per-video).
	if p.Domain == SystemDomain {
		switch p.Kind {
		case KindSyncFiles, KindRetentionDelete, KindRegenerateNFO, KindIntegrityCheck, KindYtDlpUpdate, KindBulkEditSeries, KindBulkEditVideos:
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE domain = ? AND kind = ? AND status IN (?, ?) LIMIT 1
			`, SystemDomain, p.Kind, StatusPending, StatusRunning)
			// KindRenameEpisodes: full vs scoped dedup is handled in library enqueue helpers.
		}
	}
	switch p.Kind {
	case KindDownload:
		if p.SeriesID > 0 {
			if busy, err := s.SeriesMoveOpen(p.SeriesID); err != nil {
				return err
			} else if busy {
				return ErrDuplicate
			}
		}
		if p.VideoID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND video_id = ? AND status IN (?, ?) LIMIT 1
			`, KindDownload, p.VideoID, StatusPending, StatusRunning)
		}
		return nil
	case KindScan:
		srcID := SourceIDFromPayload(payloadJSON)
		if srcID <= 0 {
			// Legacy series-wide scan: one active scan per series without source_id.
			if p.SeriesID <= 0 {
				return nil
			}
			return s.rejectIfExists(`
				SELECT 1 FROM tasks
				WHERE kind = ? AND series_id = ? AND status IN (?, ?)
				  AND COALESCE(json_extract(payload, '$.source_id'), 0) = 0
				LIMIT 1
			`, KindScan, p.SeriesID, StatusPending, StatusRunning)
		}
		return s.rejectIfExists(`
			SELECT 1 FROM tasks
			WHERE kind = ? AND status IN (?, ?)
			  AND json_extract(payload, '$.source_id') = ?
			LIMIT 1
		`, KindScan, StatusPending, StatusRunning, srcID)
	case KindRescanMetadata:
		if p.VideoID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND video_id = ? AND status IN (?, ?) LIMIT 1
			`, KindRescanMetadata, p.VideoID, StatusPending, StatusRunning)
		}
		if p.SeriesID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks
				WHERE kind = ? AND series_id = ? AND status IN (?, ?)
				  AND (video_id IS NULL OR video_id = 0)
				LIMIT 1
			`, KindRescanMetadata, p.SeriesID, StatusPending, StatusRunning)
		}
	case KindRefreshSidecars:
		if p.VideoID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND video_id = ? AND status IN (?, ?) LIMIT 1
			`, KindRefreshSidecars, p.VideoID, StatusPending, StatusRunning)
		}
	case KindImport:
		if p.VideoID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND video_id = ? AND status IN (?, ?) LIMIT 1
			`, KindImport, p.VideoID, StatusPending, StatusRunning)
		}
	case KindImportPlan:
		return s.rejectIfExists(`
			SELECT 1 FROM tasks WHERE kind = ? AND status IN (?, ?) LIMIT 1
		`, KindImportPlan, StatusPending, StatusRunning)
	case KindSponsorblockCut:
		if p.VideoID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND video_id = ? AND status IN (?, ?) LIMIT 1
			`, KindSponsorblockCut, p.VideoID, StatusPending, StatusRunning)
		}
	case KindIntegrityCheckInitial:
		if p.VideoID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND video_id = ? AND status IN (?, ?) LIMIT 1
			`, KindIntegrityCheckInitial, p.VideoID, StatusPending, StatusRunning)
		}
	case KindPrefetchSeriesMeta:
		if p.SeriesID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND series_id = ? AND status IN (?, ?) LIMIT 1
			`, KindPrefetchSeriesMeta, p.SeriesID, StatusPending, StatusRunning)
		}
	case KindPrefetchVideoMeta:
		if p.VideoID > 0 {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND video_id = ? AND status IN (?, ?) LIMIT 1
			`, KindPrefetchVideoMeta, p.VideoID, StatusPending, StatusRunning)
		}
	case KindPrefetchAddSeries, KindPrefetchAddVideo:
		tok := DraftTokenFromPayload(payloadJSON)
		if tok != "" {
			return s.rejectIfExists(`
				SELECT 1 FROM tasks WHERE kind = ? AND status IN (?, ?)
				  AND json_extract(payload, '$.draft_token') = ?
				LIMIT 1
			`, p.Kind, StatusPending, StatusRunning, tok)
		}
	}
	return nil
}

func (s *Store) rejectIfExists(query string, args ...any) error {
	var one int
	err := s.DB.SQL.QueryRow(query, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrDuplicate
}

func (s *Store) rejectDownloadQueueFull(domain string) error {
	lim, err := settings.LimitsForDomain(s.DB, domain)
	if err != nil {
		return err
	}
	max := lim.MaxDownloadQueue
	if max <= 0 {
		max = settings.DefaultMaxDownloadQueue
	}
	var n int
	err = s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
	`, domain, KindDownload, StatusPending, StatusRunning).Scan(&n)
	if err != nil {
		return err
	}
	if n >= max {
		return fmt.Errorf("%w: %d/%d download tasks", ErrQueueFull, n, max)
	}
	return nil
}
