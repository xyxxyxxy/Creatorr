package settings_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestParsePositiveInt(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
		ok   bool
	}{
		{"1", 1, true},
		{" 8 ", 8, true},
		{"0", 0, false},
		{"-2", 0, false},
		{"", 0, false},
		{"x", 0, false},
	} {
		got, err := settings.ParsePositiveInt(tc.raw, "field")
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParsePositiveInt(%q) = %d, %v; want %d ok=%v", tc.raw, got, err, tc.want, tc.ok)
		}
	}
}

func TestValidateConcurrencyLimits(t *testing.T) {
	for _, tc := range []struct {
		queue, parallel int
		ok              bool
	}{
		{8, 2, true},
		{2, 2, true},
		{2, 3, false},
		{0, 1, false},
		{4, 0, false},
	} {
		err := settings.ValidateConcurrencyLimits(tc.queue, tc.parallel)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateConcurrencyLimits(%d, %d) err=%v want ok=%v", tc.queue, tc.parallel, err, tc.ok)
		}
	}
}

func TestRateLimitOff(t *testing.T) {
	for _, s := range []string{"", " ", "0", "off", "None", "UNLIMITED"} {
		if !settings.RateLimitOff(s) {
			t.Errorf("RateLimitOff(%q) = false", s)
		}
	}
	for _, s := range []string{"500K", "2M"} {
		if settings.RateLimitOff(s) {
			t.Errorf("RateLimitOff(%q) = true", s)
		}
	}
}
