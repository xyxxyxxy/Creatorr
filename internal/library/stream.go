package library

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const keyStreamURLToken = "stream_url_token" // settings row; shown in Streaming UI (not SeedDefaults form key)

// StreamURL builds the client-facing proxy URL for a video (.strm contents).
// publicBase must be scheme+host+port with no trailing slash (Settings external_base_url).
func StreamURL(publicBase string, videoID int64, token string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if base == "" {
		return "", fmt.Errorf("%w: external Creatorr URL required for stream", ErrInvalid)
	}
	if token == "" {
		return "", fmt.Errorf("%w: stream token missing", ErrInvalid)
	}
	u, err := url.Parse(base + "/stream/videos/" + fmt.Sprintf("%d", videoID) + "/master.m3u8")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func mintStreamToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func storeStreamToken(database *db.DB, tok string) error {
	_, err := database.SQL.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, keyStreamURLToken, tok)
	return err
}

// GetStreamToken returns the stored stream URL token, or "" if missing.
func GetStreamToken(database *db.DB) (string, error) {
	if database == nil {
		return "", fmt.Errorf("%w: database", ErrInvalid)
	}
	var tok string
	err := database.SQL.QueryRow(`SELECT value FROM settings WHERE key = ?`, keyStreamURLToken).Scan(&tok)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(tok), nil
}

// EnsureStreamToken returns a stable random token stored in settings (auto-created).
func EnsureStreamToken(database *db.DB) (string, error) {
	if database == nil {
		return "", fmt.Errorf("%w: database", ErrInvalid)
	}
	tok, err := GetStreamToken(database)
	if err != nil {
		return "", err
	}
	if tok != "" {
		return tok, nil
	}
	tok, err = mintStreamToken()
	if err != nil {
		return "", err
	}
	if err := storeStreamToken(database, tok); err != nil {
		return "", err
	}
	return tok, nil
}

// RegenerateStreamToken mints a new token (invalidates existing .strm proxy URLs).
func RegenerateStreamToken(database *db.DB) (string, error) {
	if database == nil {
		return "", fmt.Errorf("%w: database", ErrInvalid)
	}
	tok, err := mintStreamToken()
	if err != nil {
		return "", err
	}
	if err := storeStreamToken(database, tok); err != nil {
		return "", err
	}
	return tok, nil
}

// ValidStreamToken reports whether tok matches the stored stream URL token.
func ValidStreamToken(database *db.DB, tok string) bool {
	if database == nil || strings.TrimSpace(tok) == "" {
		return false
	}
	want, err := GetStreamToken(database)
	if err != nil || want == "" {
		return false
	}
	return want == strings.TrimSpace(tok)
}
