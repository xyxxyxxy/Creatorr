package library

import "testing"

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
	extra := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<episodedetails>
  <title>Hello</title>
  <showtitle>Show</showtitle>
  <season>2026</season>
  <episode>1</episode>
  <custom>nope</custom>
</episodedetails>
`)
	if nfoMatchesExpected(extra, meta) {
		t.Fatal("extra element must fail")
	}
}
