package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/go-chi/chi/v5"
)

const sidecarViewMaxBytes = 2 << 20 // 2 MiB text preview

// videoFileView is one files-table row on the video detail page (media or sidecar).
type videoFileView struct {
	ID        int64
	Kind      string
	KindLabel string
	Icon      string // lucide icon name
	Name      string
	Path      string
	SizeLabel string
	Missing   bool
	ViewHref  string // file detail page
	CanDelete bool   // registered sub/thumb/other
}

func videoFileViews(lib *library.Store, seriesID, videoID int64, files []library.VideoFile) []videoFileView {
	if len(files) == 0 {
		return nil
	}
	out := make([]videoFileView, 0, len(files))
	for _, f := range files {
		v := videoFileView{
			ID:        f.ID,
			Kind:      f.Kind,
			KindLabel: sidecarKindLabel(f.Kind),
			Icon:      sidecarKindIcon(f.Kind),
			Name:      filepath.Base(f.Path),
			Path:      f.Path,
			SizeLabel: "-",
		}
		if f.ID > 0 {
			v.ViewHref = fmt.Sprintf("/series/%d/videos/%d/files/%d", seriesID, videoID, f.ID)
			v.CanDelete = library.DeletableSidecarKind(f.Kind)
		}
		if f.SizeBytes.Valid && f.SizeBytes.Int64 >= 0 {
			v.SizeLabel = library.FormatBytes(f.SizeBytes.Int64)
		} else if st, err := os.Stat(f.Path); err == nil && !st.IsDir() {
			v.SizeLabel = library.FormatBytes(st.Size())
		}
		if _, err := os.Stat(f.Path); err != nil {
			v.Missing = true
		}
		out = append(out, v)
	}
	return out
}

func videoMediaViews(lib *library.Store, seriesID, videoID int64) []videoFileView {
	if lib == nil {
		return nil
	}
	files, err := lib.ListVideoMediaFiles(videoID)
	if err != nil || len(files) == 0 {
		return nil
	}
	return videoFileViews(lib, seriesID, videoID, files)
}

func videoSidecarViews(lib *library.Store, seriesID, videoID int64) []videoFileView {
	if lib == nil {
		return nil
	}
	files, err := lib.ListVideoSidecars(videoID)
	if err != nil || len(files) == 0 {
		return nil
	}
	return videoFileViews(lib, seriesID, videoID, files)
}

// videoAllFileViews returns media rows first (video, then strm), then sidecars.
func videoAllFileViews(lib *library.Store, seriesID, videoID int64) []videoFileView {
	media := videoMediaViews(lib, seriesID, videoID)
	sides := videoSidecarViews(lib, seriesID, videoID)
	if len(media) == 0 {
		return sides
	}
	if len(sides) == 0 {
		return media
	}
	out := make([]videoFileView, 0, len(media)+len(sides))
	out = append(out, media...)
	out = append(out, sides...)
	return out
}

func sidecarKindLabel(kind string) string {
	switch kind {
	case "video":
		return "Video"
	case "nfo":
		return "NFO"
	case "json":
		return "info.json"
	case "thumb":
		return "Thumbnail"
	case "sub":
		return "Subtitle"
	case "strm":
		return "Stream link"
	case "sponsorblock":
		return "SponsorBlock"
	default:
		return kind
	}
}

func sidecarKindIcon(kind string) string {
	switch kind {
	case "video":
		// Packed media file (not the episode itself - episodes use EpisodeLucideIcon).
		return "film"
	case "nfo":
		return "file-text"
	case "json":
		return "braces"
	case "thumb":
		return "image"
	case "sub":
		return "captions"
	case "strm":
		return "radio"
	case "sponsorblock":
		return "scissors"
	default:
		return "file"
	}
}

func (h *Handler) loadVideoSidecar(seriesID, videoID, fileID int64) (*library.Video, *library.VideoFile, error) {
	v, err := h.Library.GetVideo(videoID)
	if err != nil {
		return nil, nil, err
	}
	if v == nil || v.SeriesID != seriesID {
		return nil, nil, library.ErrNotFound
	}
	f, err := h.Library.GetVideoFile(videoID, fileID)
	if err != nil {
		return nil, nil, err
	}
	return v, f, nil
}

// videoSidecarViewPage renders a sidecar inside Creatorr UI (text preview or image).
func (h *Handler) videoSidecarViewPage(w http.ResponseWriter, r *http.Request) {
	seriesID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	fid, _ := strconv.ParseInt(chi.URLParam(r, "fid"), 10, 64)
	v, f, err := h.loadVideoSidecar(seriesID, vid, fid)
	if err != nil || v == nil || f == nil {
		http.NotFound(w, r)
		return
	}
	ser, err := h.Library.GetSeries(seriesID, false)
	if err != nil || ser == nil {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(f.Path)
	st, err := os.Stat(f.Path)
	missing := err != nil
	sizeLabel := "-"
	if !missing && !st.IsDir() {
		sizeLabel = library.FormatBytes(st.Size())
	}

	rawHref := fmt.Sprintf("/series/%d/videos/%d/files/%d/raw", seriesID, vid, fid)
	view := struct {
		pageBase
		Crumbs     []breadcrumb
		Series     *library.Series
		Video      *library.Video
		FileID     int64
		Kind       string
		KindLabel  string
		Icon       string
		Name       string
		Path       string
		SizeLabel  string
		Missing    bool
		CanDelete  bool
		IsImage    bool
		IsVideo    bool
		IsText     bool
		IsJSON     bool
		Text       string
		TextPretty string
		HasPretty  bool
		Truncated  bool
		RawHref    string
	}{
		pageBase:  newPage(name, "series", flashFromQuery(r)),
		Series:    ser,
		Video:     v,
		FileID:    fid,
		Kind:      f.Kind,
		KindLabel: sidecarKindLabel(f.Kind),
		Icon:      sidecarKindIcon(f.Kind),
		Name:      name,
		Path:      f.Path,
		SizeLabel: sizeLabel,
		Missing:   missing,
		CanDelete: library.DeletableSidecarKind(f.Kind),
		IsImage:   sidecarIsImage(f.Kind, f.Path),
		IsVideo:   sidecarIsVideo(f.Kind, f.Path),
		IsText:    sidecarIsText(f.Kind, f.Path),
		IsJSON:    sidecarIsJSON(f.Kind, f.Path),
		RawHref:   rawHref,
		Crumbs: []breadcrumb{
			crumb("/series", "Series", "tv"),
			crumb(fmt.Sprintf("/series/%d", ser.ID), ser.Title, "clapperboard"),
			crumb(fmt.Sprintf("/series/%d/videos/%d", ser.ID, v.ID), v.Title, EpisodeLucideIcon),
			crumb("", name, sidecarKindIcon(f.Kind)),
		},
	}

	if !missing && view.IsText {
		text, trunc, rerr := readSidecarText(f.Path, sidecarViewMaxBytes)
		if rerr != nil {
			view.Missing = true
		} else {
			view.Text = text
			view.Truncated = trunc
			if view.IsJSON {
				if pretty, ok := prettyJSON(text); ok {
					view.TextPretty = pretty
					view.HasPretty = true
				}
			}
		}
	}

	render(w, "video_sidecar_view", view)
}

// videoSidecarFile serves raw sidecar bytes (images on the viewer page; optional Save As).
func (h *Handler) videoSidecarFile(w http.ResponseWriter, r *http.Request) {
	seriesID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	fid, _ := strconv.ParseInt(chi.URLParam(r, "fid"), 10, 64)
	_, f, err := h.loadVideoSidecar(seriesID, vid, fid)
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	path := f.Path
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(path)
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename="+strconv.Quote(name))
	w.Header().Set("Cache-Control", "private, no-store")
	if ct := sidecarViewContentType(path); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeFile(w, r, path)
}

func sidecarIsImage(kind, path string) bool {
	if kind == "thumb" {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func sidecarIsVideo(kind, path string) bool {
	if sidecarIsImage(kind, path) || sidecarIsText(kind, path) {
		return false
	}
	if kind == "video" {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".webm", ".mkv", ".mov", ".avi", ".ogv":
		return true
	default:
		return false
	}
}

func sidecarIsText(kind, path string) bool {
	if sidecarIsImage(kind, path) {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case strings.HasSuffix(base, ".info.json"), strings.HasSuffix(base, ".sponsorblock.json"), ext == ".json",
		ext == ".nfo", ext == ".strm",
		ext == ".srt", ext == ".vtt", ext == ".ass", ext == ".ssa", ext == ".sub",
		ext == ".txt", ext == ".xml":
		return true
	case kind == "nfo", kind == "json", kind == "sub", kind == "strm", kind == "sponsorblock":
		return true
	default:
		return false
	}
}

func sidecarIsJSON(kind, path string) bool {
	if kind == "json" || kind == "sponsorblock" {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	return strings.HasSuffix(base, ".info.json") || strings.HasSuffix(base, ".sponsorblock.json") || ext == ".json"
}

// prettyJSON indents valid JSON, preserving key order. Returns false if not parseable.
func prettyJSON(s string) (string, bool) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return "", false
	}
	return buf.String(), true
}

func sidecarViewContentType(path string) string {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case strings.HasSuffix(base, ".info.json"), ext == ".json":
		return "application/json; charset=utf-8"
	case ext == ".nfo", ext == ".strm", ext == ".srt", ext == ".vtt", ext == ".ass", ext == ".ssa", ext == ".sub":
		return "text/plain; charset=utf-8"
	case ext == ".mp4", ext == ".m4v":
		return "video/mp4"
	case ext == ".webm":
		return "video/webm"
	case ext == ".mkv":
		return "video/x-matroska"
	case ext == ".mov":
		return "video/quicktime"
	case ext == ".ogv":
		return "video/ogg"
	case ext == ".avi":
		return "video/x-msvideo"
	default:
		return ""
	}
}

func readSidecarText(path string, max int) (string, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	trunc := false
	if len(b) > max {
		b = b[:max]
		trunc = true
		// Avoid cutting mid-rune.
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
	}
	if !utf8.Valid(b) {
		return "", false, fmt.Errorf("not utf-8 text")
	}
	return string(b), trunc, nil
}
