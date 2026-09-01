package db

import (
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
