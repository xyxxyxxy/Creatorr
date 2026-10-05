package web

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

const (
	cookieBrowserTree = "creatorr_browser_tree"

	treeKindRoot     = "root"
	treeKindSeries   = "series"
	treeKindSource   = "source"
	treeKindNoSource = "nosource"
	treeKindVideo    = "video"
	treeKindFile     = "file"
)

// libraryTreeNode is one row in the Browser library tree component.
type libraryTreeNode struct {
	Kind        string
	ID          int64
	SeriesID    int64
	VideoID     int64
	Name        string
	Icon        string
	Href        string
	Expandable  bool
	ChildCount  int
	Depth       int
	ParentKind  string
	ParentID    int64
	ChildrenURL string // expand / first page
}

// libraryTreeLiveData is the tree panel root.
type libraryTreeLiveData struct {
	Roots []libraryTreeNode
}

// libraryTreeChildrenData is one children page fragment.
type libraryTreeChildrenData struct {
	Nodes      []libraryTreeNode
	HasMore    bool
	MoreURL    string
	ParentKind string
	ParentID   int64
	Depth      int
}

func browserTree(r *http.Request) bool {
	return readListPrefCookie(r, cookieBrowserTree) == "1"
}

func writeBrowserTreeCookie(w http.ResponseWriter, on bool) {
	if on {
		writeListPrefCookie(w, cookieBrowserTree, "1")
	} else {
		writeListPrefCookie(w, cookieBrowserTree, "")
	}
}

func browserTreeToggleHref(r *http.Request, typ string, wantTree bool) string {
	q := url.Values{}
	q.Set("type", typ)
	if wantTree {
		q.Set("tree", "1")
	} else {
		q.Set("tree", "0")
	}
	for k, vs := range r.URL.Query() {
		switch k {
		case "type", "tree", "wide", "page", "through", "at":
			continue
		}
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	return "/browser?" + q.Encode()
}

func rootFolderLabel(r library.RootFolder) string {
	if name := strings.TrimSpace(r.Name); name != "" {
		return name
	}
	base := filepath.Base(strings.TrimRight(strings.TrimSpace(r.Path), "/"))
	if base != "" && base != "." && base != "/" {
		return base
	}
	return r.Path
}

func sourceTreeLabel(src library.Source) string {
	if src.Label.Valid && strings.TrimSpace(src.Label.String) != "" {
		return strings.TrimSpace(src.Label.String)
	}
	return src.URL
}

func treeChildrenURL(parentKind string, parentID int64, offset int) string {
	q := url.Values{}
	q.Set("parent", parentKind)
	q.Set("parent_id", strconv.FormatInt(parentID, 10))
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	return "/explorer/tree-children?" + q.Encode()
}

func (h *Handler) loadLibraryTree() (libraryTreeLiveData, error) {
	var out libraryTreeLiveData
	if h.Library == nil {
		return out, nil
	}
	roots, err := h.Library.ListRoots()
	if err != nil {
		return out, err
	}
	ids := make([]int64, 0, len(roots))
	for _, r := range roots {
		ids = append(ids, r.ID)
	}
	counts, err := h.Library.CountSeriesByRootIDs(ids)
	if err != nil {
		return out, err
	}
	out.Roots = make([]libraryTreeNode, 0, len(roots))
	for _, r := range roots {
		n := counts[r.ID]
		node := libraryTreeNode{
			Kind:       treeKindRoot,
			ID:         r.ID,
			Name:       rootFolderLabel(r),
			Icon:       "folder",
			Expandable: n > 0,
			ChildCount: n,
			Depth:      0,
		}
		if node.Expandable {
			node.ChildrenURL = treeChildrenURL(treeKindRoot, r.ID, 0)
		}
		out.Roots = append(out.Roots, node)
	}
	return out, nil
}

func (h *Handler) libraryTreeChildren(w http.ResponseWriter, r *http.Request) {
	parent := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("parent")))
	parentID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("parent_id")), 10, 64)
	offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	if offset < 0 {
		offset = 0
	}
	if parentID <= 0 && parent != treeKindNoSource {
		http.Error(w, "invalid parent_id", http.StatusBadRequest)
		return
	}
	data, err := h.loadTreeChildren(parent, parentID, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	render(w, "library_tree_children", data)
}

func (h *Handler) loadTreeChildren(parent string, parentID int64, offset int) (libraryTreeChildrenData, error) {
	limit := InfiniteChunkSize
	switch parent {
	case treeKindRoot:
		return h.treeChildrenRoot(parentID, offset, limit)
	case treeKindSeries:
		return h.treeChildrenSeries(parentID, offset, limit)
	case treeKindSource:
		return h.treeChildrenSource(parentID, offset, limit)
	case treeKindNoSource:
		return h.treeChildrenNoSource(parentID, offset, limit)
	case treeKindVideo:
		return h.treeChildrenVideo(parentID, offset, limit)
	default:
		return libraryTreeChildrenData{}, fmt.Errorf("unknown parent kind")
	}
}

func (h *Handler) treeChildrenRoot(rootID int64, offset, limit int) (libraryTreeChildrenData, error) {
	out := libraryTreeChildrenData{ParentKind: treeKindRoot, ParentID: rootID, Depth: 1}
	filter := library.SeriesListFilter{RootIDs: []int64{rootID}, Sort: library.SortTitle, SortDir: library.SortDirAsc}
	total, err := h.Library.CountSeriesFiltered(filter)
	if err != nil {
		return out, err
	}
	list, err := h.Library.ListSeriesFiltered(filter, limit, offset)
	if err != nil {
		return out, err
	}
	out.Nodes = make([]libraryTreeNode, 0, len(list))
	for _, ser := range list {
		n := int(ser.SourceCount)
		// Sources and/or any videos (incl. null-source) make the series expandable.
		expandable := ser.SourceCount > 0 || ser.VideoCount > 0
		childCount := n
		if ser.VideoCount > 0 && ser.SourceCount == 0 {
			childCount = 1 // No source bucket only
		} else if ser.SourceCount > 0 {
			// Approximate tip: sources; No source may also appear.
			childCount = n
		}
		node := libraryTreeNode{
			Kind:       treeKindSeries,
			ID:         ser.ID,
			SeriesID:   ser.ID,
			Name:       ser.Title,
			Icon:       "tv",
			Href:       fmt.Sprintf("/series/%d", ser.ID),
			Expandable: expandable,
			ChildCount: childCount,
			Depth:      1,
			ParentKind: treeKindRoot,
			ParentID:   rootID,
		}
		if expandable {
			node.ChildrenURL = treeChildrenURL(treeKindSeries, ser.ID, 0)
		}
		out.Nodes = append(out.Nodes, node)
	}
	out.HasMore = offset+len(list) < total
	if out.HasMore {
		out.MoreURL = treeChildrenURL(treeKindRoot, rootID, offset+len(list))
	}
	return out, nil
}

func (h *Handler) treeChildrenSeries(seriesID int64, offset, limit int) (libraryTreeChildrenData, error) {
	out := libraryTreeChildrenData{ParentKind: treeKindSeries, ParentID: seriesID, Depth: 2}
	if _, err := h.Library.GetSeries(seriesID, false); err != nil {
		return out, fmt.Errorf("series not found")
	}
	nullCount, err := h.Library.CountVideosWithNullSource(seriesID)
	if err != nil {
		return out, err
	}
	filter := library.SourceListFilter{SeriesID: seriesID, Sort: library.SortSourceLabel, SortDir: library.SortDirAsc}
	total, err := h.Library.CountSourcesFiltered(filter)
	if err != nil {
		return out, err
	}
	list, err := h.Library.ListSourcesFiltered(filter, limit, offset)
	if err != nil {
		return out, err
	}
	counts, err := h.Library.CountVideosBySource(seriesID)
	if err != nil {
		return out, err
	}
	if offset == 0 && nullCount > 0 {
		node := libraryTreeNode{
			Kind:       treeKindNoSource,
			ID:         seriesID, // parent series id (virtual)
			SeriesID:   seriesID,
			Name:       "No source",
			Icon:       "inbox",
			Expandable: true,
			ChildCount: nullCount,
			Depth:      2,
			ParentKind: treeKindSeries,
			ParentID:   seriesID,
		}
		node.ChildrenURL = treeChildrenURL(treeKindNoSource, seriesID, 0)
		out.Nodes = append(out.Nodes, node)
	}
	for _, src := range list {
		vc := counts[src.ID]
		node := libraryTreeNode{
			Kind:       treeKindSource,
			ID:         src.ID,
			SeriesID:   seriesID,
			Name:       sourceTreeLabel(src.Source),
			Icon:       "rss",
			Href:       fmt.Sprintf("/series/%d/sources/%d", seriesID, src.ID),
			Expandable: vc > 0,
			ChildCount: vc,
			Depth:      2,
			ParentKind: treeKindSeries,
			ParentID:   seriesID,
		}
		if node.Expandable {
			node.ChildrenURL = treeChildrenURL(treeKindSource, src.ID, 0)
		}
		out.Nodes = append(out.Nodes, node)
	}
	out.HasMore = offset+len(list) < total
	if out.HasMore {
		out.MoreURL = treeChildrenURL(treeKindSeries, seriesID, offset+len(list))
	}
	return out, nil
}

func (h *Handler) treeChildrenSource(sourceID int64, offset, limit int) (libraryTreeChildrenData, error) {
	out := libraryTreeChildrenData{ParentKind: treeKindSource, ParentID: sourceID, Depth: 3}
	src, err := h.Library.GetSourceByID(sourceID)
	if err != nil || src == nil {
		return out, fmt.Errorf("source not found")
	}
	total, err := h.Library.CountVideosForSource(sourceID)
	if err != nil {
		return out, err
	}
	list, err := h.Library.ListVideosBySourcePage(sourceID, limit, offset)
	if err != nil {
		return out, err
	}
	return h.finishVideoNodes(out, list, total, offset, treeKindSource, sourceID, src.SeriesID)
}

func (h *Handler) treeChildrenNoSource(seriesID int64, offset, limit int) (libraryTreeChildrenData, error) {
	out := libraryTreeChildrenData{ParentKind: treeKindNoSource, ParentID: seriesID, Depth: 3}
	if _, err := h.Library.GetSeries(seriesID, false); err != nil {
		return out, fmt.Errorf("series not found")
	}
	filter := library.VideoListFilter{SourceIDs: []int64{library.VideoSourceImport}}
	total, err := h.Library.CountVideosFiltered(seriesID, filter)
	if err != nil {
		return out, err
	}
	list, err := h.Library.ListVideosPageFiltered(seriesID, filter, limit, offset)
	if err != nil {
		return out, err
	}
	return h.finishVideoNodes(out, list, total, offset, treeKindNoSource, seriesID, seriesID)
}

func (h *Handler) finishVideoNodes(out libraryTreeChildrenData, list []library.Video, total, offset int, parentKind string, parentID, seriesID int64) (libraryTreeChildrenData, error) {
	ids := make([]int64, 0, len(list))
	for _, v := range list {
		ids = append(ids, v.ID)
	}
	fileCounts, err := h.Library.CountFilesByVideoIDs(ids)
	if err != nil {
		return out, err
	}
	out.Nodes = make([]libraryTreeNode, 0, len(list))
	for _, v := range list {
		fc := fileCounts[v.ID]
		sid := v.SeriesID
		if sid == 0 {
			sid = seriesID
		}
		node := libraryTreeNode{
			Kind:       treeKindVideo,
			ID:         v.ID,
			SeriesID:   sid,
			Name:       v.Title,
			Icon:       "film",
			Href:       fmt.Sprintf("/series/%d/videos/%d", sid, v.ID),
			Expandable: fc > 0,
			ChildCount: fc,
			Depth:      3,
			ParentKind: parentKind,
			ParentID:   parentID,
		}
		if node.Expandable {
			node.ChildrenURL = treeChildrenURL(treeKindVideo, v.ID, 0)
		}
		out.Nodes = append(out.Nodes, node)
	}
	out.HasMore = offset+len(list) < total
	if out.HasMore {
		out.MoreURL = treeChildrenURL(parentKind, parentID, offset+len(list))
	}
	return out, nil
}

func (h *Handler) treeChildrenVideo(videoID int64, offset, limit int) (libraryTreeChildrenData, error) {
	out := libraryTreeChildrenData{ParentKind: treeKindVideo, ParentID: videoID, Depth: 4}
	v, err := h.Library.GetVideo(videoID)
	if err != nil || v == nil {
		return out, fmt.Errorf("video not found")
	}
	filter := library.FileListFilter{VideoID: videoID, Sort: library.SortFileKind}
	total, err := h.Library.CountFilesFiltered(filter)
	if err != nil {
		return out, err
	}
	list, err := h.Library.ListFilesFiltered(filter, limit, offset)
	if err != nil {
		return out, err
	}
	out.Nodes = make([]libraryTreeNode, 0, len(list))
	for _, f := range list {
		vid := int64(0)
		if f.VideoID.Valid {
			vid = f.VideoID.Int64
		}
		sid := f.SeriesID
		if sid == 0 {
			sid = v.SeriesID
		}
		href := ""
		if vid > 0 && sid > 0 {
			href = fmt.Sprintf("/series/%d/videos/%d/files/%d", sid, vid, f.ID)
		}
		out.Nodes = append(out.Nodes, libraryTreeNode{
			Kind:       treeKindFile,
			ID:         f.ID,
			SeriesID:   sid,
			VideoID:    vid,
			Name:       filepath.Base(f.Path),
			Icon:       sidecarKindIcon(f.Kind),
			Href:       href,
			Expandable: false,
			Depth:      4,
			ParentKind: treeKindVideo,
			ParentID:   videoID,
		})
	}
	out.HasMore = offset+len(list) < total
	if out.HasMore {
		out.MoreURL = treeChildrenURL(treeKindVideo, videoID, offset+len(list))
	}
	return out, nil
}
