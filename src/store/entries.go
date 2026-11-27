package store

import (
	"strings"
	"unicode/utf8"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/host"
)

// Actor is who acts. UserID comes only from the operator core stamps on the
// ask, never from the form. NoSignIn = the till has sign-in off (no
// operator): documented core rule, no role limits.
type Actor struct {
	UserID   string
	Manager  bool // operator holds `reports`
	NoSignIn bool
}

// LineInput is one service line; AmountMinor is the line total.
type LineInput struct {
	ItemID      string
	Name        string
	Qty         int
	AmountMinor int64
	Description string
	StaffID     string
}

// FreeInput is a free-mode entry (also the edit form).
type FreeInput struct {
	StaffID  string
	Payment  string
	TipMinor int64
	Lines    []LineInput
}

// Entry is one row of entries.
type Entry struct {
	ID, OriginTill, AuthorUserID, StaffID, BusinessDate, Currency, Payment string
	TotalMinor, TipMinor                                                   int64
	SaleRef, SupersedesID, FwdState, Mode, CreatedAt                       string
	Voided, Shadowed                                                       bool
}

// Line is one row of entry_lines.
type Line struct {
	LineNo      int
	ItemID      string
	Name        string
	Qty         int
	AmountMinor int64
	Description string
	StaffID     string
}

const maxLines = 50

// currentWhere selects the CURRENT entries (alias e): not voided, not
// shadowed, no live child (ADR-0149, architect §5.6).
const currentWhere = `e.voided = 0 AND e.shadowed = 0 AND NOT EXISTS
 (SELECT 1 FROM entries c WHERE c.supersedes_id = e.id AND c.shadowed = 0)`

func rowEntry(r host.Row) Entry {
	return Entry{ID: str(r, "id"), OriginTill: str(r, "origin_till_id"), AuthorUserID: str(r, "author_user_id"),
		StaffID: str(r, "staff_id"), BusinessDate: str(r, "business_date"), Currency: str(r, "currency"),
		Payment: str(r, "payment_type"), TotalMinor: num(r, "total_minor"), TipMinor: num(r, "tip_minor"),
		SaleRef: str(r, "sale_ref"), SupersedesID: str(r, "supersedes_id"), FwdState: str(r, "fwd_state"),
		Mode: str(r, "mode"), CreatedAt: str(r, "created_at"), Voided: num(r, "voided") == 1, Shadowed: num(r, "shadowed") == 1}
}

func rowLine(r host.Row) Line {
	return Line{LineNo: int(num(r, "line_no")), ItemID: str(r, "item_id"), Name: str(r, "name"), Qty: int(num(r, "qty")),
		AmountMinor: num(r, "amount_minor"), Description: str(r, "description"), StaffID: str(r, "staff_id")}
}

// GetEntry returns ErrNotFound for an unknown id.
func (s *Store) GetEntry(id string) (Entry, error) {
	rows, err := s.h.DB().Query("SELECT * FROM entries WHERE id = ?", id)
	if err != nil {
		return Entry{}, err
	}
	if len(rows) == 0 {
		return Entry{}, ErrNotFound
	}
	return rowEntry(rows[0]), nil
}

// Lines returns an entry's lines in order.
func (s *Store) Lines(entryID string) ([]Line, error) {
	rows, err := s.h.DB().Query("SELECT * FROM entry_lines WHERE entry_id = ? ORDER BY line_no", entryID)
	if err != nil {
		return nil, err
	}
	out := make([]Line, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowLine(r))
	}
	return out, nil
}

func validLines(in []LineInput) error {
	if len(in) > maxLines {
		return ErrInvalid
	}
	for _, l := range in {
		if l.Qty < 1 || l.Qty > 999 || l.AmountMinor < 0 || strings.TrimSpace(l.Name) == "" ||
			utf8.RuneCountInString(l.Name) > 120 || utf8.RuneCountInString(l.Description) > 200 {
			return ErrInvalid
		}
	}
	return nil
}

func (s *Store) validateFree(in FreeInput) error {
	if in.Payment != "cash" && in.Payment != "card" {
		return ErrInvalid
	}
	if in.TipMinor < 0 || (len(in.Lines) == 0 && in.TipMinor == 0) {
		return ErrInvalid
	}
	if err := validLines(in.Lines); err != nil {
		return err
	}
	st, err := s.GetStaff(in.StaffID)
	if err != nil || st.Archived() {
		return ErrInvalid
	}
	return nil
}

// CreateFree writes a new free-mode entry. Free entries are refused where
// the country forces linked money entry (server-side, whatever the form).
func (s *Store) CreateFree(a Actor, in FreeInput) (Entry, error) {
	if s.MoneyEntry() != "free" {
		return Entry{}, ErrForbidden
	}
	return s.createFree(a, in, "")
}

func (s *Store) createFree(a Actor, in FreeInput, supersedes string) (Entry, error) {
	if err := s.validateFree(in); err != nil {
		return Entry{}, err
	}
	shop, err := s.Shop()
	if err != nil || len(shop.Currency) != 3 || shop.BusinessDate == "" {
		return Entry{}, host.ErrUnavailable
	}
	var total int64
	for _, l := range in.Lines {
		total += l.AmountMinor
	}
	e := Entry{ID: s.h.IDs().NewID(), OriginTill: LocalTill, AuthorUserID: a.UserID, StaffID: in.StaffID,
		BusinessDate: shop.BusinessDate, Currency: shop.Currency, Payment: in.Payment, TotalMinor: total,
		TipMinor: in.TipMinor, SupersedesID: supersedes, Mode: "free", CreatedAt: s.now()}
	if err := s.write(e, in.Lines); err != nil {
		return Entry{}, err
	}
	return s.GetEntry(e.ID)
}

// write stores lines first and the entry row last (the commit point: reports
// join from entries, so lines of a half-written entry are invisible), then
// forwards. #3157 slice B may add a transactional batch; until then this
// ordering is the contract.
func (s *Store) write(e Entry, lines []LineInput) error { return s.writeNumbered(e, lines, nil) }

// writeNumbered is write with explicit line numbers (linked entries keep the
// receipt's line_no so a sale line is linked at most once).
func (s *Store) writeNumbered(e Entry, lines []LineInput, nos []int) error {
	db := s.h.DB()
	role, _ := s.h.Records().Role()
	e.FwdState = "na"
	if role == "replica" {
		e.FwdState = "pending"
	}
	for i, l := range lines {
		no := i + 1
		if nos != nil {
			no = nos[i]
		}
		if _, err := db.Exec(`INSERT INTO entry_lines (entry_id, line_no, item_id, name, qty, amount_minor, description, staff_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, e.ID, no, nullable(l.ItemID), l.Name, l.Qty, l.AmountMinor,
			nullable(l.Description), nullable(l.StaffID)); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`INSERT INTO entries (id, origin_till_id, author_user_id, staff_id, business_date, currency, payment_type,
		total_minor, tip_minor, sale_ref, supersedes_id, voided, shadowed, fwd_state, mode, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`, e.ID, e.OriginTill, e.AuthorUserID, nullable(e.StaffID),
		e.BusinessDate, e.Currency, e.Payment, e.TotalMinor, e.TipMinor, nullable(e.SaleRef), nullable(e.SupersedesID),
		b2i(e.Voided), e.FwdState, e.Mode, e.CreatedAt); err != nil {
		return err
	}
	if role == "replica" {
		s.ForwardEntry(e.ID)
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// mayChange is the edit/void rule: manager, author, or no sign-in; AND the
// row is local to this till, or this till is main.
func (s *Store) mayChange(a Actor, e Entry) error {
	if !(a.Manager || a.NoSignIn || (a.UserID != "" && a.UserID == e.AuthorUserID)) {
		return ErrForbidden
	}
	role, _ := s.h.Records().Role()
	if e.OriginTill != LocalTill && role != "main" {
		return ErrForbidden
	}
	return nil
}

func (s *Store) changeable(a Actor, id string) (Entry, error) {
	e, err := s.GetEntry(id)
	if err != nil {
		return e, err
	}
	if err := s.mayChange(a, e); err != nil {
		return e, err
	}
	rows, err := s.h.DB().Query("SELECT 1 AS n FROM entries e WHERE e.id = ? AND "+currentWhere, id)
	if err != nil {
		return e, err
	}
	if len(rows) == 0 { // voided, shadowed or already superseded
		return e, ErrConflict
	}
	return e, nil
}

// Edit writes a superseding row; the old row stays (append-only log).
func (s *Store) Edit(a Actor, id string, in FreeInput) (Entry, error) {
	old, err := s.changeable(a, id)
	if err != nil {
		return Entry{}, err
	}
	if old.Mode != "free" || old.SaleRef != "" || s.MoneyEntry() != "free" {
		return Entry{}, ErrForbidden // linked amounts come from the sale
	}
	return s.createFree(a, in, id)
}

// Void writes a void row (totals 0, voided=1) superseding the entry.
func (s *Store) Void(a Actor, id string) (Entry, error) {
	old, err := s.changeable(a, id)
	if err != nil {
		return Entry{}, err
	}
	v := Entry{ID: s.h.IDs().NewID(), OriginTill: LocalTill, AuthorUserID: a.UserID, StaffID: old.StaffID,
		BusinessDate: old.BusinessDate, Currency: old.Currency, Payment: old.Payment, SaleRef: old.SaleRef,
		SupersedesID: id, Voided: true, Mode: old.Mode, CreatedAt: s.now()}
	if err := s.write(v, nil); err != nil {
		return Entry{}, err
	}
	return s.GetEntry(v.ID)
}

// DeleteZero hard-deletes a zero-money, unlinked entry and its chain, in any
// country (ADR-0149 §4). Refused when any row of the chain carries money.
func (s *Store) DeleteZero(a Actor, id string) error {
	e, err := s.changeable(a, id)
	if err != nil {
		return err
	}
	chain := []Entry{e}
	for cur := e; cur.SupersedesID != ""; {
		p, err := s.GetEntry(cur.SupersedesID)
		if err != nil {
			break
		}
		chain = append(chain, p)
		cur = p
	}
	for _, c := range chain {
		if c.TotalMinor != 0 || c.TipMinor != 0 || c.SaleRef != "" {
			return ErrForbidden
		}
	}
	for _, c := range chain {
		if _, err := s.h.DB().Exec("DELETE FROM entry_lines WHERE entry_id = ?", c.ID); err != nil {
			return err
		}
		if _, err := s.h.DB().Exec("DELETE FROM entries WHERE id = ?", c.ID); err != nil {
			return err
		}
	}
	return nil
}

// DayList is the current entries of a business date, newest first.
func (s *Store) DayList(date string) ([]Entry, error) {
	rows, err := s.h.DB().Query("SELECT e.* FROM entries e WHERE e.business_date = ? AND "+currentWhere+" ORDER BY e.created_at DESC, e.id DESC", date)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowEntry(r))
	}
	return out, nil
}
