package library

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// CatalogField is a library-wide Catalog / SoftFill-block field.
type CatalogField = string

const (
	CatalogFieldStudio    = settings.CatalogFieldStudio
	CatalogFieldGenres    = settings.CatalogFieldGenres
	CatalogFieldTags      = settings.CatalogFieldTags
	CatalogFieldCountry   = settings.CatalogFieldCountry
	CatalogFieldMPAA      = settings.CatalogFieldMPAA
	CatalogFieldActorName = settings.CatalogFieldActorName
	CatalogFieldActorRole = settings.CatalogFieldActorRole
)

// CatalogValue is one inventory row (case-fold key collapsed).
type CatalogValue struct {
	Value       string
	SeriesCount int
	VideoCount  int
	SourceCount int
	Blocked     bool
}

// CatalogOp is a rewrite_catalog_meta operation.
const (
	CatalogOpRename      = "rename"
	CatalogOpRemove      = "remove"
	CatalogOpRemoveBlock = "remove_block"
)

// RewriteCatalogMetaParams enqueues a Catalog vocabulary rewrite.
type RewriteCatalogMetaParams struct {
	Field string
	Op    string // rename | remove | remove_block
	From  string
	To    string // rename only
}

// RewriteCatalogMetaBusy reports whether rewrite_catalog_meta is pending or running.
func (s *Store) RewriteCatalogMetaBusy() (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
	`, queue.SystemDomain, queue.KindRewriteCatalogMeta, queue.StatusPending, queue.StatusRunning).Scan(&n)
	return n > 0, err
}

// EnqueueRewriteCatalogMeta queues a path-touching Catalog rewrite.
// For remove_block, SoftFill block is written before enqueue.
func (s *Store) EnqueueRewriteCatalogMeta(p RewriteCatalogMetaParams) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	if !settings.ValidCatalogField(p.Field) {
		return 0, fmt.Errorf("%w: field", ErrInvalid)
	}
	from := strings.TrimSpace(p.From)
	if from == "" {
		return 0, fmt.Errorf("%w: from required", ErrInvalid)
	}
	op := strings.TrimSpace(p.Op)
	to := strings.TrimSpace(p.To)
	switch op {
	case CatalogOpRename:
		if to == "" {
			return 0, fmt.Errorf("%w: to required", ErrInvalid)
		}
		if strings.EqualFold(from, to) {
			return 0, fmt.Errorf("%w: from and to are the same", ErrInvalid)
		}
	case CatalogOpRemove, CatalogOpRemoveBlock:
		to = ""
	default:
		return 0, fmt.Errorf("%w: op", ErrInvalid)
	}
	if busy, err := s.RewriteCatalogMetaBusy(); err != nil {
		return 0, err
	} else if busy {
		return 0, fmt.Errorf("%w: catalog rewrite already running", ErrConflict)
	}
	if s.Queue != nil {
		if pathBusy, err := s.Queue.PathTouchingSystemBusy(); err != nil {
			return 0, err
		} else if pathBusy {
			return 0, fmt.Errorf("%w: path-touching task open", ErrConflict)
		}
	}
	if op == CatalogOpRemoveBlock {
		if err := settings.AddSoftFillBlock(s.DB, p.Field, from); err != nil {
			return 0, err
		}
	}
	if op == CatalogOpRename {
		if err := settings.MoveSoftFillBlock(s.DB, p.Field, from, to); err != nil {
			return 0, err
		}
	}
	msg := "Catalog remove " + p.Field
	switch op {
	case CatalogOpRename:
		msg = "Catalog rename " + p.Field
	case CatalogOpRemoveBlock:
		msg = "Catalog remove and block SoftFill " + p.Field
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:  queue.OriginManual,
		Kind:    queue.KindRewriteCatalogMeta,
		Domain:  queue.SystemDomain,
		Payload: map[string]any{
			"field":          p.Field,
			"op":             op,
			"from":           from,
			"to":             to,
			"table":          "series",
			"id_cursor":      0,
			"series_touched": []int64{},
			"video_touched":  []int64{},
			"patched":        0,
			"nfo_rewrote":    0,
			"nfo_failed":     0,
		},
		Message: msg,
	})
}

type catalogFoldBucket struct {
	counts map[string]int // spelling → count
	series int
	videos int
	sources int
}

// ListCatalogValues returns case-folded inventory for one Catalog field.
func (s *Store) ListCatalogValues(field string) ([]CatalogValue, error) {
	if !settings.ValidCatalogField(field) {
		return nil, fmt.Errorf("%w: field", ErrInvalid)
	}
	bl, err := settings.GetSoftFillBlocklist(s.DB)
	if err != nil {
		return nil, err
	}
	buckets := map[string]*catalogFoldBucket{}
	absorbScalar := func(table string, raw string) {
		v := strings.TrimSpace(raw)
		if v == "" {
			return
		}
		key := strings.ToLower(v)
		b := buckets[key]
		if b == nil {
			b = &catalogFoldBucket{counts: map[string]int{}}
			buckets[key] = b
		}
		b.counts[v]++
		switch table {
		case "series":
			b.series++
		case "videos":
			b.videos++
		case "sources":
			b.sources++
		}
	}
	absorbList := func(table string, raw string) {
		for _, v := range decodeStringSlice(raw) {
			absorbScalar(table, v)
		}
	}
	absorbActors := func(table, which, raw string) {
		for _, a := range decodeActors(raw) {
			switch which {
			case "name":
				absorbScalar(table, a.Name)
			case "role":
				absorbScalar(table, a.Role)
			}
		}
	}

	type colSpec struct {
		table string
		q     string
		mode  string // scalar | list | actor_name | actor_role
	}
	var specs []colSpec
	switch field {
	case CatalogFieldStudio:
		specs = []colSpec{
			{"series", `SELECT studio FROM series`, "scalar"},
			{"videos", `SELECT studio FROM videos`, "scalar"},
			{"sources", `SELECT studio FROM sources`, "scalar"},
		}
	case CatalogFieldCountry:
		specs = []colSpec{
			{"series", `SELECT country FROM series`, "scalar"},
			{"videos", `SELECT country FROM videos`, "scalar"},
			{"sources", `SELECT country FROM sources`, "scalar"},
		}
	case CatalogFieldMPAA:
		specs = []colSpec{
			{"series", `SELECT mpaa FROM series`, "scalar"},
			{"videos", `SELECT mpaa FROM videos`, "scalar"},
			{"sources", `SELECT mpaa FROM sources`, "scalar"},
		}
	case CatalogFieldGenres:
		specs = []colSpec{
			{"series", `SELECT genres FROM series`, "list"},
			{"videos", `SELECT genres FROM videos`, "list"},
			{"sources", `SELECT genres FROM sources`, "list"},
		}
	case CatalogFieldTags:
		specs = []colSpec{
			{"series", `SELECT tags FROM series`, "list"},
			{"videos", `SELECT tags FROM videos`, "list"},
			{"sources", `SELECT tags FROM sources`, "list"},
		}
	case CatalogFieldActorName:
		specs = []colSpec{
			{"series", `SELECT actors FROM series`, "actor_name"},
			{"videos", `SELECT actors FROM videos`, "actor_name"},
			{"sources", `SELECT actors FROM sources`, "actor_name"},
		}
	case CatalogFieldActorRole:
		specs = []colSpec{
			{"series", `SELECT actors FROM series`, "actor_role"},
			{"videos", `SELECT actors FROM videos`, "actor_role"},
			{"sources", `SELECT actors FROM sources`, "actor_role"},
		}
	}

	for _, sp := range specs {
		rows, qerr := s.DB.SQL.Query(sp.q)
		if qerr != nil {
			return nil, qerr
		}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				_ = rows.Close()
				return nil, err
			}
			switch sp.mode {
			case "scalar":
				absorbScalar(sp.table, raw)
			case "list":
				absorbList(sp.table, raw)
			case "actor_name":
				absorbActors(sp.table, "name", raw)
			case "actor_role":
				absorbActors(sp.table, "role", raw)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}

	out := make([]CatalogValue, 0, len(buckets))
	for _, b := range buckets {
		label := pickCanonicalSpelling(b.counts)
		out = append(out, CatalogValue{
			Value:       label,
			SeriesCount: b.series,
			VideoCount:  b.videos,
			SourceCount: b.sources,
			Blocked:     bl.Blocked(field, label),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Value) < strings.ToLower(out[j].Value)
	})
	// SoftFill-blocked with zero usage still appear.
	for _, blocked := range bl[field] {
		key := strings.ToLower(strings.TrimSpace(blocked))
		if key == "" {
			continue
		}
		found := false
		for _, row := range out {
			if strings.ToLower(row.Value) == key {
				found = true
				break
			}
		}
		if !found {
			out = append(out, CatalogValue{Value: strings.TrimSpace(blocked), Blocked: true})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Value) < strings.ToLower(out[j].Value)
	})
	return out, nil
}

func pickCanonicalSpelling(counts map[string]int) string {
	best := ""
	bestN := -1
	for spelling, n := range counts {
		if n > bestN || (n == bestN && (best == "" || spelling < best)) {
			best = spelling
			bestN = n
		}
	}
	return best
}

func catalogFoldMatch(value, from string) bool {
	return strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(from))
}
