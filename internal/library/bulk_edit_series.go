package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// BulkEditSeriesParams patches selected series. Nil field pointers mean unchanged.
// Metadata pointers (including empty string / empty slice) mean set/clear that field.
type BulkEditSeriesParams struct {
	SeriesIDs        []int64
	RootID           *int64
	QualityProfileID *int64
	DeliveryMode     *string
	Monitored        *bool
	Studio           *string
	Country          *string
	MPAA             *string
	Genres           *[]string
	Tags             *[]string
	Actors           *[]SeriesActor
}

func (p BulkEditSeriesParams) hasSettings() bool {
	return p.RootID != nil || p.QualityProfileID != nil || p.DeliveryMode != nil || p.Monitored != nil
}

func (p BulkEditSeriesParams) hasMetadata() bool {
	return p.Studio != nil || p.Country != nil || p.MPAA != nil ||
		p.Genres != nil || p.Tags != nil || p.Actors != nil
}

func (p BulkEditSeriesParams) hasAnyField() bool {
	return p.hasSettings() || p.hasMetadata()
}

// BulkEditSeriesBusy reports whether a bulk_edit_series task is pending or running.
func (s *Store) BulkEditSeriesBusy() (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
	`, queue.SystemDomain, queue.KindBulkEditSeries, queue.StatusPending, queue.StatusRunning).Scan(&n)
	return n > 0, err
}

// EnqueueBulkEditSeries queues a system-lane bulk series edit (settings and/or metadata).
func (s *Store) EnqueueBulkEditSeries(p BulkEditSeriesParams) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	ids := uniqPositive(p.SeriesIDs)
	if len(ids) == 0 {
		return 0, fmt.Errorf("%w: series_ids required", ErrInvalid)
	}
	if !p.hasAnyField() {
		return 0, fmt.Errorf("%w: at least one field required", ErrInvalid)
	}
	if p.RootID != nil {
		if _, err := s.GetRoot(*p.RootID); err != nil {
			return 0, fmt.Errorf("%w: root_id", ErrInvalid)
		}
	}
	if p.QualityProfileID != nil {
		if _, err := s.GetProfile(*p.QualityProfileID); err != nil {
			return 0, fmt.Errorf("%w: quality_profile_id", ErrInvalid)
		}
	}
	var mode string
	if p.DeliveryMode != nil {
		mode = NormalizeDeliveryMode(*p.DeliveryMode)
		if mode != DeliveryVideo && mode != DeliveryAudio {
			return 0, fmt.Errorf("%w: delivery_mode", ErrInvalid)
		}
	}

	payload := map[string]any{
		"series_ids": ids,
		"index":      0,
	}
	if p.RootID != nil {
		payload["root_id"] = *p.RootID
	}
	if p.QualityProfileID != nil {
		payload["quality_profile_id"] = *p.QualityProfileID
	}
	if p.DeliveryMode != nil {
		payload["delivery_mode"] = mode
	}
	if p.Monitored != nil {
		payload["monitored"] = *p.Monitored
	}
	if p.Studio != nil {
		payload["studio"] = *p.Studio
		payload["set_studio"] = true
	}
	if p.Country != nil {
		payload["country"] = *p.Country
		payload["set_country"] = true
	}
	if p.MPAA != nil {
		payload["mpaa"] = *p.MPAA
		payload["set_mpaa"] = true
	}
	if p.Genres != nil {
		payload["genres"] = *p.Genres
		payload["set_genres"] = true
	}
	if p.Tags != nil {
		payload["tags"] = *p.Tags
		payload["set_tags"] = true
	}
	if p.Actors != nil {
		payload["actors"] = *p.Actors
		payload["set_actors"] = true
	}

	msg := "Bulk edit series"
	if p.hasMetadata() && !p.hasSettings() {
		msg = "Bulk edit series metadata"
	}
	id, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind:    queue.KindBulkEditSeries,
		Domain:  queue.SystemDomain,
		Payload: payload,
		Message: msg,
	})
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, fmt.Errorf("%w: bulk edit already queued", ErrConflict)
		}
		return 0, err
	}
	return id, nil
}

// SetSeriesMonitoredBulk sets monitored on each id. Continues on missing series.
// Returns updated count and how many ids were skipped (not found).
func (s *Store) SetSeriesMonitoredBulk(ids []int64, monitored bool) (updated, skipped int, err error) {
	for _, id := range uniqPositive(ids) {
		if err := s.SetSeriesMonitored(id, monitored); err != nil {
			if errors.Is(err, ErrNotFound) {
				skipped++
				continue
			}
			return updated, skipped, err
		}
		updated++
	}
	return updated, skipped, nil
}

// ListSeriesIDsFiltered returns series ids matching filter (title order).
func (s *Store) ListSeriesIDsFiltered(filter SeriesListFilter) ([]int64, error) {
	var b strings.Builder
	b.WriteString(`SELECT s.id FROM series s WHERE 1=1`)
	args := []any{}
	appendSeriesListFilterSQL(&b, &args, filter)
	b.WriteString(` ORDER BY s.title COLLATE NOCASE`)
	rows, err := s.DB.SQL.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type bulkEditSeriesPayload struct {
	SeriesIDs        []int64       `json:"series_ids"`
	Index            int           `json:"index"`
	RootID           *int64        `json:"root_id"`
	QualityProfileID *int64        `json:"quality_profile_id"`
	DeliveryMode     *string       `json:"delivery_mode"`
	Monitored        *bool         `json:"monitored"`
	SetStudio        bool          `json:"set_studio"`
	Studio           string        `json:"studio"`
	SetCountry       bool          `json:"set_country"`
	Country          string        `json:"country"`
	SetMPAA          bool          `json:"set_mpaa"`
	MPAA             string        `json:"mpaa"`
	SetGenres        bool          `json:"set_genres"`
	Genres           []string      `json:"genres"`
	SetTags          bool          `json:"set_tags"`
	Tags             []string      `json:"tags"`
	SetActors        bool          `json:"set_actors"`
	Actors           []SeriesActor `json:"actors"`
}

// BulkEditSeriesPass applies queued bulk edits; resumable via payload index.
func (s *Store) BulkEditSeriesPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (updated, skipped, failed int, err error) {
	var p bulkEditSeriesPayload
	if task.Payload != "" {
		if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
			return 0, 0, 0, fmt.Errorf("%w: payload: %v", ErrInvalid, err)
		}
	}
	ids := uniqPositive(p.SeriesIDs)
	n := len(ids)
	if n == 0 {
		return 0, 0, 0, fmt.Errorf("%w: series_ids required", ErrInvalid)
	}
	if p.Index < 0 {
		p.Index = 0
	}
	for i := p.Index; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return updated, skipped, failed, err
		}
		sid := ids[i]
		if progress != nil {
			pct := float64(i) / float64(n) * 100
			progress(fmt.Sprintf("Updating %d/%d", i+1, n), &pct)
		}
		ser, gerr := s.GetSeries(sid, false)
		if gerr != nil {
			if errors.Is(gerr, ErrNotFound) {
				skipped++
			} else {
				failed++
			}
			_ = s.persistBulkEditCursor(task.ID, i+1, p)
			continue
		}
		if err := s.applyBulkEditOne(ser, p); err != nil {
			failed++
			_ = s.persistBulkEditCursor(task.ID, i+1, p)
			continue
		}
		updated++
		_ = s.persistBulkEditCursor(task.ID, i+1, p)
	}
	if progress != nil {
		pct := 100.0
		progress(BulkEditSeriesMessage(updated, skipped, failed), &pct)
	}
	return updated, skipped, failed, nil
}

func (s *Store) applyBulkEditOne(ser *Series, p bulkEditSeriesPayload) error {
	if p.RootID != nil || p.QualityProfileID != nil || p.DeliveryMode != nil {
		up := UpdateSeriesParams{}
		if p.RootID != nil {
			up.RootID = p.RootID
		}
		if p.QualityProfileID != nil {
			up.QualityProfileID = p.QualityProfileID
		}
		if p.DeliveryMode != nil {
			m := NormalizeDeliveryMode(*p.DeliveryMode)
			up.DeliveryMode = &m
		}
		if _, err := s.UpdateSeries(ser.ID, up); err != nil {
			return err
		}
		ser2, err := s.GetSeries(ser.ID, false)
		if err != nil {
			return err
		}
		ser = ser2
	}
	if p.Monitored != nil {
		if err := s.SetSeriesMonitored(ser.ID, *p.Monitored); err != nil {
			return err
		}
	}
	if !p.SetStudio && !p.SetCountry && !p.SetMPAA && !p.SetGenres && !p.SetTags && !p.SetActors {
		return nil
	}
	meta := SaveSeriesMetadataParams{
		Plot:          ser.Meta.Plot,
		SortTitle:     ser.Meta.SortTitle,
		OriginalTitle: ser.Meta.OriginalTitle,
		Studio:        ser.Meta.Studio,
		Genres:        append([]string(nil), ser.Meta.Genres...),
		Tags:          append([]string(nil), ser.Meta.Tags...),
		UniqueIDType:  ser.Meta.UniqueIDType,
		UniqueIDValue: ser.Meta.UniqueIDValue,
		Actors:        append([]SeriesActor(nil), ser.Meta.Actors...),
		Tagline:       ser.Meta.Tagline,
		Country:       ser.Meta.Country,
		MPAA:          ser.Meta.MPAA,
	}
	if p.SetStudio {
		meta.Studio = strings.TrimSpace(p.Studio)
	}
	if p.SetCountry {
		meta.Country = strings.TrimSpace(p.Country)
	}
	if p.SetMPAA {
		meta.MPAA = strings.TrimSpace(p.MPAA)
	}
	if p.SetGenres {
		meta.Genres = ParseStringListFields(p.Genres)
	}
	if p.SetTags {
		meta.Tags = ParseStringListFields(p.Tags)
	}
	if p.SetActors {
		meta.Actors = normalizeActorsList(p.Actors)
	}
	return s.SaveSeriesMetadata(ser.ID, meta)
}

func normalizeActorsList(actors []SeriesActor) []SeriesActor {
	var out []SeriesActor
	seen := map[string]struct{}{}
	for _, a := range actors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, SeriesActor{
			Name:  name,
			Role:  strings.TrimSpace(a.Role),
			Order: len(out),
		})
	}
	return out
}

func (s *Store) persistBulkEditCursor(taskID int64, index int, p bulkEditSeriesPayload) error {
	if s.Queue == nil {
		return nil
	}
	p.Index = index
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	return s.Queue.UpdatePayload(taskID, m)
}

// BulkEditSeriesMessage summarizes a finished bulk edit pass.
func BulkEditSeriesMessage(updated, skipped, failed int) string {
	parts := []string{fmt.Sprintf("Updated %d", updated)}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d", skipped))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("failed %d", failed))
	}
	return strings.Join(parts, ", ")
}

// CommonMetaString is a bulk-metadata field that is either unanimous or mixed.
type CommonMetaString struct {
	Same  bool   `json:"same"`
	Value string `json:"value,omitempty"`
}

// CommonMetaStrings is an ordered string list field (genres/tags).
type CommonMetaStrings struct {
	Same  bool     `json:"same"`
	Value []string `json:"value,omitempty"`
}

// CommonMetaActors is an ordered actor list; order is significant.
type CommonMetaActors struct {
	Same  bool          `json:"same"`
	Value []SeriesActor `json:"value,omitempty"`
}

// CommonSeriesMetadata reports which bulk-metadata fields are identical across ids.
// Same=false means mixed; Same=true with empty value/list means every series is empty.
type CommonSeriesMetadata struct {
	Studio  CommonMetaString  `json:"studio"`
	Country CommonMetaString  `json:"country"`
	MPAA    CommonMetaString  `json:"mpaa"`
	Genres  CommonMetaStrings `json:"genres"`
	Tags    CommonMetaStrings `json:"tags"`
	Actors  CommonMetaActors  `json:"actors"`
}

func stringSliceEqualOrdered(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// seriesActorsEqualOrdered compares name+role in list order (Order field ignored).
func seriesActorsEqualOrdered(a, b []SeriesActor) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i].Name) != strings.TrimSpace(b[i].Name) {
			return false
		}
		if strings.TrimSpace(a[i].Role) != strings.TrimSpace(b[i].Role) {
			return false
		}
	}
	return true
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneActors(in []SeriesActor) []SeriesActor {
	if len(in) == 0 {
		return nil
	}
	out := make([]SeriesActor, len(in))
	copy(out, in)
	return out
}

// CommonMetaInt64 is a bulk field that is either unanimous or mixed (root / profile ids).
type CommonMetaInt64 struct {
	Same  bool  `json:"same"`
	Value int64 `json:"value,omitempty"`
}

// CommonMetaBool is a bulk bool field (e.g. monitored).
type CommonMetaBool struct {
	Same  bool `json:"same"`
	Value bool `json:"value"`
}

// CommonSeriesSettings reports unanimous settings fields across series ids.
type CommonSeriesSettings struct {
	DeliveryMode     CommonMetaString `json:"delivery_mode"`
	Monitored        CommonMetaBool   `json:"monitored"`
	RootID           CommonMetaInt64  `json:"root_id"`
	QualityProfileID CommonMetaInt64  `json:"quality_profile_id"`
}

// CommonSeriesSettings returns unanimous bulk-edit settings for the given series ids.
// Missing ids are skipped; ErrNotFound if none resolve.
func (s *Store) CommonSeriesSettings(ids []int64) (CommonSeriesSettings, error) {
	ids = uniqPositive(ids)
	if len(ids) == 0 {
		return CommonSeriesSettings{}, fmt.Errorf("%w: series_ids required", ErrInvalid)
	}
	var (
		delivery              string
		monitored             bool
		rootID, profileID     int64
		deliverySame          = true
		monitoredSame         = true
		rootSame              = true
		profileSame           = true
		n                     int
	)
	for _, id := range ids {
		ser, err := s.GetSeries(id, false)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return CommonSeriesSettings{}, err
		}
		n++
		if n == 1 {
			delivery = NormalizeDeliveryMode(ser.DeliveryMode)
			monitored = ser.Monitored
			rootID = ser.RootID
			profileID = ser.QualityProfileID
			continue
		}
		if deliverySame && delivery != NormalizeDeliveryMode(ser.DeliveryMode) {
			deliverySame = false
		}
		if monitoredSame && monitored != ser.Monitored {
			monitoredSame = false
		}
		if rootSame && rootID != ser.RootID {
			rootSame = false
		}
		if profileSame && profileID != ser.QualityProfileID {
			profileSame = false
		}
	}
	if n == 0 {
		return CommonSeriesSettings{}, fmt.Errorf("%w: no series found", ErrNotFound)
	}
	out := CommonSeriesSettings{
		DeliveryMode:     CommonMetaString{Same: deliverySame},
		Monitored:        CommonMetaBool{Same: monitoredSame},
		RootID:           CommonMetaInt64{Same: rootSame},
		QualityProfileID: CommonMetaInt64{Same: profileSame},
	}
	if deliverySame {
		out.DeliveryMode.Value = delivery
	}
	if monitoredSame {
		out.Monitored.Value = monitored
	}
	if rootSame {
		out.RootID.Value = rootID
	}
	if profileSame {
		out.QualityProfileID.Value = profileID
	}
	return out, nil
}

// CommonSeriesMetadata returns unanimous bulk-edit metadata fields for the given series ids.
// Missing ids are skipped; ErrNotFound if none resolve. Actor lists compare in order.
func (s *Store) CommonSeriesMetadata(ids []int64) (CommonSeriesMetadata, error) {
	ids = uniqPositive(ids)
	if len(ids) == 0 {
		return CommonSeriesMetadata{}, fmt.Errorf("%w: series_ids required", ErrInvalid)
	}
	var (
		studio, country, mpaa string
		genres, tags          []string
		actors                []SeriesActor
		studioSame            = true
		countrySame           = true
		mpaaSame              = true
		genresSame            = true
		tagsSame              = true
		actorsSame            = true
		n                     int
	)
	for _, id := range ids {
		ser, err := s.GetSeries(id, false)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return CommonSeriesMetadata{}, err
		}
		n++
		if n == 1 {
			studio = ser.Meta.Studio
			country = ser.Meta.Country
			mpaa = ser.Meta.MPAA
			genres = cloneStrings(ser.Meta.Genres)
			tags = cloneStrings(ser.Meta.Tags)
			actors = cloneActors(ser.Meta.Actors)
			continue
		}
		if studioSame && studio != ser.Meta.Studio {
			studioSame = false
		}
		if countrySame && country != ser.Meta.Country {
			countrySame = false
		}
		if mpaaSame && mpaa != ser.Meta.MPAA {
			mpaaSame = false
		}
		if genresSame && !stringSliceEqualOrdered(genres, ser.Meta.Genres) {
			genresSame = false
		}
		if tagsSame && !stringSliceEqualOrdered(tags, ser.Meta.Tags) {
			tagsSame = false
		}
		if actorsSame && !seriesActorsEqualOrdered(actors, ser.Meta.Actors) {
			actorsSame = false
		}
	}
	if n == 0 {
		return CommonSeriesMetadata{}, fmt.Errorf("%w: no series found", ErrNotFound)
	}
	out := CommonSeriesMetadata{
		Studio:  CommonMetaString{Same: studioSame},
		Country: CommonMetaString{Same: countrySame},
		MPAA:    CommonMetaString{Same: mpaaSame},
		Genres:  CommonMetaStrings{Same: genresSame},
		Tags:    CommonMetaStrings{Same: tagsSame},
		Actors:  CommonMetaActors{Same: actorsSame},
	}
	if studioSame {
		out.Studio.Value = studio
	}
	if countrySame {
		out.Country.Value = country
	}
	if mpaaSame {
		out.MPAA.Value = mpaa
	}
	if genresSame {
		out.Genres.Value = genres
		if out.Genres.Value == nil {
			out.Genres.Value = []string{}
		}
	}
	if tagsSame {
		out.Tags.Value = tags
		if out.Tags.Value == nil {
			out.Tags.Value = []string{}
		}
	}
	if actorsSame {
		out.Actors.Value = actors
		if out.Actors.Value == nil {
			out.Actors.Value = []SeriesActor{}
		}
	}
	return out, nil
}
