//go:build !wasip1

package export_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/export"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/hosttest"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

func setup(t *testing.T) (*store.Store, *hosttest.Fake) {
	f := hosttest.New(t)
	f.SetView("shop.context.v2", []map[string]any{{"currency_code": "GBP", "currency_decimals": 2, "locale": "en", "country": "FR", "business_date": "2026-11-27"}})
	return store.New(f), f
}

func former(n int) string { return "Former staff #" + string(rune('0'+n)) }

func TestDecimal(t *testing.T) {
	cases := []struct {
		m    int64
		d    int
		want string
	}{{1234, 2, "12.34"}, {5, 2, "0.05"}, {0, 2, "0.00"}, {-250, 2, "-2.50"}, {700, 0, "700"}, {12345, 3, "12.345"}}
	for _, c := range cases {
		if got := export.Decimal(c.m, c.d); got != c.want {
			t.Fatalf("%d/%d: %s != %s", c.m, c.d, got, c.want)
		}
	}
}

func TestCellInjectionGuard(t *testing.T) {
	for _, in := range []string{"=1+1", "+cmd", "-2", "@SUM(A1)", "\t=x", "\r=x"} {
		if got := export.Cell(in); !strings.HasPrefix(got, "'") && !strings.HasPrefix(got, `"'`) {
			t.Fatalf("%q -> %q", in, got)
		}
	}
	if export.Cell("plain") != "plain" || export.Cell(`a,b`) != `"a,b"` || export.Cell(`say "hi"`) != `"say ""hi"""` {
		t.Fatal("escaping")
	}
}

func TestSummaryAndEntriesCSV(t *testing.T) {
	s, f := setup(t)
	a, _ := s.AddStaff(store.StaffInput{Name: "=Ann"})
	b, _ := s.AddStaff(store.StaffInput{Name: "Bob"})
	act := store.Actor{UserID: "u1"}
	s.CreateFree(act, store.FreeInput{StaffID: a.ID, Payment: "cash", TipMinor: 150, Lines: []store.LineInput{{Name: "Cut", Qty: 2, AmountMinor: 4000}, {Name: "Wash, dry", Qty: 1, AmountMinor: 1000}}})
	en, _ := s.CreateFree(act, store.FreeInput{StaffID: b.ID, Payment: "card", Lines: []store.LineInput{{Name: "X", Qty: 1, AmountMinor: 100}}})
	s.RemoveStaff(b.ID, store.RemoveInput{})
	_ = en
	_ = f
	res, err := export.Build(s, "summary", "2026-11-01", "2026-11-30", former)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(res.ContentB64)
	txt := string(raw)
	if !strings.HasPrefix(txt, "\xEF\xBB\xBF") {
		t.Fatal("UTF-8 BOM")
	}
	for _, want := range []string{"'=Ann", "Former staff #1", "GBP", "5000,50.00", "150,1.50", "\r\n"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("missing %q in:\n%s", want, txt)
		}
	}
	res, _ = export.Build(s, "entries", "2026-11-01", "2026-11-30", former)
	raw, _ = base64.StdEncoding.DecodeString(res.ContentB64)
	txt = string(raw)
	if !strings.Contains(txt, `"Wash, dry"`) || strings.Count(txt, "\r\n") != 4 { // header + 3 lines
		t.Fatalf("%q", txt)
	}
	if !strings.HasSuffix(res.Filename, ".csv") || !res.OK {
		t.Fatalf("%+v", res)
	}
}

func TestEmptyRangeIsHeaderOnlyAndBadRangeRefused(t *testing.T) {
	s, _ := setup(t)
	res, err := export.Build(s, "summary", "2026-11-01", "2026-11-30", former)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(res.ContentB64)
	if strings.Count(string(raw), "\r\n") != 1 {
		t.Fatalf("%q", raw)
	}
	if _, err := export.Build(s, "summary", "2026-11-30", "2026-11-01", former); err == nil {
		t.Fatal("reversed range")
	}
	if _, err := export.Build(s, "nope", "2026-11-01", "2026-11-30", former); err == nil {
		t.Fatal("unknown kind")
	}
}
