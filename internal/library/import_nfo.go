package library

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// episodeNFOXML is a minimal episodedetails decode for import apply.
type episodeNFOXML struct {
	XMLName           xml.Name `xml:"episodedetails"`
	Title             string   `xml:"title"`
	SortTitle         string   `xml:"sorttitle"`
	OriginalTitle     string   `xml:"originaltitle"`
	Plot              string   `xml:"plot"`
	Tagline           string   `xml:"tagline"`
	Studio            string   `xml:"studio"`
	Country           string   `xml:"country"`
	MPAA              string   `xml:"mpaa"`
	Aired             string   `xml:"aired"`
	Runtime           int      `xml:"runtime"`           // Emby/Kodi: minutes
	DurationInSeconds int      `xml:"durationinseconds"` // rare top-level
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

// durationSecondsFromEpisodeNFO prefers fileinfo durationinseconds, then top-level
// durationinseconds, then runtime minutes × 60.
func durationSecondsFromEpisodeNFO(doc episodeNFOXML) int {
	if d := doc.FileInfo.StreamDetails.Video.DurationInSeconds; d > 0 {
		return d
	}
	if doc.DurationInSeconds > 0 {
		return doc.DurationInSeconds
	}
	if doc.Runtime > 0 {
		return doc.Runtime * 60
	}
	return 0
}

// ParseEpisodeNFOFile reads editable episode metadata from an on-disk .nfo.
// Season / episode / remote_id are not returned (index identity stays operator/scan owned).
// aired is YYYY-MM-DD or empty (import sets upload_date from it when present).
// durationSec is soft-fill only (NULL/0 duration_seconds); 0 when unknown.
func ParseEpisodeNFOFile(path string) (p SaveVideoMetadataParams, aired string, durationSec int, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return p, "", 0, err
	}
	var doc episodeNFOXML
	if err := xml.Unmarshal(b, &doc); err != nil {
		return p, "", 0, fmt.Errorf("%w: parse nfo: %v", ErrInvalid, err)
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
	durationSec = durationSecondsFromEpisodeNFO(doc)
	return p, aired, durationSec, nil
}

// ApplyImportNFOMetadata writes editable video columns from an episode NFO (no on-disk rewrite).
// When <aired> is present, sets upload_date from it (operator intent; overrides info.json / prior row).
// Calendar-day changes reindex season/episode for that series year.
func (s *Store) ApplyImportNFOMetadata(videoID int64, nfoPath string) error {
	_, err := s.applyImportNFOMetadata(videoID, nfoPath)
	return err
}

// applyImportNFOMetadata writes editable video columns from an episode NFO (no on-disk rewrite).
// When <aired> is present, sets upload_date from it (operator intent; overrides info.json / prior row).
// Returns video IDs whose season/episode numbers changed (caller may enqueue rename when packed).
func (s *Store) applyImportNFOMetadata(videoID int64, nfoPath string) (renameIDs []int64, err error) {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return nil, err
	}
	p, aired, durationSec, err := ParseEpisodeNFOFile(nfoPath)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	if err := s.SetDurationSecondsIfEmpty(videoID, durationSec); err != nil {
		return nil, err
	}
	if aired == "" {
		return nil, nil
	}
	stored := sidecarUploadTime(aired)
	if stored == "" {
		return nil, nil
	}
	oldDay := ""
	if v.UploadDate.Valid {
		oldDay = UploadCalendarDate(v.UploadDate.String)
	}
	newDay := UploadCalendarDate(stored)
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET upload_date = ? WHERE id = ?`, stored, videoID); err != nil {
		return nil, err
	}
	if newDay == oldDay {
		return nil, nil
	}
	years := map[int]bool{}
	if y := SeasonYearFromCalendarDay(newDay); y > 0 {
		years[y] = true
	}
	if oldDay != "" {
		if y := SeasonYearFromCalendarDay(oldDay); y > 0 {
			years[y] = true
		}
	}
	for y := range years {
		c, rerr := s.ReindexSeriesUTCYear(v.SeriesID, y)
		if rerr != nil {
			return nil, rerr
		}
		renameIDs = append(renameIDs, c...)
	}
	return uniqInt64(renameIDs), nil
}

// ApplyImportNFO applies episode NFO metadata to the video row, then regenerates the
// on-disk episode .nfo from DB (never keeps the source XML bytes as library provenance).
// When <aired> changes the calendar day and media is packed, enqueues scoped Apply rename.
func (s *Store) ApplyImportNFO(videoID int64, nfoPath string, taskID int64) error {
	nfoPath = strings.TrimSpace(nfoPath)
	if nfoPath == "" {
		return fmt.Errorf("%w: nfo path required", ErrInvalid)
	}
	renameIDs, err := s.applyImportNFOMetadata(videoID, nfoPath)
	if err != nil {
		return err
	}
	mediaPath, ok, err := s.HasPackAnchor(videoID)
	if err != nil {
		return err
	}
	if ok {
		// NFO first (above); ffprobe only when duration still empty so rewrite can emit runtime.
		_ = s.SoftFillDurationFromMedia(context.Background(), videoID, mediaPath)
	}
	if _, err := s.RewriteVideoNFO(videoID, 0); err != nil {
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
		if len(renameIDs) > 0 {
			if _, qerr := s.EnqueueRenameEpisodesVideos(renameIDs); qerr != nil {
				return qerr
			}
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
