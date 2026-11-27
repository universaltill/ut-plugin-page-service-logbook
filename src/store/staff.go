package store

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

var colourRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// Staff is one logbook staff member (not necessarily a till user).
type Staff struct {
	ID, Name, Colour, LinkedUserID, Status string
	PseudoNo                               int
}

// Archived reports an archived (pseudonymised) member.
func (s Staff) Archived() bool { return s.Status == "archived" }

// StaffInput is the add/edit form.
type StaffInput struct{ Name, Colour, LinkedUserID string }

// User is a users.list.v1 row.
type User struct {
	ID     string `json:"id"`
	Name   string `json:"display_name"`
	Active bool   `json:"active"`
}

// Users lists till users (view:users).
func (s *Store) Users() ([]User, error) {
	raw, err := s.h.Views().Query("users.list.v1", nil)
	if err != nil {
		return nil, err
	}
	var u []User
	return u, json.Unmarshal(raw, &u)
}

func (s *Store) validate(in StaffInput) (StaffInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Colour = strings.ToLower(strings.TrimSpace(in.Colour))
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 {
		return in, ErrInvalid
	}
	if in.Colour != "" && !colourRe.MatchString(in.Colour) {
		return in, ErrInvalid
	}
	if in.LinkedUserID != "" {
		users, err := s.Users()
		if err != nil {
			return in, ErrInvalid
		}
		ok := false
		for _, u := range users {
			if u.ID == in.LinkedUserID && u.Active {
				ok = true
			}
		}
		if !ok {
			return in, ErrInvalid
		}
	}
	return in, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) now() string { return s.h.Clock().Now().UTC().Format("2006-01-02T15:04:05Z") }

// AddStaff creates an active member and republishes the snapshot.
func (s *Store) AddStaff(in StaffInput) (Staff, error) {
	in, err := s.validate(in)
	if err != nil {
		return Staff{}, err
	}
	id := s.h.IDs().NewID()
	if _, err := s.h.DB().Exec(`INSERT INTO staff (id, display_name, colour, linked_user_id, status, created_at)
		VALUES (?, ?, ?, ?, 'active', ?)`, id, in.Name, in.Colour, nullable(in.LinkedUserID), s.now()); err != nil {
		return Staff{}, err
	}
	s.PublishSnapshots()
	return s.GetStaff(id)
}

// EditStaff changes an active member.
func (s *Store) EditStaff(id string, in StaffInput) (Staff, error) {
	cur, err := s.GetStaff(id)
	if err != nil {
		return Staff{}, err
	}
	if cur.Archived() {
		return Staff{}, ErrForbidden
	}
	in, err = s.validate(in)
	if err != nil {
		return Staff{}, err
	}
	if _, err := s.h.DB().Exec(`UPDATE staff SET display_name = ?, colour = ?, linked_user_id = ? WHERE id = ? AND status = 'active'`,
		in.Name, in.Colour, nullable(in.LinkedUserID), id); err != nil {
		return Staff{}, err
	}
	s.PublishSnapshots()
	return s.GetStaff(id)
}

func rowStaff(r map[string]any) Staff {
	return Staff{ID: str(r, "id"), Name: str(r, "display_name"), Colour: str(r, "colour"),
		LinkedUserID: str(r, "linked_user_id"), Status: str(r, "status"), PseudoNo: int(num(r, "pseudo_no"))}
}

// GetStaff returns ErrNotFound for an unknown id.
func (s *Store) GetStaff(id string) (Staff, error) {
	rows, err := s.h.DB().Query("SELECT * FROM staff WHERE id = ?", id)
	if err != nil {
		return Staff{}, err
	}
	if len(rows) == 0 {
		return Staff{}, ErrNotFound
	}
	return rowStaff(rows[0]), nil
}

// ListStaff lists active members (the picker) or all of them.
func (s *Store) ListStaff(includeArchived bool) ([]Staff, error) {
	q := "SELECT * FROM staff WHERE status = 'active' ORDER BY display_name COLLATE NOCASE, id"
	if includeArchived {
		q = "SELECT * FROM staff ORDER BY status, display_name COLLATE NOCASE, pseudo_no, id"
	}
	rows, err := s.h.DB().Query(q)
	if err != nil {
		return nil, err
	}
	out := make([]Staff, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowStaff(r))
	}
	return out, nil
}

// RemoveResult says which branch ran.
type RemoveResult string

const (
	Archived RemoveResult = "archived"
	Deleted  RemoveResult = "deleted"
)

// RemoveInput carries the GB confirmation: the typed name and the "I have
// exported" toggle. Ignored on the archive branch.
type RemoveInput struct {
	TypedName string
	Exported  bool
}

// RemoveStaff is ADR-0149 §4. GB: hard delete of the member and all their
// entries and lines, only after the typed-name and exported confirmations.
// Any other or unknown country: archive and pseudonymise, amounts kept.
func (s *Store) RemoveStaff(id string, in RemoveInput) (RemoveResult, error) {
	st, err := s.GetStaff(id)
	if err != nil {
		return "", err
	}
	if st.Archived() {
		return "", ErrForbidden
	}
	if s.Country() != "GB" {
		if _, err := s.h.DB().Exec(`UPDATE staff SET display_name = '', colour = '', linked_user_id = NULL, status = 'archived',
			pseudo_no = (SELECT COALESCE(MAX(pseudo_no), 0) + 1 FROM staff), archived_at = ? WHERE id = ? AND status = 'active'`,
			s.now(), id); err != nil {
			return "", err
		}
		s.PublishSnapshots()
		return Archived, nil
	}
	if !in.Exported || strings.TrimSpace(in.TypedName) != st.Name {
		return "", ErrNotConfirmed
	}
	// Order: lines, entries, then the staff row last; a crash midway leaves a
	// staff row whose delete can simply be repeated.
	db := s.h.DB()
	if _, err := db.Exec(`DELETE FROM entry_lines WHERE staff_id = ? OR entry_id IN (SELECT id FROM entries WHERE staff_id = ?)`, id, id); err != nil {
		return "", err
	}
	if _, err := db.Exec(`DELETE FROM entries WHERE staff_id = ?`, id); err != nil {
		return "", err
	}
	if _, err := db.Exec(`DELETE FROM staff WHERE id = ?`, id); err != nil {
		return "", err
	}
	s.PublishSnapshots()
	return Deleted, nil
}

// Label is the display name, or the pseudonym key and number for archived
// staff; the caller renders "Former staff #N" through its i18n key.
func (st Staff) Label(former func(n int) string) string {
	if st.Archived() {
		return former(st.PseudoNo)
	}
	return st.Name
}
