package web

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestCatalogBrowserHref(t *testing.T) {
	// url.Values.Encode sorts keys: tag before type.
	if got := catalogBrowserHref("videos", settings.CatalogFieldTags, "tea"); got != "/browser?tag=tea&type=videos" {
		t.Fatalf("got %q", got)
	}
	if got := catalogBrowserHref("sources", settings.CatalogFieldStudio, "Acme"); got != "/browser?studio=Acme&type=sources" {
		t.Fatalf("studio got %q", got)
	}
	if got := catalogBrowserHref("series", settings.CatalogFieldActorRole, "Host"); got != "/browser?actor_role=Host&type=series" {
		t.Fatalf("actor_role got %q", got)
	}
	if catalogBrowserHref("sources", settings.CatalogFieldStudio, "  ") != "" {
		t.Fatal("empty value")
	}
}
