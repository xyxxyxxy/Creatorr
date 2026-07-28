package ytdlp

// options holds every flag any Client method accepts. Only the fields
// relevant to the called method are populated. Client methods also fill
// ytdlpPath / pluginDirs from Client fields.
type options struct {
	url              string
	cookies          string
	username         string
	password         string
	outdir           string
	format           string
	flaresolverr     string
	limitRate        string
	sleepRequests    string
	subLangs         []string
	subAuto          bool
	playlistEnd      int
	matchFilter      string

	ytdlpPath        string
	pluginDirs       string
	systemPluginDirs string

	potProviderURL string
	potFetch       string // youtube:fetch_pot value (auto|always|never)
}
