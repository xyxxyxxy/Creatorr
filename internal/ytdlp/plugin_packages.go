package ytdlp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PluginPackage is one yt-dlp plugin package shown on Settings → Connect.
// On-disk packages contain yt_dlp_plugins/; yt-dlp-ejs is always listed as
// bundled in the official managed binary (not a --plugin-dirs entry).
type PluginPackage struct {
	Name    string // package directory name (e.g. bgutil) or yt-dlp-ejs
	Baked   bool   // true when found under the image/system plugins path
	Bundled bool   // true when shipped inside the managed yt-dlp binary (not on disk)
}

// Source is the operator-facing origin label for Settings → Connect.
func (p PluginPackage) Source() string {
	if p.Bundled {
		return "bundled"
	}
	if p.Baked {
		return "baked"
	}
	return "mounted"
}

// DocsURL is an optional upstream docs link for known packages.
func (p PluginPackage) DocsURL() string {
	switch p.Name {
	case "bgutil":
		return "https://github.com/Brainicism/bgutil-ytdlp-pot-provider"
	case "yt-dlp-ejs":
		return "https://github.com/yt-dlp/ejs"
	default:
		return ""
	}
}

// NoteKind selects the Notes cell treatment on Connect → plugins.
type NoteKind string

const (
	NoteNone NoteKind = ""
	NoteDeno NoteKind = "deno" // Deno on PATH for EJS
	NotePOT  NoteKind = "pot"  // PO token provider plugin
)

// NoteKind returns the Notes column kind for this package (empty = dash).
func (p PluginPackage) NoteKind() NoteKind {
	switch p.Name {
	case "yt-dlp-ejs":
		return NoteDeno
	case "bgutil":
		return NotePOT
	default:
		return NoteNone
	}
}

// ListPluginPackages scans systemRoot then operatorRoot for packages that contain
// a yt_dlp_plugins directory. Duplicate names keep the baked entry. Always
// includes bundled yt-dlp-ejs when not already present on disk.
// Order: bundled, then baked, then mounted; name ascending within each group.
func ListPluginPackages(systemRoot, operatorRoot string) []PluginPackage {
	byName := map[string]PluginPackage{}
	for _, p := range scanPluginPackages(systemRoot, true) {
		byName[p.Name] = p
	}
	for _, p := range scanPluginPackages(operatorRoot, false) {
		if _, ok := byName[p.Name]; ok {
			continue
		}
		byName[p.Name] = p
	}
	if _, ok := byName["yt-dlp-ejs"]; !ok {
		byName["yt-dlp-ejs"] = PluginPackage{Name: "yt-dlp-ejs", Bundled: true}
	}
	out := make([]PluginPackage, 0, len(byName))
	for _, p := range byName {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := pluginSourceRank(out[i]), pluginSourceRank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func pluginSourceRank(p PluginPackage) int {
	switch {
	case p.Bundled:
		return 0
	case p.Baked:
		return 1
	default:
		return 2
	}
}

// ListPluginPackages returns packages under this client's system + operator plugin dirs.
func (c *Client) ListPluginPackages() []PluginPackage {
	if c == nil {
		return ListPluginPackages("", "")
	}
	return ListPluginPackages(c.SystemPluginsDir, c.PluginsDir)
}

func scanPluginPackages(root string, baked bool) []PluginPackage {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	if hasYtDlpPluginsPkg(root) {
		name := filepath.Base(root)
		if name == "" || name == "." || name == string(filepath.Separator) {
			return nil
		}
		return []PluginPackage{{Name: name, Baked: baked}}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []PluginPackage
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if hasYtDlpPluginsPkg(filepath.Join(root, name)) {
			out = append(out, PluginPackage{Name: name, Baked: baked})
		}
	}
	return out
}
