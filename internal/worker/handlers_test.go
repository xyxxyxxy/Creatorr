package worker_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func TestDefaultHandlersRequireYtDlp(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := worker.DefaultHandlers(worker.Deps{
		Library: lib,
		YtDlp:   &ytdlp.Client{Bin: filepath.Join(t.TempDir(), "missing-yt-dlp")},
		TmpRoot: t.TempDir(),
	})
	if h[queue.KindScan] == nil || h[queue.KindDownload] == nil {
		t.Fatal("expected scan/download handlers")
	}
}
