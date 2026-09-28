package library

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// ImportPlanSeriesDraft is one series to create inside an import_plan task.
type ImportPlanSeriesDraft struct {
	DraftKey         string `json:"draft_key"`
	FolderPath       string `json:"folder_path"`
	Title            string `json:"title"`
	RootID           int64  `json:"root_id"`
	QualityProfileID int64  `json:"quality_profile_id"`
	DeliveryMode     string `json:"delivery_mode"`
	Monitored        bool   `json:"monitored"`
}

// ImportPlanJob is one video bind/create inside an import_plan task.
type ImportPlanJob struct {
	Path           string   `json:"path"`
	Paths          []string `json:"paths"`
	VideoID        int64    `json:"video_id"`
	SeriesID       int64    `json:"series_id"`
	SeriesDraftKey string   `json:"series_draft_key"`
	Title          string   `json:"title"`
	RemoteID       string   `json:"remote_id"`
	HandlerID      string   `json:"handler_id"`
	WebpageURL     string   `json:"source_url"`
	UploadDate     string   `json:"upload_date"`
	Description    string   `json:"description"`
	Replace        bool     `json:"replace"`
}

// ImportPlanPayload is the KindImportPlan task payload.
type ImportPlanPayload struct {
	Series []ImportPlanSeriesDraft `json:"series"`
	Jobs   []ImportPlanJob         `json:"jobs"`
}

// EnqueueImportPlan validates drafts/jobs and enqueues one import_plan task.
// Does not create series or video rows.
func (s *Store) EnqueueImportPlan(p ImportPlanPayload) (int64, error) {
	if len(p.Series) == 0 {
		return 0, fmt.Errorf("%w: series drafts required for import plan", ErrInvalid)
	}
	if len(p.Jobs) == 0 {
		return 0, fmt.Errorf("%w: jobs required for import plan", ErrInvalid)
	}
	draftKeys := map[string]struct{}{}
	for i, d := range p.Series {
		d.DraftKey = strings.TrimSpace(d.DraftKey)
		d.FolderPath = strings.TrimSpace(d.FolderPath)
		if d.DraftKey == "" || d.FolderPath == "" {
			return 0, fmt.Errorf("%w: series[%d] draft_key and folder_path required", ErrInvalid, i)
		}
		if d.RootID <= 0 || d.QualityProfileID <= 0 {
			return 0, fmt.Errorf("%w: series[%d] root_id and quality_profile_id required", ErrInvalid, i)
		}
		if _, err := s.GetRoot(d.RootID); err != nil {
			return 0, fmt.Errorf("%w: series[%d] root: %v", ErrInvalid, i, err)
		}
		if _, err := s.GetProfile(d.QualityProfileID); err != nil {
			return 0, fmt.Errorf("%w: series[%d] quality profile", ErrInvalid, i)
		}
		nfo := filepath.Join(d.FolderPath, "tvshow.nfo")
		if !fileExists(nfo) {
			return 0, fmt.Errorf("%w: series[%d] missing tvshow.nfo under %s", ErrInvalid, i, d.FolderPath)
		}
		// Title locked to tvshow.nfo (folder basename fallback); ignore client rename.
		d.Title = ImportSeriesTitleFromFolder(d.FolderPath)
		if d.Title == "" {
			return 0, fmt.Errorf("%w: series[%d] could not resolve title", ErrInvalid, i)
		}
		if _, ok := draftKeys[d.DraftKey]; ok {
			return 0, fmt.Errorf("%w: duplicate draft_key %s", ErrInvalid, d.DraftKey)
		}
		draftKeys[d.DraftKey] = struct{}{}
		p.Series[i] = d
	}
	for i, j := range p.Jobs {
		hasVideo := j.VideoID > 0
		hasSeries := j.SeriesID > 0
		hasDraft := strings.TrimSpace(j.SeriesDraftKey) != ""
		if hasVideo {
			if hasSeries || hasDraft {
				return 0, fmt.Errorf("%w: jobs[%d] video_id cannot combine with series_id/series_draft_key", ErrInvalid, i)
			}
		} else if hasSeries == hasDraft {
			return 0, fmt.Errorf("%w: jobs[%d] need exactly one of series_id or series_draft_key when creating", ErrInvalid, i)
		}
		if hasDraft {
			if _, ok := draftKeys[strings.TrimSpace(j.SeriesDraftKey)]; !ok {
				return 0, fmt.Errorf("%w: jobs[%d] unknown series_draft_key", ErrInvalid, i)
			}
		}
		path := strings.TrimSpace(j.Path)
		var paths []string
		for _, pth := range j.Paths {
			pth = strings.TrimSpace(pth)
			if pth != "" {
				paths = append(paths, pth)
			}
		}
		if hasVideo {
			if len(paths) == 0 && path == "" {
				return 0, fmt.Errorf("%w: jobs[%d] path or paths required", ErrInvalid, i)
			}
		} else if path == "" {
			return 0, fmt.Errorf("%w: jobs[%d] path required when creating", ErrInvalid, i)
		}
		if path != "" {
			if _, _, err := s.ValidateImportSourcePath(path); err != nil {
				return 0, fmt.Errorf("%w: jobs[%d] path: %v", ErrInvalid, i, err)
			}
		}
		for _, pth := range paths {
			if _, _, err := s.ValidateImportSourcePath(pth); err != nil {
				return 0, fmt.Errorf("%w: jobs[%d] paths: %v", ErrInvalid, i, err)
			}
		}
		j.Path = path
		j.Paths = paths
		j.SeriesDraftKey = strings.TrimSpace(j.SeriesDraftKey)
		if err := s.assertImportPlanJobLock(j, draftKeys); err != nil {
			return 0, fmt.Errorf("jobs[%d]: %w", i, err)
		}
		p.Jobs[i] = j
	}
	b, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		return 0, err
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:  queue.OriginManual,
		Kind:    queue.KindImportPlan,
		Domain:  queue.SystemDomain,
		Payload: payload,
	})
}

// ImportSeriesTitleFromFolder returns tvshow.nfo <title>, else the folder basename.
func ImportSeriesTitleFromFolder(folder string) string {
	folder = filepath.Clean(strings.TrimSpace(folder))
	if folder == "" || folder == "." {
		return ""
	}
	parsed, err := ParseSeriesNFOFile(filepath.Join(folder, "tvshow.nfo"))
	if err == nil {
		if t := strings.TrimSpace(parsed.Title); t != "" {
			return t
		}
	}
	base := filepath.Base(folder)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

// ApplyImportSeriesFolder creates a series from a draft and applies tvshow.nfo + art from folder.
func (s *Store) ApplyImportSeriesFolder(d ImportPlanSeriesDraft) (seriesID int64, err error) {
	folder := filepath.Clean(strings.TrimSpace(d.FolderPath))
	nfoPath := filepath.Join(folder, "tvshow.nfo")
	parsed, err := ParseSeriesNFOFile(nfoPath)
	if err != nil {
		return 0, err
	}
	title := ImportSeriesTitleFromFolder(folder)
	if title == "" {
		return 0, fmt.Errorf("%w: series title required", ErrInvalid)
	}
	monitored := d.Monitored
	// Operator choice from confirm modal wins; d.Monitored is always set by API mapper.
	ser, err := s.CreateSeries(CreateSeriesParams{
		Title:            title,
		RootID:           d.RootID,
		QualityProfileID: d.QualityProfileID,
		Monitored:        monitored,
		DeliveryMode:     NormalizeDeliveryMode(d.DeliveryMode),
	})
	if err != nil {
		return 0, err
	}
	seriesID = ser.ID
	root, err := s.GetRoot(d.RootID)
	if err != nil {
		return seriesID, err
	}
	absRoot, err := filepath.Abs(root.Path)
	if err != nil {
		absRoot = root.Path
	}
	target := filepath.Clean(SeriesDir(absRoot, title))
	// Never rename the whole inbox tree here: jobs still hold the original paths, and a
	// successful rename would leave unpackaged media under SeriesDir. Copy art only;
	// PackMedia moves media; RemoveImportSeriesFolderIfDrained clears the drained inbox shell.
	artSrc := map[string]string{}
	if target != folder {
		artSrc = DiscoverSeriesFolderArt(folder)
	}
	params := SaveSeriesMetadataParams{
		Plot:          parsed.Plot,
		SortTitle:     parsed.SortTitle,
		OriginalTitle: parsed.OriginalTitle,
		Studio:        parsed.Studio,
		Genres:        parsed.Genres,
		Tags:          parsed.Tags,
		UniqueIDType:  parsed.UniqueIDType,
		UniqueIDValue: parsed.UniqueIDValue,
		Actors:        parsed.Actors,
		Tagline:       parsed.Tagline,
		Country:       parsed.Country,
		MPAA:          parsed.MPAA,
		ArtSrc:        artSrc,
	}
	if err := s.SaveSeriesMetadata(seriesID, params); err != nil {
		return seriesID, err
	}
	return seriesID, nil
}
