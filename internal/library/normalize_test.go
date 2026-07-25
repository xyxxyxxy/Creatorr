package library

import (
	"errors"
	"testing"
)

func TestNormalizeSourceURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.Example.com/@x", "https://example.com/@x"},
		{"https://example.com/@x", "https://example.com/@x"},
		{"https://www.example.com/watch/1", "https://example.com/watch/1"},
		{"https://m.example.com/watch?v=1", "https://m.example.com/watch?v=1"},
		{"  https://WWW.Example.com/a  ", "https://example.com/a"},
	}
	for _, tc := range cases {
		if got := normalizeSourceURL(tc.in); got != tc.want {
			t.Fatalf("normalizeSourceURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateSourceURL(t *testing.T) {
	ok := []string{
		"https://example.com/@x",
		"http://example.com/watch?v=1",
		"https://www.Example.com/a",
	}
	for _, in := range ok {
		if err := ValidateSourceURL(normalizeSourceURL(in)); err != nil {
			t.Fatalf("ValidateSourceURL(%q): %v", in, err)
		}
	}
	bad := []string{"", "sdasd", "example.com/x", "ftp://example.com/x", "://example.com", "http://"}
	for _, in := range bad {
		if err := ValidateSourceURL(normalizeSourceURL(in)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ValidateSourceURL(%q) want ErrInvalid, got %v", in, err)
		}
	}
}
