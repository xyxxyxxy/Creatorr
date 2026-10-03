package web

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

const (
	cookieModeFiles     = "creatorr_mode_files"
	cookieSortFiles     = "creatorr_sort_files"
	cookieColsFiles     = "creatorr_cols_files"
	filesLiveTarget     = "files-list-live"
	filesExplorerPageSz = SeriesPageSize
	explorerAtVideoDet  = "video-detail"
)

type filesListLiveData struct {
	Files           []filesExplorerRow
	Page            PageInfo
	Load            ListLoad
	ListMode        ListMode
	FilterTotal     int
	Filter          listViewToolbar
	FilterActive    bool
	ViewMode        string
	TableCols       []tableCol
	TableColsCookie string
	SeriesID        int64
	VideoID         int64
	ShowSeriesCol   bool
	ShowVideoCol    bool
	ShowToolbar     bool
	ShowRowActions  bool
	FilesBulkMode   bool
	ShowSelectAll   bool
	OOB             bool
}

type filesExplorerRow struct {
	ID                int64
	SeriesID          int64
	VideoID           int64
	Kind              string
	KindLabel         string
	Icon              string
	Name              string
	Path              string
	SizeLabel         string
	Missing           bool
	IntegrityFailed   bool
	IntegrityStatus   string // OK | Failed | Unchecked | N/A | Inactive
	IntegrityAgo      string // last checked since / ISO>7d (OK/Failed tip)
	IntegrityTip      string // tip when not OK/Failed
	AcquiredAt        string // absolute tip for rel_time
	AcquiredAgo       string // relative display
	SeriesTitle       string
	VideoTitle        string
	ViewHref          string
	CanCheckHash      bool
	CheckDisabledTip  string
	CanDelete         bool
	DeleteDisabledTip string
	Redirect          string
	LiveTarget        string
}

func (h *Handler) explorerBrowseFiles(w http.ResponseWriter, r *http.Request) {
	data, err := h.loadFilesListLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	maybeExplorerPushURL(w, r, explorerTypeFiles)
	render(w, "files_list_live", data)
}

func parseFileListFilter(r *http.Request) library.FileListFilter {
	f := library.FileListFilter{
		Q:       strings.TrimSpace(r.URL.Query().Get("q")),
		Kind:    strings.TrimSpace(r.URL.Query().Get("kind")),
		Status:  library.NormalizeFileStatus(r.URL.Query().Get("status")),
		Sort:    parseFileSort(r.URL.Query().Get("sort")),
		SortDir: parseSortDir(r.URL.Query().Get("dir")),
	}
	if sid, err := strconv.ParseInt(r.URL.Query().Get("series_id"), 10, 64); err == nil && sid > 0 {
		f.SeriesID = sid
	} else if sid, err := strconv.ParseInt(r.URL.Query().Get("series"), 10, 64); err == nil && sid > 0 {
		f.SeriesID = sid
	}
	if vid, err := strconv.ParseInt(r.URL.Query().Get("video_id"), 10, 64); err == nil && vid > 0 {
		f.VideoID = vid
	}
	return f
}

func parseFileSort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case library.SortFilePath:
		return library.SortFilePath
	case library.SortFileSize:
		return library.SortFileSize
	case library.SortFileAcquired:
		return library.SortFileAcquired
	case library.SortFileSeries:
		// Removed from Sort UI; treat legacy ?sort=series / cookie as Type.
		return library.SortFileKind
	case library.SortFileKind:
		return library.SortFileKind
	case "":
		return ""
	default:
		return library.SortFileKind
	}
}

func filesEmbedScope(r *http.Request) (seriesDetail, videoDetail bool) {
	at := explorerAtFrom(r, "")
	switch at {
	case explorerAtSeriesDetail:
		return true, false
	case explorerAtVideoDet:
		return false, true
	}
	return false, false
}

func (h *Handler) loadFilesListLive(w http.ResponseWriter, r *http.Request) (filesListLiveData, error) {
	filter := parseFileListFilter(r)
	seriesEmbed, videoEmbed := filesEmbedScope(r)
	embed := seriesEmbed || videoEmbed
	if !embed {
		r = mergeFilesListPrefs(r)
		filter = parseFileListFilter(r)
	}
	if filter.Sort == "" {
		// Browser: newest acquired first. Scoped embeds: group by type.
		if embed {
			filter.Sort = library.SortFileKind
		} else {
			filter.Sort = library.SortFileAcquired
		}
	}

	var viewMode string
	if embed {
		viewMode = viewList
		if v := strings.TrimSpace(r.URL.Query().Get("view")); v == viewTable {
			viewMode = viewTable
		}
	} else {
		writeFilesListPrefs(w, filter)
		var writeCookie bool
		viewMode, writeCookie = resolveViewMode(r, cookieModeFiles, viewList)
		if mode := coerceListTableView(viewMode); mode != viewMode {
			viewMode = mode
			writeCookie = true
		}
		if writeCookie {
			writeViewCookie(w, cookieModeFiles, viewMode)
		}
	}

	total, err := h.Library.CountFilesFiltered(filter)
	if err != nil {
		return filesListLiveData{}, err
	}
	load := resolvePaginatedLoad(r, total, filesExplorerPageSz, filesLiveTarget, "page")
	list, err := h.Library.ListFilesFiltered(filter, load.PageSize, OffsetSize(load.Page.Page, load.PageSize))
	if err != nil {
		return filesListLiveData{}, err
	}

	at := explorerAtFrom(r, explorerAtBrowser)
	if seriesEmbed {
		at = explorerAtSeriesDetail
	}
	if videoEmbed {
		at = explorerAtVideoDet
	}
	showSeries := !embed
	showVideo := !videoEmbed
	redir := explorerCanonicalURL(r, explorerTypeFiles)
	if redir == "" {
		redir = r.URL.RequestURI()
	}

	rows := make([]filesExplorerRow, 0, len(list))
	for _, f := range list {
		row := fileListRowToExplorer(f, redir, showSeries, showVideo)
		if row.CanCheckHash && h.Library != nil {
			if busy, _, err := h.Library.FileHashCheckBusy(row.VideoID, f.ID); err == nil && busy {
				row.CanCheckHash = false
				row.CheckDisabledTip = "Integrity check already queued"
			}
		}
		rows = append(rows, row)
	}

	var toolbar listViewToolbar
	if !embed {
		toolbar = listViewToolbar{
			Query:            filter.Q,
			QueryPlaceholder: "Search path",
			AriaLabel:        "File filters",
			SortOpts:         filesSortOpts(r, filter.Sort, filter.SortDir),
			SortDir:          library.NormalizeSortDir(filter.Sort, filter.SortDir),
			ViewOpts:         sourcesViewOpts(r, viewMode),
			ShowView:         true,
			Selects:          filesFilterSelects(r, filter),
			FilterActive:     filter.Active(),
			LiveTarget:       filesLiveTarget,
			FormAction:       "/explorer/browse",
			FilesBulkMode:    true,
		}
		if filter.Active() {
			toolbar.ClearAllHref = clearOperatorFiltersURL(r)
			toolbar.Badges = filesListBadges(r, filter)
		}
		applyExplorerToolbar(&toolbar, explorerTypeFiles, at)
	} else {
		// Embeds keep type/at/scope hidden fields for pagination.
		toolbar = listViewToolbar{LiveTarget: filesLiveTarget, FormAction: "/explorer/browse"}
		applyExplorerToolbar(&toolbar, explorerTypeFiles, at)
		if filter.SeriesID > 0 {
			toolbar.Hidden = append(toolbar.Hidden, listHiddenField{Name: "series_id", Value: strconv.FormatInt(filter.SeriesID, 10)})
		}
		if filter.VideoID > 0 {
			toolbar.Hidden = append(toolbar.Hidden, listHiddenField{Name: "video_id", Value: strconv.FormatInt(filter.VideoID, 10)})
		}
	}
	showSelectAll := total > len(list)

	page := load.Page
	page.LiveTarget = filesLiveTarget
	rewriteExplorerPageInfo(&page, explorerTypeFiles, filter.SeriesID)
	if videoEmbed {
		// Keep video_id on page links.
		page.FirstHref = appendQueryParam(page.FirstHref, "video_id", filter.VideoID)
		page.PrevHref = appendQueryParam(page.PrevHref, "video_id", filter.VideoID)
		page.NextHref = appendQueryParam(page.NextHref, "video_id", filter.VideoID)
		page.LastHref = appendQueryParam(page.LastHref, "video_id", filter.VideoID)
	}

	out := filesListLiveData{
		Files:           rows,
		Page:            page,
		Load:            load,
		ListMode:        ListModePaginated,
		FilterTotal:     total,
		Filter:          toolbar,
		FilterActive:    !embed && filter.Active(),
		ViewMode:        viewMode,
		TableCols: annotateTableColsSort(
			parseTableColsCookie(r, cookieColsFiles, filesTableColDefs(showSeries, showVideo)),
			toolbar.SortOpts, toolbar.SortDir),
		TableColsCookie: cookieColsFiles,
		SeriesID:        filter.SeriesID,
		VideoID:         filter.VideoID,
		ShowSeriesCol:   showSeries,
		ShowVideoCol:    showVideo,
		ShowToolbar:     !embed,
		ShowRowActions:  true,
		FilesBulkMode:   true,
		ShowSelectAll:   showSelectAll,
	}
	rewriteExplorerInfinite(&out.Load, explorerTypeFiles, filter.SeriesID)
	return out, nil
}

func fileListRowToExplorer(f library.FileListRow, redir string, showSeries, showVideo bool) filesExplorerRow {
	vid := int64(0)
	if f.VideoID.Valid {
		vid = f.VideoID.Int64
	}
	failed := f.IntegrityFailed()
	hasOK := f.ContentHashOkAt.Valid && strings.TrimSpace(f.ContentHashOkAt.String) != ""
	integrityAgo := ""
	now := time.Now().UTC()
	if failed {
		if f.ContentHashCheckedAt.Valid && strings.TrimSpace(f.ContentHashCheckedAt.String) != "" {
			_, integrityAgo = createdAgoPairShort(f.ContentHashCheckedAt.String, now)
		}
	} else if hasOK {
		_, integrityAgo = createdAgoPairShort(f.ContentHashOkAt.String, now)
	}
	row := filesExplorerRow{
		ID:              f.ID,
		SeriesID:        f.SeriesID,
		VideoID:         vid,
		Kind:            f.Kind,
		KindLabel:       fileExplorerKindLabel(f),
		Icon:            fileExplorerKindIcon(f),
		Name:            filepath.Base(f.Path),
		Path:            f.Path,
		SizeLabel:       "-",
		Missing:         f.Missing(),
		IntegrityFailed: failed,
		IntegrityAgo:    integrityAgo,
		SeriesTitle:     f.SeriesTitle,
		VideoTitle:      f.VideoTitle,
		Redirect:        redir,
		LiveTarget:      filesLiveTarget,
		CanDelete:       vid > 0 && library.DeletableSidecarKind(f.Kind),
	}
	if at := strings.TrimSpace(f.AcquiredAt); at != "" {
		row.AcquiredAt, row.AcquiredAgo = createdAgoPairShort(at, time.Now().UTC())
	}
	if !row.CanDelete {
		row.DeleteDisabledTip = fileDeleteDisabledTip(f)
	}
	if f.SizeBytes.Valid && f.SizeBytes.Int64 >= 0 {
		row.SizeLabel = library.FormatBytes(f.SizeBytes.Int64)
	}
	checkApplicable := true
	if f.IsSeriesMeta() {
		row.ViewHref = fmt.Sprintf("/series/%d/files/%s", f.SeriesID, f.Kind)
		if f.Kind == library.SeriesMetaFileRoleNFO {
			checkApplicable = false
			row.CanCheckHash = false
			row.CheckDisabledTip = "Integrity check skipped, can be regenerated"
		} else {
			row.CanCheckHash = !row.Missing && f.ID > 0
			if row.Missing {
				row.CheckDisabledTip = "File missing - integrity check inactive"
			}
		}
	} else if vid > 0 {
		row.ViewHref = fmt.Sprintf("/series/%d/videos/%d/files/%d", f.SeriesID, vid, f.ID)
		if f.Kind == "nfo" {
			checkApplicable = false
			row.CanCheckHash = false
			row.CheckDisabledTip = "Integrity check skipped, can be regenerated"
		} else {
			row.CanCheckHash = !row.Missing && f.ID > 0
			if row.Missing {
				row.CheckDisabledTip = "File missing - integrity check inactive"
			}
		}
	} else {
		checkApplicable = false
	}
	row.IntegrityStatus, row.IntegrityTip = fileIntegrityDisplay(failed, hasOK, checkApplicable, row.Missing, row.CheckDisabledTip)
	_ = showSeries
	_ = showVideo
	return row
}

// fileIntegrityDisplay returns Status + tip for non OK/Failed states.
func fileIntegrityDisplay(failed, hasOK, checkApplicable, missing bool, naTip string) (status, tip string) {
	switch {
	case failed:
		return "Failed", ""
	case hasOK:
		return "OK", ""
	case !checkApplicable:
		tip = strings.TrimSpace(naTip)
		if tip == "" {
			tip = "Integrity check not applicable"
		}
		return "N/A", tip
	case missing:
		return "Inactive", "File missing - integrity check inactive"
	default:
		return "Unchecked", "Not checked yet"
	}
}

func fileDeleteDisabledTip(f library.FileListRow) string {
	if f.IsSeriesMeta() {
		return "Series art and NFO are managed from series Edit, not Delete here"
	}
	switch strings.TrimSpace(f.Kind) {
	case "video", "nfo":
		return "Delete video to remove"
	case "json":
		return "info.json is packed with the media - not deleted individually"
	case "sponsorblock":
		return "SponsorBlock plans are not deleted individually"
	default:
		return "This file kind cannot be deleted individually"
	}
}

func fileExplorerKindLabel(f library.FileListRow) string {
	if f.IsSeriesMeta() {
		return seriesMetaKindLabel(f.Kind)
	}
	return sidecarKindLabel(f.Kind)
}

func fileExplorerKindIcon(f library.FileListRow) string {
	if f.IsSeriesMeta() {
		return seriesMetaKindIcon(f.Kind)
	}
	return sidecarKindIcon(f.Kind)
}

func mergeFilesListPrefs(r *http.Request) *http.Request {
	q := r.URL.Query()
	changed := false
	if _, ok := q["sort"]; !ok {
		if raw := readListPrefCookie(r, cookieSortFiles); raw != "" {
			sort, dir := parseSortCookie(raw)
			if s := parseFileSort(sort); s != "" {
				q.Set("sort", s)
				changed = true
			}
			if dir != "" {
				if d := parseSortDir(dir); d != "" {
					q.Set("dir", d)
				}
			}
		}
	}
	if !changed {
		return r
	}
	return cloneRequestQuery(r, q)
}

func writeFilesListPrefs(w http.ResponseWriter, f library.FileListFilter) {
	writeListPrefCookie(w, cookieSortFiles, encodeSortCookie(f.Sort, f.SortDir))
}

func filesSortOpts(r *http.Request, current, curDir string) []listFilterOpt {
	cur := current
	if cur == "" {
		cur = library.SortFileAcquired
	}
	opts := []listFilterOpt{
		{Value: library.SortFileAcquired, Label: "Acquired", Selected: cur == library.SortFileAcquired, Icon: "clock"},
		{Value: library.SortFileSize, Label: "Size", Selected: cur == library.SortFileSize, Icon: "hard-drive"},
		{Value: library.SortFilePath, Label: "Path", Selected: cur == library.SortFilePath, Icon: "file"},
		{Value: library.SortFileKind, Label: "Type", Selected: cur == library.SortFileKind, Icon: "shapes"},
	}
	annotateSortOpts(r, opts, library.SortFileAcquired, curDir)
	return opts
}

func filesFilterSelects(r *http.Request, f library.FileListFilter) []listFilterSelect {
	kindCur := f.Kind
	statusCur := library.NormalizeFileStatus(f.Status)
	selects := []listFilterSelect{
		{
			Name:      "kind",
			AriaLabel: "Type",
			Options: []listFilterOpt{
				{Value: "video", Label: "Video", Selected: kindCur == "video"},
				{Value: "nfo", Label: "NFO", Selected: kindCur == "nfo"},
				{Value: "json", Label: "info.json", Selected: kindCur == "json"},
				{Value: "thumb", Label: "Thumb", Selected: kindCur == "thumb"},
				{Value: "sub", Label: "Subtitle", Selected: kindCur == "sub"},
				{Value: "sponsorblock", Label: "SponsorBlock", Selected: kindCur == "sponsorblock"},
				{Value: "poster", Label: "Poster", Selected: kindCur == "poster"},
				{Value: "banner", Label: "Banner", Selected: kindCur == "banner"},
				{Value: "fanart", Label: "Fanart", Selected: kindCur == "fanart"},
				{Value: "clearlogo", Label: "Clearlogo", Selected: kindCur == "clearlogo"},
			},
		},
		{
			Name:      "status",
			AriaLabel: "Status",
			Options: []listFilterOpt{
				{Value: library.FileStatusFailed, Label: "Failed", Selected: statusCur == library.FileStatusFailed},
				{Value: library.FileStatusOK, Label: "OK", Selected: statusCur == library.FileStatusOK},
				{Value: library.FileStatusUnchecked, Label: "Unchecked", Selected: statusCur == library.FileStatusUnchecked},
				{Value: library.FileStatusInactive, Label: "Inactive", Selected: statusCur == library.FileStatusInactive},
				{Value: library.FileStatusNA, Label: "N/A", Selected: statusCur == library.FileStatusNA},
			},
		},
	}
	annotateFilterSelects(r, selects)
	return selects
}

func filesListBadges(r *http.Request, f library.FileListFilter) []listViewBadge {
	var out []listViewBadge
	if f.Kind != "" {
		out = append(out, listViewBadge{Label: "Type: " + f.Kind, Href: clearQueryKeys(r, "kind")})
	}
	if st := library.NormalizeFileStatus(f.Status); st != "" {
		label := "Status: " + fileStatusFilterLabel(st)
		out = append(out, listViewBadge{Label: label, Href: clearQueryKeys(r, "status")})
	}
	if f.Q != "" {
		out = append(out, listViewBadge{Label: "Search", Href: clearQueryKeys(r, "q")})
	}
	return out
}

func fileStatusFilterLabel(status string) string {
	switch library.NormalizeFileStatus(status) {
	case library.FileStatusFailed:
		return "Failed"
	case library.FileStatusOK:
		return "OK"
	case library.FileStatusUnchecked:
		return "Unchecked"
	case library.FileStatusInactive:
		return "Inactive"
	case library.FileStatusNA:
		return "N/A"
	default:
		return status
	}
}

func filesTableColDefs(showSeries, showVideo bool) []tableColDef {
	cols := []tableColDef{
		{Key: "kind", Label: "Type", Default: true},
		{Key: "path", Label: "File", Default: true},
		{Key: "filepath", Label: "Path", Default: true},
		{Key: "integrity", Label: "Integrity", Default: true},
		{Key: "size", Label: "Size", Default: true},
	}
	if showSeries {
		cols = append(cols, tableColDef{Key: "series", Label: "Series", Default: true})
	}
	if showVideo {
		cols = append(cols, tableColDef{Key: "video", Label: "Video", Default: true})
	}
	cols = append(cols, tableColDef{Key: "acquired", Label: "Acquired", Default: true})
	return cols
}

func appendQueryParam(raw, key string, id int64) string {
	if raw == "" || id <= 0 {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set(key, strconv.FormatInt(id, 10))
	u.RawQuery = q.Encode()
	return u.String()
}
