package library_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestListProfilesAlphabetical(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	lib := library.NewStore(d, queue.NewStore(d))

	for _, name := range []string{"zebra", "Alpha", "best", "1080p"} {
		if _, err := lib.CreateProfile(name, "bv*+ba"); err != nil {
			t.Fatal(err)
		}
	}
	list, err := lib.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1080p", "Alpha", "best", "zebra"}
	if len(list) != len(want) {
		t.Fatalf("len=%d want %d", len(list), len(want))
	}
	for i := range want {
		if list[i].Name != want[i] {
			t.Fatalf("list[%d]=%q want %q", i, list[i].Name, want[i])
		}
	}
}

func TestProfileInfoCardsRequireReencode(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	lib := library.NewStore(d, queue.NewStore(d))

	_, err = lib.CreateProfileFull("t", "bv*+ba/b", 0, 0, nil, []string{"sponsor"}, false, true, false)
	if err == nil {
		t.Fatal("expected error when info_cards without reencode_cut")
	}

	p, err := lib.CreateProfileFull("t2", "bv*+ba/b", 0, 0, nil, []string{"sponsor"}, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !p.SponsorBlockReencodeCut || p.SponsorBlockInfoCards {
		t.Fatalf("got reenc=%v cards=%v", p.SponsorBlockReencodeCut, p.SponsorBlockInfoCards)
	}

	p2, err := lib.CreateProfileFull("t3", "bv*+ba/b", 0, 0, nil, []string{"sponsor"}, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !p2.SponsorBlockReencodeCut || !p2.SponsorBlockInfoCards {
		t.Fatalf("got reenc=%v cards=%v", p2.SponsorBlockReencodeCut, p2.SponsorBlockInfoCards)
	}

	off := false
	on := true
	_, err = lib.UpdateProfileParams(p2.ID, library.UpdateProfileParams{
		SponsorBlockReencodeCut: &off,
		SponsorBlockInfoCards:   &on,
	})
	if err == nil {
		t.Fatal("expected update reject cards without reencode")
	}
}
