package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestRetentionDaysSecondsRoundTrip(t *testing.T) {
	if got := library.RetentionSecondsFromDays(7); got != 7*24*60*60 {
		t.Fatalf("7 days → %d", got)
	}
	if got := library.RetentionDaysFromSeconds(7 * 24 * 60 * 60); got != 7 {
		t.Fatalf("7d seconds → %d days", got)
	}
	if got := library.RetentionDaysFromSeconds(1); got != 1 {
		t.Fatalf("1 second ceil → %d want 1", got)
	}
	if got := library.RetentionDaysFromSeconds(0); got != 0 {
		t.Fatalf("0 → %d", got)
	}
	if got := library.RetentionSecondsFromDays(0); got != 0 {
		t.Fatalf("0 days → %d", got)
	}
}
