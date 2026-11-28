package store

import (
	"sort"
	"time"
)

// Wire is the forwarded payload of one entry (<= 64 KiB: <= 50 lines, field
// caps keep an entry far below).
type Wire struct {
	Entry WireEntry  `json:"entry"`
	Lines []WireLine `json:"lines"`
}

// WireEntry mirrors entries (origin_till_id is not sent: main stamps it from
// core's from_till_id, a sender-supplied value is ignored).
type WireEntry struct {
	ID           string `json:"id"`
	Author       string `json:"author_user_id"`
	StaffID      string `json:"staff_id"`
	BusinessDate string `json:"business_date"`
	Currency     string `json:"currency"`
	Payment      string `json:"payment_type"`
	TotalMinor   int64  `json:"total_minor"`
	TipMinor     int64  `json:"tip_minor"`
	SaleRef      string `json:"sale_ref"`
	SupersedesID string `json:"supersedes_id"`
	Voided       bool   `json:"voided"`
	Mode         string `json:"mode"`
	CreatedAt    string `json:"created_at"`
}

// WireLine mirrors entry_lines.
type WireLine struct {
	LineNo      int    `json:"line_no"`
	ItemID      string `json:"item_id"`
	Name        string `json:"name"`
	Qty         int    `json:"qty"`
	AmountMinor int64  `json:"amount_minor"`
	Description string `json:"description"`
	StaffID     string `json:"staff_id"`
}

// ForwardEntry sends one entry through the records channel and marks it
// queued ('na' when the host says this till is main). The outbox is
// idempotent per id, so retrying is safe. A host or read error leaves
// fwd_state='pending' for the maintenance retry.
func (s *Store) ForwardEntry(id string) {
	e, err := s.GetEntry(id)
	if err != nil {
		return
	}
	lines, err := s.Lines(id)
	if err != nil {
		return // never forward an entry without its lines; stays pending
	}
	w := Wire{Entry: WireEntry{e.ID, e.AuthorUserID, e.StaffID, e.BusinessDate, e.Currency, e.Payment, e.TotalMinor,
		e.TipMinor, e.SaleRef, e.SupersedesID, e.Voided, e.Mode, e.CreatedAt}}
	for _, l := range lines {
		w.Lines = append(w.Lines, WireLine{l.LineNo, l.ItemID, l.Name, l.Qty, l.AmountMinor, l.Description, l.StaffID})
	}
	queued, err := s.h.Records().Forward("entries", id, w)
	if err != nil {
		return
	}
	state := "queued"
	if !queued { // this till is main after all (pending from an unreadable role)
		state = "na"
	}
	_, _ = s.h.DB().Exec("UPDATE entries SET fwd_state = ? WHERE id = ? AND fwd_state = 'pending'", state, id)
}

// RetryPending re-forwards rows still pending (maintenance tick).
func (s *Store) RetryPending() int {
	rows, err := s.h.DB().Query("SELECT id FROM entries WHERE fwd_state = 'pending' ORDER BY created_at LIMIT 100")
	if err != nil {
		return 0
	}
	for _, r := range rows {
		s.ForwardEntry(str(r, "id"))
	}
	return len(rows)
}

// orphanAge is how old a parent-less line must be before the sweep removes
// it. An entry write takes milliseconds; an hour is far past any write in
// flight (the event deadline is 10 s).
const orphanAge = time.Hour

// SweepOrphans deletes lines whose entry row never landed (a crash between
// the line writes and the commit-point write). The host runs events of one
// plugin concurrently, so a parent-less line may belong to an entry being
// written right now: only lines older than orphanAge are removed (created_at,
// migration 0002; a line from before 0002 has none and is old).
func (s *Store) SweepOrphans() {
	cutoff := s.h.Clock().Now().UTC().Add(-orphanAge).Format("2006-01-02T15:04:05Z")
	_, _ = s.h.DB().Exec(`DELETE FROM entry_lines WHERE entry_id NOT IN (SELECT id FROM entries)
		AND (created_at IS NULL OR created_at < ?)`, cutoff)
}

// replicaKeepDays: a replica's delivered copies are transient (architect
// 5.7); the main holds the log.
const replicaKeepDays = 35

// TrimReplica (replica only, maintenance tick) deletes settled local copies:
// delivered (queued and gone from the outbox) or rejected rows that are older
// than 35 days, or whose member left the staff snapshot (archived or erased
// on the main). Undelivered rows are never trimmed. A trimmed row takes the
// settled rows it supersedes with it, so no older row becomes current again.
// Nothing is trimmed when the role, the outbox status or the snapshot cannot
// be read. Returns the number of entries removed.
func (s *Store) TrimReplica() int {
	if role, err := s.Role(); err != nil || role != "replica" {
		return 0
	}
	sts, err := s.h.Records().Status("entries")
	if err != nil {
		return 0
	}
	open := map[string]bool{}
	for _, st := range sts {
		if st.Status != "rejected" {
			open[st.ID] = true
		}
	}
	snap, found, err := s.snapshotStaff()
	if err != nil {
		return 0
	}
	inSnap := map[string]bool{}
	for _, st := range snap {
		inSnap[st.ID] = true
	}
	rows, err := s.h.DB().Query("SELECT id, staff_id, supersedes_id, created_at FROM entries WHERE fwd_state = 'queued'")
	if err != nil {
		return 0
	}
	cutoff := s.h.Clock().Now().UTC().Add(-replicaKeepDays * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	settled := map[string]string{} // id -> supersedes_id
	doomed := map[string]bool{}
	for _, r := range rows {
		id := str(r, "id")
		if open[id] {
			continue
		}
		settled[id] = str(r, "supersedes_id")
		staff := str(r, "staff_id")
		if str(r, "created_at") < cutoff || (found && staff != "" && !inSnap[staff]) {
			doomed[id] = true
		}
	}
	for id := range doomed {
		for p := settled[id]; p != ""; p = settled[p] {
			if _, ok := settled[p]; !ok || doomed[p] {
				break
			}
			doomed[p] = true
		}
	}
	ids := make([]string, 0, len(doomed))
	for id := range doomed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s.removeEntry(id)
	}
	return len(ids)
}
