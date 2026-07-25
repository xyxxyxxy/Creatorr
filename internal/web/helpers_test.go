package web

import "testing"

func TestDisplayURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"https://example.com/v", "example.com/v"},
		{"https://www.example.com/v", "example.com/v"},
		{"HTTPS://WWW.Example.com/V", "Example.com/V"},
		{"http://www.example.com/v", "http://example.com/v"},
		{"http://example.com/v", "http://example.com/v"},
		{"ftp://files.example.com/a", "ftp://files.example.com/a"},
		{"www.example.com/x", "example.com/x"},
		{"  https://www.x.test  ", "x.test"},
		{"", ""},
		{"not-a-url", "not-a-url"},
	}
	for _, tc := range cases {
		if got := DisplayURL(tc.in); got != tc.want {
			t.Errorf("DisplayURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
