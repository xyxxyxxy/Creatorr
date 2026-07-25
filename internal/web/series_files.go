package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/go-chi/chi/v5"
)

// seriesMetaFileView is one Files-table row for series folder metadata (art / tvshow.nfo).
type seriesMetaFileView struct {
	Role      string
	KindLabel string
	Icon      string
	Name      string
	Path      string
	SizeLabel string
	ViewHref  string
}

func seriesMetaFileViews(lib *library.Store, ser *library.Series) []seriesMetaFileView {
	if lib == nil || ser == nil {
		return nil
	}
	root, err := lib.GetRoot(ser.RootID)
	if err != nil {
		return nil
	}
	dir := library.SeriesDir(root.Path, ser.Title)
	files := library.ListSeriesMetaFiles(dir)
	if len(files) == 0 {
		return nil
	}
	out := make([]seriesMetaFileView, 0, len(files))
	for _, f := range files {
		name := filepath.Base(f.Path)
		sizeLabel := "-"
		if st, err := os.Stat(f.Path); err == nil && !st.IsDir() {
			sizeLabel = library.FormatBytes(st.Size())
		}
		out = append(out, seriesMetaFileView{
			Role:      f.Role,
			KindLabel: seriesMetaKindLabel(f.Role),
			Icon:      seriesMetaKindIcon(f.Role),
			Name:      name,
			Path:      f.Path,
			SizeLabel: sizeLabel,
			ViewHref:  fmt.Sprintf("/series/%d/files/%s", ser.ID, f.Role),
		})
	}
	return out
}

func seriesMetaKindLabel(role string) string {
	switch role {
	case library.SeriesMetaFileRoleNFO:
		return "NFO"
	case library.ArtPoster:
		return "Poster"
	case library.ArtBanner:
		return "Banner"
	case library.ArtFanart:
		return "Fanart"
	case library.ArtClearlogo:
		return "Clearlogo"
	default:
		return role
	}
}

func seriesMetaKindIcon(role string) string {
	switch role {
	case library.SeriesMetaFileRoleNFO:
		return "file-text"
	case library.ArtPoster, library.ArtBanner, library.ArtFanart, library.ArtClearlogo:
		return "image"
	default:
		return "file"
	}
}

func (h *Handler) resolveSeriesMetaFile(seriesID int64, role string) (*library.Series, string, error) {
	ser, err := h.Library.GetSeries(seriesID, false)
	if err != nil || ser == nil {
		return nil, "", library.ErrNotFound
	}
	root, err := h.Library.GetRoot(ser.RootID)
	if err != nil {
		return nil, "", err
	}
	dir := library.SeriesDir(root.Path, ser.Title)
	path := library.ResolveSeriesMetaFile(dir, role)
	if path == "" {
		return nil, "", library.ErrNotFound
	}
	// Ensure path stays under the series folder.
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", library.ErrNotFound
	}
	return ser, cleanPath, nil
}

// seriesMetaFileViewPage renders a series metadata file preview (text or image).
func (h *Handler) seriesMetaFileViewPage(w http.ResponseWriter, r *http.Request) {
	seriesID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	role := chi.URLParam(r, "role")
	ser, path, err := h.resolveSeriesMetaFile(seriesID, role)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(path)
	st, err := os.Stat(path)
	missing := err != nil
	sizeLabel := "-"
	if !missing && !st.IsDir() {
		sizeLabel = library.FormatBytes(st.Size())
	}
	rawHref := fmt.Sprintf("/series/%d/files/%s/raw", seriesID, role)
	kind := role
	if role == library.SeriesMetaFileRoleNFO {
		kind = "nfo"
	} else {
		kind = "thumb" // image art for preview helpers
	}
	view := struct {
		pageBase
		Crumbs     []breadcrumb
		Series     *library.Series
		Role       string
		KindLabel  string
		Icon       string
		Name       string
		Path       string
		SizeLabel  string
		Missing    bool
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
		pageBase:  newPage(name, "series", nil),
		Series:    ser,
		Role:      role,
		KindLabel: seriesMetaKindLabel(role),
		Icon:      seriesMetaKindIcon(role),
		Name:      name,
		Path:      path,
		SizeLabel: sizeLabel,
		Missing:   missing,
		IsImage:   sidecarIsImage(kind, path),
		IsVideo:   false,
		IsText:    sidecarIsText(kind, path),
		IsJSON:    false,
		RawHref:   rawHref,
		Crumbs: []breadcrumb{
			crumb("/series", "Series", "tv"),
			crumb(fmt.Sprintf("/series/%d", ser.ID), ser.Title, "clapperboard"),
			crumb("", name, seriesMetaKindIcon(role)),
		},
	}
	if !missing && view.IsText {
		text, trunc, rerr := readSidecarText(path, sidecarViewMaxBytes)
		if rerr != nil {
			view.Missing = true
		} else {
			view.Text = text
			view.Truncated = trunc
		}
	}
	render(w, "series_meta_file_view", view)
}

// seriesMetaFileRaw serves raw series metadata bytes.
func (h *Handler) seriesMetaFileRaw(w http.ResponseWriter, r *http.Request) {
	seriesID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	role := chi.URLParam(r, "role")
	_, path, err := h.resolveSeriesMetaFile(seriesID, role)
	if err != nil {
		http.NotFound(w, r)
		return
	}
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
