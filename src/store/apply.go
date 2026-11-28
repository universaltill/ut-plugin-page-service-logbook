package store

import (
	"regexp"
	"unicode/utf8"
)

// ApplyError is the plugin's answer to plugin.records.received.ask when a
// record is not applied: Retry=false is terminal (core marks it rejected).
type ApplyError struct {
	Retry  bool
	Reason string
}

func (e *ApplyError) Error() string { return e.Reason }

var (
	idRe   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func invalid() *ApplyError { return &ApplyError{false, "invalid"} }

func validWire(w Wire) bool {
	e := w.Entry
	if !idRe.MatchString(e.ID) || len(e.Currency) != 3 || !dateRe.MatchString(e.BusinessDate) ||
		(e.Payment != "cash" && e.Payment != "card") || e.TotalMinor < 0 || e.TipMinor < 0 || e.TotalMinor > MaxMinor || e.TipMinor > MaxMinor ||
		(e.Mode != "free" && e.Mode != "linked") || len(w.Lines) > maxLines ||
		(e.SupersedesID != "" && !idRe.MatchString(e.SupersedesID)) || e.CreatedAt == "" {
		return false
	}
	seen := map[int]bool{}
	for _, l := range w.Lines {
		if l.Qty < 1 || l.Qty > 999 || l.AmountMinor < 0 || l.AmountMinor > MaxMinor || l.LineNo < 1 || seen[l.LineNo] ||
			utf8.RuneCountInString(l.Name) > 120 || l.Name == "" || utf8.RuneCountInString(l.Description) > 200 {
			return false
		}
		seen[l.LineNo] = true
	}
	return true
}

// Apply stores a record forwarded by fromTill (core's from_till_id, never a
// payload field). Idempotent: a duplicate is success. Lines first, entry
// last, as on the origin till. Terminal refusals: a record for a member the
// main does not hold (unknown, or erased by a GB delete: never resurrected)
// and a new link of a sale line or tip a current entry already carries (the
// main's own link, or an earlier till's, wins; counted once).
func (s *Store) Apply(fromTill string, w Wire) *ApplyError {
	if fromTill == "" || fromTill == LocalTill || !validWire(w) {
		return invalid()
	}
	if _, err := s.GetEntry(w.Entry.ID); err == nil {
		return nil
	}
	e := w.Entry
	if ae := s.knownStaff(w); ae != nil {
		return ae
	}
	if ae := s.notLinkedYet(w); ae != nil {
		return ae
	}
	if e.SupersedesID != "" {
		if p, err := s.GetEntry(e.SupersedesID); err == nil && p.OriginTill != fromTill {
			return &ApplyError{false, "not_authorised"}
		}
	}
	db := s.h.DB()
	claim := "" // a new link claims its sale lines (claims.go), as notLinkedYet reads
	if e.SaleRef != "" && e.SupersedesID == "" && !e.Voided {
		claim = e.SaleRef
	}
	for _, l := range w.Lines {
		in := LineInput{ItemID: l.ItemID, Name: l.Name, Qty: l.Qty, AmountMinor: l.AmountMinor, Description: l.Description, StaffID: l.StaffID}
		if err := s.writeLine(sqlApplyLine, e.ID, l.LineNo, in, claim); err != nil {
			if err == ErrConflict { // linked meanwhile by another event
				_, _ = db.Exec(sqlDropUncommittedLines, e.ID)
				return &ApplyError{false, "already_linked"}
			}
			return &ApplyError{true, "db"}
		}
	}
	ie := Entry{SaleRef: e.SaleRef, TipMinor: e.TipMinor, SupersedesID: e.SupersedesID, Voided: e.Voided}
	q, args := entryInsert("INSERT OR IGNORE", insertCols, "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 'na', ?, ?", ie, []any{e.ID, fromTill,
		e.Author, nullable(e.StaffID), e.BusinessDate, e.Currency, e.Payment, e.TotalMinor, e.TipMinor, nullable(e.SaleRef),
		nullable(e.SupersedesID), b2i(e.Voided), e.Mode, e.CreatedAt})
	n, err := db.Exec(q, args...)
	if err != nil {
		return &ApplyError{true, "db"}
	}
	if n == 0 {
		// OR IGNORE skips a duplicate id (a redelivery: success) or, for a
		// new tip link, the tip guard (the sale's tip is already counted).
		// A failed read is no verdict: retry, never a terminal refusal.
		if _, gerr := s.GetEntry(e.ID); gerr == nil {
			return nil
		} else if gerr != ErrNotFound {
			return &ApplyError{true, "db"}
		}
		_, _ = db.Exec(sqlDropUncommittedLines, e.ID)
		return &ApplyError{false, "already_linked"}
	}
	if e.SupersedesID != "" {
		if err := s.resolveConflict(e.SupersedesID); err != nil {
			return &ApplyError{true, "db"}
		}
	}
	return nil
}

// resolveConflict: of several live children of one parent, the main-authored
// row wins, else the earlier (created_at, id); losers are shadowed (kept).
func (s *Store) resolveConflict(parent string) error {
	rows, err := s.h.DB().Query(`SELECT id FROM entries WHERE supersedes_id = ? AND shadowed = 0
		ORDER BY CASE WHEN origin_till_id = 'local' THEN 0 ELSE 1 END, created_at, id`, parent)
	if err != nil || len(rows) < 2 {
		return err
	}
	for _, r := range rows[1:] {
		if _, err := s.h.DB().Exec("UPDATE entries SET shadowed = 1 WHERE id = ?", str(r, "id")); err != nil {
			return err
		}
	}
	return nil
}

// knownStaff: the entry's member and every line's member must exist on the
// main (archived is fine: amounts of a former member are kept).
func (s *Store) knownStaff(w Wire) *ApplyError {
	ids := []string{w.Entry.StaffID}
	for _, l := range w.Lines {
		if l.StaffID != "" {
			ids = append(ids, l.StaffID)
		}
	}
	for _, id := range ids {
		if id == "" {
			return &ApplyError{false, "unknown_staff"}
		}
		if _, err := s.GetStaff(id); err != nil {
			if err == ErrNotFound {
				return &ApplyError{false, "unknown_staff"}
			}
			return &ApplyError{true, "db"}
		}
	}
	return nil
}

// notLinkedYet refuses a NEW linked record (not an edit or void of an
// existing one) whose sale line, or the sale's tip, is already carried by a
// current entry.
func (s *Store) notLinkedYet(w Wire) *ApplyError {
	e := w.Entry
	if e.SaleRef == "" || e.SupersedesID != "" || e.Voided {
		return nil
	}
	done, err := s.linkedLines(e.SaleRef)
	if err != nil {
		return &ApplyError{true, "db"}
	}
	for _, l := range w.Lines {
		if done[l.LineNo] {
			return &ApplyError{false, "already_linked"}
		}
	}
	if e.TipMinor > 0 {
		rows, err := s.h.DB().Query("SELECT 1 AS n FROM entries e WHERE e.sale_ref = ? AND e.tip_minor > 0 AND "+currentWhere, e.SaleRef)
		if err != nil {
			return &ApplyError{true, "db"}
		}
		if len(rows) > 0 {
			return &ApplyError{false, "already_linked"}
		}
	}
	return nil
}
