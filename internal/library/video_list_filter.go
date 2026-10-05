package library

import "strings"

func appendVideoListFilterSQL(b *strings.Builder, args *[]any, f VideoListFilter) {
	if title := strings.TrimSpace(f.Title); title != "" {
		col := videoTextColumn(f.QField)
		b.WriteString(` AND ` + col + ` LIKE ? ESCAPE '\' COLLATE NOCASE`)
		*args = append(*args, likeContainsPattern(title))
	}
	appendInt64In(b, args, "series_id", f.SeriesIDs)
	if len(f.Statuses) > 0 {
		b.WriteString(` AND status IN (` + sqlIntPlaceholders(len(f.Statuses)) + `)`)
		for _, st := range f.Statuses {
			*args = append(*args, st)
		}
	}
	appendVideoSourceIDsSQL(b, args, f.SourceIDs)
	mediaTypes := trimNonEmptyStrings(f.MediaTypes)
	if len(mediaTypes) > 0 {
		b.WriteString(` AND media_type != ''`)
		appendStringsIn(b, args, "media_type", mediaTypes, false)
	}
	appendStringsIn(b, args, "studio", f.Studios, true)
	appendStringsIn(b, args, "country", f.Countries, true)
	appendStringsIn(b, args, "mpaa", f.MPAAs, true)
	appendJSONStringListMatch(b, args, "genres", f.Genres)
	appendJSONStringListMatch(b, args, "tags", f.Tags)
	appendJSONActorNameMatch(b, args, "actors", f.Actors)
	appendVideoPresenceSQL(b, f.Empty, f.NotEmpty)
	years := uniqNonZeroInts(f.Years)
	if len(years) > 0 {
		b.WriteString(` AND upload_date IS NOT NULL AND trim(upload_date) != ''`)
		appendIntsIn(b, args, `CAST(strftime('%Y', upload_date) AS INTEGER)`, years)
	}
	appendVideoPackRolesSQL(b, args, f.PackRoles)
	if f.FromDay == "" && f.ToDay == "" {
		return
	}
	b.WriteString(` AND upload_date IS NOT NULL AND upload_date != ''`)
	if f.FromDay != "" {
		b.WriteString(` AND date(upload_date) >= date(?)`)
		*args = append(*args, f.FromDay)
	}
	if f.ToDay != "" {
		b.WriteString(` AND date(upload_date) <= date(?)`)
		*args = append(*args, f.ToDay)
	}
}

func appendVideoSourceIDsSQL(b *strings.Builder, args *[]any, ids []int64) {
	if len(ids) == 0 {
		return
	}
	var hasImport bool
	var posIDs []int64
	for _, id := range ids {
		if id == VideoSourceImport {
			hasImport = true
		} else if id > 0 {
			posIDs = append(posIDs, id)
		}
	}
	if !hasImport && len(posIDs) == 0 {
		return
	}
	var parts []string
	if hasImport {
		parts = append(parts, `source_id IS NULL`)
	}
	if len(posIDs) == 1 {
		parts = append(parts, `source_id = ?`)
		*args = append(*args, posIDs[0])
	} else if len(posIDs) > 1 {
		parts = append(parts, `source_id IN (`+sqlIntPlaceholders(len(posIDs))+`)`)
		for _, id := range posIDs {
			*args = append(*args, id)
		}
	}
	appendAndOrGroup(b, parts)
}

func appendVideoPackRolesSQL(b *strings.Builder, args *[]any, roles []string) {
	var parts []string
	for _, role := range roles {
		role = strings.TrimSpace(role)
		switch role {
		case "":
			continue
		case PackRoleRegular:
			parts = append(parts, SQLPackRoleRegularPred)
		case VideoPackRoleAnySpecial:
			parts = append(parts, `NOT (`+SQLPackRoleRegularPred+`)`)
		default:
			parts = append(parts, `special_feature = ?`)
			*args = append(*args, NormalizePackRole(role))
		}
	}
	appendAndOrGroup(b, parts)
}
