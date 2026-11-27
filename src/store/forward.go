package store

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
// queued. The outbox is idempotent per id, so retrying is safe. A host
// error leaves fwd_state='pending' for the maintenance retry.
func (s *Store) ForwardEntry(id string) {
	e, err := s.GetEntry(id)
	if err != nil {
		return
	}
	lines, _ := s.Lines(id)
	w := Wire{Entry: WireEntry{e.ID, e.AuthorUserID, e.StaffID, e.BusinessDate, e.Currency, e.Payment, e.TotalMinor,
		e.TipMinor, e.SaleRef, e.SupersedesID, e.Voided, e.Mode, e.CreatedAt}}
	for _, l := range lines {
		w.Lines = append(w.Lines, WireLine{l.LineNo, l.ItemID, l.Name, l.Qty, l.AmountMinor, l.Description, l.StaffID})
	}
	if _, err := s.h.Records().Forward("entries", id, w); err != nil {
		return
	}
	_, _ = s.h.DB().Exec("UPDATE entries SET fwd_state = 'queued' WHERE id = ? AND fwd_state = 'pending'", id)
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

// SweepOrphans deletes lines whose entry row never landed (a crash between
// the line writes and the commit-point write). It assumes the host runs one
// event at a time per plugin, so a sweep never overlaps a write; confirm that
// when #3157 slice B lands (open item, notes/04-dev-plugin.md).
func (s *Store) SweepOrphans() {
	_, _ = s.h.DB().Exec(`DELETE FROM entry_lines WHERE entry_id NOT IN (SELECT id FROM entries)`)
}
