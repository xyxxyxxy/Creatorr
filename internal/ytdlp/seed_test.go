package ytdlp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveBinPrefersAbsCandidate(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := BinCandidates
	BinCandidates = []string{bin, "yt-dlp"}
	t.Cleanup(func() { BinCandidates = old })

	got, err := ResolveBin()
	if err != nil {
		t.Fatal(err)
	}
	if got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
}

func TestResolveBinFallsBackToPATH(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	old := BinCandidates
	BinCandidates = []string{filepath.Join(dir, "missing"), "yt-dlp"}
	t.Cleanup(func() { BinCandidates = old })

	got, err := ResolveBin()
	if err != nil {
		t.Fatal(err)
	}
	want, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveBinMissing(t *testing.T) {
	old := BinCandidates
	BinCandidates = []string{filepath.Join(t.TempDir(), "nope")}
	t.Cleanup(func() { BinCandidates = old })
	if _, err := ResolveBin(); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsurePluginsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins")
	if err := EnsurePluginsDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBinaryOK(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	// yt-dlp uses --version (not -v, which is verbose).
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 2024.01.01\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ver, err := VerifyBinary(bin)
	if err != nil {
		t.Fatal(err)
	}
	if ver != "2024.01.01" {
		t.Fatalf("version %q", ver)
	}
}

func TestVerifyBinaryMissing(t *testing.T) {
	_, err := VerifyBinary(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyBinaryBroken(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBinary(bin); err == nil {
		t.Fatal("expected error")
	}
}
