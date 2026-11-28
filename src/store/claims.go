package store

import "strings"

// Sale-line claims (migration 0002, fix-plug finding 6). A linked entry's
// lines carry the sale id in entry_lines.sale_ref, and a unique index on
// (sale_ref, line_no) admits one claim per sale line. The "not linked yet"
// read before a write is only a fast, friendly refusal: the host runs events
// concurrently and has no transaction, so the index is what keeps a line
// from being counted twice. A void does not touch the claim; the next link
// of that line releases a claim whose entry is no longer current (voided,
// shadowed) and retries once. A claim whose entry row does not exist (a write
// in flight, or a crash before the commit point) is never released here; the
// maintenance sweep removes such lines once they are old (SweepOrphans).

const (
	lineCols = `entry_id, line_no, item_id, name, qty, amount_minor, description, staff_id, sale_ref, created_at`
	// sqlInsertLine is the origin till's line write (a new entry id).
	sqlInsertLine = `INSERT INTO entry_lines (` + lineCols + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	// sqlApplyLine is the main's write of a forwarded line: a redelivery of
	// the same (entry_id, line_no) is a no-op, a second claim of a sale line
	// still fails (the conflict target names only the primary key).
	sqlApplyLine = `INSERT INTO entry_lines (` + lineCols + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (entry_id, line_no) DO NOTHING`
	sqlReleaseStaleClaim = `UPDATE entry_lines SET sale_ref = NULL WHERE sale_ref = ? AND line_no = ?
		AND entry_id IN (SELECT e.id FROM entries e WHERE NOT (` + currentWhere + `))`
	sqlDropUncommittedLines = `DELETE FROM entry_lines WHERE entry_id = ? AND entry_id NOT IN (SELECT id FROM entries)`
)

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}

// writeLine writes one line; saleRef != "" claims (saleRef, no). A claim held
// by a current entry is ErrConflict.
func (s *Store) writeLine(q, entryID string, no int, l LineInput, saleRef string) error {
	db := s.h.DB()
	args := []any{entryID, no, nullable(l.ItemID), l.Name, l.Qty, l.AmountMinor, nullable(l.Description),
		nullable(l.StaffID), nullable(saleRef), s.now()}
	_, err := db.Exec(q, args...)
	if saleRef == "" || !isUnique(err) {
		return err
	}
	if _, rerr := db.Exec(sqlReleaseStaleClaim, saleRef, no); rerr != nil {
		return rerr
	}
	if _, err = db.Exec(q, args...); isUnique(err) {
		return ErrConflict
	}
	return err
}
