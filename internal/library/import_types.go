package library

// ImportCandidate is one untracked inbox file with match suggestions.
type ImportCandidate struct {
	Path              string         `json:"path"`
	Filename          string         `json:"filename"`
	Role              string         `json:"role"` // video | nfo | json | thumb | sub | other
	IDs               []ImportIDHint `json:"ids"`
	SuggestedVideoID  *int64         `json:"suggested_video_id"`
	SuggestedSeriesID *int64         `json:"suggested_series_id"`
	SuggestedTitle    string         `json:"suggested_title,omitempty"`
	SuggestedRemoteID string         `json:"suggested_remote_id,omitempty"`
	// SuggestedRemoteIDGenerated is reserved (always false); synthetic remotes are not suggested.
	SuggestedRemoteIDGenerated bool `json:"suggested_remote_id_generated,omitempty"`
	// SuggestedUploadDate is RFC3339 UTC prefill for unmatched create (sidecar, else file mtime).
	SuggestedUploadDate string `json:"suggested_upload_date,omitempty"`
	// SuggestedUploadDateFromMtime is true when SuggestedUploadDate came from file mtime (not sidecar).
	SuggestedUploadDateFromMtime bool               `json:"suggested_upload_date_from_mtime,omitempty"`
	SuggestedHandler             string             `json:"suggested_handler_id,omitempty"`
	SuggestedWebpageURL          string             `json:"suggested_webpage_url,omitempty"`
	// SuggestedPackRole is path-detected Specials/extras role when a series folder is known.
	SuggestedPackRole string `json:"suggested_special_feature,omitempty"`
	MatchType         string `json:"match_type,omitempty"`
	MatchLabel        string `json:"match_label,omitempty"`
	VideoSuggestions             []VideoSuggestion  `json:"video_suggestions"`
	SeriesSuggestions            []SeriesSuggestion `json:"series_suggestions"`
	// SeriesFolderDraftKey locks this candidate to an unknown tvshow.nfo tree (create on confirm).
	SeriesFolderDraftKey string `json:"series_folder_draft_key,omitempty"`
	// SeriesFolderLocked is true when a tvshow.nfo tree owns this path (known or draft).
	SeriesFolderLocked bool `json:"series_folder_locked,omitempty"`
}

// ImportIDHint is an extracted remote id from filename/sidecars.
type ImportIDHint struct {
	HandlerID string `json:"handler_id"`
	RemoteID  string `json:"remote_id"`
}

// VideoSuggestion is a title-similarity hit.
type VideoSuggestion struct {
	VideoID     int64   `json:"video_id"`
	SeriesID    int64   `json:"series_id"`
	Title       string  `json:"title"`
	SeriesTitle string  `json:"series_title"`
	RemoteID    string  `json:"remote_id"`
	Score       float64 `json:"score"`
}

// SeriesSuggestion is a series-title similarity hit.
type SeriesSuggestion struct {
	SeriesID int64   `json:"series_id"`
	Title    string  `json:"title"`
	Score    float64 `json:"score"`
}

// ImportScanResult is the scan response body.
type ImportScanResult struct {
	ImportPath    string               `json:"import_path"`
	Candidates    []ImportCandidate    `json:"candidates"`
	SeriesFolders []ImportSeriesFolder `json:"series_folders"`
}

// ImportPickerVideo is a dropdown row for the Import UI.
type ImportPickerVideo struct {
	ID          int64  `json:"id"`
	SeriesID    int64  `json:"series_id"`
	Title       string `json:"title"`
	SeriesTitle string `json:"series_title"`
	Status      string `json:"status"`
	HasMedia    bool   `json:"has_media"` // true when a kind=video files row exists
	HasThumb    bool   `json:"has_thumb"` // true when a kind=thumb files row exists
	PackRole    string `json:"special_feature"`
}

// ImportPickerSeries is a series row for the Import Match UI (no sources loaded).
type ImportPickerSeries struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	RootID int64  `json:"root_id"`
}

// ImportPickerVideoQuery filters ListImportPickerVideos.
type ImportPickerVideoQuery struct {
	SeriesID *int64
	Q        string
	HasMedia *bool
	IDs      []int64
	Limit    int
}
