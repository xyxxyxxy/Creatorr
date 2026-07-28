package library

import (
	"database/sql"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
)

// QualityProfile is a named yt-dlp format selector (passed to handlers as --format)
// plus optional maturity delays (0 = that pass off for all series using the profile)
// and optional SponsorBlock mark/remove/reencode/info-cards settings.
type QualityProfile struct {
	ID                      int64
	Name                    string
	FormatSelector          string
	MaturityRedownloadHours int // 0 = media maturity off
	MaturitySidecarHours    int // 0 = sidecar maturity off (UI shows days)
	SponsorBlockMark        []string
	SponsorBlockRemove      []string
	SponsorBlockReencodeCut bool
	SponsorBlockInfoCards   bool
	VerifyMedia             bool // post-pack null-decode verify (default off)
}

// MaturitySidecarDays returns floor(hours/24) for UI display.
func (p QualityProfile) MaturitySidecarDays() int {
	return MaturitySidecarHoursToDays(p.MaturitySidecarHours)
}

// MaturityMediaPreset returns the nearest media slider index.
func (p QualityProfile) MaturityMediaPreset() int {
	return MaturityMediaPresetIndex(p.MaturityRedownloadHours)
}

// MaturityMediaLabel returns the nearest media preset label for the table/UI.
func (p QualityProfile) MaturityMediaLabel() string {
	return MaturityMediaLabel(p.MaturityRedownloadHours)
}

// MaturitySidecarPreset returns the nearest sidecar slider index.
func (p QualityProfile) MaturitySidecarPreset() int {
	return MaturitySidecarPresetIndex(p.MaturitySidecarDays())
}

// MaturitySidecarLabel returns the nearest preset label for the table/UI.
func (p QualityProfile) MaturitySidecarLabel() string {
	return MaturitySidecarLabel(p.MaturitySidecarDays())
}

// SponsorBlockEnabled reports whether mark or remove is configured.
func (p QualityProfile) SponsorBlockEnabled() bool {
	return sponsorblock.SBEnabled(p.SponsorBlockMark, p.SponsorBlockRemove)
}

// SponsorBlockHasRemove reports whether remove/cut categories are configured.
func (p QualityProfile) SponsorBlockHasRemove() bool {
	return len(sponsorblock.NormalizeCategoryList(p.SponsorBlockRemove)) > 0
}

// SponsorBlockHasMark reports whether mark/chapter categories are configured.
func (p QualityProfile) SponsorBlockHasMark() bool {
	return len(sponsorblock.NormalizeCategoryList(p.SponsorBlockMark)) > 0
}

// SponsorBlockLucideIcon is the quality-profile table icon name:
// shield-minus (remove on), shield-half (mark only), shield-off (both empty).
func (p QualityProfile) SponsorBlockLucideIcon() string {
	if p.SponsorBlockHasRemove() {
		return "shield-minus"
	}
	if p.SponsorBlockHasMark() {
		return "shield-half"
	}
	return "shield-off"
}

// SponsorBlockIconTip is a short tooltip for the table icon.
func (p QualityProfile) SponsorBlockIconTip() string {
	switch {
	case p.SponsorBlockHasRemove() && p.SponsorBlockHasMark():
		return "SponsorBlock cut + chapters"
	case p.SponsorBlockHasRemove():
		return "SponsorBlock cut"
	case p.SponsorBlockHasMark():
		return "SponsorBlock chapters only"
	default:
		return "SponsorBlock off"
	}
}

// SponsorBlockConfig returns the SB config slice.
func (p QualityProfile) SponsorBlockConfig() sponsorblock.ProfileConfig {
	return sponsorblock.ProfileConfig{
		Mark:        p.SponsorBlockMark,
		Remove:      p.SponsorBlockRemove,
		ReencodeCut: p.SponsorBlockReencodeCut,
		InfoCards:   p.SponsorBlockInfoCards,
	}
}

func validateProfileSB(mark, remove []string, reencode, cards bool) error {
	if err := sponsorblock.ValidateMarkRemove(mark, remove); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if cards && !reencode {
		return fmt.Errorf("%w: sponsorblock_info_cards requires sponsorblock_reencode_cut", ErrInvalid)
	}
	return nil
}

func scanProfile(scanner interface {
	Scan(dest ...any) error
}) (*QualityProfile, error) {
	var p QualityProfile
	var markJSON, removeJSON string
	var reencode, cards, verify int
	if err := scanner.Scan(
		&p.ID, &p.Name, &p.FormatSelector,
		&p.MaturityRedownloadHours, &p.MaturitySidecarHours,
		&markJSON, &removeJSON, &reencode, &cards, &verify,
	); err != nil {
		return nil, err
	}
	mark, err := sponsorblock.ParseCategoryListJSON(markJSON)
	if err != nil {
		return nil, err
	}
	remove, err := sponsorblock.ParseCategoryListJSON(removeJSON)
	if err != nil {
		return nil, err
	}
	p.SponsorBlockMark = mark
	p.SponsorBlockRemove = remove
	p.SponsorBlockReencodeCut = reencode != 0
	p.SponsorBlockInfoCards = cards != 0
	p.VerifyMedia = verify != 0
	return &p, nil
}

const profileSelectCols = `id, name, format_selector, maturity_redownload_hours, maturity_sidecar_hours,
		sponsorblock_mark, sponsorblock_remove, sponsorblock_reencode_cut, sponsorblock_info_cards, verify_media`

func (s *Store) ListProfiles() ([]QualityProfile, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT ` + profileSelectCols + `
		FROM quality_profiles ORDER BY name COLLATE NOCASE, id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []QualityProfile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetProfile(id int64) (*QualityProfile, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT `+profileSelectCols+`
		FROM quality_profiles WHERE id = ?
	`, id)
	p, err := scanProfile(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// CreateProfile inserts a quality profile (maturity delays default 0 = off; SB empty).
func (s *Store) CreateProfile(name, formatSelector string) (*QualityProfile, error) {
	return s.CreateProfileFull(name, formatSelector, 0, 0, nil, nil, false, false, false)
}

// CreateProfileFull inserts a quality profile with maturity delays, SponsorBlock fields, and verify_media.
func (s *Store) CreateProfileFull(name, formatSelector string, redownloadHours, sidecarHours int, mark, remove []string, reencodeCut, infoCards, verifyMedia bool) (*QualityProfile, error) {
	if err := requireNonEmpty("name", name); err != nil {
		return nil, err
	}
	if err := requireNonEmpty("format_selector", formatSelector); err != nil {
		return nil, err
	}
	redownloadHours = ClampMaturityRedownloadHours(redownloadHours)
	sidecarHours = ClampMaturitySidecarHours(sidecarHours)
	mark = sponsorblock.NormalizeCategoryList(mark)
	remove = sponsorblock.NormalizeCategoryList(remove)
	if err := validateProfileSB(mark, remove, reencodeCut, infoCards); err != nil {
		return nil, err
	}
	reenc := 0
	if reencodeCut {
		reenc = 1
	}
	cards := 0
	if infoCards {
		cards = 1
	}
	verify := 0
	if verifyMedia {
		verify = 1
	}
	res, err := s.DB.SQL.Exec(`
		INSERT INTO quality_profiles (
			name, format_selector, maturity_redownload_hours, maturity_sidecar_hours,
			sponsorblock_mark, sponsorblock_remove, sponsorblock_reencode_cut, sponsorblock_info_cards,
			verify_media
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, name, formatSelector, redownloadHours, sidecarHours,
		sponsorblock.CategoryListJSON(mark), sponsorblock.CategoryListJSON(remove), reenc, cards, verify)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	id, _ := res.LastInsertId()
	return s.GetProfile(id)
}

// UpdateProfileParams patches profile fields; nil pointers mean unchanged.
type UpdateProfileParams struct {
	Name                    *string
	FormatSelector          *string
	MaturityRedownloadHours *int
	MaturitySidecarHours    *int
	SponsorBlockMark        *[]string
	SponsorBlockRemove      *[]string
	SponsorBlockReencodeCut *bool
	SponsorBlockInfoCards   *bool
	VerifyMedia             *bool
}

// UpdateProfile updates name and/or format selector.
func (s *Store) UpdateProfile(id int64, name, formatSelector *string) (*QualityProfile, error) {
	return s.UpdateProfileParams(id, UpdateProfileParams{Name: name, FormatSelector: formatSelector})
}

// UpdateProfileParams applies a partial profile update.
func (s *Store) UpdateProfileParams(id int64, p UpdateProfileParams) (*QualityProfile, error) {
	cur, err := s.GetProfile(id)
	if err != nil {
		return nil, err
	}
	n, fs := cur.Name, cur.FormatSelector
	redownload, sidecar := cur.MaturityRedownloadHours, cur.MaturitySidecarHours
	mark, remove := cur.SponsorBlockMark, cur.SponsorBlockRemove
	reencode, cards := cur.SponsorBlockReencodeCut, cur.SponsorBlockInfoCards
	verify := cur.VerifyMedia
	if p.Name != nil {
		if err := requireNonEmpty("name", *p.Name); err != nil {
			return nil, err
		}
		n = *p.Name
	}
	if p.FormatSelector != nil {
		if err := requireNonEmpty("format_selector", *p.FormatSelector); err != nil {
			return nil, err
		}
		fs = *p.FormatSelector
	}
	if p.MaturityRedownloadHours != nil {
		redownload = ClampMaturityRedownloadHours(*p.MaturityRedownloadHours)
	}
	if p.MaturitySidecarHours != nil {
		sidecar = ClampMaturitySidecarHours(*p.MaturitySidecarHours)
	}
	if p.SponsorBlockMark != nil {
		mark = sponsorblock.NormalizeCategoryList(*p.SponsorBlockMark)
	}
	if p.SponsorBlockRemove != nil {
		remove = sponsorblock.NormalizeCategoryList(*p.SponsorBlockRemove)
	}
	if p.SponsorBlockReencodeCut != nil {
		reencode = *p.SponsorBlockReencodeCut
	}
	if p.SponsorBlockInfoCards != nil {
		cards = *p.SponsorBlockInfoCards
	}
	if p.VerifyMedia != nil {
		verify = *p.VerifyMedia
	}
	if err := validateProfileSB(mark, remove, reencode, cards); err != nil {
		return nil, err
	}
	reencInt, cardInt, verifyInt := 0, 0, 0
	if reencode {
		reencInt = 1
	}
	if cards {
		cardInt = 1
	}
	if verify {
		verifyInt = 1
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE quality_profiles SET name = ?, format_selector = ?,
		  maturity_redownload_hours = ?, maturity_sidecar_hours = ?,
		  sponsorblock_mark = ?, sponsorblock_remove = ?,
		  sponsorblock_reencode_cut = ?, sponsorblock_info_cards = ?,
		  verify_media = ?
		WHERE id = ?
	`, n, fs, redownload, sidecar,
		sponsorblock.CategoryListJSON(mark), sponsorblock.CategoryListJSON(remove),
		reencInt, cardInt, verifyInt, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s.GetProfile(id)
}
