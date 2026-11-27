// Package export builds the logbook CSVs answered to export.requested.ask
// (Settings > Data, manager-gated by core). The page vocabulary has no
// download component, so CSV is never a link on a plugin page (BA G4).
package export

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

// Result is the export answer (plugin-manifest.md): UTF-8 CSV with a BOM,
// base64 encoded.
type Result struct {
	OK         bool   `json:"ok"`
	Filename   string `json:"filename"`
	ContentB64 string `json:"content_b64"`
}

// Decimal formats minor units with the currency's decimals, integers only.
func Decimal(minor int64, decimals int) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	s := fmt.Sprintf("%d", minor)
	if decimals > 0 {
		for len(s) <= decimals {
			s = "0" + s
		}
		s = s[:len(s)-decimals] + "." + s[len(s)-decimals:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Cell escapes one CSV cell and defuses spreadsheet formulas: a cell that
// starts with = + - @ TAB or CR is prefixed with an apostrophe.
func Cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		s = "'" + s
	}
	if strings.ContainsAny(s, ",\"\r\n") {
		s = `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func row(cells ...string) string {
	for i, c := range cells {
		cells[i] = Cell(c)
	}
	return strings.Join(cells, ",") + "\r\n"
}

// Build makes "summary" (one row per staff x currency) or "entries" (one row
// per current entry line) for [from, to]; archived staff show their
// pseudonym through former.
func Build(s *store.Store, kind, from, to string, former func(int) string) (Result, error) {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 365*24*time.Hour {
		return Result{}, store.ErrInvalid
	}
	shop, err := s.Shop()
	if err != nil {
		return Result{}, err
	}
	staff, err := s.ListStaff(true)
	if err != nil {
		return Result{}, err
	}
	name := map[string]string{}
	for _, st := range staff {
		name[st.ID] = st.Label(former)
	}
	var b strings.Builder
	b.WriteString("\xEF\xBB\xBF")
	d := shop.Decimals
	switch kind {
	case "summary":
		b.WriteString(row("staff", "currency", "services", "revenue_minor", "revenue", "tips_minor", "tips", "cash_minor", "cash", "card_minor", "card"))
		agg, err := s.Aggregate(from, to)
		if err != nil {
			return Result{}, err
		}
		sort.SliceStable(agg, func(i, j int) bool { return name[agg[i].StaffID] < name[agg[j].StaffID] })
		for _, a := range agg {
			b.WriteString(row(name[a.StaffID], a.Currency, fmt.Sprint(a.Services), fmt.Sprint(a.Revenue), Decimal(a.Revenue, d),
				fmt.Sprint(a.Tips), Decimal(a.Tips, d), fmt.Sprint(a.Cash), Decimal(a.Cash, d), fmt.Sprint(a.Card), Decimal(a.Card, d)))
		}
	case "entries":
		b.WriteString(row("date", "staff", "currency", "payment", "item", "description", "qty", "amount_minor", "amount", "tip_minor", "tip", "sale_ref", "entry_id"))
		lines, err := s.ExportLines(from, to)
		if err != nil {
			return Result{}, err
		}
		for _, l := range lines {
			b.WriteString(row(l.Date, name[l.StaffID], l.Currency, l.Payment, l.Item, l.Description, fmt.Sprint(l.Qty),
				fmt.Sprint(l.AmountMinor), Decimal(l.AmountMinor, d), fmt.Sprint(l.TipMinor), Decimal(l.TipMinor, d), l.SaleRef, l.EntryID))
		}
	default:
		return Result{}, store.ErrInvalid
	}
	return Result{OK: true, Filename: fmt.Sprintf("logbook-%s-%s-%s.csv", kind, from, to),
		ContentB64: base64.StdEncoding.EncodeToString([]byte(b.String()))}, nil
}
