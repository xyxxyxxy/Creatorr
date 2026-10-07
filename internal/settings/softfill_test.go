package settings_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/testutil"
)

func TestSoftFillDefaultsOn(t *testing.T) {
	d := testutil.OpenDB(t)
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	for _, fn := range []struct {
		name string
		get  func() (bool, error)
	}{
		{"tags", func() (bool, error) { return settings.SoftFillTagsEnabled(d) }},
		{"genres", func() (bool, error) { return settings.SoftFillGenresEnabled(d) }},
		{"domain", func() (bool, error) { return settings.SoftFillDomainTagEnabled(d) }},
	} {
		on, err := fn.get()
		if err != nil || !on {
			t.Fatalf("%s: on=%v err=%v", fn.name, on, err)
		}
	}
}

func TestSoftFillBlocklistAddRemoveCaseFold(t *testing.T) {
	d := testutil.OpenDB(t)
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := settings.AddSoftFillBlock(d, settings.CatalogFieldTags, "Tea"); err != nil {
		t.Fatal(err)
	}
	blocked, err := settings.IsSoftFillBlocked(d, settings.CatalogFieldTags, "tea")
	if err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	if err := settings.AddSoftFillBlock(d, settings.CatalogFieldTags, "TEA"); err != nil {
		t.Fatal(err)
	}
	list, err := settings.ListSoftFillBlocks(d, settings.CatalogFieldTags)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err := settings.RemoveSoftFillBlock(d, settings.CatalogFieldTags, "tea"); err != nil {
		t.Fatal(err)
	}
	blocked, err = settings.IsSoftFillBlocked(d, settings.CatalogFieldTags, "Tea")
	if err != nil || blocked {
		t.Fatalf("after remove blocked=%v err=%v", blocked, err)
	}
}

func TestMoveSoftFillBlock(t *testing.T) {
	d := testutil.OpenDB(t)
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := settings.AddSoftFillBlock(d, settings.CatalogFieldGenres, "News"); err != nil {
		t.Fatal(err)
	}
	if err := settings.MoveSoftFillBlock(d, settings.CatalogFieldGenres, "News", "Current events"); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := settings.IsSoftFillBlocked(d, settings.CatalogFieldGenres, "News"); blocked {
		t.Fatal("old still blocked")
	}
	if blocked, _ := settings.IsSoftFillBlocked(d, settings.CatalogFieldGenres, "Current events"); !blocked {
		t.Fatal("new not blocked")
	}
}
