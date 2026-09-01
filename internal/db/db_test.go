package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	_ "modernc.org/sqlite"
)

func TestOpenFreshSchema(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 2 {
		t.Fatalf("schema_version=%d want 2", ver)
	}
	assertColumn(t, d.SQL, "sources", "full_scan_limit", true)
	assertColumn(t, d.SQL, "sources", "scan_cutoff", false)
}

func TestMigrateV2AddsFullScanLimitDropsCutoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(`
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version (version) VALUES (1);
		CREATE TABLE root_folders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL UNIQUE,
			retention_ttl_seconds INTEGER
		);
		CREATE TABLE quality_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			format_selector TEXT NOT NULL
		);
		CREATE TABLE series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			root_id INTEGER NOT NULL REFERENCES root_folders(id),
			quality_profile_id INTEGER NOT NULL REFERENCES quality_profiles(id),
			monitored INTEGER NOT NULL DEFAULT 1,
			added_at TEXT NOT NULL
		);
		CREATE TABLE sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			label TEXT,
			kind TEXT NOT NULL DEFAULT 'feed',
			scan_cron TEXT NOT NULL DEFAULT '0 3 * * 0',
			index_as_ignored INTEGER NOT NULL DEFAULT 0,
			title_regexp_include TEXT,
			title_regexp_exclude TEXT,
			scan_cutoff TEXT,
			full_scan_done INTEGER NOT NULL DEFAULT 0,
			UNIQUE(series_id, url)
		);
		INSERT INTO root_folders (path) VALUES ('/tmp/root');
		INSERT INTO quality_profiles (name, format_selector) VALUES ('Default', 'bv*+ba/b');
		INSERT INTO series (title, root_id, quality_profile_id, added_at) VALUES ('S', 1, 1, '2026-01-01T00:00:00Z');
		INSERT INTO sources (series_id, url, scan_cutoff) VALUES (1, 'https://www.example.com/@x', '2020-01-01');
	`)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	_ = sqlDB.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	defer func() { _ = d.Close() }()

	var ver int
	if err := d.SQL.QueryRow(`SELECT version FROM schema_version`).Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != 2 {
		t.Fatalf("schema_version=%d want 2", ver)
	}
	assertColumn(t, d.SQL, "sources", "full_scan_limit", true)
	assertColumn(t, d.SQL, "sources", "scan_cutoff", false)

	var limit int
	if err := d.SQL.QueryRow(`SELECT full_scan_limit FROM sources WHERE id = 1`).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if limit != 0 {
		t.Fatalf("full_scan_limit=%d want 0 default", limit)
	}
}

func assertColumn(t *testing.T, sqlDB *sql.DB, table, column string, want bool) {
	t.Helper()
	rows, err := sqlDB.Query(`PRAGMA table_info("` + table + `")`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if found != want {
		t.Fatalf("column %s.%s present=%v want %v", table, column, found, want)
	}
}

func TestWorkerHeartbeat(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()

	at, err := d.WorkerHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if !at.IsZero() {
		t.Fatalf("expected zero heartbeat, got %v", at)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := d.TouchWorkerHeartbeat(now); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, err := d.WorkerHeartbeat()
	if err != nil {
		t.Fatalf("heartbeat after touch: %v", err)
	}
	if got.IsZero() {
		t.Fatal("expected non-zero heartbeat")
	}
}

func TestOpenBusyTimeoutOnPooledConns(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()

	ctx := context.Background()
	c1, err := d.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c1.Close() }()
	c2, err := d.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()

	readTimeout := func(c *sql.Conn) int {
		t.Helper()
		var ms int
		if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&ms); err != nil {
			t.Fatal(err)
		}
		return ms
	}
	if got := readTimeout(c1); got < 10000 {
		t.Fatalf("conn1 busy_timeout=%d want >=10000", got)
	}
	if got := readTimeout(c2); got < 10000 {
		t.Fatalf("conn2 busy_timeout=%d want >=10000", got)
	}
	var mode string
	if err := c2.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode=%q want wal", mode)
	}
}
