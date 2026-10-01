package library

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// CreateImportVideoParams creates an indexed video for an unmatched import.
type CreateImportVideoParams struct {
	SeriesID    int64
	Title       string
	RemoteID    string // empty → derived from path/sidecars
	HandlerID   string // site hint for history only (not a DB column)
	WebpageURL  string
	UploadDate  string // required after merge (RFC3339 UTC; sidecars / UI date-only adapted)
	Description string
	PackRole    string // optional; empty → detect from path under series folder
}

// CreateImportVideo inserts a wanted video under seriesID from path metadata (no enqueue).
// Used by EnqueueImportCreate and import_plan worker.
func (s *Store) CreateImportVideo(path string, p CreateImportVideoParams) (videoID int64, abs string, meta importMeta, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, "", meta, fmt.Errorf("%w: path required", ErrInvalid)
	}
	if p.SeriesID <= 0 {
		return 0, "", meta, fmt.Errorf("%w: series_id required", ErrInvalid)
	}
	if _, err := s.GetSeries(p.SeriesID, false); err != nil {
		return 0, "", meta, err
	}
	abs, err = s.ValidateImportMediaPath(path)
	if err != nil {
		return 0, "", meta, err
	}
	rawRole := strings.TrimSpace(p.PackRole)
	packRole := NormalizePackRole(rawRole)
	if rawRole == "" {
		if ser, serr := s.GetSeries(p.SeriesID, false); serr == nil {
			if root, rerr := s.GetRoot(ser.RootID); rerr == nil {
				packRole = DetectPackRoleFromPath(SeriesDir(root.Path, ser.Title), abs)
			}
		}
	}
	if err := ValidatePackRole(packRole); err != nil {
		return 0, "", meta, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	hints := extractImportIDs(abs)
	meta = readImportMeta(abs, hints)
	if strings.TrimSpace(p.Title) != "" {
		meta.Title = strings.TrimSpace(p.Title)
	}
	if strings.TrimSpace(p.RemoteID) != "" {
		meta.RemoteID = strings.TrimSpace(p.RemoteID)
	}
	if strings.TrimSpace(p.HandlerID) != "" {
		meta.HandlerID = strings.TrimSpace(p.HandlerID)
	}
	if strings.TrimSpace(p.WebpageURL) != "" {
		meta.WebpageURL = strings.TrimSpace(p.WebpageURL)
	}
	if strings.TrimSpace(p.UploadDate) != "" {
		meta.UploadDate = sidecarUploadTime(p.UploadDate)
	}
	if meta.UploadDate == "" {
		meta.UploadDate = fileModTimeUploadDate(abs)
	}
	if meta.UploadDate == "" {
		return 0, "", meta, fmt.Errorf("%w: upload_date required for unmatched import", ErrInvalid)
	}
	if strings.TrimSpace(p.Description) != "" {
		meta.Description = strings.TrimSpace(p.Description)
	}
	if meta.Title == "" {
		return 0, "", meta, fmt.Errorf("%w: title required for unmatched import", ErrInvalid)
	}
	assignFromID := meta.RemoteID == ""
	if assignFromID {
		tmp, err := tempVideoRemoteID()
		if err != nil {
			return 0, "", meta, err
		}
		meta.RemoteID = tmp
	}
	if meta.HandlerID == "" {
		meta.HandlerID = "yt-dlp"
	}
	var season, episode int
	var seasonVal, episodeVal any
	if IsSpecialEpisode(packRole) || IsSpecialFeature(packRole) {
		// Numbers assigned by pack-role reindex after insert.
	} else {
		var aerr error
		season, episode, aerr = s.AssignSeasonEpisode(p.SeriesID, meta.UploadDate, 0, 0)
		if aerr != nil {
			return 0, "", meta, aerr
		}
		if meta.UploadDate != "" {
			seasonVal = season
			episodeVal = episode
		}
	}
	var uploadVal, webpage any
	if meta.UploadDate != "" {
		uploadVal = meta.UploadDate
	}
	if meta.WebpageURL != "" {
		webpage = meta.WebpageURL
	}
	var res sql.Result
	res, err = s.DB.SQL.Exec(`
		INSERT INTO videos (
		  series_id, source_id, remote_id, title, upload_date,
		  source_url, status, season, episode, description, thumbnail_url, special_feature
		) VALUES (?, NULL, ?, ?, ?, ?, 'wanted', ?, ?, ?, NULL, ?)
	`, p.SeriesID, meta.RemoteID, meta.Title, uploadVal, webpage, seasonVal, episodeVal, meta.Description, packRole)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return 0, "", meta, fmt.Errorf("%w: video with this remote_id already exists in series", ErrConflict)
		}
		return 0, "", meta, err
	}
	videoID, _ = res.LastInsertId()
	if assignFromID {
		if err := s.setVideoRemoteIDToPK(videoID); err != nil {
			_, _ = s.DB.SQL.Exec(`DELETE FROM videos WHERE id = ?`, videoID)
			return 0, "", meta, err
		}
		meta.RemoteID = strconv.FormatInt(videoID, 10)
	}
	if IsSpecialEpisode(packRole) || IsSpecialFeature(packRole) {
		changed, rerr := s.ReindexPackRoleBucket(p.SeriesID, packRole)
		if rerr != nil {
			_, _ = s.DB.SQL.Exec(`DELETE FROM videos WHERE id = ?`, videoID)
			return 0, "", meta, rerr
		}
		_ = s.repackEpisodeNumberChanges(changed, 0)
	} else if meta.UploadDate != "" {
		changed, rerr := s.ReindexSeriesUTCYear(p.SeriesID, SeasonYearFromUpload(meta.UploadDate))
		if rerr != nil {
			_, _ = s.DB.SQL.Exec(`DELETE FROM videos WHERE id = ?`, videoID)
			return 0, "", meta, rerr
		}
		_ = s.repackEpisodeNumberChanges(changed, 0)
	}
	return videoID, abs, meta, nil
}

// EnqueueImportCreate creates a new video under seriesID from path metadata, then enqueues import.
func (s *Store) EnqueueImportCreate(path string, p CreateImportVideoParams) (taskID, videoID int64, err error) {
	absCheck, err := s.ValidateImportMediaPath(path)
	if err != nil {
		return 0, 0, err
	}
	if err := s.assertImportPathAllowsSeries(absCheck, p.SeriesID); err != nil {
		return 0, 0, err
	}
	videoID, abs, meta, err := s.CreateImportVideo(path, p)
	if err != nil {
		return 0, 0, err
	}
	taskID, err = s.EnqueueImport(abs, videoID, false)
	if err != nil {
		// Best-effort cleanup so a failed enqueue does not leave an orphan wanted row.
		_, _ = s.DB.SQL.Exec(`DELETE FROM videos WHERE id = ?`, videoID)
		return 0, 0, err
	}
	_ = s.AddVideoHistory(videoID, "import_created", "Created from unmatched import", map[string]any{
		"path":      abs,
		"remote_id": meta.RemoteID,
		"site":      meta.HandlerID,
	}, taskID)
	return taskID, videoID, nil
}

// EnqueueImport queues an import task that packs media from the import inbox into
// the series folder. Sidecar paths attach to a video that already has media.
// After a successful pack the worker enqueues integrity_check_initial when the
// series quality profile has File integrity on (ignores mature-only timing).
// When replace is true and the video already has packed media, existing library
// media (and companion sidecars) are removed during the import task.
func (s *Store) EnqueueImport(path string, videoID int64, replace bool) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("%w: path required", ErrInvalid)
	}
	abs, err := s.ValidateImportInboxPath(path)
	if err != nil {
		return 0, err
	}
	role, _ := ClassifyImportFile(filepath.Base(abs))
	if role == ImportRoleJSON {
		return 0, fmt.Errorf("%w: info.json cannot be imported alone (provenance travels with media)", ErrInvalid)
	}
	if IsImportSidecarRole(role) {
		return s.EnqueueAttachSidecars(videoID, []string{abs})
	}
	if role != ImportRoleVideo {
		return 0, fmt.Errorf("%w: only media or sidecar files can be imported", ErrInvalid)
	}
	abs, err = s.ValidateImportMediaPath(path)
	if err != nil {
		return 0, err
	}
	if err := s.assertImportPathAllowsVideo(abs, videoID); err != nil {
		return 0, err
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	_ = s.applyDetectedPackRole(v, abs)
	hasMedia := false
	if _, ok, err := s.HasVideoFile(videoID); err != nil {
		return 0, err
	} else if ok {
		if !replace {
			return 0, fmt.Errorf("%w: video already has a file on disk", ErrConflict)
		}
		hasMedia = true
	}
	busy, err := s.hasPendingImport(videoID, abs)
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: import already queued", ErrConflict)
	}
	msg := fmt.Sprintf("Import %s", filepath.Base(abs))
	if hasMedia {
		msg = fmt.Sprintf("Replace %s", filepath.Base(abs))
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:   queue.OriginManual,
		Kind:     queue.KindImport,
		Domain:   queue.SystemDomain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Payload: map[string]any{
			"path": abs, "video_id": videoID,
			"replace": hasMedia,
		},
		Message: msg,
	})
}

// EnqueueAttachSidecars queues a task that attaches inbox sidecar paths to a
// video that already has media. Paths are moved beside the media on run.
func (s *Store) EnqueueAttachSidecars(videoID int64, paths []string) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	if len(paths) == 0 {
		return 0, fmt.Errorf("%w: paths required", ErrInvalid)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	mediaPath, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("%w: video has no media file to attach sidecars to", ErrInvalid)
	}
	absPaths := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, p := range paths {
		abs, err := s.ValidateImportInboxPath(p)
		if err != nil {
			return 0, err
		}
		role, _ := ClassifyImportFile(filepath.Base(abs))
		if role == ImportRoleJSON {
			return 0, fmt.Errorf("%w: info.json cannot be attached alone (provenance travels with media)", ErrInvalid)
		}
		if !IsImportSidecarRole(role) {
			return 0, fmt.Errorf("%w: %s is not a sidecar", ErrInvalid, filepath.Base(abs))
		}
		if role == ImportRoleNFO && !ImportSidecarStemMatchesMedia(abs, mediaPath) {
			return 0, fmt.Errorf("%w: .nfo data import requires a same-basename video file beside it", ErrInvalid)
		}
		if role == ImportRoleThumb && !ImportSidecarStemMatchesMedia(abs, mediaPath) {
			return 0, fmt.Errorf("%w: thumbnail import requires a same-basename video file beside it", ErrInvalid)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		absPaths = append(absPaths, abs)
	}
	if len(absPaths) == 0 {
		return 0, fmt.Errorf("%w: paths required", ErrInvalid)
	}
	busy, err := s.hasPendingImport(videoID, absPaths[0])
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: import already queued", ErrConflict)
	}
	msg := fmt.Sprintf("Attach %d sidecar(s)", len(absPaths))
	if len(absPaths) == 1 {
		msg = fmt.Sprintf("Attach sidecar %s", filepath.Base(absPaths[0]))
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:   queue.OriginManual,
		Kind:     queue.KindImport,
		Domain:   queue.SystemDomain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Payload: map[string]any{
			"mode": "sidecars", "paths": absPaths, "video_id": videoID,
		},
		Message: msg,
	})
}

// AttachSidecarFiles registers sidecar paths on a video that already has media.
// Inbox paths (under ImportRoot) are moved beside the media first (subs keep
// language suffix; thumbs become `{stem}-thumb{ext}`). Paths already beside the
// media file are registered as-is. NFO is data import only (same-stem beside
// media); source XML is not kept. Rejects info.json (provenance packs only with media).
func (s *Store) AttachSidecarFiles(videoID int64, paths []string, taskID int64) error {
	mediaPath, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: video has no media file", ErrInvalid)
	}
	mediaAbs, err := filepath.Abs(mediaPath)
	if err != nil {
		mediaAbs = mediaPath
	}
	mediaDir := filepath.Dir(mediaAbs)
	mediaStem := strings.TrimSuffix(filepath.Base(mediaAbs), filepath.Ext(mediaAbs))

	type item struct {
		kind string
		path string
	}
	var items []item
	var nfoPaths []string
	for _, p := range paths {
		abs, err := filepath.Abs(strings.TrimSpace(p))
		if err != nil {
			abs = strings.TrimSpace(p)
		}
		if !fileExists(abs) {
			return fmt.Errorf("%w: sidecar not found: %s", ErrNotFound, abs)
		}
		role, _ := ClassifyImportFile(filepath.Base(abs))
		if role == ImportRoleJSON {
			return fmt.Errorf("%w: info.json cannot be attached alone (provenance travels with media)", ErrInvalid)
		}
		if filepath.Dir(abs) != mediaDir {
			if !s.pathUnderImportInbox(abs) {
				return fmt.Errorf("%w: sidecar must be beside the video media file", ErrInvalid)
			}
			if role == ImportRoleNFO {
				return fmt.Errorf("%w: .nfo data import requires a same-basename video file beside it", ErrInvalid)
			}
			dest, derr := inboxSidecarLibraryDest(abs, mediaDir, mediaStem, role)
			if derr != nil {
				return derr
			}
			if DestinationOccupied(dest, nil) {
				return fmt.Errorf("%w: destination exists: %s", ErrInvalid, filepath.Base(dest))
			}
			if err := moveFile(abs, dest); err != nil {
				return fmt.Errorf("move sidecar into library: %w", err)
			}
			abs = dest
		}
		if role == ImportRoleNFO {
			if !ImportSidecarStemMatchesMedia(abs, mediaAbs) {
				return fmt.Errorf("%w: .nfo data import requires a same-basename video file beside it", ErrInvalid)
			}
			nfoPaths = append(nfoPaths, abs)
			continue
		}
		if role == ImportRoleThumb && !ImportSidecarStemMatchesMedia(abs, mediaAbs) {
			return fmt.Errorf("%w: thumbnail import requires a same-basename video file beside it", ErrInvalid)
		}
		if !IsImportSidecarRole(role) {
			return fmt.Errorf("%w: not a sidecar: %s", ErrInvalid, filepath.Base(abs))
		}
		items = append(items, item{kind: role, path: abs})
	}
	if len(items) == 0 && len(nfoPaths) == 0 {
		return fmt.Errorf("%w: paths required", ErrInvalid)
	}

	for _, nfo := range nfoPaths {
		if err := s.ApplyImportNFO(videoID, nfo, taskID); err != nil {
			return err
		}
	}
	if len(items) == 0 {
		return nil
	}

	acquired := nowRFC3339()
	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	attached := make([]string, 0, len(items))
	for _, it := range items {
		if it.kind != ImportRoleSub {
			if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ? AND kind = ?`, videoID, it.kind); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ? AND kind = 'sub' AND path = ?`, videoID, it.path); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, ?, ?, NULL)
		`, videoID, it.path, it.kind, acquired); err != nil {
			return err
		}
		attached = append(attached, it.kind+":"+filepath.Base(it.path))
	}
	detail, _ := json.Marshal(map[string]any{"paths": attached})
	if taskID <= 0 {
		return fmt.Errorf("%w: task_id required for sidecar_attach history", ErrInvalid)
	}
	if _, err := tx.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, 'sidecar_attach', ?, ?, ?)
	`, videoID, acquired, fmt.Sprintf("Attached %d sidecar(s)", len(items)), string(detail), taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// pathUnderImportInbox reports whether abs lives under ImportRoot (inbox move source).
func (s *Store) pathUnderImportInbox(abs string) bool {
	root := strings.TrimSpace(s.ImportRoot)
	if root == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// inboxSidecarLibraryDest builds the library path for an inbox sub/thumb beside media.
func inboxSidecarLibraryDest(src, mediaDir, mediaStem, role string) (string, error) {
	switch role {
	case ImportRoleSub:
		srcStem := guessSubtitleWorkStem(src)
		suffix := SubtitleLangAndExt(src, srcStem)
		if suffix == "" {
			suffix = filepath.Ext(src)
		}
		if suffix == "" {
			return "", fmt.Errorf("%w: subtitle has no extension", ErrInvalid)
		}
		return filepath.Join(mediaDir, mediaStem+suffix), nil
	case ImportRoleThumb:
		ext := strings.ToLower(filepath.Ext(src))
		if ext == "" {
			ext = ".jpg"
		}
		return filepath.Join(mediaDir, mediaStem+"-thumb"+ext), nil
	default:
		return "", fmt.Errorf("%w: cannot move %s from inbox", ErrInvalid, role)
	}
}

// ValidateImportInboxPath ensures path is an existing file under ImportRoot.
func (s *Store) ValidateImportInboxPath(path string) (abs string, err error) {
	abs, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return "", fmt.Errorf("%w: file not found", ErrNotFound)
	}
	root := strings.TrimSpace(s.ImportRoot)
	if root == "" {
		return "", fmt.Errorf("%w: import root not configured", ErrInvalid)
	}
	absRoot, rerr := filepath.Abs(root)
	if rerr != nil {
		return "", fmt.Errorf("%w: import root not configured", ErrInvalid)
	}
	rel, rerr := filepath.Rel(absRoot, abs)
	if rerr != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%w: path must be under import root", ErrInvalid)
	}
	return abs, nil
}

// ValidateImportMediaPath ensures path is an existing media file under ImportRoot.
func (s *Store) ValidateImportMediaPath(path string) (string, error) {
	abs, err := s.ValidateImportInboxPath(path)
	if err != nil {
		return "", err
	}
	if !mediaExts[strings.ToLower(filepath.Ext(abs))] {
		return "", fmt.Errorf("%w: unsupported media extension", ErrInvalid)
	}
	return abs, nil
}

func (s *Store) hasPendingImport(videoID int64, path string) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE kind = ? AND status IN ('pending','running')
		  AND (video_id = ? OR instr(payload, ?) > 0)
	`, queue.KindImport, videoID, path).Scan(&n)
	return n > 0, err
}
