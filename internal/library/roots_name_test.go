package library_test

import (
	"testing"
)

func TestCreateRootRejectsRelativePath(t *testing.T) {
	s := openLib(t)
	if _, err := s.CreateRoot("rel", "var/media/library", nil); err == nil {
		t.Fatal("want error for relative path")
	}
}

func TestCreateRootEmptyNameAllowed(t *testing.T) {
	s := openLib(t)
	dir := t.TempDir()
	root, err := s.CreateRoot("", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.Name != "" {
		t.Fatalf("name=%q want empty", root.Name)
	}
}

func TestUpdateRootEmptyNameAllowed(t *testing.T) {
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
	if updated.Name != "" {
		t.Fatalf("name=%q want empty", updated.Name)
	}
}

func TestCreateRootAllowsDuplicateEmptyNames(t *testing.T) {
	s := openLib(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	if _, err := s.CreateRoot("", dir1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRoot("", dir2, nil); err != nil {
		t.Fatal(err)
	}
}
