package integrity_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library/integrity"
)

func TestDeriveFileIntegrity(t *testing.T) {
	same := "2026-10-02T12:00:00Z"
	later := "2026-10-02T13:00:00Z"
	cases := []struct {
		name    string
		checked string
		okAt    string
		want    integrity.FileIntegrityState
	}{
		{"never", "", "", integrity.FileIntegrityNever},
		{"never checked empty ok", "", same, integrity.FileIntegrityNever},
		{"ok equal", same, same, integrity.FileIntegrityOK},
		{"failed ok unset", same, "", integrity.FileIntegrityFailed},
		{"failed checked newer", later, same, integrity.FileIntegrityFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := integrity.DeriveFileIntegrity(tc.checked, tc.okAt)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
