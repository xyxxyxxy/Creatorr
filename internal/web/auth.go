package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/auth"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func (h *Handler) setupGet(w http.ResponseWriter, r *http.Request) {
	needs, err := settings.NeedsSetup(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !needs {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	username, _ := settings.AuthUsername(h.Queue.DB)
	render(w, "setup", map[string]any{
		"Title":    "Setup",
		"Username": username,
		"Error":    r.URL.Query().Get("err"),
	})
}

func (h *Handler) setupPost(w http.ResponseWriter, r *http.Request) {
	needs, err := settings.NeedsSetup(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !needs {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")
	if username == "" {
		http.Redirect(w, r, "/setup?err="+url.QueryEscape("username required"), http.StatusSeeOther)
		return
	}
	if err := auth.ValidatePassword(password, confirm); err != nil {
		http.Redirect(w, r, "/setup?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Redirect(w, r, "/setup?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	res, err := settings.CompleteSetup(h.Queue.DB, username, hash)
	if err != nil {
		http.Redirect(w, r, "/setup?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if res.AlreadySetup {
		http.Redirect(w, r, "/login?err="+url.QueryEscape("setup already completed"), http.StatusSeeOther)
		return
	}
	if err := auth.IssueSession(w, r, h.Queue.DB, username); err != nil {
		http.Redirect(w, r, "/login?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) loginGet(w http.ResponseWriter, r *http.Request) {
	needs, err := settings.NeedsSetup(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if needs {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	render(w, "login", map[string]any{
		"Title": "Login",
		"Error": r.URL.Query().Get("err"),
		"Next":  auth.SafeNextPath(r.URL.Query().Get("next")),
	})
}

func (h *Handler) loginLimiter() *auth.LoginLimiter {
	return auth.DefaultLoginLimiter
}

func (h *Handler) loginPost(w http.ResponseWriter, r *http.Request) {
	needs, err := settings.NeedsSetup(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if needs {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	ip := auth.ClientIP(r)
	lim := h.loginLimiter()
	if !lim.Allow(ip) {
		http.Redirect(w, r, "/login?err="+url.QueryEscape("too many failed attempts; try again later"), http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	wantUser, err := settings.AuthUsername(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	hash, err := settings.AuthPasswordHash(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !auth.CheckLogin(wantUser, username, hash, password) {
		lim.Fail(ip)
		http.Redirect(w, r, "/login?err="+url.QueryEscape("invalid username or password"), http.StatusSeeOther)
		return
	}
	lim.Success(ip)
	if err := auth.IssueSession(w, r, h.Queue.DB, wantUser); err != nil {
		http.Redirect(w, r, "/login?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, auth.SafeNextPath(r.FormValue("next")), http.StatusSeeOther)
}

func (h *Handler) logoutPost(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
