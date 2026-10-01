package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SeriesActor is one <actor> for tvshow.nfo.
type SeriesActor struct {
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"`
	Order int    `json:"order,omitempty"`
}

// SeriesMeta is editable show metadata (plus computed premiered). Art is on disk only.
type SeriesMeta struct {
	Plot          string
	SortTitle     string
	OriginalTitle string
	Studio        string
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
	Tagline       string
	Country       string
	MPAA          string
	Premiered     string // YYYY-MM-DD UTC day; computed, not operator-editable
}

// SeriesDirMaxRunes caps the on-disk series folder name (same as {series:100}).
const SeriesDirMaxRunes = 100

// SeriesDir returns root/sanitizeName(title) with a 100-rune cap.
func SeriesDir(root, title string) string {
	return filepath.Join(root, sanitizeName(title, SeriesDirMaxRunes))
}

// EnsureSeriesDirCapped renames an uncapped historical series folder to the capped path when needed.
func (s *Store) EnsureSeriesDirCapped(rootPath, title string) error {
	capped := SeriesDir(rootPath, title)
	uncapped := filepath.Join(rootPath, sanitizeName(title, 0))
	if filepath.Clean(capped) == filepath.Clean(uncapped) {
		return nil
	}
	if !dirExists(uncapped) {
		return nil
	}
	if dirExists(capped) || fileExists(capped) {
		return fmt.Errorf("%w: capped series folder already exists: %s", ErrConflict, capped)
	}
	if err := os.MkdirAll(filepath.Dir(capped), 0o755); err != nil {
		return err
	}
	if err := os.Rename(uncapped, capped); err != nil {
		return fmt.Errorf("rename series folder to capped name: %w", err)
	}
	_, err := s.DB.SQL.Exec(`
		UPDATE files SET path = ? || substr(path, ?)
		WHERE path = ? OR path LIKE ?
	`, capped, len(uncapped)+1, uncapped, uncapped+string(filepath.Separator)+"%")
	if err != nil {
		return fmt.Errorf("update file paths after series dir cap: %w", err)
	}
	return nil
}

// SaveSeriesMetadataParams updates editable metadata + optional art ops.
type SaveSeriesMetadataParams struct {
	Plot          string
	SortTitle     string
	OriginalTitle string
	Studio        string
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
	Tagline       string
	Country       string
	MPAA          string
	// ArtSrc maps role → local path to copy (upload or prefetch cache). Empty = leave.
	ArtSrc map[string]string
	// ArtClear roles to delete on disk.
	ArtClear map[string]bool
}

// SaveSeriesMetadata writes DB fields, ensures series dir, applies art, writes tvshow.nfo.
func (s *Store) SaveSeriesMetadata(seriesID int64, p SaveSeriesMetadataParams) error {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	uidType, uidVal := coalesceUniqueID(p.UniqueIDType, p.UniqueIDValue, ser.Meta.UniqueIDType, ser.Meta.UniqueIDValue)
	sortTitle := omitWhenEqualTitle(p.SortTitle, ser.Title)
	origTitle := omitWhenEqualTitle(p.OriginalTitle, ser.Title)
	_, err = s.DB.SQL.Exec(`
		UPDATE series SET
		  plot = ?, sorttitle = ?, originaltitle = ?, studio = ?,
		  genres = ?, tags = ?, uniqueid_type = ?, uniqueid_value = ?,
		  actors = ?, tagline = ?, country = ?, mpaa = ?
		WHERE id = ?
	`, strings.TrimSpace(p.Plot), sortTitle, origTitle,
		strings.TrimSpace(p.Studio), encodeStringSlice(p.Genres), encodeStringSlice(p.Tags),
		uidType, uidVal,
		encodeActors(p.Actors), strings.TrimSpace(p.Tagline), strings.TrimSpace(p.Country),
		strings.TrimSpace(p.MPAA), seriesID)
	if err != nil {
		return err
	}
	dir := SeriesDir(root.Path, ser.Title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create series dir: %w", err)
	}
	for _, role := range seriesArtRoles {
		if p.ArtClear[role] {
			removeArtRole(dir, role)
			continue
		}
		if src := p.ArtSrc[role]; src != "" {
			if err := installArtFile(dir, role, src); err != nil {
				return fmt.Errorf("install %s: %w", role, err)
			}
		}
	}
	ser, err = s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	return s.writeSeriesNFOFor(ser, root.Path)
}

// SeriesHasBusyMediaTasks reports pending/running download, sponsorblock_cut, or media_verify.
func (s *Store) SeriesHasBusyMediaTasks(seriesID int64) (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks t
		LEFT JOIN videos v ON v.id = t.video_id
		WHERE t.status IN ('pending', 'running')
		  AND t.kind IN ('download', 'sponsorblock_cut', 'integrity_check_initial')
		  AND (t.series_id = ? OR v.series_id = ?)
	`, seriesID, seriesID).Scan(&n)
	return n > 0, err
}

// MetaSuggestions lists distinct values for Metadata form datalists.
// One library-wide pool per field name across series + videos (never entity-scoped).
type MetaSuggestions struct {
	Studios    []string
	Genres     []string
	Tags       []string
	Countries  []string
	MPAAs      []string
	ActorNames []string
	ActorRoles []string
}

// DefaultMPAASuggestions are US TV Parental Guidelines seeded into the Content
// rating datalist (free-text; operators may still type any value).
var DefaultMPAASuggestions = []string{
	"TV-Y", "TV-Y7", "TV-G", "TV-PG", "TV-14", "TV-MA",
}

// ListMetaSuggestions returns sorted unique values for studio, genres, tags, country,
// mpaa, actor name, and actor role - pooled from both series and videos rows.
// Same field name ⇒ same pool whether the form is series or video Metadata.
// mpaa also includes US TV Parental Guidelines defaults (union with library values).
func (s *Store) ListMetaSuggestions() (MetaSuggestions, error) {
	var out MetaSuggestions
	studios := map[string]struct{}{}
	genres := map[string]struct{}{}
	tags := map[string]struct{}{}
	countries := map[string]struct{}{}
	mpaas := map[string]struct{}{}
	names := map[string]struct{}{}
	roles := map[string]struct{}{}

	for _, v := range DefaultMPAASuggestions {
		mpaas[v] = struct{}{}
	}

	absorb := func(studio, genresJSON, tagsJSON, country, mpaa, actorsJSON string) {
		if v := strings.TrimSpace(studio); v != "" {
			studios[v] = struct{}{}
		}
		if v := strings.TrimSpace(country); v != "" {
			countries[v] = struct{}{}
		}
		if v := strings.TrimSpace(mpaa); v != "" {
			mpaas[v] = struct{}{}
		}
		for _, g := range decodeStringSlice(genresJSON) {
			if v := strings.TrimSpace(g); v != "" {
				genres[v] = struct{}{}
			}
		}
		for _, t := range decodeStringSlice(tagsJSON) {
			if v := strings.TrimSpace(t); v != "" {
				tags[v] = struct{}{}
			}
		}
		for _, a := range decodeActors(actorsJSON) {
			if v := strings.TrimSpace(a.Name); v != "" {
				names[v] = struct{}{}
			}
			if v := strings.TrimSpace(a.Role); v != "" {
				roles[v] = struct{}{}
			}
		}
	}

	for _, q := range []string{
		`SELECT studio, genres, tags, country, mpaa, actors FROM series`,
		`SELECT studio, genres, tags, country, mpaa, actors FROM videos`,
	} {
		rows, err := s.DB.SQL.Query(q)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var studio, genresJSON, tagsJSON, country, mpaa, actorsJSON string
			if err := rows.Scan(&studio, &genresJSON, &tagsJSON, &country, &mpaa, &actorsJSON); err != nil {
				_ = rows.Close()
				return out, err
			}
			absorb(studio, genresJSON, tagsJSON, country, mpaa, actorsJSON)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return out, err
		}
	}

	out.Studios = sortedKeys(studios)
	out.Genres = sortedKeys(genres)
	out.Tags = sortedKeys(tags)
	out.Countries = sortedKeys(countries)
	out.MPAAs = sortedKeys(mpaas)
	out.ActorNames = sortedKeys(names)
	out.ActorRoles = sortedKeys(roles)
	return out, nil
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}
