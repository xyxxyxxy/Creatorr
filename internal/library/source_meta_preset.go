package library

import (
	"database/sql"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// SoftFillVideoMetaFromSourcePreset merges source default catalog fields into video fields.
// Singles: fill only when existing empty/NULL. Lists: union. Actors: by name; fill role if empty.
func SoftFillVideoMetaFromSourcePreset(
	studio, country, mpaa, packRole string,
	genres, tags []string,
	actors []SeriesActor,
	preset Source,
) (studioOut, countryOut, mpaaOut, packRoleOut string, genresOut, tagsOut []string, actorsOut []SeriesActor, changed bool) {
	studioOut, countryOut, mpaaOut = studio, country, mpaa
	packRoleOut = packRole
	genresOut = append([]string(nil), ParseStringListFields(genres)...)
	tagsOut = append([]string(nil), ParseStringListFields(tags)...)
	actorsOut = append([]SeriesActor(nil), actors...)

	if strings.TrimSpace(studioOut) == "" && strings.TrimSpace(preset.Studio) != "" {
		studioOut = strings.TrimSpace(preset.Studio)
		changed = true
	}
	if strings.TrimSpace(countryOut) == "" && strings.TrimSpace(preset.Country) != "" {
		countryOut = strings.TrimSpace(preset.Country)
		changed = true
	}
	if strings.TrimSpace(mpaaOut) == "" && strings.TrimSpace(preset.MPAA) != "" {
		mpaaOut = strings.TrimSpace(preset.MPAA)
		changed = true
	}
	// Regular is NULL/empty; SoftFill kind when video has no kind and preset sets one.
	if NormalizePackRole(packRoleOut) == PackRoleRegular || strings.TrimSpace(packRoleOut) == "" {
		if ps := strings.TrimSpace(preset.SpecialFeature); ps != "" && NormalizePackRole(ps) != PackRoleRegular {
			packRoleOut = NormalizePackRole(ps)
			changed = true
		}
	}

	beforeG := encodeStringSlice(genresOut)
	genresOut = MergeCategoryGenres(genresOut, preset.Genres)
	if encodeStringSlice(genresOut) != beforeG {
		changed = true
	}
	beforeT := encodeStringSlice(tagsOut)
	tagsOut = MergeCategoryGenres(tagsOut, preset.Tags)
	if encodeStringSlice(tagsOut) != beforeT {
		changed = true
	}

	mergedActors, actorsChanged := mergeActorsSoftFill(actorsOut, preset.Actors)
	actorsOut = mergedActors
	if actorsChanged {
		changed = true
	}
	return
}

func mergeActorsSoftFill(existing, preset []SeriesActor) ([]SeriesActor, bool) {
	if len(preset) == 0 {
		return existing, false
	}
	out := append([]SeriesActor(nil), existing...)
	changed := false
	index := map[string]int{}
	for i, a := range out {
		n := strings.ToLower(strings.TrimSpace(a.Name))
		if n != "" {
			index[n] = i
		}
	}
	for _, p := range preset {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if i, ok := index[key]; ok {
			if strings.TrimSpace(out[i].Role) == "" && strings.TrimSpace(p.Role) != "" {
				out[i].Role = strings.TrimSpace(p.Role)
				changed = true
			}
			continue
		}
		out = append(out, SeriesActor{Name: name, Role: strings.TrimSpace(p.Role)})
		index[key] = len(out) - 1
		changed = true
	}
	return out, changed
}

// ApplySourceMetadataPreset SoftFills the video's source catalog defaults onto the video row.
func (s *Store) ApplySourceMetadataPreset(videoID int64) (bool, error) {
	if videoID <= 0 {
		return false, nil
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return false, err
	}
	if !v.SourceID.Valid || v.SourceID.Int64 <= 0 {
		return false, nil
	}
	src, err := s.GetSource(v.SeriesID, v.SourceID.Int64)
	if err != nil {
		if err == ErrNotFound {
			return false, nil
		}
		return false, err
	}
	if !sourcePresetHasValues(src) {
		return false, nil
	}
	preset := filterSourcePresetSoftFillBlocks(*src, s)
	studio, country, mpaa, packRole, genres, tags, actors, changed := SoftFillVideoMetaFromSourcePreset(
		v.Studio, v.Country, v.MPAA, v.PackRole, v.Genres, v.Tags, v.Actors, preset,
	)
	if !changed {
		return false, nil
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  studio = ?, country = ?, mpaa = ?, special_feature = ?,
		  genres = ?, tags = ?, actors = ?
		WHERE id = ?
	`, studio, country, mpaa, PackRoleDBValue(packRole),
		encodeStringSlice(genres), encodeStringSlice(tags), encodeActors(actors), videoID)
	if err != nil {
		return false, err
	}
	return true, nil
}

func filterSourcePresetSoftFillBlocks(src Source, s *Store) Source {
	bl, err := settings.GetSoftFillBlocklist(s.DB)
	if err != nil || len(bl) == 0 {
		return src
	}
	if bl.Blocked(settings.CatalogFieldStudio, src.Studio) {
		src.Studio = ""
	}
	if bl.Blocked(settings.CatalogFieldCountry, src.Country) {
		src.Country = ""
	}
	if bl.Blocked(settings.CatalogFieldMPAA, src.MPAA) {
		src.MPAA = ""
	}
	src.Genres = settings.FilterSoftFillBlockedStrings(bl, settings.CatalogFieldGenres, src.Genres)
	src.Tags = settings.FilterSoftFillBlockedStrings(bl, settings.CatalogFieldTags, src.Tags)
	if len(src.Actors) > 0 {
		var actors []SeriesActor
		for _, a := range src.Actors {
			if bl.Blocked(settings.CatalogFieldActorName, a.Name) {
				continue
			}
			if bl.Blocked(settings.CatalogFieldActorRole, a.Role) {
				a.Role = ""
			}
			actors = append(actors, a)
		}
		src.Actors = actors
	}
	return src
}

func sourcePresetHasValues(src *Source) bool {
	if src == nil {
		return false
	}
	if strings.TrimSpace(src.Studio) != "" || strings.TrimSpace(src.Country) != "" || strings.TrimSpace(src.MPAA) != "" {
		return true
	}
	if strings.TrimSpace(src.SpecialFeature) != "" && NormalizePackRole(src.SpecialFeature) != PackRoleRegular {
		return true
	}
	if len(ParseStringListFields(src.Genres)) > 0 || len(ParseStringListFields(src.Tags)) > 0 || len(src.Actors) > 0 {
		return true
	}
	return false
}

func scanNullPackRole(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return strings.TrimSpace(ns.String)
}
