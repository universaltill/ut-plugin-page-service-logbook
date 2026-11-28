//go:build !wasip1

package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

func seed(t *testing.T, e env, staff, date, cur, pay string, total, tip int64, qty int) store.Entry {
	t.Helper()
	id := e.f.IDs().NewID()
	_, err := e.f.DB().Exec(`INSERT INTO entries (id, origin_till_id, author_user_id, staff_id, business_date, currency, payment_type, total_minor, tip_minor, created_at)
		VALUES (?, 'local', 'u1', ?, ?, ?, ?, ?, ?, '2026-11-27T09:00:00Z')`, id, staff, date, cur, pay, total, tip)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.f.DB().Exec(`INSERT INTO entry_lines (entry_id, line_no, name, qty, amount_minor) VALUES (?, 1, 'Cut', ?, ?)`, id, qty, total)
	if err != nil {
		t.Fatal(err)
	}
	en, _ := e.s.GetEntry(id)
	return en
}

func TestAggregatePerStaffAndCurrencyExcludesVoidedSuperseded(t *testing.T) {
	e := newEnv(t, "GB")
	a, b := mustStaff(t, e, "Ann"), mustStaff(t, e, "Bob")
	seed(t, e, a.ID, "2026-11-10", "GBP", "cash", 3000, 200, 2)
	seed(t, e, a.ID, "2026-11-11", "GBP", "card", 2000, 0, 1)
	seed(t, e, a.ID, "2026-11-12", "EUR", "card", 1000, 0, 1)
	seed(t, e, b.ID, "2026-11-12", "GBP", "cash", 500, 50, 1)
	seed(t, e, b.ID, "2026-12-01", "GBP", "cash", 9999, 0, 1) // out of range
	gone, _ := e.s.CreateFree(ann, free(a.ID, 7000, 0))
	e.s.Void(ann, gone.ID)
	old, _ := e.s.CreateFree(ann, free(b.ID, 100, 0))
	e.s.Edit(ann, old.ID, free(b.ID, 200, 0)) // old superseded
	rows, err := e.s.Aggregate("2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]store.AggRow{}
	for _, r := range rows {
		got[r.StaffID+r.Currency] = r
	}
	g := got[a.ID+"GBP"]
	if g.Services != 3 || g.Revenue != 5000 || g.Tips != 200 || g.Cash != 3200 || g.Card != 2000 {
		t.Fatalf("%+v", g)
	}
	if got[a.ID+"EUR"].Revenue != 1000 {
		t.Fatalf("currencies never mix: %+v", got)
	}
	if got[b.ID+"GBP"].Revenue != 700 { // 500 + edited 200; the 9999 is out of range
		t.Fatalf("%+v", got[b.ID+"GBP"])
	}
}

func TestRanges(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }
	from, to, err := store.RangeFor("week", d("2026-11-27"), "", "") // Friday
	if err != nil || from != "2026-11-23" || to != "2026-11-29" {
		t.Fatalf("week starts Monday: %s %s %v", from, to, err)
	}
	from, to, _ = store.RangeFor("month", d("2026-02-10"), "", "")
	if from != "2026-02-01" || to != "2026-02-28" {
		t.Fatalf("%s %s", from, to)
	}
	from, to, _ = store.RangeFor("day", d("2026-11-27"), "", "")
	if from != "2026-11-27" || to != from {
		t.Fatal("day")
	}
	if _, _, err := store.RangeFor("custom", d("2026-11-27"), "2025-01-01", "2026-11-27"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("custom range is capped at 366 days: %v", err)
	}
	if _, _, err := store.RangeFor("custom", d("2026-11-27"), "2026-11-05", "2026-11-01"); !errors.Is(err, store.ErrInvalid) {
		t.Fatal("reversed")
	}
	if f, tt, err := store.RangeFor("custom", d("2026-11-27"), "2026-01-01", "2026-12-31"); err != nil || f != "2026-01-01" || tt != "2026-12-31" {
		t.Fatalf("365 days is fine: %v", err)
	}
}

func TestCompareMonthYearAndPercent(t *testing.T) {
	e := newEnv(t, "GB")
	a := mustStaff(t, e, "Ann")
	seed(t, e, a.ID, "2026-11-05", "GBP", "cash", 15000, 0, 1) // this month
	seed(t, e, a.ID, "2026-10-05", "GBP", "cash", 10000, 0, 1) // last month
	seed(t, e, a.ID, "2025-11-05", "GBP", "cash", 20000, 0, 1) // same period last year
	seed(t, e, a.ID, "2025-12-25", "GBP", "cash", 99999, 0, 1) // later than the elapsed period last year
	c, err := e.s.Compare("2026-11-27")
	if err != nil {
		t.Fatal(err)
	}
	m := c.Month[0]
	if m.Cur != 15000 || m.Prev != 10000 || m.Abs != 5000 || !m.PctOK || m.PctBP != 5000 || store.FormatBP(m.PctBP) != "50.0%" {
		t.Fatalf("%+v", m)
	}
	y := c.Year[0]
	if y.Cur != 15000+10000 || y.Prev != 20000 {
		t.Fatalf("year-to-date vs the same days last year: %+v", y)
	}
	z := store.Delta(500, 0)
	if z.PctOK {
		t.Fatal("percent change on a zero base is n/a")
	}
	if store.FormatBP(-1234) != "-12.3%" || store.FormatBP(5) != "0.0%" {
		t.Fatalf("%s %s", store.FormatBP(-1234), store.FormatBP(5))
	}
	// shop total row (per currency) exists
	foundTotal := false
	for _, r := range c.Month {
		if r.StaffID == "" && r.Currency == "GBP" {
			foundTotal = true
		}
	}
	if !foundTotal {
		t.Fatalf("shop total missing: %+v", c.Month)
	}
}

func TestCompareBoundariesJanAndLeapDay(t *testing.T) {
	e := newEnv(t, "GB")
	a := mustStaff(t, e, "Ann")
	seed(t, e, a.ID, "2025-12-31", "GBP", "cash", 100, 0, 1)
	seed(t, e, a.ID, "2026-01-02", "GBP", "cash", 300, 0, 1)
	c, _ := e.s.Compare("2026-01-15")
	if c.Month[0].Prev != 100 || c.Month[0].Cur != 300 {
		t.Fatalf("January compares with December: %+v", c.Month[0])
	}
	// 29 Feb 2028 vs last year: 28 Feb 2027 is the last day available
	seed(t, e, a.ID, "2027-02-28", "GBP", "cash", 700, 0, 1)
	seed(t, e, a.ID, "2027-03-01", "GBP", "cash", 900, 0, 1)
	c, err := e.s.Compare("2028-02-29")
	if err != nil {
		t.Fatal(err)
	}
	var y store.DeltaRow
	for _, r := range c.Year {
		if r.StaffID == a.ID {
			y = r
		}
	}
	if y.Prev != 700 {
		t.Fatalf("leap day clamps to 28 Feb of last year: %+v", y)
	}
}

func TestTipsSuggest(t *testing.T) {
	e := newEnv(t, "GB")
	a := mustStaff(t, e, "Ann")
	e.s.EditStaff(a.ID, store.StaffInput{Name: "Ann", LinkedUserID: "u1"})
	b := mustStaff(t, e, "Bob") // no login
	c := mustStaff(t, e, "Cat")
	seed(t, e, a.ID, "2026-11-10", "GBP", "card", 100, 250, 1)
	seed(t, e, a.ID, "2026-11-11", "GBP", "card", 100, 100, 1)
	seed(t, e, a.ID, "2026-11-12", "EUR", "card", 100, 999, 1) // other currency dropped
	seed(t, e, b.ID, "2026-11-10", "GBP", "cash", 100, 70, 1)
	seed(t, e, c.ID, "2026-12-10", "GBP", "cash", 100, 70, 1) // out of range
	voided := seed(t, e, a.ID, "2026-11-13", "GBP", "card", 100, 500, 1)
	e.s.Void(boss, voided.ID)
	sug, err := e.s.SuggestTips("2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(sug.Suggestions) != 1 || sug.Suggestions[0].UserID != "u1" || sug.Suggestions[0].AmountMinor != 350 || sug.Suggestions[0].Currency != "GBP" {
		t.Fatalf("%+v", sug)
	}
	if len(sug.Unmapped) != 1 || sug.Unmapped[0].Label != "Bob" || sug.Unmapped[0].AmountMinor != 70 {
		t.Fatalf("%+v", sug.Unmapped)
	}
}

func TestReconcile(t *testing.T) {
	e := newEnv(t, "GB")
	a := mustStaff(t, e, "Ann")
	seed(t, e, a.ID, "2026-11-27", "GBP", "cash", 3000, 0, 1)
	seed(t, e, a.ID, "2026-11-27", "GBP", "card", 2000, 100, 1)
	gb := func(id string, pay ...map[string]any) map[string]any {
		r := receipt(id, false, 0, pay...)
		r["currency"] = "GBP"
		return r
	}
	ret := gb("r9", cashPay(1))
	ret["sale_type"] = "return"
	e.f.SetView("sales.receipts.v1", []map[string]any{gb("r1", cashPay(3000)), gb("r2", cardPay(2000)), ret})
	rows, err := e.s.Reconcile(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%+v", rows)
	}
	r := rows[0]
	if r.Date != "2026-11-27" || r.LogCash != 3000 || r.LogCard != 2100 || r.TillCash != 3000 || r.TillCard != 2000 || r.CardDiff() != 100 || r.CashDiff() != 0 {
		t.Fatalf("returns are not takings; diff = logbook - till: %+v", r)
	}
	if _, err := e.s.Reconcile(32); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("limited to 31 days: %v", err)
	}
}

// Tester gap (mutation): till takings include the payment tip, and the diff
// is logbook minus till (sign matters to the operator).
func TestReconcileTipsAndDiffSign_Tester(t *testing.T) {
	e := newEnv(t, "GB")
	a := mustStaff(t, e, "Ann")
	seed(t, e, a.ID, "2026-11-27", "GBP", "cash", 3000, 0, 1)
	r1 := receipt("r1", false, 0, map[string]any{"method_id": "cash", "amount_minor": 2500, "tip_minor": 200})
	r1["currency"] = "GBP"
	e.f.SetView("sales.receipts.v1", []map[string]any{r1})
	rows, err := e.s.Reconcile(1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %+v", err, rows)
	}
	if rows[0].TillCash != 2700 || rows[0].CashDiff() != 300 {
		t.Fatalf("till cash = amount+tip, diff = log - till: %+v", rows[0])
	}
}
