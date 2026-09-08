package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

var (
	// ErrDuplicate means an equivalent pending/running task already exists.
	ErrDuplicate = errors.New("task already queued")
	// ErrQueueFull means the domain download queue is at max_download_queue.
	ErrQueueFull = errors.New("download queue full")
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"

	KindScan               = "scan"
	KindDownload           = "download"
	KindRescanMetadata     = "rescan_metadata"
	KindRefreshSidecars    = "refresh_sidecars"
	KindImport             = "import"
	KindPrefetchSeriesMeta = "prefetch_series_meta"
	KindPrefetchVideoMeta  = "prefetch_video_meta"
	KindPrefetchAddSeries  = "prefetch_add_series"
	KindPrefetchAddVideo   = "prefetch_add_video"
	KindProbeSourceTitle   = "probe_source_title"
	KindSyncFiles          = "sync_files"
	KindRetentionDelete    = "retention_delete"
	KindRenameEpisodes     = "rename_episodes"
	KindRegenerateNFO      = "regenerate_nfo"
	KindIntegrityCheck        = "integrity_check"
	KindDeleteFiles           = "delete_files"
	KindDeleteSidecar         = "delete_sidecar"
	KindSponsorblockCut       = "sponsorblock_cut"
	KindIntegrityCheckInitial = "integrity_check_initial"
	KindYtDlpUpdate           = "ytdlp_update"
	KindBulkEditSeries     = "bulk_edit_series"
	KindBulkEditVideos     = "bulk_edit_videos"

	// SystemDomain is the queue lane for maintenance tasks.
	SystemDomain = "system"
)

// IsPrefetchKind is true for ClaimInteractive metadata prefetch / probe tasks.
// These do not occupy max_parallel_tasks slots.
func IsPrefetchKind(kind string) bool {
	return kind == KindPrefetchSeriesMeta || kind == KindPrefetchVideoMeta ||
		kind == KindPrefetchAddSeries || kind == KindPrefetchAddVideo ||
		kind == KindProbeSourceTitle
}

// IsInteractiveKind is true for tasks that must not wait behind other work
// (prefetch ClaimInteractive). Finish does not start domain cooldown.
// Prefetch kinds do not occupy parallel slots.
func IsInteractiveKind(kind string) bool {
	return IsPrefetchKind(kind)
}

// PrioritySyncFilesDue bumps cron sync_files ahead of pending apply naming.
const PrioritySyncFilesDue = 50

// PriorityRetentionDeleteDue bumps cron retention_delete ahead of pending apply naming.
const PriorityRetentionDeleteDue = 50

// PriorityDownloadNow places a Queue download at the front of the domain lane.
const PriorityDownloadNow = 100

// PrioritySponsorblockCut keeps SponsorBlock cut/encode behind other system work.
const PrioritySponsorblockCut = -10

// PriorityYtDlpUpdateBoot enqueues boot yt-dlp update ahead of default system work.
const PriorityYtDlpUpdateBoot = 40

// PriorityYtDlpUpdateDue is cron/manual yt-dlp update priority.
const PriorityYtDlpUpdateDue = 50

// Task origin: kick source (writable values only).
const (
	OriginManual    = "manual"
	OriginScheduled = "scheduled"
	OriginBoot      = "boot"
	OriginTask      = "task"
)

// ValidOrigin reports whether origin is a writable provenance value.
func ValidOrigin(origin string) bool {
	switch origin {
	case OriginManual, OriginScheduled, OriginBoot, OriginTask:
		return true
	default:
		return false
	}
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

// Task is a queued unit of work.
type Task struct {
	ID           int64
	Kind         string
	Status       string
	SeriesID     sql.NullInt64
	VideoID      sql.NullInt64
	Payload      string
	ErrorCode    string
	ErrorMessage string
	Message      string
	Detail       string   // structured outcome JSON for History
	Commands     []string // shell-formatted external argv lines (yt-dlp/ffmpeg/…)
	Logs         []string // progress lines (live buffer or persisted on failed)
	Progress     sql.NullFloat64
	Domain       string
	Priority     int
	Origin       string
	ParentTaskID sql.NullInt64
	CreatedAt    string
	StartedAt    sql.NullString
	FinishedAt   sql.NullString
	QueuePos     int // 1 = front among pending+running in domain; set by ListActive
}

// EnqueueParams creates a pending task.
type EnqueueParams struct {
	Kind              string
	Domain            string
	SeriesID          int64
	VideoID           int64
	Payload           map[string]any
	Priority          int
	Message           string
	Origin            string // required: manual|scheduled|boot|task
	ParentTaskID      int64  // required when Origin=task; forces OriginTask when >0
	BypassDownloadCap bool   // Queue download: skip max_download_queue
}

// Store wraps queue operations.
type Store struct {
	DB *db.DB

	mu       sync.Mutex
	cooldown map[string]time.Time // domain -> earliest next claim
	cancels  sync.Map             // taskID (int64) -> context.CancelFunc for running tasks

	// Logs holds in-memory progress lines for running tasks; flushed to tasks.logs on failed Finish.
	Logs *TaskLogs
	// Live holds latest message + progress fraction for running tasks (not persisted).
	Live *LiveState
	// Commands holds yt-dlp/ffmpeg argv lines while running (deduped); flushed to SQLite on Finish/Cancel when policy allows.
	Commands *TaskCommands

	// OnCancelled is invoked after a task is marked cancelled (pending or running).
	// Optional; library.NewStore wires source/video history recording.
	OnCancelled func(Task)
}

func NewStore(database *db.DB) *Store {
	return &Store{
		DB:       database,
		cooldown: make(map[string]time.Time),
		Logs:     newTaskLogs(),
		Live:     newLiveState(),
		Commands: newTaskCommands(),
	}
}

func (s *Store) notifyCancelled(tasks ...Task) {
	if s == nil || s.OnCancelled == nil {
		return
	}
	for _, t := range tasks {
		t.Status = StatusCancelled
		s.OnCancelled(t)
	}
}

// RegisterRunning ties a cancel func to a claimed task so Cancel can abort the worker.
func (s *Store) RegisterRunning(id int64, cancel context.CancelFunc) {
	if id <= 0 || cancel == nil {
		return
	}
	s.cancels.Store(id, cancel)
}

// UnregisterRunning drops the cancel hook after the worker finishes.
func (s *Store) UnregisterRunning(id int64) {
	s.cancels.Delete(id)
}

func (s *Store) abortRunning(id int64) {
	if v, ok := s.cancels.Load(id); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
}

// DomainFromURL extracts hostname for queue lane (lowercased, www. stripped).
func DomainFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		if strings.Contains(raw, ".") && !strings.Contains(raw, "/") {
			if d := settings.NormalizeDomain(raw); d != "" {
				return d
			}
		}
		return "unknown"
	}
	host := settings.NormalizeDomain(u.Hostname())
	if host == "" {
		return "unknown"
	}
	return host
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
	res, err := s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, series_id, video_id, payload, message, domain, priority, origin, parent_task_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.Kind, StatusPending, series, video, payload, nullStr(p.Message), domain, p.Priority, p.Origin, parent, now)
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
	res, err := s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, series_id, video_id, payload, message, domain, priority, origin, parent_task_id, created_at, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.Kind, StatusRunning, series, video, payload, nullStr(p.Message), domain, p.Priority, p.Origin, parent, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) rejectDuplicate(p EnqueueParams, payloadJSON string) error {
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

// ClaimNext picks the next runnable pending task respecting inactive/paused domains,
// per-domain max_parallel_tasks, and cooldown. Interactive kinds are excluded (see ClaimInteractive).
func (s *Store) ClaimNext() (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	rows, err := s.DB.SQL.Query(`
		SELECT t.id, t.kind, t.status, t.series_id, t.video_id, t.payload,
		       COALESCE(t.error_code,''), COALESCE(t.error_message,''), COALESCE(t.message,''),
		       COALESCE(t.detail,''), t.progress, t.domain, t.priority, t.created_at, t.started_at, t.finished_at,
		       t.origin, t.parent_task_id
		FROM tasks t
		WHERE t.status = ?
		  AND t.kind NOT IN (?, ?, ?, ?, ?)
		  AND NOT EXISTS (
		    SELECT 1 FROM domains d WHERE d.domain = t.domain AND d.active = 0
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM domain_runtime r WHERE r.domain = t.domain AND r.paused != 0
		  )
		ORDER BY t.priority DESC, t.id ASC
	`, StatusPending, KindPrefetchSeriesMeta, KindPrefetchVideoMeta, KindPrefetchAddSeries, KindPrefetchAddVideo, KindProbeSourceTitle)
	if err != nil {
		return nil, err
	}
	return s.claimFromRows(rows, now, true)
}

// ClaimInteractive claims a pending interactive task (e.g. prefetch_series_meta),
// ignoring per-domain running tasks, cooldown, and soft pause. Still requires domain active.
func (s *Store) ClaimInteractive() (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	rows, err := s.DB.SQL.Query(`
		SELECT t.id, t.kind, t.status, t.series_id, t.video_id, t.payload,
		       COALESCE(t.error_code,''), COALESCE(t.error_message,''), COALESCE(t.message,''),
		       COALESCE(t.detail,''), t.progress, t.domain, t.priority, t.created_at, t.started_at, t.finished_at,
		       t.origin, t.parent_task_id
		FROM tasks t
		WHERE t.status = ?
		  AND t.kind IN (?, ?, ?, ?, ?)
		  AND NOT EXISTS (
		    SELECT 1 FROM domains d WHERE d.domain = t.domain AND d.active = 0
		  )
		ORDER BY t.priority DESC, t.id ASC
	`, StatusPending, KindPrefetchSeriesMeta, KindPrefetchVideoMeta, KindPrefetchAddSeries, KindPrefetchAddVideo, KindProbeSourceTitle)
	if err != nil {
		return nil, err
	}
	return s.claimFromRows(rows, now, false)
}

func (s *Store) claimFromRows(rows *sql.Rows, now time.Time, respectCooldown bool) (*Task, error) {
	var candidates []*Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		if respectCooldown {
			if t.Domain != SystemDomain {
				if until, ok := s.cooldown[t.Domain]; ok && now.Before(until) {
					continue
				}
			}
			if !s.domainHasParallelSlot(t.Domain) {
				continue
			}
		}
		candidates = append(candidates, t)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, t := range candidates {
		if respectCooldown && !s.domainHasParallelSlot(t.Domain) {
			continue
		}
		started := now.UTC().Format(time.RFC3339Nano)
		res, err := s.DB.SQL.Exec(`
			UPDATE tasks SET status = ?, started_at = ?, message = COALESCE(NULLIF(message,''), ?)
			WHERE id = ? AND status = ?
		`, StatusRunning, started, "Running", t.ID, StatusPending)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			continue
		}
		t.Status = StatusRunning
		t.StartedAt = sql.NullString{String: started, Valid: true}
		if respectCooldown {
			s.startDomainCooldown(t.Domain)
		}
		return t, nil
	}
	return nil, nil
}

// domainHasParallelSlot reports whether another non-interactive task may start on domain.
// Caller must hold s.mu when using cooldown-aware claim paths.
// System lane is always serial (max 1), regardless of Settings max_parallel_tasks.
func (s *Store) domainHasParallelSlot(domain string) bool {
	max := 1
	if domain != SystemDomain {
		lim, err := settings.LimitsForDomain(s.DB, domain)
		if err != nil {
			return false
		}
		max = lim.MaxParallelTasks
		if max < 1 {
			max = settings.DefaultMaxParallelTasks
		}
	}
	var n int
	_ = s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE domain = ? AND status = ?
		  AND kind NOT IN (?, ?, ?, ?, ?)
	`, domain, StatusRunning, KindPrefetchSeriesMeta, KindPrefetchVideoMeta, KindPrefetchAddSeries, KindPrefetchAddVideo, KindProbeSourceTitle).Scan(&n)
	// Prefetch kinds are excluded from the count.
	return n < max
}

func (s *Store) startDomainCooldown(domain string) {
	if domain == "" || domain == SystemDomain {
		return
	}
	lim, err := settings.LimitsForDomain(s.DB, domain)
	if err != nil || lim.TaskCooldownSeconds <= 0 {
		return
	}
	s.cooldown[domain] = time.Now().Add(time.Duration(lim.TaskCooldownSeconds) * time.Second)
}

// StartCooldownForDomains arms task_cooldown_seconds for each hostname (skips empty,
// system, and DomainDefault). Dedupes. Used at process boot so ClaimNext waits before
// the first non-interactive claim after restart. Caller need not hold s.mu.
// Returns how many unique domains are cooling afterward.
func (s *Store) StartCooldownForDomains(domains []string) int {
	if s == nil || len(domains) == 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]struct{}{}
	for _, raw := range domains {
		d := settings.NormalizeDomain(raw)
		if d == "" || d == SystemDomain || d == settings.DomainDefault || d == "unknown" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		s.startDomainCooldown(d)
	}
	n := 0
	for d := range seen {
		if until, ok := s.cooldown[d]; ok && time.Now().Before(until) {
			n++
		}
	}
	return n
}

// HasPendingOrRunningDomain reports whether any task is pending or running for domain.
func (s *Store) HasPendingOrRunningDomain(domain string) (bool, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return false, nil
	}
	var one int
	err := s.DB.SQL.QueryRow(`
		SELECT 1 FROM tasks WHERE domain = ? AND status IN (?, ?) LIMIT 1
	`, domain, StatusPending, StatusRunning).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// HasPendingOrRunningKind reports whether any task of kind is pending or running
// (optionally scoped to domain; empty domain = any).
func (s *Store) HasPendingOrRunningKind(kind, domain string) (bool, error) {
	var one int
	var err error
	if domain != "" {
		err = s.DB.SQL.QueryRow(`
			SELECT 1 FROM tasks WHERE kind = ? AND domain = ? AND status IN (?, ?) LIMIT 1
		`, kind, domain, StatusPending, StatusRunning).Scan(&one)
	} else {
		err = s.DB.SQL.QueryRow(`
			SELECT 1 FROM tasks WHERE kind = ? AND status IN (?, ?) LIMIT 1
		`, kind, StatusPending, StatusRunning).Scan(&one)
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CountMediaActive returns pending+running count for download, sponsorblock_cut,
// and integrity_check_initial across all domains.
func (s *Store) CountMediaActive() (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE kind IN (?, ?, ?) AND status IN (?, ?)
	`, KindDownload, KindSponsorblockCut, KindIntegrityCheckInitial, StatusPending, StatusRunning).Scan(&n)
	return n, err
}

// Finish marks a task done, failed, or cancelled.
// Domain cooldown is started on ClaimNext only (not here); interactive finishes never cool down.
func (s *Store) Finish(id int64, status, message, errCode, errMsg string) error {
	if status != StatusDone && status != StatusFailed && status != StatusCancelled {
		return fmt.Errorf("invalid finish status %q", status)
	}

	if err := s.persistCommands(id, status, s.taskKind(id)); err != nil {
		return err
	}
	if err := s.persistLogs(id, status); err != nil {
		return err
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, finished_at = ?, message = ?, error_code = NULLIF(?, ''), error_message = NULLIF(?, '')
		WHERE id = ? AND status = ?
	`, status, finished, message, errCode, errMsg, id, StatusRunning)
	if err != nil {
		return err
	}
	s.clearLive(id)
	return nil
}

// CooldownUntil returns when the domain may claim again. Zero time if not cooling down.
// System lane never cools down (serial max-1 only).
func (s *Store) CooldownUntil(domain string) time.Time {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == SystemDomain {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.cooldown[domain]
	if !ok || !time.Now().Before(until) {
		return time.Time{}
	}
	return until
}

// UpdateProgress sets in-memory message/progress for a running task (not written to SQLite).
// When progress is nil, live progress is cleared (message-only / spinner UI).
// Final message is persisted on Finish/Cancel. Restart requeues running tasks anyway.
func (s *Store) UpdateProgress(id int64, message string, progress *float64) error {
	if s == nil || id <= 0 {
		return nil
	}
	s.Live.Set(id, message, progress)
	return nil
}

// UpdatePayload replaces the JSON payload on a running or pending task (cursor resume).
func (s *Store) UpdatePayload(id int64, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET payload = ? WHERE id = ? AND status IN (?, ?)`, string(b), id, StatusPending, StatusRunning)
	return err
}

// SetDetail stores structured outcome JSON on a task (History detail).
func (s *Store) SetDetail(id int64, detail string) error {
	_, err := s.DB.SQL.Exec(`UPDATE tasks SET detail = NULLIF(?, '') WHERE id = ?`, detail, id)
	return err
}

// MergeDetailJSON merges patch keys into tasks.detail (JSON object).
// Non-JSON existing detail is preserved under key "error".
func (s *Store) MergeDetailJSON(id int64, patch map[string]any) error {
	if s == nil || id <= 0 || len(patch) == 0 {
		return nil
	}
	var cur string
	err := s.DB.SQL.QueryRow(`SELECT COALESCE(detail, '') FROM tasks WHERE id = ?`, id).Scan(&cur)
	if err != nil {
		return err
	}
	m := map[string]any{}
	if strings.TrimSpace(cur) != "" {
		if json.Unmarshal([]byte(cur), &m) != nil || m == nil {
			m = map[string]any{"error": cur}
		}
	}
	for k, v := range patch {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return s.SetDetail(id, string(b))
}

// AppendCommand appends a shell-formatted external command line in memory.
// bin selects policy (yt-dlp / ffmpeg / ffprobe). Flushed to tasks.commands on Finish/Cancel when allowed.
func (s *Store) AppendCommand(id int64, bin, line string) error {
	if s == nil || id <= 0 {
		return nil
	}
	s.Commands.Append(id, bin, line)
	return nil
}

// persistCommands writes buffered command lines to SQLite once (History), or clears them
// when PersistCommandsOnStatus says not to keep this kind/status.
func (s *Store) persistCommands(id int64, status, kind string) error {
	if s == nil || id <= 0 {
		return nil
	}
	if !PersistCommandsOnStatus(kind, status) {
		s.Commands.Clear(id)
		_, err := s.DB.SQL.Exec(`UPDATE tasks SET commands = '[]' WHERE id = ?`, id)
		return err
	}
	lines := s.Commands.Snapshot(id)
	if len(lines) == 0 {
		s.Commands.Clear(id)
		return nil
	}
	b, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET commands = ? WHERE id = ?`, string(b), id)
	if err != nil {
		return err
	}
	s.Commands.Clear(id)
	return nil
}

// PersistLogsOnStatus reports whether progress log lines should be written to SQLite.
// Only failed tasks keep the ring (debugging yt-dlp / PO / remux); done and cancelled drop it.
func PersistLogsOnStatus(status string) bool {
	return status == StatusFailed
}

// persistLogs writes buffered progress lines to tasks.logs on failure, else clears the column.
func (s *Store) persistLogs(id int64, status string) error {
	if s == nil || id <= 0 {
		return nil
	}
	if !PersistLogsOnStatus(status) {
		s.Logs.Clear(id)
		_, err := s.DB.SQL.Exec(`UPDATE tasks SET logs = '[]' WHERE id = ?`, id)
		return err
	}
	lines := s.Logs.Snapshot(id)
	if len(lines) == 0 {
		s.Logs.Clear(id)
		_, err := s.DB.SQL.Exec(`UPDATE tasks SET logs = '[]' WHERE id = ?`, id)
		return err
	}
	b, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET logs = ? WHERE id = ?`, string(b), id)
	if err != nil {
		return err
	}
	s.Logs.Clear(id)
	return nil
}

func (s *Store) taskKind(id int64) string {
	var kind string
	_ = s.DB.SQL.QueryRow(`SELECT kind FROM tasks WHERE id = ?`, id).Scan(&kind)
	return kind
}

// Cancel marks pending/running task cancelled with reason manual and aborts a running worker if registered.
func (s *Store) Cancel(id int64) error {
	_, err := s.CancelWithReason(id, CancelReasonManual)
	return err
}

// CancelWithReason marks a pending/running task cancelled with a closed-set reason code.
// Returns the status before cancel (pending or running) so callers can record Activity
// for pending tasks; running cancels are recorded by the worker when the handler returns.
func (s *Store) CancelWithReason(id int64, reason string) (prevStatus string, err error) {
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return "", err
	}
	err = s.DB.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&prevStatus)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("task %d not cancellable", id)
	}
	if err != nil {
		return "", err
	}
	if prevStatus != StatusPending && prevStatus != StatusRunning {
		return prevStatus, fmt.Errorf("task %d not cancellable", id)
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, finished_at = ?, message = ?
		WHERE id = ? AND status IN (?, ?)
	`, StatusCancelled, finished, message, id, StatusPending, StatusRunning)
	if err != nil {
		return prevStatus, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return prevStatus, fmt.Errorf("task %d not cancellable", id)
	}
	_ = s.persistCommands(id, StatusCancelled, s.taskKind(id))
	_ = s.persistLogs(id, StatusCancelled)
	s.clearLive(id)
	s.abortRunning(id)
	if t, err := s.GetTask(id); err == nil && t != nil {
		s.notifyCancelled(*t)
	}
	return prevStatus, nil
}

// CancelDownloadsForVideo cancels pending and running download, sponsorblock_cut,
// and integrity_check_initial tasks for one video.
// Returns snapshots with pre-cancel Status (pending|running) and the cancel Message applied.
func (s *Store) CancelDownloadsForVideo(videoID int64, reason string) ([]Task, error) {
	if videoID <= 0 {
		return nil, fmt.Errorf("video_id required")
	}
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE kind IN (?, ?, ?) AND video_id = ? AND status IN (?, ?)
	`, KindDownload, KindSponsorblockCut, KindIntegrityCheckInitial, videoID, StatusPending, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	for _, t := range out {
		_, err := s.DB.SQL.Exec(`
			UPDATE tasks SET status = ?, finished_at = ?, message = ?
			WHERE id = ? AND status IN (?, ?)
		`, StatusCancelled, finished, message, t.ID, StatusPending, StatusRunning)
		if err != nil {
			return out, err
		}
		_ = s.persistCommands(t.ID, StatusCancelled, t.Kind)
		_ = s.persistLogs(t.ID, StatusCancelled)
		s.clearLive(t.ID)
		s.abortRunning(t.ID)
	}
	s.notifyCancelled(out...)
	return out, nil
}

// ListByParentTaskID returns tasks spawned by parentID, oldest first.
func (s *Store) ListByParentTaskID(parentID int64) ([]Task, error) {
	if parentID <= 0 {
		return nil, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE parent_task_id = ?
		ORDER BY created_at ASC, id ASC
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTask returns a task by id (any status), or nil if missing.
func (s *Store) GetTask(id int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id,
		       COALESCE(NULLIF(commands, ''), '[]'),
		       COALESCE(NULLIF(logs, ''), '[]')
		FROM tasks WHERE id = ?
	`, id)
	t, err := s.scanTaskWithExtras(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// TaskStatus returns the current status string for a task id (empty if missing).
func (s *Store) TaskStatus(id int64) (string, error) {
	var st string
	err := s.DB.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&st)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return st, err
}

// CancelAll cancels all pending tasks with reason manual and returns snapshots for Activity.
func (s *Store) CancelAll() ([]Task, error) {
	message, err := CancelReasonMessage(CancelReasonManual)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE status = ?
	`, StatusPending)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, finished_at = ?, message = ?
		WHERE status = ?
	`, StatusCancelled, finished, message, StatusPending)
	if err != nil {
		return out, err
	}
	s.notifyCancelled(out...)
	return out, nil
}

// CancelPendingDomain cancels pending tasks for one domain lane and returns snapshots for Activity.
func (s *Store) CancelPendingDomain(domain string) ([]Task, error) {
	return s.cancelDomain(domain, CancelReasonManual, StatusPending)
}

// CancelDomain cancels pending and running tasks for one domain lane (e.g. on deactivate).
// Returns snapshots with pre-cancel Status; callers should write Activity for pending rows
// (running cancels are recorded by the worker when the handler returns).
func (s *Store) CancelDomain(domain, reason string) ([]Task, error) {
	return s.cancelDomain(domain, reason, StatusPending, StatusRunning)
}

func (s *Store) cancelDomain(domain, reason string, statuses ...string) ([]Task, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return nil, fmt.Errorf("domain required")
	}
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return nil, err
	}
	if len(statuses) == 0 {
		return nil, nil
	}
	args := make([]any, 0, 1+len(statuses))
	args = append(args, domain)
	ph := make([]byte, 0, len(statuses)*2)
	for i, st := range statuses {
		if i > 0 {
			ph = append(ph, ',')
		}
		ph = append(ph, '?')
		args = append(args, st)
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE domain = ? AND status IN (`+string(ph)+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	for _, t := range out {
		_, err := s.DB.SQL.Exec(`
			UPDATE tasks SET status = ?, finished_at = ?, message = ?
			WHERE id = ? AND status IN (?, ?)
		`, StatusCancelled, finished, message, t.ID, StatusPending, StatusRunning)
		if err != nil {
			return out, err
		}
		_ = s.persistCommands(t.ID, StatusCancelled, t.Kind)
		_ = s.persistLogs(t.ID, StatusCancelled)
		s.clearLive(t.ID)
		s.abortRunning(t.ID)
	}
	s.notifyCancelled(out...)
	return out, nil
}

// CancelPendingScansForSeries cancels all pending scan tasks for a series (full + tip).
// Running scans are left alone. Prefer CancelPendingTipScansForSeries on unmonitor.
func (s *Store) CancelPendingScansForSeries(seriesID int64) (int64, error) {
	if seriesID <= 0 {
		return 0, fmt.Errorf("series_id required")
	}
	return s.cancelPendingScans(
		`kind = ? AND status = ? AND series_id = ?`,
		CancelReasonSeriesDeleted,
		KindScan, StatusPending, seriesID,
	)
}

// CancelPendingTipScansForSeries cancels pending tip Scan tasks only.
// Full scans keep running/queued when a series is unmonitored.
func (s *Store) CancelPendingTipScansForSeries(seriesID int64) (int64, error) {
	if seriesID <= 0 {
		return 0, fmt.Errorf("series_id required")
	}
	return s.cancelPendingScans(
		`kind = ? AND status = ? AND series_id = ?
		  AND COALESCE(json_extract(payload, '$.mode'), '') = 'scan'`,
		CancelReasonSeriesUnmonitored,
		KindScan, StatusPending, seriesID,
	)
}

// CancelPendingScansForSource cancels pending scan tasks for one source (payload source_id).
// Running scans are left alone. reason must be a closed cancel-reason code.
func (s *Store) CancelPendingScansForSource(sourceID int64, reason string) (int64, error) {
	if sourceID <= 0 {
		return 0, fmt.Errorf("source_id required")
	}
	return s.cancelPendingScans(
		`kind = ? AND status = ?
		  AND CAST(json_extract(payload, '$.source_id') AS INTEGER) = ?`,
		reason,
		KindScan, StatusPending, sourceID,
	)
}

func (s *Store) cancelPendingScans(where, reason string, args ...any) (int64, error) {
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return 0, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return 0, err
		}
		ids = append(ids, t.ID)
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		_, err := s.DB.SQL.Exec(`
			UPDATE tasks SET status = ?, finished_at = ?, message = ?
			WHERE id = ? AND status = ?
		`, StatusCancelled, finished, message, id, StatusPending)
		if err != nil {
			return int64(len(out)), err
		}
	}
	s.notifyCancelled(out...)
	return int64(len(out)), nil
}

// ListActive returns running + pending tasks with queue positions per domain.
func (s *Store) ListActive() ([]Task, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE status IN (?, ?)
		ORDER BY domain ASC, CASE status WHEN ? THEN 0 ELSE 1 END, priority DESC, id ASC
	`, StatusRunning, StatusPending, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	pos := map[string]int{}
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		pos[t.Domain]++
		t.QueuePos = pos[t.Domain]
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ListActiveFileDelete returns pending/running delete_files tasks (system lane).
func (s *Store) ListActiveFileDelete() ([]Task, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE kind = ? AND status IN (?, ?)
		ORDER BY id ASC
	`, KindDeleteFiles, StatusRunning, StatusPending)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ListActiveForSeries returns pending/running tasks tied to a series.
func (s *Store) ListActiveForSeries(seriesID int64) ([]Task, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE series_id = ? AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id ASC
	`, seriesID, StatusRunning, StatusPending, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ActiveTaskForVideo returns the best pending/running task for a video (running preferred).
// Also matches system-lane delete_files payloads that list video_id or its series.
func (s *Store) ActiveTaskForVideo(videoID int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE video_id = ? AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, videoID, StatusRunning, StatusPending, StatusRunning)
	t, err := s.scanTask(row)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil && t != nil {
		return t, nil
	}
	return s.activeFileDeleteTaskForVideo(videoID)
}

func (s *Store) activeFileDeleteTaskForVideo(videoID int64) (*Task, error) {
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, videoID).Scan(&seriesID)
	tasks, err := s.ListActiveFileDelete()
	if err != nil {
		return nil, err
	}
	var best *Task
	for i := range tasks {
		t := &tasks[i]
		sids, vids := FileDeleteIDsFromPayload(t.Payload)
		hit := false
		for _, vid := range vids {
			if vid == videoID {
				hit = true
				break
			}
		}
		if !hit && seriesID > 0 {
			for _, sid := range sids {
				if sid == seriesID {
					hit = true
					break
				}
			}
		}
		if !hit {
			continue
		}
		if t.Status == StatusRunning {
			return t, nil
		}
		if best == nil {
			best = t
		}
	}
	return best, nil
}

// SourceIDFromPayload reads source_id from a task payload JSON (scan batches).
func SourceIDFromPayload(payload string) int64 {
	var p struct {
		SourceID int64 `json:"source_id"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return 0
	}
	return p.SourceID
}

// FileDeleteIDsFromPayload reads series_ids and video_ids from a delete_files payload.
func FileDeleteIDsFromPayload(payload string) (seriesIDs, videoIDs []int64) {
	var p struct {
		SeriesIDs []int64 `json:"series_ids"`
		VideoIDs  []int64 `json:"video_ids"`
	}
	_ = json.Unmarshal([]byte(payload), &p)
	return p.SeriesIDs, p.VideoIDs
}

// URLFromPayload reads url from a task payload JSON (series meta prefetch).
func URLFromPayload(payload string) string {
	var p struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.URL)
}

// DraftTokenFromPayload reads draft_token from prefetch_add_series / prefetch_add_video payload JSON.
func DraftTokenFromPayload(payload string) string {
	var p struct {
		DraftToken string `json:"draft_token"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.DraftToken)
}

// ActiveScanForSeries returns the running or pending scan task for a series, if any.
func (s *Store) ActiveScanForSeries(seriesID int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE kind = ? AND series_id = ? AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, KindScan, seriesID, StatusRunning, StatusPending, StatusRunning)
	t, err := s.scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) scanTask(scanner interface {
	Scan(dest ...any) error
}) (*Task, error) {
	var t Task
	err := scanner.Scan(
		&t.ID, &t.Kind, &t.Status, &t.SeriesID, &t.VideoID, &t.Payload,
		&t.ErrorCode, &t.ErrorMessage, &t.Message, &t.Detail,
		&t.Progress, &t.Domain, &t.Priority, &t.CreatedAt, &t.StartedAt, &t.FinishedAt,
		&t.Origin, &t.ParentTaskID,
	)
	if err != nil {
		return nil, err
	}
	s.applyLive(&t)
	return &t, nil
}

func (s *Store) scanTaskWithExtras(scanner interface {
	Scan(dest ...any) error
}) (*Task, error) {
	var t Task
	var commandsJSON, logsJSON string
	err := scanner.Scan(
		&t.ID, &t.Kind, &t.Status, &t.SeriesID, &t.VideoID, &t.Payload,
		&t.ErrorCode, &t.ErrorMessage, &t.Message, &t.Detail,
		&t.Progress, &t.Domain, &t.Priority, &t.CreatedAt, &t.StartedAt, &t.FinishedAt,
		&t.Origin, &t.ParentTaskID,
		&commandsJSON, &logsJSON,
	)
	if err != nil {
		return nil, err
	}
	t.Commands = parseCommandsJSON(commandsJSON)
	t.Logs = parseCommandsJSON(logsJSON)
	s.applyLive(&t)
	s.applyCommands(&t)
	s.applyLogs(&t)
	return &t, nil
}

func parseCommandsJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// applyLogs overlays in-memory progress lines onto a running/pending task row.
func (s *Store) applyLogs(t *Task) {
	if s == nil || t == nil || (t.Status != StatusPending && t.Status != StatusRunning) {
		return
	}
	if lines := s.Logs.Snapshot(t.ID); len(lines) > 0 {
		t.Logs = lines
	}
}

// RequeueStaleRunning marks interrupted running tasks as pending after process restart.
func (s *Store) RequeueStaleRunning() (int64, error) {
	res, err := s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, started_at = NULL, message = 'Requeued after restart', progress = NULL
		WHERE status = ?
	`, StatusPending, StatusRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
