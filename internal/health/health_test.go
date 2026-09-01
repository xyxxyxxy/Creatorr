package health_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/health"
)

func TestHealthDBAndWorker(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "creatorr.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()

	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("write yt-dlp: %v", err)
	}
	cfg := config.Config{
		DBPath:            filepath.Join(dir, "creatorr.db"),
		InitialRootFolder: filepath.Join(dir, "library"),
		ImportRoot:        filepath.Join(dir, "import"),
		YtDlpBin:          bin,
	}
	now := time.Now()
	rep := (&health.Checker{
		DB:  d,
		Cfg: cfg,
		WorkerAt: func() time.Time {
			return now
		},
	}).Run(context.Background())
	if rep.Status == health.StatusDown {
		t.Fatalf("unexpected down: %+v", rep)
	}
	var sawDB, sawWorker, sawYtDlp bool
	for _, c := range rep.Checks {
		if c.Name == "db" && c.Status == health.StatusOK {
			sawDB = true
		}
		if c.Name == "worker" && c.Status == health.StatusOK {
			sawWorker = true
		}
		if c.Name == "ytdlp" && c.Status == health.StatusOK {
			sawYtDlp = true
		}
	}
	if !sawDB || !sawWorker || !sawYtDlp {
		t.Fatalf("expected db+worker+ytdlp ok: %+v", rep.Checks)
	}
}
