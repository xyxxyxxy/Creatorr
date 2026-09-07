package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func ScanHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "scan deps missing")
		}
		var payload struct {
			SourceID int64  `json:"source_id"`
			SeriesID int64  `json:"series_id"`
			Mode     string `json:"mode"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		if payload.SourceID == 0 {
			// Legacy series-wide scan: enqueue per-source full scans and exit.
			if !t.SeriesID.Valid {
				return apperrors.New(apperrors.CodeScanFailed, "scan task missing source_id")
			}
			n, _, err := d.Library.EnqueueFullScansForSeries(t.SeriesID.Int64)
			if err != nil {
				return err
			}
			progress(fmt.Sprintf("Queued %d full scans", n), ptrFloat(1))
			return nil
		}
		src, err := d.Library.GetSourceByID(payload.SourceID)
		if err != nil {
			return err
		}
		seriesID := src.SeriesID
		label := src.URL
		if src.Label.Valid && src.Label.String != "" {
			label = src.Label.String
		}
		progress(fmt.Sprintf("Listing %s", label), ptrFloat(0.05))

		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-scan-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()

		jar, err := domains.TempJarForURL(d.Library.DB, work, src.URL)
		if err != nil {
			mode := library.SourceHistModeScan
			if !src.FullScanDone {
				mode = library.SourceHistModeFull
			}
			_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanError, err.Error(), map[string]any{
				"mode": mode,
				"code": apperrors.CodeCookieInvalid,
			}, t.ID)
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}

		domain := queue.DomainFromURL(src.URL)
		lim, _ := settings.LimitsForDomain(d.Library.DB, domain)

		fullScan := !src.FullScanDone
		mode := library.SourceHistModeScan
		playlistEnd := 0
		if fullScan {
			mode = library.SourceHistModeFull
			playlistEnd = src.FullScanLimit
		}
		entries, err := listEntries(ctx, d, src.URL, jar, playlistEnd, lim)
		if err != nil {
			code, msg := classify(err)
			_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanError, msg+": "+err.Error(), map[string]any{
				"mode": mode,
				"code": code,
			}, t.ID)
			return err
		}

		var createdIDs, updatedIDs []int64
		var ignoredIndexAsIgnoredIDs []int64
		var skippedTitleInclude, skippedTitleExclude []map[string]string
		var created, updated int
		hitKnown := false

		recordSkipTitle := func(dest *[]map[string]string, remoteID, title string) {
			*dest = append(*dest, map[string]string{
				"remote_id": remoteID,
				"title":     title,
			})
		}

		recordUpsert := func(res library.UpsertResult, li library.ListedVideo) {
			if res.Skipped {
				switch res.SkipReason {
				case library.SkipReasonTitleRegexpInclude:
					recordSkipTitle(&skippedTitleInclude, li.RemoteID, li.Title)
				case library.SkipReasonTitleRegexpExclude:
					recordSkipTitle(&skippedTitleExclude, li.RemoteID, li.Title)
				}
				return
			}
			if res.Created {
				created++
				createdIDs = append(createdIDs, res.VideoID)
				if res.IgnoreReason == library.IgnoreReasonIndexAsIgnored {
					ignoredIndexAsIgnoredIDs = append(ignoredIndexAsIgnoredIDs, res.VideoID)
				}
			} else {
				updated++
				updatedIDs = append(updatedIDs, res.VideoID)
			}
		}

		if fullScan {
			progress(fmt.Sprintf("Full scan (listed=%d)", len(entries)), ptrFloat(0.2))
			nEntries := len(entries)
			for i, e := range entries {
				li := library.EntryFromYtDlp(e, src.ID)
				res, err := d.Library.UpsertListed(seriesID, li, t.ID)
				if err != nil {
					return err
				}
				recordUpsert(res, li)
				if nEntries > 0 && (i%10 == 0 || i == nEntries-1) {
					progress(fmt.Sprintf("Indexing %d/%d…", i+1, nEntries),
						ptrFloat(0.2+0.75*float64(i+1)/float64(nEntries)))
				}
			}
			_ = d.Library.MarkFullScanDone(src.ID)
			_, _ = d.Library.RecomputeSeriesPremiered(seriesID)
		} else {
			progress(fmt.Sprintf("Scan (listed=%d)", len(entries)), ptrFloat(0.2))
			var news []ytdlp.Entry
			nEntries := len(entries)
			for i, e := range entries {
				if ok, reason := library.TitlePassesFilters(src.TitleRegexpInclude, src.TitleRegexpExclude, e.Title); !ok {
					switch reason {
					case library.SkipReasonTitleRegexpInclude:
						recordSkipTitle(&skippedTitleInclude, e.ID, e.Title)
					case library.SkipReasonTitleRegexpExclude:
						recordSkipTitle(&skippedTitleExclude, e.ID, e.Title)
					}
					continue // walk past; do not treat as known tip stop
				}
				known, err := d.Library.VideoExistsByRemote(seriesID, e.ID)
				if err != nil {
					return err
				}
				if known {
					hitKnown = true
					break
				}
				news = append(news, e)
				if nEntries > 0 && (i%20 == 0 || i == nEntries-1) {
					progress(fmt.Sprintf("Walking tip %d/%d…", i+1, nEntries),
						ptrFloat(0.2+0.3*float64(i+1)/float64(nEntries)))
				}
			}
			nNews := len(news)
			for i, e := range news {
				li := library.EntryFromYtDlp(e, src.ID)
				res, err := d.Library.UpsertListed(seriesID, li, t.ID)
				if err != nil {
					return err
				}
				recordUpsert(res, li)
				if nNews > 0 && (i%5 == 0 || i == nNews-1) {
					progress(fmt.Sprintf("Indexing new %d/%d…", i+1, nNews),
						ptrFloat(0.5+0.45*float64(i+1)/float64(nNews)))
				}
			}
		}

		scanMsg := fmt.Sprintf("Scan: indexed %d videos (%d new)", created+updated, created)
		if n := len(skippedTitleInclude); n > 0 {
			scanMsg += fmt.Sprintf(", %d skipped by title include", n)
		}
		if n := len(skippedTitleExclude); n > 0 {
			scanMsg += fmt.Sprintf(", %d skipped by title exclude", n)
		}
		if n := len(ignoredIndexAsIgnoredIDs); n > 0 {
			scanMsg += fmt.Sprintf(", %d marked as ignored", n)
		}

		histDetail := map[string]any{
			"mode":                         mode,
			"created":                      created,
			"updated":                      updated,
			"created_ids":                  createdIDs,
			"updated_ids":                  updatedIDs,
			"skipped_title_regexp_include": skippedTitleInclude,
			"skipped_title_regexp_exclude": skippedTitleExclude,
			"ignored_index_as_ignored_ids": ignoredIndexAsIgnoredIDs,
			"hit_known":                    hitKnown,
			"full_scan_limit":              playlistEnd,
		}
		_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanned, scanMsg, histDetail, t.ID)

		msg := scanMsg
		detailBytes, _ := json.Marshal(map[string]any{
			"created":                      created,
			"updated":                      updated,
			"created_ids":                  createdIDs,
			"updated_ids":                  updatedIDs,
			"skipped_title_regexp_include": skippedTitleInclude,
			"skipped_title_regexp_exclude": skippedTitleExclude,
			"ignored_index_as_ignored_ids": ignoredIndexAsIgnoredIDs,
			"source_id":                    src.ID,
			"full":                         fullScan,
			"hit_known":                    hitKnown,
			"full_scan_limit":              playlistEnd,
		})
		_ = d.Library.Queue.UpdateProgress(t.ID, msg, ptrFloat(1))
		_ = d.Library.Queue.SetDetail(t.ID, string(detailBytes))
		progress(msg, ptrFloat(1))

		return nil
	}
}
