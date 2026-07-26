package library

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// episodeNFOXML is a minimal episodedetails decode for import apply.
type episodeNFOXML struct {
	XMLName       xml.Name `xml:"episodedetails"`
	Title         string   `xml:"title"`
	SortTitle     string   `xml:"sorttitle"`
	OriginalTitle string   `xml:"originaltitle"`
	Plot          string   `xml:"plot"`
	Tagline       string   `xml:"tagline"`
	Studio        string   `xml:"studio"`
	Country       string   `xml:"country"`
	MPAA          string   `xml:"mpaa"`
	Aired         string   `xml:"aired"`
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

// ParseEpisodeNFOFile reads editable episode metadata from an on-disk .nfo.
// Season / episode / remote_id are not returned (index identity stays operator/scan owned).
// aired is YYYY-MM-DD or empty (soft-fill upload_date only when the video has none).
func ParseEpisodeNFOFile(path string) (p SaveVideoMetadataParams, aired string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return p, "", err
	}
	var doc episodeNFOXML
	if err := xml.Unmarshal(b, &doc); err != nil {
		return p, "", fmt.Errorf("%w: parse nfo: %v", ErrInvalid, err)
	}
	p.Title = strings.TrimSpace(doc.Title)
	p.SortTitle = strings.TrimSpace(doc.SortTitle)
	p.OriginalTitle = strings.TrimSpace(doc.OriginalTitle)
	p.Plot = strings.TrimSpace(doc.Plot)
	p.Tagline = strings.TrimSpace(doc.Tagline)
	p.Studio = strings.TrimSpace(doc.Studio)
	p.Country = strings.TrimSpace(doc.Country)
	p.MPAA = strings.TrimSpace(doc.MPAA)
	for _, g := range doc.Genres {
		if t := strings.TrimSpace(g); t != "" {
			p.Genres = append(p.Genres, t)
		}
	}
	for _, t := range doc.Tags {
		if s := strings.TrimSpace(t); s != "" {
			p.Tags = append(p.Tags, s)
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
		p.Actors = append(p.Actors, SeriesActor{Name: name, Role: strings.TrimSpace(a.Role), Order: order})
	}
	for _, u := range doc.UniqueIDs {
		val := strings.TrimSpace(u.Value)
		if val == "" {
			continue
		}
		p.UniqueIDType = strings.TrimSpace(u.Type)
		p.UniqueIDValue = val
		if strings.EqualFold(strings.TrimSpace(u.Default), "true") {
			break
		}
	}
	aired = strings.TrimSpace(doc.Aired)
	return p, aired, nil
}

// ApplyImportNFOMetadata writes editable video columns from an episode NFO (no on-disk rewrite).
// Soft-fills upload_date from <aired> only when the video has no upload_date yet.
func (s *Store) ApplyImportNFOMetadata(videoID int64, nfoPath string) error {
	return s.applyImportNFOMetadata(videoID, nfoPath)
}

// applyImportNFOMetadata writes editable video columns from an episode NFO (no on-disk rewrite).
// Soft-fills upload_date from <aired> only when the video has no upload_date yet.
func (s *Store) applyImportNFOMetadata(videoID int64, nfoPath string) error {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	p, aired, err := ParseEpisodeNFOFile(nfoPath)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = v.Title
	}
	sortTitle := omitWhenEqualTitle(p.SortTitle, title)
	origTitle := omitWhenEqualTitle(p.OriginalTitle, title)
	uidType, uidVal := coalesceUniqueID(p.UniqueIDType, p.UniqueIDValue, v.UniqueIDType, v.UniqueIDValue)
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  title = ?, description = ?,
		  sorttitle = ?, originaltitle = ?, studio = ?,
		  genres = ?, tags = ?, uniqueid_type = ?, uniqueid_value = ?,
		  actors = ?, tagline = ?, country = ?, mpaa = ?
		WHERE id = ?
	`, title, strings.TrimSpace(p.Plot),
		sortTitle, origTitle, strings.TrimSpace(p.Studio),
		encodeStringSlice(p.Genres), encodeStringSlice(p.Tags),
		uidType, uidVal,
		encodeActors(p.Actors), strings.TrimSpace(p.Tagline), strings.TrimSpace(p.Country),
		strings.TrimSpace(p.MPAA), videoID)
	if err != nil {
		return err
	}
	if aired == "" {
		return nil
	}
	needDate := !v.UploadDate.Valid || strings.TrimSpace(v.UploadDate.String) == ""
	if !needDate {
		return nil
	}
	stored := sidecarUploadTime(aired)
	if stored == "" {
		return nil
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET upload_date = ? WHERE id = ?`, stored, videoID)
	return err
}

// ApplyImportNFO applies episode NFO metadata to the video row, then regenerates the
// on-disk episode .nfo from DB (never keeps the source XML bytes as library provenance).
func (s *Store) ApplyImportNFO(videoID int64, nfoPath string, taskID int64) error {
	nfoPath = strings.TrimSpace(nfoPath)
	if nfoPath == "" {
		return fmt.Errorf("%w: nfo path required", ErrInvalid)
	}
	if err := s.applyImportNFOMetadata(videoID, nfoPath); err != nil {
		return err
	}
	if _, err := s.RewriteVideoNFO(videoID, 0); err != nil {
		return err
	}
	mediaPath, ok, err := s.HasPackAnchor(videoID)
	if err != nil {
		return err
	}
	if ok {
		libNFO := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + ".nfo"
		srcAbs, aerr := filepath.Abs(nfoPath)
		if aerr != nil {
			srcAbs = nfoPath
		}
		libAbs, aerr := filepath.Abs(libNFO)
		if aerr != nil {
			libAbs = libNFO
		}
		if srcAbs != libAbs {
			_ = os.Remove(nfoPath)
		}
		// Ensure files row even when RewriteVideoNFO skipped write (bytes already matched).
		if err := s.RegisterFileKind(videoID, libNFO, "nfo"); err != nil {
			return err
		}
	}
	if taskID > 0 {
		if err := s.AddVideoHistory(videoID, "nfo_applied", "Episode metadata applied from NFO; library NFO regenerated", map[string]any{
			"source": nfoPath,
		}, taskID); err != nil {
			return err
		}
	}
	return nil
}
