package library

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// Import series draft kinds for import_plan.
const (
	ImportSeriesDraftNFO  = "nfo_folder" // requires tvshow.nfo under folder_path
	ImportSeriesDraftBare = "bare"       // title-only create; no inbox folder
)

// ImportPlanSeriesDraft is one series to create inside an import_plan task.
type ImportPlanSeriesDraft struct {
	DraftKey         string `json:"draft_key"`
	Kind             string `json:"kind"` // nfo_folder | bare; empty → nfo_folder
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
	Series          []ImportPlanSeriesDraft `json:"series"`
	Jobs            []ImportPlanJob         `json:"jobs"`
	SeriesUnitIndex int                     `json:"series_unit_index"`
	JobIndex        int                     `json:"job_index"`
	DraftSeriesIDs  map[string]int64        `json:"draft_series_ids,omitempty"`
}

func (p ImportPlanPayload) PersistMap() map[string]any {
	b, err := json.Marshal(p)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func normalizeImportSeriesDraftKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case ImportSeriesDraftBare:
		return ImportSeriesDraftBare
	default:
		return ImportSeriesDraftNFO
	}
}

// EnqueueImportPlan validates drafts/jobs and enqueues one import_plan task.
// Does not create series or video rows. series may be empty when every job
// binds or creates under an existing series_id.
func (s *Store) EnqueueImportPlan(p ImportPlanPayload) (int64, error) {
	if len(p.Jobs) == 0 {
		return 0, fmt.Errorf("%w: jobs required for import plan", ErrInvalid)
	}
	if p.DraftSeriesIDs == nil {
		p.DraftSeriesIDs = map[string]int64{}
	}
	draftKeys := map[string]struct{}{}
	bareDrafts := map[string]struct{}{}
	// folderKey under root → draft_key (intra-plan title uniqueness)
	titleSlots := map[string]string{} // "rootID|folderKey" → draft_key
	for i, d := range p.Series {
		d.DraftKey = strings.TrimSpace(d.DraftKey)
		d.Kind = normalizeImportSeriesDraftKind(d.Kind)
		d.FolderPath = strings.TrimSpace(d.FolderPath)
		d.Title = strings.TrimSpace(d.Title)
		if d.DraftKey == "" {
			return 0, fmt.Errorf("%w: series[%d] draft_key required", ErrInvalid, i)
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
		if d.Kind == ImportSeriesDraftBare {
			if d.Title == "" {
				return 0, fmt.Errorf("%w: series[%d] title required for bare draft", ErrInvalid, i)
			}
			d.FolderPath = ""
			bareDrafts[d.DraftKey] = struct{}{}
		} else {
			if d.FolderPath == "" {
				return 0, fmt.Errorf("%w: series[%d] folder_path required for nfo_folder draft", ErrInvalid, i)
			}
			nfo := filepath.Join(d.FolderPath, "tvshow.nfo")
			if !fileExists(nfo) {
				return 0, fmt.Errorf("%w: series[%d] missing tvshow.nfo under %s", ErrInvalid, i, d.FolderPath)
			}
			// Title locked to tvshow.nfo (folder basename fallback) unless client rename provided.
			diskTitle := ImportSeriesTitleFromFolder(d.FolderPath)
			if d.Title == "" {
				d.Title = diskTitle
			}
			if d.Title == "" {
				return 0, fmt.Errorf("%w: series[%d] could not resolve title", ErrInvalid, i)
			}
		}
		if _, ok := draftKeys[d.DraftKey]; ok {
			return 0, fmt.Errorf("%w: duplicate draft_key %s", ErrInvalid, d.DraftKey)
		}
		folderKey := sanitizeName(d.Title, SeriesDirMaxRunes)
		slot := fmt.Sprintf("%d|%s", d.RootID, folderKey)
		if other, ok := titleSlots[slot]; ok {
			return 0, fmt.Errorf("%w: series drafts %q and %q share the same folder name under this root", ErrInvalid, other, d.DraftKey)
		}
		if taken, err := s.seriesFolderTaken(d.RootID, d.Title, 0); err != nil {
			return 0, err
		} else if taken {
			return 0, fmt.Errorf("%w: a series with title %q already exists under the chosen root", ErrConflict, d.Title)
		}
		titleSlots[slot] = d.DraftKey
		draftKeys[d.DraftKey] = struct{}{}
		if d.DeliveryMode == "" {
			d.DeliveryMode = DeliveryVideo
		}
		p.Series[i] = d
	}
	createRemotes := map[string]int{}
	for i, j := range p.Jobs {
		hasVideo := j.VideoID > 0
		hasSeries := j.SeriesID > 0
		hasDraft := strings.TrimSpace(j.SeriesDraftKey) != ""
		if hasVideo {
			// Bind / sidecar / resume-after-create: series fields optional.
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
			if _, err := s.ValidateImportInboxPath(path); err != nil {
				return 0, fmt.Errorf("%w: jobs[%d] path: %v", ErrInvalid, i, err)
			}
		}
		for _, pth := range paths {
			if _, err := s.ValidateImportInboxPath(pth); err != nil {
				return 0, fmt.Errorf("%w: jobs[%d] paths: %v", ErrInvalid, i, err)
			}
		}
		j.Path = path
		j.Paths = paths
		j.SeriesDraftKey = strings.TrimSpace(j.SeriesDraftKey)
		j.RemoteID = strings.TrimSpace(j.RemoteID)
		if !hasVideo {
			if rid := j.RemoteID; rid != "" {
				createRemotes[rid]++
				if createRemotes[rid] > 1 {
					return 0, fmt.Errorf("%w: duplicate create remote_id %q in plan", ErrInvalid, rid)
				}
			}
		}
		if err := s.assertImportPlanJobLock(j, draftKeys, bareDrafts); err != nil {
			return 0, fmt.Errorf("jobs[%d]: %w", i, err)
		}
		p.Jobs[i] = j
	}
	p.SeriesUnitIndex = 0
	p.JobIndex = 0
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

// ApplyImportSeriesDraft creates a series from an import_plan draft (nfo_folder or bare).
func (s *Store) ApplyImportSeriesDraft(d ImportPlanSeriesDraft) (seriesID int64, err error) {
	kind := normalizeImportSeriesDraftKind(d.Kind)
	if kind == ImportSeriesDraftBare {
		title := strings.TrimSpace(d.Title)
		if title == "" {
			return 0, fmt.Errorf("%w: series title required", ErrInvalid)
		}
		monitored := d.Monitored
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
		return ser.ID, nil
	}
	return s.ApplyImportSeriesFolder(d)
}

// ApplyImportSeriesFolder creates a series from a draft and applies tvshow.nfo + art from folder.
func (s *Store) ApplyImportSeriesFolder(d ImportPlanSeriesDraft) (seriesID int64, err error) {
	folder := filepath.Clean(strings.TrimSpace(d.FolderPath))
	nfoPath := filepath.Join(folder, "tvshow.nfo")
	parsed, err := ParseSeriesNFOFile(nfoPath)
	if err != nil {
		return 0, err
	}
	title := strings.TrimSpace(d.Title)
	if title == "" {
		title = ImportSeriesTitleFromFolder(folder)
	}
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

// ImportPlanUnit is one series group inside an import_plan (draft and/or jobs).
type ImportPlanUnit struct {
	Draft    *ImportPlanSeriesDraft // nil when jobs use existing series_id only
	DraftKey string
	SeriesID int64 // set for existing-series units
	Jobs     []int // indexes into payload.Jobs
}

// BuildImportPlanUnits groups jobs by draft_key or series_id. Drafts appear in
// payload order first; then remaining series_id groups in first-seen job order.
func BuildImportPlanUnits(p ImportPlanPayload) []ImportPlanUnit {
	byDraft := map[string][]int{}
	bySeries := map[int64][]int{}
	seriesOrder := []int64{}
	for i, j := range p.Jobs {
		if dk := strings.TrimSpace(j.SeriesDraftKey); dk != "" {
			byDraft[dk] = append(byDraft[dk], i)
			continue
		}
		sid := j.SeriesID
		if j.VideoID > 0 && sid <= 0 {
			// Bind-only: resolve series from video at run time; group by video's series via 0 bucket key.
			// Use negative video id as temporary group so binds stay ordered; worker resolves series.
			key := -j.VideoID
			if _, ok := bySeries[key]; !ok {
				seriesOrder = append(seriesOrder, key)
			}
			bySeries[key] = append(bySeries[key], i)
			continue
		}
		if sid > 0 {
			if _, ok := bySeries[sid]; !ok {
				seriesOrder = append(seriesOrder, sid)
			}
			bySeries[sid] = append(bySeries[sid], i)
		}
	}
	var units []ImportPlanUnit
	seenDraft := map[string]struct{}{}
	for i := range p.Series {
		d := &p.Series[i]
		dk := d.DraftKey
		seenDraft[dk] = struct{}{}
		units = append(units, ImportPlanUnit{
			Draft:    d,
			DraftKey: dk,
			Jobs:     byDraft[dk],
		})
	}
	for dk, idxs := range byDraft {
		if _, ok := seenDraft[dk]; ok {
			continue
		}
		units = append(units, ImportPlanUnit{DraftKey: dk, Jobs: idxs})
	}
	for _, sid := range seriesOrder {
		idxs := bySeries[sid]
		u := ImportPlanUnit{Jobs: idxs}
		if sid > 0 {
			u.SeriesID = sid
		}
		units = append(units, u)
	}
	return units
}
