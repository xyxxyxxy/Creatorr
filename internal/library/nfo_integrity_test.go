package library

import (
	"bytes"
	"strings"
	"testing"
)

func TestNFOMatchesExpectedExtrasRejected(t *testing.T) {
	meta := EpisodeNFO{
		Title:       "Hello",
		SeriesTitle: "Show",
		Season:      2026,
		Episode:     1,
	}
	good := FormatEpisodeNFO(meta)
	if !nfoMatchesExpected(good, meta) {
		t.Fatal("expected match for formatter output")
	}
	if bytes.Contains(good, []byte("<showtitle>")) {
		t.Fatal("formatter must omit showtitle")
	}
	extra := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<episodedetails>
  <title>Hello</title>
  <season>2026</season>
  <episode>1</episode>
  <plot>Hello</plot>
  <custom>nope</custom>
</episodedetails>
`)
	if nfoMatchesExpected(extra, meta) {
		t.Fatal("extra element must fail")
	}
}

func TestNFOMatchesExpectedIgnoresLegacyShowtitle(t *testing.T) {
	meta := EpisodeNFO{
		Title:       "Hello",
		SeriesTitle: "Show",
		Season:      2026,
		Episode:     1,
	}
	want := FormatEpisodeNFO(meta)
	// Insert legacy showtitle after title; other fields match expected.
	legacy := []byte(strings.Replace(
		string(want),
		"</title>\n",
		"</title>\n  <showtitle>Show</showtitle>\n",
		1,
	))
	if !bytes.Contains(legacy, []byte("<showtitle>")) {
		t.Fatal("fixture missing showtitle")
	}
	if !nfoMatchesExpected(legacy, meta) {
		t.Fatal("legacy showtitle on disk must not fail integrity")
	}
}
