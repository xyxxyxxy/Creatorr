package db

import (
	"database/sql"
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
