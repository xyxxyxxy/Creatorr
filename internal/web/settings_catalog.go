package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

type catalogFieldTab struct {
	Key   string
	Label string
}

var catalogFieldTabs = []catalogFieldTab{
	{settings.CatalogFieldTags, "Tags"},
	{settings.CatalogFieldGenres, "Genres"},
	{settings.CatalogFieldStudio, "Studio"},
	{settings.CatalogFieldCountry, "Country"},
	{settings.CatalogFieldMPAA, "Content rating"},
	{settings.CatalogFieldActorName, "Actor names"},
	{settings.CatalogFieldActorRole, "Actor roles"},
}

func (h *Handler) settingsCatalog(w http.ResponseWriter, r *http.Request) {
	field := strings.TrimSpace(r.URL.Query().Get("field"))
	if field == "" {
		field = settings.CatalogFieldTags
	}
	if !settings.ValidCatalogField(field) {
		http.Redirect(w, r, "/settings/catalog?field="+settings.CatalogFieldTags, http.StatusSeeOther)
		return
	}
	values, err := h.Library.ListCatalogValues(field)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	busy, _ := h.Library.RewriteCatalogMetaBusy()
	softTags, _ := settings.SoftFillTagsEnabled(h.Queue.DB)
	softGenres, _ := settings.SoftFillGenresEnabled(h.Queue.DB)
	pathBusy, _ := h.Queue.PathTouchingSystemBusy()
	warnSoft := (field == settings.CatalogFieldTags && softTags) ||
		(field == settings.CatalogFieldGenres && softGenres)
	render(w, "settings_catalog", struct {
		pageBase
		Field      string
		Tabs       []catalogFieldTab
		Values     []library.CatalogValue
		Busy       bool
		PathBusy   bool
		WarnSoft   bool
	}{
		pageBase: newSettingsPage("Settings · Catalog", "catalog", flashFromQuery(r)),
		Field:    field,
		Tabs:     catalogFieldTabs,
		Values:   values,
		Busy:     busy,
		PathBusy: pathBusy && !busy,
		WarnSoft: warnSoft,
	})
}

func (h *Handler) settingsCatalogValues(w http.ResponseWriter, r *http.Request) {
	field := strings.TrimSpace(r.URL.Query().Get("field"))
	if !settings.ValidCatalogField(field) {
		http.Error(w, "invalid field", http.StatusBadRequest)
		return
	}
	values, err := h.Library.ListCatalogValues(field)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	busy, _ := h.Library.RewriteCatalogMetaBusy()
	softTags, _ := settings.SoftFillTagsEnabled(h.Queue.DB)
	softGenres, _ := settings.SoftFillGenresEnabled(h.Queue.DB)
	warnSoft := (field == settings.CatalogFieldTags && softTags) ||
		(field == settings.CatalogFieldGenres && softGenres)
	render(w, "settings_catalog_values", struct {
		Field    string
		Values   []library.CatalogValue
		Busy     bool
		WarnSoft bool
	}{
		Field:    field,
		Values:   values,
		Busy:     busy,
		WarnSoft: warnSoft,
	})
}

func (h *Handler) actionCatalogVocabRename(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	field := strings.TrimSpace(r.FormValue("field"))
	from := strings.TrimSpace(r.FormValue("from"))
	to := strings.TrimSpace(r.FormValue("to"))
	id, err := h.Library.EnqueueRewriteCatalogMeta(library.RewriteCatalogMetaParams{
		Field: field,
		Op:    library.CatalogOpRename,
		From:  from,
		To:    to,
	})
	h.redirectCatalog(w, r, field, id, err)
}

func (h *Handler) actionCatalogVocabRemove(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	field := strings.TrimSpace(r.FormValue("field"))
	from := strings.TrimSpace(r.FormValue("from"))
	block := r.FormValue("block") == "1"
	op := library.CatalogOpRemove
	if block {
		op = library.CatalogOpRemoveBlock
	}
	id, err := h.Library.EnqueueRewriteCatalogMeta(library.RewriteCatalogMetaParams{
		Field: field,
		Op:    op,
		From:  from,
	})
	h.redirectCatalog(w, r, field, id, err)
}

func (h *Handler) actionCatalogVocabUnblock(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	field := strings.TrimSpace(r.FormValue("field"))
	from := strings.TrimSpace(r.FormValue("from"))
	if err := settings.RemoveSoftFillBlock(h.Queue.DB, field, from); err != nil {
		redirectSettings(w, r, catalogURL(field), "err="+urlQuery(err.Error()))
		return
	}
	redirectSettings(w, r, catalogURL(field), "ok="+urlQuery("SoftFill block removed"))
}

func (h *Handler) redirectCatalog(w http.ResponseWriter, r *http.Request, field string, taskID int64, err error) {
	if err != nil {
		msg := err.Error()
		if errors.Is(err, library.ErrConflict) || errors.Is(err, queue.ErrDuplicate) {
			msg = "Catalog rewrite busy or path-touching task open"
		}
		redirectSettings(w, r, catalogURL(field), "err="+urlQuery(msg))
		return
	}
	redirectSettings(w, r, catalogURL(field), "ok="+urlQuery(fmt.Sprintf("Catalog rewrite queued (#%d)", taskID)))
}

func catalogURL(field string) string {
	if !settings.ValidCatalogField(field) {
		field = settings.CatalogFieldTags
	}
	return "/settings/catalog?field=" + url.QueryEscape(field)
}
