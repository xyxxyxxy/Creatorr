package library

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"sort"
	"strings"
)

// nfoMatchesExpected reports whether disk NFO matches expected EpisodeNFO by
// structural/value compare (not byte-equal to FormatEpisodeNFO). Extra unknown
// elements or attributes fail. Whitespace/pretty-print differences are ignored.
func nfoMatchesExpected(disk []byte, meta EpisodeNFO) bool {
	wantBytes := FormatEpisodeNFO(meta)
	var want, got episodeNFOXMLCompare
	if err := xml.Unmarshal(wantBytes, &want); err != nil {
		return false
	}
	if err := xml.Unmarshal(disk, &got); err != nil {
		return false
	}
	if !episodeNFOXMLEqual(want, got) {
		return false
	}
	return nfoDiskHasOnlyExpectedElements(disk)
}

// nfoDiskMatchesVideo loads disk NFO beside packed media and compares to DB metadata.
func (s *Store) nfoDiskMatchesVideo(videoID int64) (match bool, path string, err error) {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return false, "", err
	}
	mediaPath, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return false, "", err
	}
	if !ok || mediaPath == "" {
		return true, "", nil // no media → nothing to check
	}
	meta, nfoPath, err := s.episodeNFOBeside(v, mediaPath)
	if err != nil {
		return false, "", err
	}
	b, err := os.ReadFile(nfoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nfoPath, nil
		}
		return false, nfoPath, err
	}
	return nfoMatchesExpected(b, meta), nfoPath, nil
}

// episodeNFOXMLCompare extends import decode with season/showtitle for integrity.
type episodeNFOXMLCompare struct {
	XMLName           xml.Name `xml:"episodedetails"`
	Title             string   `xml:"title"`
	SortTitle         string   `xml:"sorttitle"`
	OriginalTitle     string   `xml:"originaltitle"`
	ShowTitle         string   `xml:"showtitle"`
	Season            int      `xml:"season"`
	Episode           int      `xml:"episode"`
	Plot              string   `xml:"plot"`
	Tagline           string   `xml:"tagline"`
	Studio            string   `xml:"studio"`
	Country           string   `xml:"country"`
	MPAA              string   `xml:"mpaa"`
	Aired             string   `xml:"aired"`
	Runtime           int      `xml:"runtime"`
	DurationInSeconds int      `xml:"durationinseconds"`
	Genres            []string `xml:"genre"`
	Tags              []string `xml:"tag"`
	UniqueIDs         []struct {
		Type    string `xml:"type,attr"`
		Default string `xml:"default,attr"`
		Value   string `xml:",chardata"`
	} `xml:"uniqueid"`
	Actors []struct {
		Name  string `xml:"name"`
		Role  string `xml:"role"`
		Order int    `xml:"order"`
	} `xml:"actor"`
	FileInfo struct {
		StreamDetails struct {
			Video struct {
				DurationInSeconds int `xml:"durationinseconds"`
			} `xml:"video"`
		} `xml:"streamdetails"`
	} `xml:"fileinfo"`
}

func episodeNFOXMLEqual(a, b episodeNFOXMLCompare) bool {
	if strings.TrimSpace(a.Title) != strings.TrimSpace(b.Title) {
		return false
	}
	if strings.TrimSpace(a.SortTitle) != strings.TrimSpace(b.SortTitle) {
		return false
	}
	if strings.TrimSpace(a.OriginalTitle) != strings.TrimSpace(b.OriginalTitle) {
		return false
	}
	if strings.TrimSpace(a.ShowTitle) != strings.TrimSpace(b.ShowTitle) {
		return false
	}
	if a.Season != b.Season || a.Episode != b.Episode {
		return false
	}
	if strings.TrimSpace(a.Plot) != strings.TrimSpace(b.Plot) {
		return false
	}
	if strings.TrimSpace(a.Tagline) != strings.TrimSpace(b.Tagline) {
		return false
	}
	if strings.TrimSpace(a.Studio) != strings.TrimSpace(b.Studio) {
		return false
	}
	if strings.TrimSpace(a.Country) != strings.TrimSpace(b.Country) {
		return false
	}
	if strings.TrimSpace(a.MPAA) != strings.TrimSpace(b.MPAA) {
		return false
	}
	if strings.TrimSpace(a.Aired) != strings.TrimSpace(b.Aired) {
		return false
	}
	if a.Runtime != b.Runtime {
		return false
	}
	if !stringMultisetEqual(a.Genres, b.Genres) || !stringMultisetEqual(a.Tags, b.Tags) {
		return false
	}
	if !uniqueIDEqual(a, b) {
		return false
	}
	if !actorsEqual(a, b) {
		return false
	}
	if a.FileInfo.StreamDetails.Video.DurationInSeconds != b.FileInfo.StreamDetails.Video.DurationInSeconds {
		return false
	}
	return true
}

func stringMultisetEqual(a, b []string) bool {
	norm := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, s := range in {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		sort.Strings(out)
		return out
	}
	aa, bb := norm(a), norm(b)
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func uniqueIDEqual(a, b episodeNFOXMLCompare) bool {
	pick := func(doc episodeNFOXMLCompare) (typ, def, val string) {
		for _, u := range doc.UniqueIDs {
			v := strings.TrimSpace(u.Value)
			if v == "" {
				continue
			}
			return strings.TrimSpace(u.Type), strings.TrimSpace(u.Default), v
		}
		return "", "", ""
	}
	at, ad, av := pick(a)
	bt, bd, bv := pick(b)
	return at == bt && strings.EqualFold(ad, bd) && av == bv
}

func actorsEqual(a, b episodeNFOXMLCompare) bool {
	type key struct {
		Name  string
		Role  string
		Order int
	}
	norm := func(doc episodeNFOXMLCompare) []key {
		var out []key
		for i, act := range doc.Actors {
			name := strings.TrimSpace(act.Name)
			if name == "" {
				continue
			}
			order := act.Order
			if order == 0 {
				order = i
			}
			out = append(out, key{Name: name, Role: strings.TrimSpace(act.Role), Order: order})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Name != out[j].Name {
				return out[i].Name < out[j].Name
			}
			if out[i].Role != out[j].Role {
				return out[i].Role < out[j].Role
			}
			return out[i].Order < out[j].Order
		})
		return out
	}
	aa, bb := norm(a), norm(b)
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

var nfoAllowedElems = map[string]map[string]struct{}{
	"episodedetails": {
		"title": {}, "sorttitle": {}, "originaltitle": {}, "showtitle": {},
		"season": {}, "episode": {}, "plot": {}, "tagline": {}, "studio": {},
		"genre": {}, "tag": {}, "aired": {}, "runtime": {}, "country": {}, "mpaa": {},
		"uniqueid": {}, "actor": {}, "fileinfo": {},
	},
	"actor":     {"name": {}, "role": {}, "order": {}},
	"fileinfo":  {"streamdetails": {}},
	"streamdetails": {"video": {}},
	"video":     {"durationinseconds": {}},
	"uniqueid":  {}, // chardata only; attrs checked separately
}

var nfoAllowedAttrs = map[string]map[string]struct{}{
	"uniqueid": {"type": {}, "default": {}},
}

// nfoDiskHasOnlyExpectedElements fails when disk has unknown elements/attrs.
func nfoDiskHasOnlyExpectedElements(disk []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(disk))
	var stack []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if len(stack) == 0 {
				if name != "episodedetails" {
					return false
				}
				stack = append(stack, name)
				continue
			}
			parent := stack[len(stack)-1]
			allowed, ok := nfoAllowedElems[parent]
			if !ok {
				return false
			}
			if _, ok := allowed[name]; !ok {
				return false
			}
			if attrs, ok := nfoAllowedAttrs[name]; ok {
				for _, a := range t.Attr {
					an := strings.ToLower(a.Name.Local)
					if _, ok := attrs[an]; !ok {
						return false
					}
				}
			} else if len(t.Attr) > 0 {
				// no attrs allowed on this element
				for _, a := range t.Attr {
					// ignore xmlns
					if strings.EqualFold(a.Name.Space, "xmlns") || a.Name.Local == "xmlns" {
						continue
					}
					return false
				}
			}
			stack = append(stack, name)
		case xml.EndElement:
			if len(stack) == 0 {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
}
