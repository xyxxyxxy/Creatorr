package library

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

type catalogRewritePayload struct {
	Field         string  `json:"field"`
	Op            string  `json:"op"`
	From          string  `json:"from"`
	To            string  `json:"to"`
	Table         string  `json:"table"` // series | videos | sources | nfo_videos | nfo_series | done
	IDCursor      int64   `json:"id_cursor"`
	SeriesTouched []int64 `json:"series_touched"`
	VideoTouched  []int64 `json:"video_touched"`
	Patched       int     `json:"patched"`
	NFORewrote    int     `json:"nfo_rewrote"`
	NFOFailed     int     `json:"nfo_failed"`
}

func (p catalogRewritePayload) persistMap() map[string]any {
	return map[string]any{
		"field":          p.Field,
		"op":             p.Op,
		"from":           p.From,
		"to":             p.To,
		"table":          p.Table,
		"id_cursor":      p.IDCursor,
		"series_touched": p.SeriesTouched,
		"video_touched":  p.VideoTouched,
		"patched":        p.Patched,
		"nfo_rewrote":    p.NFORewrote,
		"nfo_failed":     p.NFOFailed,
	}
}

func (p *catalogRewritePayload) remove() bool {
	return p.Op == CatalogOpRemove || p.Op == CatalogOpRemoveBlock
}

// RewriteCatalogMetaPass applies Catalog rename/remove across series/videos/sources then NFOs.
func (s *Store) RewriteCatalogMetaPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) error {
	var p catalogRewritePayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	if p.Table == "" {
		p.Table = "series"
	}
	persist := func() error {
		return s.Queue.UpdatePayload(task.ID, p.persistMap())
	}
	report := func(msg string) {
		if progress != nil {
			progress(msg, nil)
		}
	}

	for {
		select {
		case <-ctx.Done():
			_ = persist()
			return ctx.Err()
		default:
		}
		switch p.Table {
		case "series":
			if err := s.catalogPatchTable(ctx, &p, "series", persist, report); err != nil {
				return err
			}
			p.Table = "videos"
			p.IDCursor = 0
			_ = persist()
		case "videos":
			if err := s.catalogPatchTable(ctx, &p, "videos", persist, report); err != nil {
				return err
			}
			p.Table = "sources"
			p.IDCursor = 0
			_ = persist()
		case "sources":
			if err := s.catalogPatchTable(ctx, &p, "sources", persist, report); err != nil {
				return err
			}
			p.Table = "nfo_videos"
			p.IDCursor = 0
			_ = persist()
		case "nfo_videos":
			if err := s.catalogRewriteVideoNFOs(ctx, &p, task.ID, persist, report); err != nil {
				return err
			}
			p.Table = "nfo_series"
			p.IDCursor = 0
			_ = persist()
		case "nfo_series":
			if err := s.catalogRewriteSeriesNFOs(ctx, &p, persist, report); err != nil {
				return err
			}
			p.Table = "done"
			_ = persist()
			if progress != nil {
				pct := 1.0
				progress(fmt.Sprintf("Catalog rewrite: patched=%d nfo=%d failed=%d", p.Patched, p.NFORewrote, p.NFOFailed), &pct)
			}
			return nil
		case "done":
			return nil
		default:
			return fmt.Errorf("unknown catalog rewrite phase %q", p.Table)
		}
	}
}

func (s *Store) catalogPatchTable(
	ctx context.Context,
	p *catalogRewritePayload,
	table string,
	persist func() error,
	report func(string),
) error {
	colSQL, mode := catalogColumnSQL(p.Field)
	if colSQL == "" {
		return fmt.Errorf("unsupported field %q", p.Field)
	}
	q := fmt.Sprintf(`SELECT id, %s FROM %s WHERE id > ? ORDER BY id`, colSQL, table)
	rows, err := s.DB.SQL.Query(q, p.IDCursor)
	if err != nil {
		return err
	}
	type row struct {
		id  int64
		raw string
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.raw); err != nil {
			_ = rows.Close()
			return err
		}
		batch = append(batch, r)
	}
	_ = rows.Close()

	remove := p.remove()
	for _, r := range batch {
		select {
		case <-ctx.Done():
			_ = persist()
			return ctx.Err()
		default:
		}
		var next string
		var changed bool
		switch mode {
		case "scalar":
			next, changed = patchCatalogScalar(r.raw, p.From, p.To, remove)
		case "list":
			next, changed = patchCatalogStringList(r.raw, p.From, p.To, remove)
		case "actors":
			next, changed = patchCatalogActors(r.raw, p.Field, p.From, p.To, remove)
		}
		p.IDCursor = r.id
		if !changed {
			_ = persist()
			continue
		}
		_, err := s.DB.SQL.Exec(fmt.Sprintf(`UPDATE %s SET %s = ? WHERE id = ?`, table, colSQL), next, r.id)
		if err != nil {
			return err
		}
		p.Patched++
		switch table {
		case "series":
			p.SeriesTouched = appendUniqInt64(p.SeriesTouched, r.id)
		case "videos":
			p.VideoTouched = appendUniqInt64(p.VideoTouched, r.id)
		}
		_ = persist()
		report(fmt.Sprintf("Catalog patch %s id=%d patched=%d", table, r.id, p.Patched))
	}
	return nil
}

func catalogColumnSQL(field string) (col, mode string) {
	switch field {
	case CatalogFieldStudio:
		return "studio", "scalar"
	case CatalogFieldCountry:
		return "country", "scalar"
	case CatalogFieldMPAA:
		return "mpaa", "scalar"
	case CatalogFieldGenres:
		return "genres", "list"
	case CatalogFieldTags:
		return "tags", "list"
	case CatalogFieldActorName, CatalogFieldActorRole:
		return "actors", "actors"
	default:
		return "", ""
	}
}

func appendUniqInt64(ids []int64, id int64) []int64 {
	for _, x := range ids {
		if x == id {
			return ids
		}
	}
	return append(ids, id)
}

func (s *Store) catalogRewriteVideoNFOs(
	ctx context.Context,
	p *catalogRewritePayload,
	taskID int64,
	persist func() error,
	report func(string),
) error {
	ids := append([]int64(nil), p.VideoTouched...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if id <= p.IDCursor {
			continue
		}
		select {
		case <-ctx.Done():
			_ = persist()
			return ctx.Err()
		default:
		}
		_, err := s.RewriteVideoNFO(id, taskID)
		if err != nil {
			p.NFOFailed++
		} else {
			p.NFORewrote++
		}
		p.IDCursor = id
		_ = persist()
		report(fmt.Sprintf("Catalog NFO video=%d rewrote=%d", id, p.NFORewrote))
	}
	return nil
}

func (s *Store) catalogRewriteSeriesNFOs(
	ctx context.Context,
	p *catalogRewritePayload,
	persist func() error,
	report func(string),
) error {
	ids := append([]int64(nil), p.SeriesTouched...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if id <= p.IDCursor {
			continue
		}
		select {
		case <-ctx.Done():
			_ = persist()
			return ctx.Err()
		default:
		}
		ser, err := s.GetSeries(id, false)
		if err != nil {
			p.NFOFailed++
			p.IDCursor = id
			_ = persist()
			continue
		}
		root, err := s.GetRoot(ser.RootID)
		if err != nil {
			p.NFOFailed++
			p.IDCursor = id
			_ = persist()
			continue
		}
		if err := s.writeSeriesNFOFor(ser, root.Path); err != nil {
			p.NFOFailed++
		} else {
			p.NFORewrote++
		}
		p.IDCursor = id
		_ = persist()
		report(fmt.Sprintf("Catalog NFO series=%d rewrote=%d", id, p.NFORewrote))
	}
	return nil
}
