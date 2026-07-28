package web

import (
	"path/filepath"
	"testing"
)

func TestWebDevLoadDisk(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// Package tests run with cwd = package dir (internal/web).
	t.Setenv("CREATORR_WEB_DEV", "1")
	t.Setenv("CREATORR_WEB_DIR", root)
	if !WebDev() {
		t.Fatal("WebDev should be true")
	}
	tmpl, err := loadDisk()
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Lookup("series_list") == nil {
		t.Fatal("missing series_list template")
	}
	if tmpl.Lookup("video_list_row") == nil {
		t.Fatal("missing video_list_row partial")
	}
}
