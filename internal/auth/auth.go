package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

const (
	CookieName     = "creatorr_session"
	SessionTTL     = 7 * 24 * time.Hour
	MinPasswordLen = 4
	MaxPasswordLen = 72 // bcrypt limit in bytes
	HeaderAPIKey   = "X-Api-Key"
)

// dummyPasswordHash is a real bcrypt hash used only so failed username checks
// still pay bcrypt cost (login timing).
var dummyPasswordHash = mustDummyHash()

func mustDummyHash() string {
	b, err := bcrypt.GenerateFromPassword([]byte("creatorr-timing-dummy"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// CheckLogin runs bcrypt against the real hash when the username matches, else
// against a dummy hash, so wrong-username attempts do not skip bcrypt work.
// Returns true only when both username and password match.
func CheckLogin(wantUser, gotUser, passwordHash, password string) bool {
	hash := passwordHash
	if gotUser != wantUser || strings.TrimSpace(passwordHash) == "" {
		hash = dummyPasswordHash
	}
	passOK := CheckPassword(hash, password)
	userOK := gotUser == wantUser && strings.TrimSpace(passwordHash) != ""
	return passOK && userOK
}

// trustForwardedProto is set at process start when CREATORR_TRUST_PROXY is enabled.
var trustForwardedProto bool

// SetTrustForwardedProto controls whether X-Forwarded-Proto is trusted for Secure cookies.
func SetTrustForwardedProto(trust bool) {
	trustForwardedProto = trust
}

// SafeNextPath returns next if it is a same-origin relative path, else "/".
// Rejects protocol-relative, backslash, and non-path characters.
func SafeNextPath(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || next == "/" {
		return "/"
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	if strings.ContainsAny(next, "\\@ \t\n\r") {
		return "/"
	}
	for _, r := range next {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '/' || r == '_' || r == '-' || r == '.' || r == '?' || r == '=' || r == '&':
		default:
			return "/"
		}
	}
	return next
}

// ValidatePassword checks length rules (UTF-8 rune count for min; byte length for bcrypt max).
func ValidatePassword(password, confirm string) error {
	if password == "" {
		return fmt.Errorf("password required")
	}
	if len([]rune(password)) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(password) > MaxPasswordLen {
		return fmt.Errorf("password must be at most %d bytes", MaxPasswordLen)
	}
	if password != confirm {
		return fmt.Errorf("passwords do not match")
	}
	return nil
}

// HashPassword returns a bcrypt hash of password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword compares plaintext to bcrypt hash.
func CheckPassword(hash, password string) bool {
	if hash == "" || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

type sessionPayload struct {
	U     string `json:"u"`
	Exp   int64  `json:"exp"`
	Epoch int64  `json:"epoch"`
}

// SignSession returns a signed cookie value for username until exp with epoch.
func SignSession(secret, username string, epoch int64, exp time.Time) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("empty cookie secret")
	}
	p := sessionPayload{U: username, Exp: exp.Unix(), Epoch: epoch}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifySession parses and validates a signed cookie. Returns username and remaining ok.
func VerifySession(secret, cookieVal, wantUser string, wantEpoch int64, now time.Time) (username string, ok bool) {
	parts := strings.Split(cookieVal, ".")
	if len(parts) != 2 {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	expected := mac.Sum(nil)
	if subtle.ConstantTimeCompare(expected, sig) != 1 {
		return "", false
	}
	var p sessionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", false
	}
	if p.Exp < now.Unix() {
		return "", false
	}
	if p.Epoch != wantEpoch {
		return "", false
	}
	if wantUser != "" && p.U != wantUser {
		return "", false
	}
	return p.U, true
}

// APIKeyMatches compares request key to stored key in constant time.
func APIKeyMatches(stored, provided string) bool {
	if stored == "" || provided == "" {
		return false
	}
	a := []byte(stored)
	b := []byte(provided)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// RequestIsHTTPS reports TLS, or X-Forwarded-Proto=https only when TrustForwardedProto is on.
func RequestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !trustForwardedProto {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// SetSessionCookie writes the auth cookie.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, value string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   RequestIsHTTPS(r),
		Expires:  exp,
		MaxAge:   int(time.Until(exp).Seconds()),
	})
}

// ClearSessionCookie expires the auth cookie.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   RequestIsHTTPS(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// IssueSession signs and sets a fresh sliding session cookie.
func IssueSession(w http.ResponseWriter, r *http.Request, database *db.DB, username string) error {
	secret, err := settings.AuthCookieSecret(database)
	if err != nil {
		return err
	}
	epoch, err := settings.AuthSessionEpoch(database)
	if err != nil {
		return err
	}
	exp := time.Now().UTC().Add(SessionTTL)
	val, err := SignSession(secret, username, epoch, exp)
	if err != nil {
		return err
	}
	SetSessionCookie(w, r, val, exp)
	return nil
}

func isPublicPath(path string) bool {
	if path == "/api/health" {
		return true
	}
	if strings.HasPrefix(path, "/static/") {
		return true
	}
	if path == "/setup" || path == "/login" || path == "/logout" {
		return true
	}
	return false
}

func wantsJSON(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func writeJSONError(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"code":%q,"message":%q}`, code, message)
}

func redirectAuth(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// Middleware enforces setup gate and Forms/API-key auth.
func Middleware(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if isPublicPath(path) {
				next.ServeHTTP(w, r)
				return
			}
			needs, err := settings.NeedsSetup(database)
			if err != nil {
				writeJSONError(w, apperrors.CodeInternal, "auth check failed", http.StatusInternalServerError)
				return
			}
			if needs {
				if wantsJSON(r) {
					writeJSONError(w, apperrors.CodeSetupRequired, "complete setup first", http.StatusUnauthorized)
					return
				}
				redirectAuth(w, r, "/setup")
				return
			}
			if authed, err := authenticate(r, database, w); err != nil {
				writeJSONError(w, apperrors.CodeInternal, "auth check failed", http.StatusInternalServerError)
				return
			} else if authed {
				next.ServeHTTP(w, r)
				return
			}
			if wantsJSON(r) {
				writeJSONError(w, apperrors.CodeUnauthorized, "authentication required", http.StatusUnauthorized)
				return
			}
			redirectAuth(w, r, "/login")
		})
	}
}

// authenticate returns true if cookie or API key is valid. May slide the session cookie.
func authenticate(r *http.Request, database *db.DB, w http.ResponseWriter) (bool, error) {
	if APIKeyMatches(mustAPIKey(database), r.Header.Get(HeaderAPIKey)) {
		return true, nil
	}
	c, err := r.Cookie(CookieName)
	if err != nil || c == nil || c.Value == "" {
		return false, nil
	}
	secret, err := settings.AuthCookieSecret(database)
	if err != nil {
		return false, err
	}
	user, err := settings.AuthUsername(database)
	if err != nil {
		return false, err
	}
	epoch, err := settings.AuthSessionEpoch(database)
	if err != nil {
		return false, err
	}
	u, ok := VerifySession(secret, c.Value, user, epoch, time.Now().UTC())
	if !ok {
		return false, nil
	}
	// Sliding renewal.
	_ = IssueSession(w, r, database, u)
	return true, nil
}

func mustAPIKey(database *db.DB) string {
	k, _ := settings.APIKey(database)
	return k
}
