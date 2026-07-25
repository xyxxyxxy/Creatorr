package library_test

import (
	"path/filepath"
	"testing"
)

func TestCreateRootRejectsRelativePath(t *testing.T) {
	s := openLib(t)
	if _, err := s.CreateRoot("rel", "var/media/library", nil); err == nil {
		t.Fatal("want error for relative path")
	}
}

func TestCreateRootEmptyNameUsesPathBase(t *testing.T) {
	s := openLib(t)
	dir := t.TempDir()
	root, err := s.CreateRoot("", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Base(dir)
	if root.Name != want {
		t.Fatalf("name=%q want %q", root.Name, want)
	}
}

func TestUpdateRootEmptyNameUsesPathBase(t *testing.T) {
	s := openLib(t)
	dir := t.TempDir()
	root, err := s.CreateRoot("old", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	updated, err := s.UpdateRoot(root.ID, &empty, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Base(dir)
	if updated.Name != want {
		t.Fatalf("name=%q want %q", updated.Name, want)
	}
}
