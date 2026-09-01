package settings

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const (
	KeyAuthUsername     = "auth_username"
	KeyAuthPasswordHash = "auth_password_hash"
	KeyAPIKey           = "api_key"
	KeyAuthCookieSecret = "auth_cookie_secret"
	KeyAuthSessionEpoch = "auth_session_epoch"
)

// AuthKeys are seeded but not listed on Settings → General (special UI / internal).
var AuthKeys = []string{
	KeyAuthUsername,
	KeyAuthPasswordHash,
	KeyAPIKey,
	KeyAuthCookieSecret,
	KeyAuthSessionEpoch,
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SeedAuthSecrets inserts missing auth keys. API key and cookie secret get random values once.
func SeedAuthSecrets(database *db.DB) error {
	defaults := map[string]string{
		KeyAuthUsername:     "admin",
		KeyAuthPasswordHash: "",
		KeyAuthSessionEpoch: "0",
	}
	for key, val := range defaults {
		if _, err := database.SQL.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO NOTHING
		`, key, val); err != nil {
			return fmt.Errorf("seed setting %s: %w", key, err)
		}
	}
	for _, key := range []string{KeyAPIKey, KeyAuthCookieSecret} {
		var n int
		if err := database.SQL.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, key).Scan(&n); err != nil {
			return fmt.Errorf("seed check %s: %w", key, err)
		}
		if n > 0 {
			continue
		}
		nBytes := 32
		if key == KeyAPIKey {
			nBytes = 16 // 32 hex chars
		}
		secret, err := randomHex(nBytes)
		if err != nil {
			return fmt.Errorf("seed %s: %w", key, err)
		}
		if _, err := database.SQL.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, key, secret); err != nil {
			return fmt.Errorf("seed setting %s: %w", key, err)
		}
	}
	return nil
}

// NeedsSetup is true when no password hash is stored (first-boot / after recover).
func NeedsSetup(database *db.DB) (bool, error) {
	hash, err := Get(database, KeyAuthPasswordHash)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(hash) == "", nil
}

// AuthUsername returns the configured username (default admin if missing).
func AuthUsername(database *db.DB) (string, error) {
	u, err := Get(database, KeyAuthUsername)
	if err != nil {
		return "", err
	}
	u = strings.TrimSpace(u)
	if u == "" {
		return "admin", nil
	}
	return u, nil
}

// AuthPasswordHash returns the bcrypt hash (empty = setup mode).
func AuthPasswordHash(database *db.DB) (string, error) {
	return Get(database, KeyAuthPasswordHash)
}

// APIKey returns the single API key.
func APIKey(database *db.DB) (string, error) {
	return Get(database, KeyAPIKey)
}

// AuthCookieSecret returns the HMAC signing secret.
func AuthCookieSecret(database *db.DB) (string, error) {
	return Get(database, KeyAuthCookieSecret)
}

// AuthSessionEpoch returns the session epoch counter.
func AuthSessionEpoch(database *db.DB) (int64, error) {
	raw, err := Get(database, KeyAuthSessionEpoch)
	if err != nil {
		return 0, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
}

// setRaw upserts any known key without the public Set allowlist (auth helpers only).
func setRaw(database *db.DB, key, value string) error {
	if _, ok := Help[key]; !ok {
		return fmt.Errorf("unknown setting key %q", key)
	}
	_, err := database.SQL.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// RegenerateAPIKey replaces the API key and returns the new value (32 hex chars).
func RegenerateAPIKey(database *db.DB) (string, error) {
	key, err := randomHex(16)
	if err != nil {
		return "", err
	}
	if err := setRaw(database, KeyAPIKey, key); err != nil {
		return "", err
	}
	return key, nil
}

// CompleteSetupResult is returned when setup wins or loses the race.
type CompleteSetupResult struct {
	AlreadySetup bool
}

// CompleteSetup atomically sets username + password hash if still unset.
// Returns AlreadySetup=true if another writer won (caller should send user to login).
func CompleteSetup(database *db.DB, username, passwordHash string) (CompleteSetupResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return CompleteSetupResult{}, fmt.Errorf("username required")
	}
	if strings.TrimSpace(passwordHash) == "" {
		return CompleteSetupResult{}, fmt.Errorf("password hash required")
	}
	tx, err := database.SQL.Begin()
	if err != nil {
		return CompleteSetupResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var hash string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, KeyAuthPasswordHash).Scan(&hash)
	if err != nil && err != sql.ErrNoRows {
		return CompleteSetupResult{}, err
	}
	if strings.TrimSpace(hash) != "" {
		return CompleteSetupResult{AlreadySetup: true}, nil
	}
	res, err := tx.Exec(`
		UPDATE settings SET value = ? WHERE key = ? AND value = ''
	`, passwordHash, KeyAuthPasswordHash)
	if err != nil {
		return CompleteSetupResult{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Row missing: insert; or race lost.
		res2, err := tx.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
			WHERE settings.value = ''
		`, KeyAuthPasswordHash, passwordHash)
		if err != nil {
			return CompleteSetupResult{}, err
		}
		n2, _ := res2.RowsAffected()
		if n2 == 0 {
			return CompleteSetupResult{AlreadySetup: true}, nil
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, KeyAuthUsername, username); err != nil {
		return CompleteSetupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CompleteSetupResult{}, err
	}
	return CompleteSetupResult{}, nil
}

// UpdateAuthCredentials updates username and/or password hash and bumps session epoch when either changes.
// passwordHash empty means leave hash unchanged. Returns new epoch after update.
func UpdateAuthCredentials(database *db.DB, username string, passwordHash string, bumpEpoch bool) (newEpoch int64, err error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, fmt.Errorf("username required")
	}
	tx, err := database.SQL.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	curUser, err := getTx(tx, KeyAuthUsername)
	if err != nil {
		return 0, err
	}
	curHash, err := getTx(tx, KeyAuthPasswordHash)
	if err != nil {
		return 0, err
	}
	changed := strings.TrimSpace(curUser) != username
	if passwordHash != "" && passwordHash != curHash {
		changed = true
	}
	if _, err := tx.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, KeyAuthUsername, username); err != nil {
		return 0, err
	}
	if passwordHash != "" {
		if _, err := tx.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, KeyAuthPasswordHash, passwordHash); err != nil {
			return 0, err
		}
	}
	epoch, err := getTx(tx, KeyAuthSessionEpoch)
	if err != nil {
		return 0, err
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(epoch), 10, 64)
	if bumpEpoch && changed {
		n++
		if _, err := tx.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, KeyAuthSessionEpoch, strconv.FormatInt(n, 10)); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func getTx(tx *sql.Tx, key string) (string, error) {
	var v string
	err := tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}
