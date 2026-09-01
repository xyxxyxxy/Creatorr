package ytdlp

import (
	"os"
	"path/filepath"
)

// DefaultBootstrapBin is the image-internal yt-dlp copy (not the runtime path).
const DefaultBootstrapBin = "/usr/local/share/creatorr/yt-dlp"

// Paths holds managed and bootstrap yt-dlp locations for this layout.
type Paths struct {
	Managed   string
	Bootstrap string
}

// PathsForLayout returns yt-dlp paths for container (/data) or local var/ dev layout.
func PathsForLayout(dataDirExists bool) Paths {
	if dataDirExists {
		return Paths{
			Managed:   "/data/bin/yt-dlp",
			Bootstrap: DefaultBootstrapBin,
		}
	}
	return Paths{
		Managed:   filepath.Join("var", "data", "bin", "yt-dlp"),
		Bootstrap: "",
	}
}

// DataDirExists reports whether the container data root is present.
func DataDirExists() bool {
	fi, err := os.Stat("/data")
	return err == nil && fi.IsDir()
}
