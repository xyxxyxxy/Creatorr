package library

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// seriesNFOXML is a minimal tvshow decode for import apply.
type seriesNFOXML struct {
	XMLName       xml.Name `xml:"tvshow"`
	Title         string   `xml:"title"`
	SortTitle     string   `xml:"sorttitle"`
	OriginalTitle string   `xml:"originaltitle"`
	Plot          string   `xml:"plot"`
	Tagline       string   `xml:"tagline"`
	Studio        string   `xml:"studio"`
	Country       string   `xml:"country"`
	MPAA          string   `xml:"mpaa"`
	Premiered     string   `xml:"premiered"`
	Status        string   `xml:"status"`
	Genres        []string `xml:"genre"`
	Tags          []string `xml:"tag"`
	UniqueIDs     []struct {
		Type    string `xml:"type,attr"`
		Default string `xml:"default,attr"`
		Value   string `xml:",chardata"`
	} `xml:"uniqueid"`
	Actors []struct {
		Name  string `xml:"name"`
		Role  string `xml:"role"`
		Order int    `xml:"order"`
	} `xml:"actor"`
}

// ParsedSeriesNFO is editable show metadata read from tvshow.nfo for import preview/apply.
type ParsedSeriesNFO struct {
	Title         string
	SortTitle     string
	OriginalTitle string
	Plot          string
	Tagline       string
	Studio        string
	Country       string
	MPAA          string
	Premiered     string // YYYY-MM-DD when present
	Monitored     bool   // from <status> Continuing → true, Ended → false; default true when missing
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
}

// ParseSeriesNFOFile reads show metadata from an on-disk tvshow.nfo.
func ParseSeriesNFOFile(path string) (ParsedSeriesNFO, error) {
	var out ParsedSeriesNFO
	b, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	var doc seriesNFOXML
	if err := xml.Unmarshal(b, &doc); err != nil {
		return out, fmt.Errorf("%w: parse tvshow.nfo: %v", ErrInvalid, err)
	}
	out.Title = strings.TrimSpace(doc.Title)
	out.SortTitle = strings.TrimSpace(doc.SortTitle)
	out.OriginalTitle = strings.TrimSpace(doc.OriginalTitle)
	out.Plot = strings.TrimSpace(doc.Plot)
	out.Tagline = strings.TrimSpace(doc.Tagline)
	out.Studio = strings.TrimSpace(doc.Studio)
	out.Country = strings.TrimSpace(doc.Country)
	out.MPAA = strings.TrimSpace(doc.MPAA)
	out.Premiered = strings.TrimSpace(doc.Premiered)
	if day := UploadCalendarDate(out.Premiered); day != "" {
		out.Premiered = day
	}
	for _, g := range doc.Genres {
		if t := strings.TrimSpace(g); t != "" {
			out.Genres = append(out.Genres, t)
		}
	}
	for _, t := range doc.Tags {
		if s := strings.TrimSpace(t); s != "" {
			out.Tags = append(out.Tags, s)
		}
	}
	for i, a := range doc.Actors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		order := a.Order
		if order == 0 {
			order = i
		}
		out.Actors = append(out.Actors, SeriesActor{Name: name, Role: strings.TrimSpace(a.Role), Order: order})
	}
	for _, u := range doc.UniqueIDs {
		val := strings.TrimSpace(u.Value)
		if val == "" {
			continue
		}
		out.UniqueIDType = strings.TrimSpace(u.Type)
		out.UniqueIDValue = val
		if strings.EqualFold(strings.TrimSpace(u.Default), "true") {
			break
		}
	}
	out.Monitored = monitoredFromSeriesStatus(doc.Status)
	return out, nil
}

func monitoredFromSeriesStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ended":
		return false
	default:
		// Continuing, missing, or unknown → monitored (Create series default).
		return true
	}
}

// DiscoverSeriesFolderArt returns absolute paths for series art beside tvshow.nfo (roles only).
func DiscoverSeriesFolderArt(dir string) map[string]string {
	out := map[string]string{}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return out
	}
	for _, role := range seriesArtRoles {
		if p := findArtFile(dir, role); p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				out[role] = abs
			} else {
				out[role] = p
			}
		}
	}
	return out
}

// SeriesFolderMetaPaths lists tvshow.nfo + art files under dir (absolute paths).
func SeriesFolderMetaPaths(dir string) []string {
	var out []string
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return out
	}
	nfo := filepath.Join(dir, "tvshow.nfo")
	if fileExists(nfo) {
		if abs, err := filepath.Abs(nfo); err == nil {
			out = append(out, abs)
		} else {
			out = append(out, nfo)
		}
	}
	for _, role := range seriesArtRoles {
		if p := findArtFile(dir, role); p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				out = append(out, abs)
			} else {
				out = append(out, p)
			}
		}
	}
	return out
}
