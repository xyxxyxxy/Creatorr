package web

import (
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// catalogValueView is one Catalog inventory row plus Browser deep links.
type catalogValueView struct {
	library.CatalogValue
	SeriesHref string
	VideoHref  string
	SourceHref string
}

func catalogFilterQueryKey(field string) string {
	switch field {
	case settings.CatalogFieldGenres:
		return "genre"
	case settings.CatalogFieldTags:
		return "tag"
	case settings.CatalogFieldStudio:
		return "studio"
	case settings.CatalogFieldCountry:
		return "country"
	case settings.CatalogFieldMPAA:
		return "mpaa"
	case settings.CatalogFieldActorName:
		return "actor"
	default:
		// actor_role has no Browser filter.
		return ""
	}
}

func catalogBrowserHref(explorerType, field, value string) string {
	key := catalogFilterQueryKey(field)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return ""
	}
	q := url.Values{}
	q.Set("type", explorerType)
	q.Set(key, value)
	return "/browser?" + q.Encode()
}

func catalogValueViews(field string, values []library.CatalogValue) []catalogValueView {
	out := make([]catalogValueView, len(values))
	for i, v := range values {
		out[i] = catalogValueView{
			CatalogValue: v,
			SeriesHref:   catalogBrowserHref("series", field, v.Value),
			VideoHref:    catalogBrowserHref("videos", field, v.Value),
			SourceHref:   catalogBrowserHref("sources", field, v.Value),
		}
	}
	return out
}
