package ytdlp

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestPathsForLayoutLocal(t *testing.T) {
	p := PathsForLayout(false)
	if p.Managed != filepath.Join("var", "data", "bin", "yt-dlp") {
		t.Fatalf("managed %q", p.Managed)
	}
	if p.Bootstrap != "" {
		t.Fatalf("bootstrap %q", p.Bootstrap)
	}
}

func TestPathsForLayoutContainer(t *testing.T) {
	p := PathsForLayout(true)
	if p.Managed != "/data/bin/yt-dlp" {
		t.Fatalf("managed %q", p.Managed)
	}
	if p.Bootstrap != DefaultBootstrapBin {
		t.Fatalf("bootstrap %q", p.Bootstrap)
	}
}
