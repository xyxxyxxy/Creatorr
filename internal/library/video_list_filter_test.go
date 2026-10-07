package library

import "testing"

func TestLikeContainsPattern(t *testing.T) {
	got := likeContainsPattern(`a%b_c\d`)
	want := `%a\%b\_c\\d%`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestVideoListFilterActive(t *testing.T) {
	if (VideoListFilter{}).Active() {
		t.Fatal("empty should be inactive")
	}
	if !(VideoListFilter{Title: "  hi "}).Active() {
		t.Fatal("title should be active")
	}
	if (VideoListFilter{Title: "  hi "}).MenuActive() {
		t.Fatal("title alone should not be menu-active")
	}
	if !(VideoListFilter{Statuses: []string{"wanted"}}).Active() {
		t.Fatal("status should be active")
	}
	if !(VideoListFilter{Statuses: []string{"wanted"}}).MenuActive() {
		t.Fatal("status should be menu-active")
	}
	if !(VideoListFilter{Years: []int{2024}}).Active() {
		t.Fatal("year should be active")
	}
	if !(VideoListFilter{Empty: []string{PresenceUploadDate}}).Active() {
		t.Fatal("empty upload_date should be active")
	}
	if !(VideoListFilter{PackRoles: []string{PackRoleRegular}}).Active() {
		t.Fatal("pack role should be active")
	}
	if !(VideoListFilter{PackRoles: []string{VideoPackRoleAnySpecial}}).Active() {
		t.Fatal("any special should be active")
	}
}
