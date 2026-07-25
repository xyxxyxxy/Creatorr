package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

func TestOpenFreshSchema(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := d.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestWorkerHeartbeat(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

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
	defer d.Close()

	ctx := context.Background()
	c1, err := d.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	c2, err := d.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

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
