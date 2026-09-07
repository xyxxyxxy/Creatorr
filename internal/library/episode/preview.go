package episode

// PreviewApplyRenameCap is the max rename rows returned in a preview modal.
const PreviewApplyRenameCap = 500

// ApplyRenamePreviewItem is one planned disk rename for PreviewApplyEpisodeNaming.
type ApplyRenamePreviewItem struct {
	VideoID     int64
	Title       string
	SeriesTitle string
	From        string // path under root when possible
	To          string
}

// ApplyRenamePreview is a dry-run of Apply episode format for a maintenance scope.
type ApplyRenamePreview struct {
	Items        []ApplyRenamePreviewItem
	TotalChanges int
	SkippedBusy  int
	Unchanged    int
	Truncated    bool
}
