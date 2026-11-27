package store

import (
	"encoding/json"
	"sort"
)

// Receipt is the part of a sales.receipts.v1 row the logbook uses.
type Receipt struct {
	ID           string        `json:"id"`
	ReceiptNo    string        `json:"receipt_no"`
	SaleType     string        `json:"sale_type"`
	BusinessDate string        `json:"business_date"`
	Currency     string        `json:"currency"`
	TotalMinor   int64         `json:"total_minor"`
	TipMinor     int64         `json:"tip_minor"`
	FiscalSigned bool          `json:"fiscal_signed"`
	Lines        []ReceiptLine `json:"lines"`
	Payments     []Payment     `json:"payments"`
}

// ReceiptLine is one sold line (total tax included).
type ReceiptLine struct {
	LineNo     int     `json:"line_no"`
	ItemID     string  `json:"item_id"`
	Name       string  `json:"name"`
	Quantity   float64 `json:"quantity"`
	TotalMinor int64   `json:"total_minor"`
}

// Payment is one tender of a receipt.
type Payment struct {
	MethodID    string `json:"method_id"`
	AmountMinor int64  `json:"amount_minor"`
	TipMinor    int64  `json:"tip_minor"`
}

// PaymentType: cash when the largest payment is cash, else card (documented
// limitation for mixed sales, BA #3702).
func (r Receipt) PaymentType() string {
	var best Payment
	for _, p := range r.Payments {
		if p.AmountMinor > best.AmountMinor {
			best = p
		}
	}
	if best.MethodID == "cash" {
		return "cash"
	}
	return "card"
}

func (s *Store) receiptsRaw(daysAgo int) ([]Receipt, error) {
	var all []Receipt
	for off := 0; off < 1000; off += 100 {
		raw, err := s.h.Views().Query("sales.receipts.v1", map[string]int{"days_ago": daysAgo, "offset": off, "limit": 100})
		if err != nil {
			return nil, err
		}
		var page []Receipt
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < 100 {
			break
		}
	}
	return all, nil
}

// linkedLines is line_no -> true for receipt lines already carried by a
// current entry of that sale (linked entries keep the receipt's line_no).
func (s *Store) linkedLines(saleID string) (map[int]bool, error) {
	rows, err := s.h.DB().Query(`SELECT l.line_no FROM entry_lines l JOIN entries e ON e.id = l.entry_id
		WHERE e.sale_ref = ? AND `+currentWhere, saleID)
	if err != nil {
		return nil, err
	}
	m := map[int]bool{}
	for _, r := range rows {
		m[int(num(r, "line_no"))] = true
	}
	return m, nil
}

// Receipts lists the sales a linked entry may pick from, one business day
// (0-31 days ago): only completed sales (never returns), only fiscally
// signed ones where the country forces linked entry (DE/TR), and without the
// ones whose lines are all linked already. A till lists only what it holds.
func (s *Store) Receipts(daysAgo int) ([]Receipt, error) {
	if daysAgo < 0 || daysAgo > 31 {
		return nil, ErrInvalid
	}
	all, err := s.receiptsRaw(daysAgo)
	if err != nil {
		return nil, err
	}
	signedOnly := MoneyEntryForced(s.Country())
	var out []Receipt
	for _, r := range all {
		if r.SaleType != "sale" || (signedOnly && !r.FiscalSigned) {
			continue
		}
		done, err := s.linkedLines(r.ID)
		if err != nil {
			return nil, err
		}
		open := 0
		for _, l := range r.Lines {
			if !done[l.LineNo] {
				open++
			}
		}
		if open == 0 && len(r.Lines) > 0 {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// LinkInput names the sale and who did which line. It carries NO money: the
// amounts are read again from the view (a posted amount does not exist here).
type LinkInput struct {
	SaleID  string
	DaysAgo int
	Assign  map[int]string // receipt line_no -> staff id
	TipTo   string         // staff id; default: staff of the first line
}

// CreateLinked writes one entry per distinct staff member (same sale_ref).
func (s *Store) CreateLinked(a Actor, in LinkInput) ([]Entry, error) {
	if s.MoneyEntry() != "linked" {
		return nil, ErrForbidden
	}
	offered, err := s.Receipts(in.DaysAgo)
	if err != nil {
		return nil, err
	}
	var rc *Receipt
	for i := range offered {
		if offered[i].ID == in.SaleID {
			rc = &offered[i]
		}
	}
	if rc == nil {
		// not offered: unknown, a return, unsigned in DE/TR, or fully linked
		if all, _ := s.receiptsRaw(in.DaysAgo); containsSale(all, in.SaleID) {
			if done, _ := s.linkedLines(in.SaleID); len(done) > 0 {
				return nil, ErrConflict
			}
		}
		return nil, ErrInvalid
	}
	done, err := s.linkedLines(rc.ID)
	if err != nil {
		return nil, err
	}
	type group struct {
		total int64
		lines []LineInput
		nos   []int
	}
	groups := map[string]*group{}
	var order []string
	first := ""
	for _, l := range rc.Lines {
		if done[l.LineNo] {
			continue
		}
		sid, ok := in.Assign[l.LineNo]
		if !ok {
			return nil, ErrInvalid
		}
		st, err := s.GetStaff(sid)
		if err != nil || st.Archived() {
			return nil, ErrInvalid
		}
		if first == "" {
			first = sid
		}
		g := groups[sid]
		if g == nil {
			g = &group{}
			groups[sid] = g
			order = append(order, sid)
		}
		q := 1
		if l.Quantity >= 1 && l.Quantity <= 999 && l.Quantity == float64(int(l.Quantity)) {
			q = int(l.Quantity)
		}
		g.total += l.TotalMinor
		g.lines = append(g.lines, LineInput{ItemID: l.ItemID, Name: clip(l.Name, 120), Qty: q, AmountMinor: l.TotalMinor, StaffID: sid})
		g.nos = append(g.nos, l.LineNo)
	}
	if len(order) == 0 {
		return nil, ErrConflict
	}
	tipTo := in.TipTo
	if tipTo == "" {
		tipTo = first
	}
	if rc.TipMinor > 0 {
		if st, err := s.GetStaff(tipTo); err != nil || st.Archived() {
			return nil, ErrInvalid
		}
		if groups[tipTo] == nil {
			groups[tipTo] = &group{}
			order = append(order, tipTo)
		}
	}
	sort.Strings(order)
	var out []Entry
	for _, sid := range order {
		g := groups[sid]
		e := Entry{ID: s.h.IDs().NewID(), OriginTill: LocalTill, AuthorUserID: a.UserID, StaffID: sid,
			BusinessDate: rc.BusinessDate, Currency: rc.Currency, Payment: rc.PaymentType(), TotalMinor: g.total,
			SaleRef: rc.ID, Mode: "linked", CreatedAt: s.now()}
		if sid == tipTo {
			e.TipMinor = rc.TipMinor
		}
		if err := s.writeNumbered(e, g.lines, g.nos); err != nil {
			return out, err
		}
		got, _ := s.GetEntry(e.ID)
		out = append(out, got)
	}
	return out, nil
}

func containsSale(rs []Receipt, id string) bool {
	for _, r := range rs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
