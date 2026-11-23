package library_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestIsCrossDevice(t *testing.T) {
	if library.IsCrossDevice(nil) {
		t.Fatal("nil")
	}
	if library.IsCrossDevice(errors.New("other")) {
		t.Fatal("other")
	}
	link := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.EXDEV}
	if !library.IsCrossDevice(link) {
		t.Fatal("LinkError EXDEV")
	}
	if !library.IsCrossDevice(syscall.EXDEV) {
		t.Fatal("bare EXDEV")
	}
}

func TestCopyDirThenRemove(t *testing.T) {
	src := t.TempDir()
	dstParent := t.TempDir()
	dst := filepath.Join(dstParent, "out")
	sub := filepath.Join(src, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("bbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := library.CopyDirThenRemove(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}
	gotA, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil || string(gotA) != "aaa" {
		t.Fatalf("a.txt=%q err=%v", gotA, err)
	}
	gotB, err := os.ReadFile(filepath.Join(dst, "nested", "b.txt"))
	if err != nil || string(gotB) != "bbb" {
		t.Fatalf("b.txt=%q err=%v", gotB, err)
	}
}

func TestCopyDirThenRemoveCancel(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := library.CopyDirThenRemove(ctx, src, dst)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want canceled", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must remain: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("dest must be cleaned: %v", err)
	}
}

func TestCopyDirThenRemoveRejectsSymlink(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	target := filepath.Join(src, "real.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}
	err := library.CopyDirThenRemove(context.Background(), src, dst)
	if err == nil {
		t.Fatal("want unsupported type error")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must remain: %v", err)
	}
}

func TestMoveSeriesFolderEXDEVFallback(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := s.GetRoot(rootID)
	oldDir := filepath.Join(root.Path, "Show")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "ep.mkv"), []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	newRoot, err := s.CreateRoot("other", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE series SET root_id = ? WHERE id = ?`, newRoot.ID, ser.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSeries(ser.ID, false)

	prev := library.SwapSeriesFolderRenamer(func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	})
	t.Cleanup(func() { library.SwapSeriesFolderRenamer(prev) })

	if err := s.MoveSeriesFolder(context.Background(), got, "Show", rootID); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(newRoot.Path, "Show")
	if _, err := os.Stat(filepath.Join(newDir, "ep.mkv")); err != nil {
		t.Fatalf("dest media: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}
}

func TestMoveSeriesFolderOrphanDest(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := s.GetRoot(rootID)
	oldDir := filepath.Join(root.Path, "Show")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "ep.mkv"), []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	newRoot, err := s.CreateRoot("other", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(newRoot.Path, "Show")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "junk"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE series SET root_id = ? WHERE id = ?`, newRoot.ID, ser.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSeries(ser.ID, false)
	if err := s.MoveSeriesFolder(context.Background(), got, "Show", rootID); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(orphan, "ep.mkv"))
	if err != nil || string(body) != "good" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(orphan, "junk")); !os.IsNotExist(err) {
		t.Fatal("orphan junk should be gone")
	}
}
