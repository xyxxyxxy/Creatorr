package library

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// Store is library CRUD over SQLite.
type Store struct {
	DB         *db.DB
	Queue      *queue.Store
	ImportRoot string // import inbox path (/media/import or var/media/import)
	CacheDir   string // /cache or var/cache
}

func NewStore(database *db.DB, q *queue.Store) *Store {
	s := &Store{DB: database, Queue: q}
	if q != nil {
		q.OnCancelled = func(t queue.Task) {
			_ = s.RecordTaskCancelled(&t)
		}
	}
	return s
}

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func requireNonEmpty(name, v string) error {
	if v == "" {
		return fmt.Errorf("%w: %s required", ErrInvalid, name)
	}
	return nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	// SQLite: "UNIQUE constraint failed: …" - do not treat NOT NULL / CHECK as unique.
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

// normalizeSourceURL trims space, lowercases the host, and strips a leading www.
func normalizeSourceURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host := settings.NormalizeDomain(u.Hostname())
	if host == "" {
		return raw
	}
	if p := u.Port(); p != "" {
		u.Host = host + ":" + p
	} else {
		u.Host = host
	}
	return u.String()
}

// ValidateSourceURL requires a non-empty http(s) URL with a host.
func ValidateSourceURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%w: url required", ErrInvalid)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("%w: url must be http(s) with a host", ErrInvalid)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: url must be http or https", ErrInvalid)
	}
	return nil
}
