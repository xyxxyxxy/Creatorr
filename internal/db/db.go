package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// schemaVersion is the latest schema. Fresh installs record this; existing DBs migrate up.
const schemaVersion = 5

// busyTimeoutMS is how long pooled connections wait on SQLITE_BUSY before failing.
// Must be set via DSN _pragma so every pool conn gets it (Exec PRAGMA only hits one conn).
const busyTimeoutMS = 10000

// DB wraps SQLite access.
type DB struct {
	SQL *sql.DB
}

// openDSN builds a modernc.org/sqlite DSN with per-connection pragmas.
// Exec("PRAGMA …") only configures one pool conn - DSN _pragma applies to every checkout.
func openDSN(path string) string {
	abs := path
	if !filepath.IsAbs(path) {
		if a, err := filepath.Abs(path); err == nil {
			abs = a
		}
	}
	// Keep query literal (do not url.QueryEscape parentheses) - modernc expects _pragma=name(value).
	// _txlock=immediate: Begin takes the write lock up front so deferred read→write
	// upgrades cannot deadlock with another writer (that returns SQLITE_BUSY immediately,
	// busy_timeout skipped). Callers wait up to busy_timeout instead.
	return fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_txlock=immediate",
		filepath.ToSlash(abs),
		busyTimeoutMS,
	)
}

// Open creates/opens the database file and applies the fresh schema.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	sqlDB, err := sql.Open("sqlite", openDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// >1 so nested QueryRow-while-rows/tx-open (maturity, domains, …) do not
	// deadlock the pool. Writers still serialize in SQLite; busy_timeout waits
	// on SQLITE_BUSY. Nested Query-while-rows-open remains unsafe across conns
	// for long holds - close rows before the next Query when possible.
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(0)

	d := &DB{SQL: sqlDB}
	if err := d.applySchema(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	return d.SQL.Close()
}

func (d *DB) Ping() error {
	return d.SQL.Ping()
}

// applySchema installs schema.sql (CREATE IF NOT EXISTS), ensures a schema_version
// row, then runs stepwise migrations for existing databases.
func (d *DB) applySchema() error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	if _, err := d.SQL.Exec(string(schema)); err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}

	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&n); err != nil {
		return fmt.Errorf("schema_version count: %w", err)
	}
	if n == 0 {
		// Fresh DB: tables already match latest schema.sql; record current version.
		if _, err := d.SQL.Exec(`INSERT INTO schema_version (version) VALUES (?)`, schemaVersion); err != nil {
			return fmt.Errorf("insert schema_version: %w", err)
		}
	}
	if err := d.migrate(); err != nil {
		return err
	}
	return nil
}

// TouchWorkerHeartbeat writes the worker heartbeat timestamp.
// Kept for schema compatibility / tests; production health uses an in-process clock.
func (d *DB) TouchWorkerHeartbeat(at time.Time) error {
	ts := at.UTC().Format(time.RFC3339Nano)
	_, err := d.SQL.Exec(`
		INSERT INTO worker_state (id, heartbeat_at) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET heartbeat_at = excluded.heartbeat_at
	`, ts)
	return err
}

// WorkerHeartbeat returns the last heartbeat, or zero time if unset.
func (d *DB) WorkerHeartbeat() (time.Time, error) {
	return d.WorkerHeartbeatContext(context.Background())
}

// WorkerHeartbeatContext is WorkerHeartbeat with a cancellable context.
func (d *DB) WorkerHeartbeatContext(ctx context.Context) (time.Time, error) {
	var s string
	err := d.SQL.QueryRowContext(ctx, `SELECT heartbeat_at FROM worker_state WHERE id = 1`).Scan(&s)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, s)
}
