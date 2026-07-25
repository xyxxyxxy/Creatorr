package web

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

//go:embed static/* static/vendor/* templates/* partials/*
var embedded embed.FS

var (
	tmplOnce sync.Once
	tmpl     *template.Template
	staticH  http.Handler
)

// WebDev reports whether CREATORR_WEB_DEV is enabled (reload HTML/CSS/JS from disk).
func WebDev() bool {
	v := strings.TrimSpace(os.Getenv("CREATORR_WEB_DEV"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// WebDir is the on-disk UI root (templates/, partials/, static/). Default internal/web.
func WebDir() string {
	if d := strings.TrimSpace(os.Getenv("CREATORR_WEB_DIR")); d != "" {
		return d
	}
	return "internal/web"
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"today": func() string {
			return time.Now().UTC().Format("2006-01-02")
		},
		"yesNo": func(b bool) string {
			if b {
				return "yes"
			}
			return "no"
		},
		"pct": func(p *float64) int {
			if p == nil {
				return 0
			}
			return int(*p * 100)
		},
		// progressActive is true only for mid-progress (0,1); nil/0/100 → spinner UI.
		"progressActive": func(p *float64) bool {
			return p != nil && *p > 0 && *p < 1
		},
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: odd argument count")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				k, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key must be string")
				}
				m[k] = values[i+1]
			}
			return m, nil
		},
		"list": func(items ...any) []any {
			return items
		},
		"displayURL": DisplayURL,
		"retentionDays": func(n sql.NullInt64) int64 {
			if !n.Valid {
				return 0
			}
			return library.RetentionDaysFromSeconds(n.Int64)
		},
		"retentionDaysValue": func(n sql.NullInt64) string {
			if !n.Valid || n.Int64 <= 0 {
				return ""
			}
			return strconv.FormatInt(library.RetentionDaysFromSeconds(n.Int64), 10)
		},
		"toJSON": func(v any) (template.JS, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return template.JS(b), nil
		},
		"joinStrings": func(ss []string) string {
			return strings.Join(ss, ", ")
		},
		"containsString": func(list any, want string) bool {
			switch v := list.(type) {
			case []string:
				for _, s := range v {
					if s == want {
						return true
					}
				}
			case []any:
				for _, x := range v {
					if s, ok := x.(string); ok && s == want {
						return true
					}
				}
			}
			return false
		},
		"formatActors":      formatActorsTemplate,
		"historyEventError": historyEventError,
		"prettyCommand":     exectrace.Pretty,
		"rateLimitParts": func(raw any) map[string]string {
			var s string
			switch v := raw.(type) {
			case string:
				s = v
			case nil:
			default:
				s = fmt.Sprint(v)
			}
			val, unit := settings.SplitDownloadRateLimit(s)
			return map[string]string{"Value": val, "Unit": unit}
		},
	}
}

func formatActorsTemplate(actors []library.SeriesActor) string {
	return library.FormatActorsForm(actors)
}

func parseTemplates(fsys fs.FS) (*template.Template, error) {
	return template.New("").Funcs(templateFuncs()).ParseFS(fsys,
		"partials/*.html",
		"templates/*.html",
	)
}

func loadDisk() (*template.Template, error) {
	root := WebDir()
	return parseTemplates(os.DirFS(root))
}

func load() {
	tmplOnce.Do(func() {
		var err error
		tmpl, err = parseTemplates(embedded)
		if err != nil {
			panic(err)
		}
		sub, err := fs.Sub(embedded, "static")
		if err != nil {
			panic(err)
		}
		staticH = http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	})
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var t *template.Template
	if WebDev() {
		parsed, err := loadDisk()
		if err != nil {
			http.Error(w, "web-dev templates: "+err.Error(), http.StatusInternalServerError)
			return
		}
		t = parsed
	} else {
		load()
		t = tmpl
	}
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// StaticHandler serves CSS/JS under /static/.
// With CREATORR_WEB_DEV, serves from {CREATORR_WEB_DIR}/static with no-store cache.
func StaticHandler() http.Handler {
	if WebDev() {
		dir := filepath.Join(WebDir(), "static")
		fs := http.FileServer(http.Dir(dir))
		return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			fs.ServeHTTP(w, r)
		}))
	}
	load()
	return staticH
}
