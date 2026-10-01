package queue_test

import (
	"errors"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestSeriesMoveExclusivity(t *testing.T) {
	s := openStore(t)
	seriesOf := func(remote string) int64 {
		var id int64
		if err := s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, seedVideo(t, s, remote)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	s1, s2, s3 := seriesOf("m1"), seriesOf("m2"), seriesOf("m3")
	move := func(series int64) error {
		_, err := s.Enqueue(queue.EnqueueParams{
			Origin: queue.OriginManual, Kind: queue.KindSeriesMove, Domain: queue.SystemDomain, SeriesID: series,
		})
		return err
	}
	sys := func(kind string) error {
		_, err := s.Enqueue(queue.EnqueueParams{Origin: queue.OriginManual, Kind: kind, Domain: queue.SystemDomain})
		return err
	}

	// Other path-touching kinds block a new move.
	if err := sys(queue.KindSyncFiles); err != nil {
		t.Fatal(err)
	}
	if err := move(s1); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("move while sync_files open: %v", err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE tasks SET status = 'done' WHERE kind = ?`, queue.KindSyncFiles); err != nil {
		t.Fatal(err)
	}

	if err := move(s1); err != nil {
		t.Fatal(err)
	}
	// Same series twice and any other series_move are duplicates (path-touching exclusivity).
	if err := move(s1); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("second move same series: %v", err)
	}
	if err := move(s2); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("move other series while one open: %v", err)
	}
	// Open move blocks the other path-touching kinds.
	for _, k := range []string{queue.KindRenameEpisodes, queue.KindRegenerateNFO, queue.KindSyncFiles, queue.KindRetentionDelete} {
		if err := sys(k); !errors.Is(err, queue.ErrDuplicate) {
			t.Fatalf("%s while series_move open: %v", k, err)
		}
	}
	// Downloads for that series are refused too; other series fine.
	dl := func(series int64) error {
		_, err := s.Enqueue(queue.EnqueueParams{
			Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", SeriesID: series,
		})
		return err
	}
	if err := dl(s1); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("download during move: %v", err)
	}
	if err := dl(s3); err != nil {
		t.Fatalf("download other series: %v", err)
	}
	if busy, err := s.PathTouchingSystemBusy(); err != nil || !busy {
		t.Fatalf("PathTouchingSystemBusy=%v err=%v", busy, err)
	}
}
