package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// migrate applies stepwise upgrades from the stored schema_version to schemaVersion.
// Steps are idempotent where practical so interrupted upgrades can resume.
func (d *DB) migrate() error {
	ver, err := d.currentSchemaVersion()
	if err != nil {
		return err
	}
	if ver > schemaVersion {
		return fmt.Errorf("database schema version %d newer than supported %d", ver, schemaVersion)
	}
	for ver < schemaVersion {
		next := ver + 1
		switch next {
		case 2:
			if err := d.migrateTo2(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 3:
			if err := d.migrateTo3(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 4:
			if err := d.migrateTo4(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 5:
			if err := d.migrateTo5(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 6:
			if err := d.migrateTo6(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 7:
			if err := d.migrateTo7(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 8:
			if err := d.migrateTo8(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 9:
			if err := d.migrateTo9(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 10:
			if err := d.migrateTo10(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 11:
			if err := d.migrateTo11(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 12:
			if err := d.migrateTo12(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 13:
			if err := d.migrateTo13(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 14:
			if err := d.migrateTo14(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 15:
			if err := d.migrateTo15(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 16:
			if err := d.migrateTo16(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 17:
			if err := d.migrateTo17(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 18:
			if err := d.migrateTo18(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 19:
			if err := d.migrateTo19(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 20:
			if err := d.migrateTo20(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 21:
			if err := d.migrateTo21(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 22:
			if err := d.migrateTo22(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 23:
			if err := d.migrateTo23(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 24:
			if err := d.migrateTo24(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 25:
			if err := d.migrateTo25(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		case 26:
			if err := d.migrateTo26(); err != nil {
				return fmt.Errorf("migrate to %d: %w", next, err)
			}
		default:
			return fmt.Errorf("no migration defined for schema version %d", next)
		}
		if err := d.setSchemaVersion(next); err != nil {
			return fmt.Errorf("set schema version %d: %w", next, err)
		}
		ver = next
	}
	return nil
}

func (d *DB) currentSchemaVersion() (int, error) {
	var ver int
	err := d.SQL.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&ver)
	if err != nil {
		return 0, fmt.Errorf("read schema_version: %w", err)
	}
	return ver, nil
}

func (d *DB) setSchemaVersion(ver int) error {
	res, err := d.SQL.Exec(`UPDATE schema_version SET version = ?`, ver)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_, err = d.SQL.Exec(`INSERT INTO schema_version (version) VALUES (?)`, ver)
	}
	return err
}

// migrateTo2 adds sources.full_scan_limit and drops unused sources.scan_cutoff.
func (d *DB) migrateTo2() error {
	hasLimit, err := d.tableHasColumn("sources", "full_scan_limit")
	if err != nil {
		return err
	}
	if !hasLimit {
		if _, err := d.SQL.Exec(`ALTER TABLE sources ADD COLUMN full_scan_limit INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add full_scan_limit: %w", err)
		}
	}
	hasCutoff, err := d.tableHasColumn("sources", "scan_cutoff")
	if err != nil {
		return err
	}
	if hasCutoff {
		if _, err := d.SQL.Exec(`ALTER TABLE sources DROP COLUMN scan_cutoff`); err != nil {
			return fmt.Errorf("drop scan_cutoff: %w", err)
		}
	}
	return nil
}

// migrateTo3 drops source-download hold: reset held videos and remove hold history rows.
func (d *DB) migrateTo3() error {
	if _, err := d.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE status = 'wanted_source_error'`); err != nil {
		return fmt.Errorf("clear wanted_source_error: %w", err)
	}
	if _, err := d.SQL.Exec(`DELETE FROM video_history WHERE event IN ('source_failed', 'wanted_source_error')`); err != nil {
		return fmt.Errorf("delete source hold history: %w", err)
	}
	if _, err := d.SQL.Exec(`UPDATE video_history SET event = 'download_failed' WHERE event = 'wanted_download_error'`); err != nil {
		return fmt.Errorf("rewrite legacy wanted_download_error history: %w", err)
	}
	return nil
}

// migrateTo4 adds videos.acquired_via and backfills existing rows (import vs source).
func (d *DB) migrateTo4() error {
	has, err := d.tableHasColumn("videos", "acquired_via")
	if err != nil {
		return err
	}
	if !has {
		if _, err := d.SQL.Exec(`ALTER TABLE videos ADD COLUMN acquired_via TEXT NOT NULL DEFAULT 'source'`); err != nil {
			return fmt.Errorf("add acquired_via: %w", err)
		}
	}
	hasImport, err := d.tableHasColumn("videos", "import_src")
	if err != nil {
		return err
	}
	if hasImport {
		if _, err := d.SQL.Exec(`
			UPDATE videos SET acquired_via = 'import'
			WHERE import_src IS NOT NULL AND TRIM(import_src) != ''
		`); err != nil {
			return fmt.Errorf("backfill acquired_via import: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`
		UPDATE videos SET acquired_via = 'source'
		WHERE acquired_via IS NULL OR TRIM(acquired_via) = ''
	`); err != nil {
		return fmt.Errorf("backfill acquired_via source: %w", err)
	}
	return nil
}

// defaultEpisodeFormat matches settings.DefaultEpisodeFormat (kept here to avoid import cycles).
const defaultEpisodeFormat = "S{year}/S{year}E{episode} [{id}]"

// migrateTo5 adds root_folders.episode_format, copies legacy settings.episode_format, then drops that key.
func (d *DB) migrateTo5() error {
	has, err := d.tableHasColumn("root_folders", "episode_format")
	if err != nil {
		return err
	}
	if !has {
		if _, err := d.SQL.Exec(`
			ALTER TABLE root_folders ADD COLUMN episode_format TEXT NOT NULL DEFAULT '` + defaultEpisodeFormat + `'
		`); err != nil {
			return fmt.Errorf("add episode_format: %w", err)
		}
	}
	fmtStr := defaultEpisodeFormat
	var raw sql.NullString
	err = d.SQL.QueryRow(`SELECT value FROM settings WHERE key = 'episode_format'`).Scan(&raw)
	switch {
	case err == nil && raw.Valid:
		if trimmed := strings.TrimSpace(raw.String); trimmed != "" {
			fmtStr = trimmed
		}
	case errors.Is(err, sql.ErrNoRows):
		// no legacy key
	case err != nil && strings.Contains(err.Error(), "no such table"):
		// settings missing on minimal fixtures
	case err != nil:
		return fmt.Errorf("read settings episode_format: %w", err)
	}
	if _, err := d.SQL.Exec(`UPDATE root_folders SET episode_format = ?`, fmtStr); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil
		}
		return fmt.Errorf("backfill root episode_format: %w", err)
	}
	if _, err := d.SQL.Exec(`DELETE FROM settings WHERE key = 'episode_format'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete settings episode_format: %w", err)
		}
	}
	return nil
}

// migrateTo6 drops auto_ignore_media_types from series and/or sources (feature removed).
func (d *DB) migrateTo6() error {
	hasSer, err := d.tableHasColumn("series", "auto_ignore_media_types")
	if err != nil {
		return err
	}
	if hasSer {
		if _, err := d.SQL.Exec(`ALTER TABLE series DROP COLUMN auto_ignore_media_types`); err != nil {
			return fmt.Errorf("drop series.auto_ignore_media_types: %w", err)
		}
	}
	hasSrc, err := d.tableHasColumn("sources", "auto_ignore_media_types")
	if err != nil {
		return err
	}
	if hasSrc {
		if _, err := d.SQL.Exec(`ALTER TABLE sources DROP COLUMN auto_ignore_media_types`); err != nil {
			return fmt.Errorf("drop sources.auto_ignore_media_types: %w", err)
		}
	}
	return nil
}

// migrateTo7 drops videos.tool (redundant with acquired_via).
func (d *DB) migrateTo7() error {
	has, err := d.tableHasColumn("videos", "tool")
	if err != nil {
		return err
	}
	if !has {
		return nil
	}
	if _, err := d.SQL.Exec(`ALTER TABLE videos DROP COLUMN tool`); err != nil {
		return fmt.Errorf("drop videos.tool: %w", err)
	}
	return nil
}

// migrateTo8 makes videos.acquired_via nullable and clears it when never acquired.
// v4 backfilled source for every row; only pack/import should set the column.
func (d *DB) migrateTo8() error {
	has, err := d.tableHasColumn("videos", "acquired_via")
	if err != nil {
		return err
	}
	if !has {
		if _, err := d.SQL.Exec(`ALTER TABLE videos ADD COLUMN acquired_via TEXT`); err != nil {
			return fmt.Errorf("add acquired_via: %w", err)
		}
		return nil
	}
	hasNew, err := d.tableHasColumn("videos", "acquired_via_new")
	if err != nil {
		return err
	}
	if !hasNew {
		if _, err := d.SQL.Exec(`ALTER TABLE videos ADD COLUMN acquired_via_new TEXT`); err != nil {
			return fmt.Errorf("add acquired_via_new: %w", err)
		}
	}
	hasAcquiredAt, err := d.tableHasColumn("videos", "acquired_at")
	if err != nil {
		return err
	}
	if hasAcquiredAt {
		if _, err := d.SQL.Exec(`
			UPDATE videos SET acquired_via_new = acquired_via
			WHERE acquired_at IS NOT NULL AND TRIM(acquired_at) != ''
			  AND acquired_via IS NOT NULL AND TRIM(acquired_via) != ''
		`); err != nil {
			return fmt.Errorf("copy acquired_via for acquired rows: %w", err)
		}
		// Defensive: acquired_at set but via empty → live source.
		if _, err := d.SQL.Exec(`
			UPDATE videos SET acquired_via_new = 'source'
			WHERE acquired_at IS NOT NULL AND TRIM(acquired_at) != ''
			  AND (acquired_via_new IS NULL OR TRIM(acquired_via_new) = '')
		`); err != nil {
			return fmt.Errorf("backfill acquired_via source for acquired rows: %w", err)
		}
	}
	// No acquired_at in this DB shape: leave acquired_via_new NULL (unacquired).
	if _, err := d.SQL.Exec(`ALTER TABLE videos DROP COLUMN acquired_via`); err != nil {
		return fmt.Errorf("drop acquired_via: %w", err)
	}
	if _, err := d.SQL.Exec(`ALTER TABLE videos RENAME COLUMN acquired_via_new TO acquired_via`); err != nil {
		return fmt.Errorf("rename acquired_via_new: %w", err)
	}
	return nil
}

// migrateTo9 bumps exact old default episode_format to year-sequential padded default.
func (d *DB) migrateTo9() error {
	const legacy = `S{year}/S{year}E{episode} [{id}]`
	const next = `S{year}/S{year}E{episode:04} [{id}]`
	if _, err := d.SQL.Exec(`UPDATE root_folders SET episode_format = ? WHERE episode_format = ?`, next, legacy); err != nil {
		return fmt.Errorf("bump episode_format default: %w", err)
	}
	return nil
}

// migrateTo10 adds files.content_hash, clears NFO size_bytes when present, renames
// verify_failed status/notify and media_verify / integrity_check task kinds.
func (d *DB) migrateTo10() error {
	hasHash, err := d.tableHasColumn("files", "content_hash")
	if err != nil {
		return err
	}
	if !hasHash {
		if _, err := d.SQL.Exec(`ALTER TABLE files ADD COLUMN content_hash TEXT`); err != nil {
			return fmt.Errorf("add content_hash: %w", err)
		}
	}
	// NFO: never size-check; clear stored sizes except known-missing sentinel (-1).
	if _, err := d.SQL.Exec(`
		UPDATE files SET size_bytes = NULL
		WHERE kind = 'nfo' AND size_bytes IS NOT NULL AND size_bytes != -1
	`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("clear nfo size_bytes: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`UPDATE videos SET status = 'integrity_check_failed' WHERE status = 'verify_failed'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rename video status verify_failed: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`UPDATE notifications SET event = 'integrity_check_failed' WHERE event = 'verify_failed'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rename notification verify_failed: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`UPDATE tasks SET kind = 'integrity_check_initial' WHERE kind = 'media_verify'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rename tasks media_verify: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`UPDATE tasks SET kind = 'integrity_check' WHERE kind = 'verify_all_media'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rename tasks verify_all_media: %w", err)
		}
	}
	return nil
}

// migrateTo11 adds tasks.origin + parent_task_id, backfills all origin=manual,
// and strips legacy yt-dlp payload/detail trigger keys.
func (d *DB) migrateTo11() error {
	hasOrigin, err := d.tableHasColumn("tasks", "origin")
	if err != nil {
		return err
	}
	if !hasOrigin {
		if _, err := d.SQL.Exec(`ALTER TABLE tasks ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual'`); err != nil {
			return fmt.Errorf("add tasks.origin: %w", err)
		}
	}
	hasParent, err := d.tableHasColumn("tasks", "parent_task_id")
	if err != nil {
		return err
	}
	if !hasParent {
		if _, err := d.SQL.Exec(`ALTER TABLE tasks ADD COLUMN parent_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL`); err != nil {
			return fmt.Errorf("add tasks.parent_task_id: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id)`); err != nil {
		return fmt.Errorf("idx_tasks_parent: %w", err)
	}
	if _, err := d.SQL.Exec(`UPDATE tasks SET origin = 'manual'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("backfill tasks.origin manual: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`
		UPDATE tasks SET payload = json_remove(payload, '$.trigger')
		WHERE json_valid(payload) AND json_type(payload, '$.trigger') IS NOT NULL
	`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("strip payload.trigger: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`
		UPDATE tasks SET detail = json_remove(detail, '$.trigger')
		WHERE detail IS NOT NULL AND TRIM(detail) != ''
		  AND json_valid(detail) AND json_type(detail, '$.trigger') IS NOT NULL
	`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("strip detail.trigger: %w", err)
		}
	}
	return nil
}

// migrateTo13 adds tasks.logs for progress lines kept on failed tasks.
func (d *DB) migrateTo13() error {
	has, err := d.tableHasColumn("tasks", "logs")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	if _, err := d.SQL.Exec(`ALTER TABLE tasks ADD COLUMN logs TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return fmt.Errorf("add tasks.logs: %w", err)
	}
	return nil
}

// migrateTo14 adds domains.cookies_after_fail (omit stored jar until failure; cookie retry).
func (d *DB) migrateTo14() error {
	has, err := d.tableHasColumn("domains", "cookies_after_fail")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	if _, err := d.SQL.Exec(`ALTER TABLE domains ADD COLUMN cookies_after_fail INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add domains.cookies_after_fail: %w", err)
	}
	return nil
}

// migrateTo15 replaces tasks.priority with per-domain queue_seq (FIFO append / MoveToFront).
// Open tasks get queue_seq = id (claim order matches historical id order; priority ignored).
func (d *DB) migrateTo15() error {
	hasSeq, err := d.tableHasColumn("tasks", "queue_seq")
	if err != nil {
		return err
	}
	if !hasSeq {
		if _, err := d.SQL.Exec(`ALTER TABLE tasks ADD COLUMN queue_seq INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add tasks.queue_seq: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`
		UPDATE tasks
		SET queue_seq = id
		WHERE status IN ('pending', 'running')
	`); err != nil {
		return fmt.Errorf("backfill tasks.queue_seq from id: %w", err)
	}
	hasPri, err := d.tableHasColumn("tasks", "priority")
	if err != nil {
		return err
	}
	if hasPri {
		if _, err := d.SQL.Exec(`ALTER TABLE tasks DROP COLUMN priority`); err != nil {
			return fmt.Errorf("drop tasks.priority: %w", err)
		}
	}
	return nil
}

// migrateTo16 adds pack_role and special episode/feature format columns.
func (d *DB) migrateTo16() error {
	has, err := d.tableHasColumn("videos", "pack_role")
	if err != nil {
		return err
	}
	if !has {
		// Also skip if v18 already renamed to special_feature on a partial upgrade path.
		hasSF, err := d.tableHasColumn("videos", "special_feature")
		if err != nil {
			return err
		}
		if !hasSF {
			if _, err := d.SQL.Exec(`ALTER TABLE videos ADD COLUMN pack_role TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("add videos.pack_role: %w", err)
			}
		}
	}
	has, err = d.tableHasColumn("root_folders", "special_episode_format")
	if err != nil {
		return err
	}
	if !has {
		if _, err := d.SQL.Exec(`ALTER TABLE root_folders ADD COLUMN special_episode_format TEXT NOT NULL DEFAULT '[{id}]'`); err != nil {
			return fmt.Errorf("add root_folders.special_episode_format: %w", err)
		}
	}
	has, err = d.tableHasColumn("root_folders", "special_feature_format")
	if err != nil {
		return err
	}
	if !has {
		if _, err := d.SQL.Exec(`ALTER TABLE root_folders ADD COLUMN special_feature_format TEXT NOT NULL DEFAULT '{episode:02} {title:100}'`); err != nil {
			return fmt.Errorf("add root_folders.special_feature_format: %w", err)
		}
	}
	return nil
}

// migrateTo17 sets pack_role default/value to episode for regular videos (empty → episode).
func (d *DB) migrateTo17() error {
	has, err := d.tableHasColumn("videos", "pack_role")
	if err != nil {
		return err
	}
	if !has {
		return nil // already renamed in a later step, or fresh path
	}
	if _, err := d.SQL.Exec(`
		UPDATE videos SET pack_role = 'episode'
		WHERE pack_role IS NULL OR trim(pack_role) = ''
	`); err != nil {
		return fmt.Errorf("backfill videos.pack_role episode: %w", err)
	}
	return nil
}

// migrateTo18 renames videos.pack_role to special_feature.
func (d *DB) migrateTo18() error {
	hasOld, err := d.tableHasColumn("videos", "pack_role")
	if err != nil {
		return err
	}
	hasNew, err := d.tableHasColumn("videos", "special_feature")
	if err != nil {
		return err
	}
	if hasOld && !hasNew {
		if _, err := d.SQL.Exec(`ALTER TABLE videos RENAME COLUMN pack_role TO special_feature`); err != nil {
			return fmt.Errorf("rename videos.pack_role to special_feature: %w", err)
		}
		return nil
	}
	if !hasNew {
		if _, err := d.SQL.Exec(`ALTER TABLE videos ADD COLUMN special_feature TEXT NOT NULL DEFAULT 'episode'`); err != nil {
			return fmt.Errorf("add videos.special_feature: %w", err)
		}
	}
	return nil
}

// migrateTo19 drops videos.import_src (inbox path provenance; acquired_via=import is enough).
func (d *DB) migrateTo19() error {
	has, err := d.tableHasColumn("videos", "import_src")
	if err != nil {
		return err
	}
	if !has {
		return nil
	}
	if _, err := d.SQL.Exec(`ALTER TABLE videos DROP COLUMN import_src`); err != nil {
		return fmt.Errorf("drop videos.import_src: %w", err)
	}
	return nil
}

// migrateTo21 adds operator-only notes on series and videos (not NFO / packed files).
func (d *DB) migrateTo21() error {
	for _, ddl := range []struct {
		table string
		sql   string
	}{
		{"series", `ALTER TABLE series ADD COLUMN notes TEXT NOT NULL DEFAULT ''`},
		{"videos", `ALTER TABLE videos ADD COLUMN notes TEXT NOT NULL DEFAULT ''`},
	} {
		has, err := d.tableHasColumn(ddl.table, "notes")
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := d.SQL.Exec(ddl.sql); err != nil {
			return fmt.Errorf("add %s.notes: %w", ddl.table, err)
		}
	}
	return nil
}

// migrateTo22 removes legacy delete_sidecar bookkeeping tasks and sidecar_deleted history.
func (d *DB) migrateTo22() error {
	if _, err := d.SQL.Exec(`
		UPDATE tasks SET parent_task_id = NULL
		WHERE parent_task_id IN (SELECT id FROM tasks WHERE kind = 'delete_sidecar')
	`); err != nil {
		if !strings.Contains(err.Error(), "no such table") && !strings.Contains(err.Error(), "no such column") {
			return fmt.Errorf("clear delete_sidecar parent refs: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`
		DELETE FROM notifications
		WHERE task_id IN (SELECT id FROM tasks WHERE kind = 'delete_sidecar')
	`); err != nil {
		if !strings.Contains(err.Error(), "no such table") && !strings.Contains(err.Error(), "no such column") {
			return fmt.Errorf("delete delete_sidecar notifications: %w", err)
		}
	}
	hasTaskID, err := d.tableHasColumn("video_history", "task_id")
	if err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return err
		}
		hasTaskID = false
	}
	if hasTaskID {
		if _, err := d.SQL.Exec(`
			DELETE FROM video_history
			WHERE event = 'sidecar_deleted'
			   OR task_id IN (SELECT id FROM tasks WHERE kind = 'delete_sidecar')
		`); err != nil {
			if !strings.Contains(err.Error(), "no such table") {
				return fmt.Errorf("delete sidecar_deleted history: %w", err)
			}
		}
	} else if _, err := d.SQL.Exec(`DELETE FROM video_history WHERE event = 'sidecar_deleted'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete sidecar_deleted history: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`DELETE FROM tasks WHERE kind = 'delete_sidecar'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete delete_sidecar tasks: %w", err)
		}
	}
	return nil
}

// migrateTo20 renames domains.cookies_after_fail → smart_cookies and adds sources cookie_smart_* learning cols.
func (d *DB) migrateTo20() error {
	hasOld, err := d.tableHasColumn("domains", "cookies_after_fail")
	if err != nil {
		return err
	}
	hasNew, err := d.tableHasColumn("domains", "smart_cookies")
	if err != nil {
		return err
	}
	if hasOld && !hasNew {
		if _, err := d.SQL.Exec(`ALTER TABLE domains RENAME COLUMN cookies_after_fail TO smart_cookies`); err != nil {
			return fmt.Errorf("rename domains.cookies_after_fail to smart_cookies: %w", err)
		}
	} else if !hasNew {
		if _, err := d.SQL.Exec(`ALTER TABLE domains ADD COLUMN smart_cookies INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add domains.smart_cookies: %w", err)
		}
	}

	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"cookie_smart_prefer", `ALTER TABLE sources ADD COLUMN cookie_smart_prefer INTEGER NOT NULL DEFAULT 0`},
		{"cookie_smart_ring", `ALTER TABLE sources ADD COLUMN cookie_smart_ring TEXT NOT NULL DEFAULT '[]'`},
		{"cookie_smart_n", `ALTER TABLE sources ADD COLUMN cookie_smart_n INTEGER NOT NULL DEFAULT 0`},
	} {
		has, err := d.tableHasColumn("sources", col.name)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := d.SQL.Exec(col.ddl); err != nil {
			return fmt.Errorf("add sources.%s: %w", col.name, err)
		}
	}
	return nil
}

// migrateTo12 renames leftover video_history events from legacy verify_* names
// (migrateTo10 covered videos.status, notifications, and tasks.kind only).
func (d *DB) migrateTo12() error {
	if _, err := d.SQL.Exec(`UPDATE video_history SET event = 'integrity_checked' WHERE event = 'verified'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rename video_history verified: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`UPDATE video_history SET event = 'integrity_check_failed' WHERE event = 'verify_failed'`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rename video_history verify_failed: %w", err)
		}
	}
	// Task detail / history rows may still name the old kinds inside JSON.
	if _, err := d.SQL.Exec(`
		UPDATE video_history SET detail = REPLACE(detail, '"media_verify"', '"integrity_check_initial"')
		WHERE detail LIKE '%media_verify%'
	`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rewrite video_history detail media_verify: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`
		UPDATE video_history SET detail = REPLACE(detail, '"verify_all_media"', '"integrity_check"')
		WHERE detail LIKE '%verify_all_media%'
	`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("rewrite video_history detail verify_all_media: %w", err)
		}
	}
	return nil
}

func (d *DB) tableHasColumn(table, column string) (bool, error) {
	// PRAGMA table_info cannot take bound parameters for the table name.
	rows, err := d.SQL.Query(`PRAGMA table_info(` + quoteIdent(table) + `)`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	want := strings.ToLower(column)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if strings.ToLower(name) == want {
			return true, nil
		}
	}
	return false, rows.Err()
}

// quoteIdent wraps a trusted identifier for PRAGMA / DDL (not user input).
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// migrateTo23 renames video status integrity_check_failed → downloaded_integrity_failed
// (file kept; downloaded subgroup). Notify/history event names stay integrity_check_failed.
func (d *DB) migrateTo23() error {
	if _, err := d.SQL.Exec(`UPDATE videos SET status = 'downloaded_integrity_failed' WHERE status = 'integrity_check_failed'`); err != nil {
		return fmt.Errorf("rename integrity_check_failed status: %w", err)
	}
	return nil
}

// migrateTo25: tasks.interrupt_count for restart requeue tally on Tasks Explorer.
func (d *DB) migrateTo25() error {
	has, err := d.tableHasColumn("tasks", "interrupt_count")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	if _, err := d.SQL.Exec(`ALTER TABLE tasks ADD COLUMN interrupt_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add tasks.interrupt_count: %w", err)
	}
	return nil
}

// migrateTo26: per-file integrity attempt/success stamps (derived failed when checked_at > ok_at).
func (d *DB) migrateTo26() error {
	for _, col := range []string{"content_hash_checked_at", "content_hash_ok_at"} {
		has, err := d.tableHasColumn("files", col)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := d.SQL.Exec(`ALTER TABLE files ADD COLUMN ` + col + ` TEXT`); err != nil {
			return fmt.Errorf("add files.%s: %w", col, err)
		}
	}
	return nil
}

// migrateTo24: nullable videos.special_feature (regular=NULL), source catalog preset cols, seed domain into sources.tags.
func (d *DB) migrateTo24() error {
	if err := d.migrateVideosSpecialFeatureNullable(); err != nil {
		return err
	}
	if err := d.migrateSourcesCatalogCols(); err != nil {
		return err
	}
	return d.seedSourceDomainTags()
}

func (d *DB) migrateVideosSpecialFeatureNullable() error {
	has, err := d.tableHasColumn("videos", "special_feature")
	if err != nil {
		return err
	}
	if !has {
		if _, err := d.SQL.Exec(`ALTER TABLE videos ADD COLUMN special_feature TEXT`); err != nil {
			return fmt.Errorf("add videos.special_feature: %w", err)
		}
		return nil
	}
	hasNew, err := d.tableHasColumn("videos", "special_feature_new")
	if err != nil {
		return err
	}
	if !hasNew {
		if _, err := d.SQL.Exec(`ALTER TABLE videos ADD COLUMN special_feature_new TEXT`); err != nil {
			return fmt.Errorf("add videos.special_feature_new: %w", err)
		}
	}
	if _, err := d.SQL.Exec(`
		UPDATE videos SET special_feature_new = NULLIF(TRIM(special_feature), '')
		WHERE special_feature IS NOT NULL AND TRIM(special_feature) != '' AND TRIM(special_feature) != 'episode'
	`); err != nil {
		return fmt.Errorf("copy videos.special_feature_new: %w", err)
	}
	if _, err := d.SQL.Exec(`ALTER TABLE videos DROP COLUMN special_feature`); err != nil {
		return fmt.Errorf("drop videos.special_feature: %w", err)
	}
	if _, err := d.SQL.Exec(`ALTER TABLE videos RENAME COLUMN special_feature_new TO special_feature`); err != nil {
		return fmt.Errorf("rename special_feature_new: %w", err)
	}
	return nil
}

func (d *DB) migrateSourcesCatalogCols() error {
	cols := []struct {
		name string
		ddl  string
	}{
		{"studio", `ALTER TABLE sources ADD COLUMN studio TEXT NOT NULL DEFAULT ''`},
		{"country", `ALTER TABLE sources ADD COLUMN country TEXT NOT NULL DEFAULT ''`},
		{"mpaa", `ALTER TABLE sources ADD COLUMN mpaa TEXT NOT NULL DEFAULT ''`},
		{"genres", `ALTER TABLE sources ADD COLUMN genres TEXT NOT NULL DEFAULT '[]'`},
		{"tags", `ALTER TABLE sources ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'`},
		{"actors", `ALTER TABLE sources ADD COLUMN actors TEXT NOT NULL DEFAULT '[]'`},
		{"special_feature", `ALTER TABLE sources ADD COLUMN special_feature TEXT`},
	}
	for _, c := range cols {
		has, err := d.tableHasColumn("sources", c.name)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := d.SQL.Exec(c.ddl); err != nil {
			return fmt.Errorf("add sources.%s: %w", c.name, err)
		}
	}
	return nil
}

func (d *DB) seedSourceDomainTags() error {
	rows, err := d.SQL.Query(`SELECT id, url, COALESCE(tags, '[]') FROM sources`)
	if err != nil {
		return fmt.Errorf("list sources for domain tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type row struct {
		id   int64
		url  string
		tags string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.url, &r.tags); err != nil {
			return err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range list {
		host := hostnameFromURL(r.url)
		if host == "" {
			continue
		}
		merged := prependJSONStringTag(r.tags, host)
		if merged == r.tags {
			continue
		}
		if _, err := d.SQL.Exec(`UPDATE sources SET tags = ? WHERE id = ?`, merged, r.id); err != nil {
			return fmt.Errorf("seed source %d domain tag: %w", r.id, err)
		}
	}
	return nil
}

func hostnameFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	// Avoid importing net/url cycle concerns: light parse.
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		// strip port; keep IPv6 bracket form as-is if present
		if !strings.HasPrefix(rest, "[") {
			rest = rest[:i]
		}
	}
	rest = strings.TrimPrefix(strings.ToLower(rest), "www.")
	if rest == "" || rest == "localhost" || strings.HasSuffix(rest, ".local") {
		return ""
	}
	return rest
}

func prependJSONStringTag(rawJSON, tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return rawJSON
	}
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		rawJSON = "[]"
	}
	var items []string
	_ = json.Unmarshal([]byte(rawJSON), &items)
	fold := strings.ToLower(tag)
	out := []string{tag}
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" || strings.ToLower(it) == fold {
			continue
		}
		out = append(out, it)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return rawJSON
	}
	return string(b)
}
