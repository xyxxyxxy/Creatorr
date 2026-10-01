package worker_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func TestDefaultHandlersRequireYtDlp(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := worker.DefaultHandlers(worker.Deps{
		Library: lib,
		YtDlp:   &ytdlp.Client{Bin: filepath.Join(t.TempDir(), "missing-yt-dlp")},
		TmpRoot: t.TempDir(),
	})
	if h[queue.KindScan] == nil || h[queue.KindDownload] == nil {
		t.Fatal("expected scan/download handlers")
	}
}

// Every Kind* constant in queue/kinds.go must have a handler (parsed so new kinds cannot be forgotten).
func TestDefaultHandlersCoverEveryQueueKind(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "queue", "kinds.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if strings.HasPrefix(name.Name, "Kind") && ok && lit.Kind == token.STRING {
					v, _ := strconv.Unquote(lit.Value)
					kinds = append(kinds, v)
				}
			}
		}
	}
	if len(kinds) < 20 {
		t.Fatalf("parsed only %d kinds", len(kinds))
	}
	d, err := db.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	lib := library.NewStore(d, queue.NewStore(d))
	h := worker.DefaultHandlers(worker.Deps{
		Library: lib,
		YtDlp:   &ytdlp.Client{Bin: filepath.Join(t.TempDir(), "missing-yt-dlp")},
		TmpRoot: t.TempDir(),
	})
	for _, k := range kinds {
		if h[k] == nil {
			t.Errorf("no handler for kind %q", k)
		}
	}
	if _, ok := h["delete_sidecar"]; ok {
		t.Error("delete_sidecar kind was removed; handler must not be registered")
	}
}
