package domains

import (
	"errors"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// ErrFlareCookieExclusive is returned when Use FlareSolverr On would coexist with a
// non-empty host cookie jar. Flare merges anonymous browser cookies over the jar
// (same names win), which overwrites account/session cookies.
var ErrFlareCookieExclusive = errors.New(
	"cannot enable Use FlareSolverr with a non-empty cookie jar: FlareSolverr opens an anonymous browser and merges its cookies over the stored jar (same cookie names win), which overwrites account/session cookies so the pasted jar would not be what yt-dlp actually sends",
)

// RejectFlareWithCookies errors when flare is On and cookieContent is non-empty.
func RejectFlareWithCookies(flareOn bool, cookieContent string) error {
	if flareOn && strings.TrimSpace(cookieContent) != "" {
		return ErrFlareCookieExclusive
	}
	return nil
}

// HostFlareOn reports whether the host override has Use FlareSolverr effective On.
func HostFlareOn(database *db.DB, domain string) (bool, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == settings.DomainDefault {
		return false, nil
	}
	lim, err := settings.LimitsForDomain(database, domain)
	if err != nil {
		return false, err
	}
	return lim.UseFlareSolverr, nil
}

// RejectFlareOnWithStoredCookies errors when turning Flare On while a jar is stored.
func RejectFlareOnWithStoredCookies(database *db.DB, domain string) error {
	content, err := GetCookies(database, domain)
	if err != nil {
		return err
	}
	return RejectFlareWithCookies(true, content)
}

// RejectCookiesWhileFlareOn errors when saving a non-empty jar while Flare is On.
func RejectCookiesWhileFlareOn(database *db.DB, domain, content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	on, err := HostFlareOn(database, domain)
	if err != nil {
		return err
	}
	return RejectFlareWithCookies(on, content)
}
