package store

import (
	"fmt"
	"sort"
	"time"
)

const dateFmt = "2006-01-02"

// AggRow is one staff x currency line of the reports panel. Revenue is the
// services total (tip excluded). Cash and Card are takings by payment type
// (services plus tip), the number a till count is compared against.
type AggRow struct {
	StaffID, Currency                   string
	Services, Revenue, Tips, Cash, Card int64
}

// Aggregate sums CURRENT entries (not voided, not superseded, not shadowed)
// by business_date in [from, to], per staff and currency, never mixing
// currencies. Services = sum of line quantities.
func (s *Store) Aggregate(from, to string) ([]AggRow, error) {
	rows, err := s.h.DB().Query(`SELECT COALESCE(e.staff_id, '') AS staff_id, e.currency AS currency,
		SUM((SELECT COALESCE(SUM(l.qty), 0) FROM entry_lines l WHERE l.entry_id = e.id)) AS services,
		SUM(e.total_minor) AS revenue, SUM(e.tip_minor) AS tips,
		SUM(CASE WHEN e.payment_type = 'cash' THEN e.total_minor + e.tip_minor ELSE 0 END) AS cash,
		SUM(CASE WHEN e.payment_type = 'card' THEN e.total_minor + e.tip_minor ELSE 0 END) AS card
		FROM entries e WHERE e.business_date BETWEEN ? AND ? AND `+currentWhere+`
		GROUP BY e.staff_id, e.currency ORDER BY e.currency, e.staff_id`, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]AggRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, AggRow{str(r, "staff_id"), str(r, "currency"), num(r, "services"), num(r, "revenue"),
			num(r, "tips"), num(r, "cash"), num(r, "card")})
	}
	return out, nil
}

// RangeFor resolves a report range over business_date. Weeks start Monday; a
// custom range is capped at 366 days.
func RangeFor(kind string, anchor time.Time, from, to string) (string, string, error) {
	switch kind {
	case "day":
		return anchor.Format(dateFmt), anchor.Format(dateFmt), nil
	case "week":
		off := (int(anchor.Weekday()) + 6) % 7
		mon := anchor.AddDate(0, 0, -off)
		return mon.Format(dateFmt), mon.AddDate(0, 0, 6).Format(dateFmt), nil
	case "month":
		first := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.UTC)
		return first.Format(dateFmt), first.AddDate(0, 1, -1).Format(dateFmt), nil
	case "custom":
		f, err1 := time.Parse(dateFmt, from)
		t, err2 := time.Parse(dateFmt, to)
		if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 365*24*time.Hour {
			return "", "", ErrInvalid
		}
		return from, to, nil
	}
	return "", "", ErrInvalid
}

// DeltaRow compares revenue of two periods. PctBP is basis points; PctOK is
// false when the base is 0 ("n/a", never an invented number).
type DeltaRow struct {
	StaffID, Currency string
	Cur, Prev, Abs    int64
	PctBP             int64
	PctOK             bool
}

// Delta computes the absolute and percentage change in integers.
func Delta(cur, prev int64) DeltaRow {
	d := DeltaRow{Cur: cur, Prev: prev, Abs: cur - prev}
	if prev != 0 {
		d.PctOK = true
		d.PctBP = (cur - prev) * 10000 / prev
	}
	return d
}

// FormatBP renders basis points as a one-decimal percentage without floats.
func FormatBP(bp int64) string {
	sign := ""
	if bp < 0 {
		sign, bp = "-", -bp
	}
	return fmt.Sprintf("%s%d.%d%%", sign, bp/100, bp%100/10)
}

// Comparison is this month vs last and this year vs last (same elapsed
// period), per staff and as a shop total (StaffID "") per currency.
type Comparison struct{ Month, Year []DeltaRow }

// Compare uses revenue (services total). The month is compared with the
// whole previous month; the year with the same elapsed days of last year.
func (s *Store) Compare(today string) (Comparison, error) {
	t, err := time.Parse(dateFmt, today)
	if err != nil {
		return Comparison{}, ErrInvalid
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	pm := first.AddDate(0, -1, 0)
	pmEnd := first.AddDate(0, 0, -1)
	month, err := s.deltas(first.Format(dateFmt), today, pm.Format(dateFmt), pmEnd.Format(dateFmt))
	if err != nil {
		return Comparison{}, err
	}
	jan := time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	pyFrom := time.Date(t.Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
	pyTo := time.Date(t.Year()-1, t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if pyTo.Month() != t.Month() { // 29 Feb -> 1 Mar: clamp to the last day of the month
		pyTo = time.Date(t.Year()-1, t.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	}
	year, err := s.deltas(jan.Format(dateFmt), today, pyFrom.Format(dateFmt), pyTo.Format(dateFmt))
	if err != nil {
		return Comparison{}, err
	}
	return Comparison{month, year}, nil
}

func (s *Store) deltas(cf, ct, pf, pt string) ([]DeltaRow, error) {
	cur, err := s.Aggregate(cf, ct)
	if err != nil {
		return nil, err
	}
	prev, err := s.Aggregate(pf, pt)
	if err != nil {
		return nil, err
	}
	type k struct{ staff, cur string }
	c, p := map[k]int64{}, map[k]int64{}
	keys := map[k]bool{}
	for _, r := range cur {
		c[k{r.StaffID, r.Currency}] += r.Revenue
		c[k{"", r.Currency}] += r.Revenue
		keys[k{r.StaffID, r.Currency}], keys[k{"", r.Currency}] = true, true
	}
	for _, r := range prev {
		p[k{r.StaffID, r.Currency}] += r.Revenue
		p[k{"", r.Currency}] += r.Revenue
		keys[k{r.StaffID, r.Currency}], keys[k{"", r.Currency}] = true, true
	}
	var list []k
	for kk := range keys {
		list = append(list, kk)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].cur != list[j].cur {
			return list[i].cur < list[j].cur
		}
		return list[i].staff < list[j].staff
	})
	out := make([]DeltaRow, 0, len(list))
	for _, kk := range list {
		d := Delta(c[kk], p[kk])
		d.StaffID, d.Currency = kk.staff, kk.cur
		out = append(out, d)
	}
	return out, nil
}

// ReconRow compares one day's logbook takings with the till's, by payment
// type. Diff = logbook minus till.
type ReconRow struct {
	Date, Currency     string
	LogCash, LogCard   int64
	TillCash, TillCard int64
}

func (r ReconRow) CashDiff() int64 { return r.LogCash - r.TillCash }
func (r ReconRow) CardDiff() int64 { return r.LogCard - r.TillCard }

// Reconcile covers at most the last 31 days (sales.receipts.v1's window;
// sales.by_day / payments.breakdown are rolling windows, not per date).
// Till takings are the payments of completed sales (returns excluded).
func (s *Store) Reconcile(days int) ([]ReconRow, error) {
	if days < 1 || days > 31 {
		return nil, ErrInvalid
	}
	shop, err := s.Shop()
	if err != nil {
		return nil, err
	}
	today, err := time.Parse(dateFmt, shop.BusinessDate)
	if err != nil {
		return nil, ErrInvalid
	}
	var out []ReconRow
	for d := 0; d < days; d++ {
		date := today.AddDate(0, 0, -d).Format(dateFmt)
		rcs, err := s.receiptsRaw(d)
		if err != nil {
			return nil, err
		}
		till := map[string]*ReconRow{}
		get := func(cur string) *ReconRow {
			if till[cur] == nil {
				till[cur] = &ReconRow{Date: date, Currency: cur}
			}
			return till[cur]
		}
		for _, r := range rcs {
			if r.SaleType != "sale" {
				continue
			}
			row := get(r.Currency)
			for _, p := range r.Payments {
				if p.MethodID == "cash" {
					row.TillCash += p.AmountMinor + p.TipMinor
				} else {
					row.TillCard += p.AmountMinor + p.TipMinor
				}
			}
		}
		agg, err := s.Aggregate(date, date)
		if err != nil {
			return nil, err
		}
		for _, a := range agg {
			row := get(a.Currency)
			row.LogCash += a.Cash
			row.LogCard += a.Card
		}
		var curs []string
		for c := range till {
			curs = append(curs, c)
		}
		sort.Strings(curs)
		for _, c := range curs {
			out = append(out, *till[c])
		}
	}
	return out, nil
}

// TipSuggestion is one payout pre-fill row (#7108): only mapped users.
type TipSuggestion struct {
	UserID      string `json:"user_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// TipUnmapped is a staff member with tips but no till user.
type TipUnmapped struct {
	Label       string `json:"label"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// Tips is the answer to tips.suggest.ask.
type Tips struct {
	Suggestions []TipSuggestion `json:"suggestions"`
	Unmapped    []TipUnmapped   `json:"unmapped"`
}

// SuggestTips sums current tips per staff in the shop's own currency. Only a
// suggestion: the manager confirms; the plugin never writes the ledger.
func (s *Store) SuggestTips(from, to string) (Tips, error) {
	shop, err := s.Shop()
	if err != nil {
		return Tips{}, err
	}
	agg, err := s.Aggregate(from, to)
	if err != nil {
		return Tips{}, err
	}
	staff, err := s.ListStaff(false)
	if err != nil {
		return Tips{}, err
	}
	by := map[string]Staff{}
	for _, st := range staff {
		by[st.ID] = st
	}
	out := Tips{Suggestions: []TipSuggestion{}, Unmapped: []TipUnmapped{}}
	byUser := map[string]int64{}
	var users []string
	for _, r := range agg {
		st, ok := by[r.StaffID]
		if !ok || r.Currency != shop.Currency || r.Tips == 0 {
			continue // archived or gone staff and other currencies are dropped
		}
		if st.LinkedUserID == "" {
			out.Unmapped = append(out.Unmapped, TipUnmapped{st.Name, r.Tips, r.Currency})
			continue
		}
		if _, seen := byUser[st.LinkedUserID]; !seen {
			users = append(users, st.LinkedUserID)
		}
		byUser[st.LinkedUserID] += r.Tips
	}
	sort.Strings(users)
	for _, u := range users {
		out.Suggestions = append(out.Suggestions, TipSuggestion{u, byUser[u], shop.Currency})
	}
	return out, nil
}

// ExportLine is one CSV row of the entries export: a current entry's line,
// or the entry itself when it has none (a tip-only entry).
type ExportLine struct {
	Date, EntryID, StaffID, Currency, Payment, Item, Description, SaleRef string
	Qty                                                                   int64
	AmountMinor, TipMinor                                                 int64
}

// ExportLines lists current entries' lines in [from, to] (voided and
// shadowed never appear). The tip is on the entry's first row only.
func (s *Store) ExportLines(from, to string) ([]ExportLine, error) {
	rows, err := s.h.DB().Query(`SELECT e.business_date AS d, e.id AS id, COALESCE(e.staff_id, '') AS staff, e.currency AS cur,
		e.payment_type AS pay, COALESCE(l.name, '') AS item, COALESCE(l.description, '') AS descr, COALESCE(e.sale_ref, '') AS sale,
		COALESCE(l.qty, 0) AS qty, COALESCE(l.amount_minor, 0) AS amt, e.tip_minor AS tip, COALESCE(l.line_no, 1) AS no
		FROM entries e LEFT JOIN entry_lines l ON l.entry_id = e.id
		WHERE e.business_date BETWEEN ? AND ? AND `+currentWhere+` ORDER BY e.business_date, e.created_at, e.id, l.line_no`, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]ExportLine, 0, len(rows))
	for _, r := range rows {
		tip := num(r, "tip")
		if num(r, "no") > 1 {
			tip = 0
		}
		out = append(out, ExportLine{str(r, "d"), str(r, "id"), str(r, "staff"), str(r, "cur"), str(r, "pay"), str(r, "item"),
			str(r, "descr"), str(r, "sale"), num(r, "qty"), num(r, "amt"), tip})
	}
	return out, nil
}
