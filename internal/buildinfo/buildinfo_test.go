package buildinfo

import "testing"

func TestShortRevision(t *testing.T) {
	orig := Revision
	t.Cleanup(func() { Revision = orig })

	cases := []struct {
		in, want string
	}{
		{"", "unknown"},
		{"  ", "unknown"},
		{"abc1234", "abc1234"},
		{"abcdef0123456789", "abcdef0"},
		{"unknown", "unknown"},
		{"main", "main"},
	}
	for _, tc := range cases {
		Revision = tc.in
		if got := ShortRevision(); got != tc.want {
			t.Fatalf("ShortRevision(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCommitURL(t *testing.T) {
	orig := Revision
	t.Cleanup(func() { Revision = orig })

	Revision = "deadbeefcafebabe"
	want := GitHubURL + "/commit/deadbeefcafebabe"
	if got := CommitURL(); got != want {
		t.Fatalf("CommitURL=%q want %q", got, want)
	}

	Revision = "unknown"
	if got := CommitURL(); got != "" {
		t.Fatalf("CommitURL(unknown)=%q want empty", got)
	}

	Revision = "main"
	if got := CommitURL(); got != "" {
		t.Fatalf("CommitURL(main)=%q want empty", got)
	}
}
