package web

import "testing"

func TestProgressActive(t *testing.T) {
	cases := []struct {
		p    *float64
		want bool
	}{
		{nil, false},
		{ptrF(0), false},
		{ptrF(1), false},
		{ptrF(0.01), true},
		{ptrF(0.99), true},
	}
	for _, tc := range cases {
		got := tc.p != nil && *tc.p > 0 && *tc.p < 1
		if got != tc.want {
			t.Fatalf("p=%v got %v want %v", tc.p, got, tc.want)
		}
	}
}

func ptrF(v float64) *float64 { return &v }
