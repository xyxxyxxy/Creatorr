package episode_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library/episode"
)

func TestYearFromUpload(t *testing.T) {
	if got := episode.YearFromUpload("2024-01-15T14:30:00Z"); got != 2024 {
		t.Fatalf("got %d", got)
	}
	if got := episode.YearFromUpload(""); got != 0 {
		t.Fatalf("undated year %d", got)
	}
}

func TestYearFromCalendarDay(t *testing.T) {
	if got := episode.YearFromCalendarDay("2024-06-01"); got != 2024 {
		t.Fatalf("got %d", got)
	}
	if got := episode.YearFromCalendarDay(""); got != 0 {
		t.Fatalf("empty %d", got)
	}
}
