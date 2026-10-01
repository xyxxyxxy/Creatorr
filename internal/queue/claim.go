package queue

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// ClearCooldown clears an in-memory domain cooldown. Returns true when an active cooldown was cleared.
func (s *Store) ClearCooldown(domain string) bool {
	domain = settings.NormalizeDomain(strings.TrimSpace(domain))
	if domain == "" || domain == SystemDomain || domain == "unknown" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.cooldown[domain]
	if !ok || !time.Now().Before(until) {
		delete(s.cooldown, domain)
		return false
	}
	delete(s.cooldown, domain)
	return true
}

// MarkDownloadNowImmediate sets payload.download_now on a pending download task.
func (s *Store) MarkDownloadNowImmediate(id int64) error {
	if id <= 0 {
		return fmt.Errorf("task id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var kind, status, payload string
	err := s.DB.SQL.QueryRow(`SELECT kind, status, payload FROM tasks WHERE id = ?`, id).Scan(&kind, &status, &payload)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task not found")
	}
	if err != nil {
		return err
	}
	if kind != KindDownload || status != StatusPending {
		return fmt.Errorf("only pending download tasks can be marked download-now")
	}
	var m map[string]any
	if strings.TrimSpace(payload) == "" {
		m = map[string]any{}
	} else if err := json.Unmarshal([]byte(payload), &m); err != nil {
		m = map[string]any{}
	}
	if m == nil {
		m = map[string]any{}
	}
	m[PayloadKeyDownloadNow] = true
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET payload = ?, message = COALESCE(NULLIF(message,''), ?) WHERE id = ? AND status = ?`,
		string(b), "Download now", id, StatusPending)
	return err
}

// ClaimNext picks the next runnable pending task respecting inactive/paused domains,
// per-domain max_parallel_tasks, and cooldown. Immediate kinds are excluded (see ClaimImmediate).
func (s *Store) ClaimNext() (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	rows, err := s.DB.SQL.Query(`
		SELECT t.id, t.kind, t.status, t.series_id, t.video_id, t.payload,
		       COALESCE(t.error_code,''), COALESCE(t.error_message,''), COALESCE(t.message,''),
		       COALESCE(t.detail,''), t.progress, t.domain, t.queue_seq, t.created_at, t.started_at, t.finished_at,
		       t.origin, t.parent_task_id
		FROM tasks t
		WHERE t.status = ?
		  AND t.kind NOT IN (?, ?, ?, ?, ?)
		  AND NOT (t.kind = ? AND `+sqlDownloadNowTrue+`)
		  AND NOT EXISTS (
		    SELECT 1 FROM domains d WHERE d.domain = t.domain AND d.active = 0
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM domain_runtime r WHERE r.domain = t.domain AND r.paused != 0
		  )
		ORDER BY t.queue_seq ASC, t.id ASC
	`, StatusPending, KindPrefetchSeriesMeta, KindPrefetchVideoMeta, KindPrefetchAddSeries, KindPrefetchAddVideo, KindProbeSourceTitle, KindDownload)
	if err != nil {
		return nil, err
	}
	return s.claimFromRows(rows, now, true)
}

// ClaimImmediate claims a pending immediate task (prefetch / probe / download-now),
// ignoring per-domain running tasks, cooldown, and soft pause. Still requires domain active.
func (s *Store) ClaimImmediate() (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	rows, err := s.DB.SQL.Query(`
		SELECT t.id, t.kind, t.status, t.series_id, t.video_id, t.payload,
		       COALESCE(t.error_code,''), COALESCE(t.error_message,''), COALESCE(t.message,''),
		       COALESCE(t.detail,''), t.progress, t.domain, t.queue_seq, t.created_at, t.started_at, t.finished_at,
		       t.origin, t.parent_task_id
		FROM tasks t
		WHERE t.status = ?
		  AND (
		    t.kind IN (?, ?, ?, ?, ?)
		    OR (t.kind = ? AND `+sqlDownloadNowTrue+`)
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM domains d WHERE d.domain = t.domain AND d.active = 0
		  )
		ORDER BY t.queue_seq ASC, t.id ASC
	`, StatusPending, KindPrefetchSeriesMeta, KindPrefetchVideoMeta, KindPrefetchAddSeries, KindPrefetchAddVideo, KindProbeSourceTitle, KindDownload)
	if err != nil {
		return nil, err
	}
	return s.claimFromRows(rows, now, false)
}

// ClaimInteractive is an alias for ClaimImmediate (prefetch path).
func (s *Store) ClaimInteractive() (*Task, error) {
	return s.ClaimImmediate()
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
			if IsDownloadNowPayload(t.Payload) && t.Kind == KindDownload {
				continue
			}
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
