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
	if !(VideoListFilter{Statuses: []string{"wanted"}}).Active() {
		t.Fatal("status should be active")
	}
}
