package ytdlp

// options holds every flag any Client method accepts. Only the fields
// relevant to the called method are populated. Client methods also fill
// ytdlpPath / ffmpegPath / pluginDirs from Client fields.
type options struct {
	url              string
	cookies          string
	outdir           string
	format           string
	flaresolverr     string
	limitRate        string
	sleepRequests    string
	subLangs         []string
	subAuto          bool
	playlistEnd      int
	hlsDir           string
	hlsStartSec      float64
	hlsStartNumber   int
	videoURL         string
	audioURL         string
	videoHeadersJSON string
	audioHeadersJSON string
	matchFilter      string

	ytdlpPath        string
	ffmpegPath       string
	pluginDirs       string
	systemPluginDirs string

	potProviderURL string
	potFetch       string // youtube:fetch_pot value (auto|always|never)
}
