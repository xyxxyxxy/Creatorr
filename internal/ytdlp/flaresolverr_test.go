package ytdlp

import (
	"strings"
	"testing"
)

func TestMergeNetscapeJarAppendsAndOverrides(t *testing.T) {
	existing := []byte("# Netscape HTTP Cookie File\n" +
		"example.com\tFALSE\t/\tFALSE\t0\tsession\tOLD\n" +
		"example.com\tFALSE\t/\tFALSE\t0\tkeepme\tSTILLHERE\n")
	fresh := []flareCookie{
		{Name: "session", Value: "NEW", Domain: "example.com", Path: "/"},
		{Name: "cf_clearance", Value: "abc", Domain: ".example.com", Path: "/", Secure: true, Expires: 1999999999},
	}
	merged := string(mergeNetscapeJar(existing, fresh))

	if !strings.Contains(merged, "session\tNEW") {
		t.Fatalf("expected updated session cookie, got:\n%s", merged)
	}
	if strings.Contains(merged, "session\tOLD") {
		t.Fatalf("stale session cookie value should have been replaced:\n%s", merged)
	}
	if !strings.Contains(merged, "keepme\tSTILLHERE") {
		t.Fatalf("unrelated existing cookie should be preserved:\n%s", merged)
	}
	if !strings.Contains(merged, "cf_clearance\tabc") {
		t.Fatalf("expected new cf_clearance cookie, got:\n%s", merged)
	}
	if !strings.Contains(merged, ".example.com\tTRUE\t/\tTRUE\t1999999999\tcf_clearance\tabc") {
		t.Fatalf("expected well-formed Netscape line for cf_clearance, got:\n%s", merged)
	}
}

func TestMergeNetscapeJarEmptyExisting(t *testing.T) {
	fresh := []flareCookie{{Name: "a", Value: "1", Domain: "example.com"}}
	merged := string(mergeNetscapeJar(nil, fresh))
	if !strings.Contains(merged, "a\t1") {
		t.Fatalf("expected fresh cookie in merged jar, got:\n%s", merged)
	}
}

func TestMergeNetscapeJarSkipsUnnamedCookies(t *testing.T) {
	fresh := []flareCookie{{Name: "", Value: "1", Domain: "example.com"}}
	merged := string(mergeNetscapeJar(nil, fresh))
	lines := 0
	for _, l := range strings.Split(strings.TrimSpace(merged), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines++
		}
	}
	if lines != 0 {
		t.Fatalf("expected no cookie lines, got:\n%s", merged)
	}
}
