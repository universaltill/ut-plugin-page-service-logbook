package app

import (
	"sort"
	"time"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/views"
)

func (q ask) rangeInput(k string) string {
	if v := q.formStr(k); v != "" {
		return v
	}
	return q.Params[k]
}

func (a *App) reportsDoc(q ask, notice []any) (views.Doc, error) {
	d := views.Doc{Title: views.K("service_logbook.reports.title")}
	d.Components = append(d.Components, notice...)
	if !q.manager() { // core gates reports.panels too; re-checked here
		d.Components = append(d.Components, views.Notice("error", "service_logbook.err.manager_only"))
		return d, nil
	}
	shop, err := a.S.Shop()
	if err != nil {
		return d, err
	}
	today, err := time.Parse("2006-01-02", shop.BusinessDate)
	if err != nil {
		return d, err
	}
	kind := q.rangeInput("range")
	if kind == "" {
		kind = "week"
	}
	from, to, rerr := store.RangeFor(kind, today, q.rangeInput("from"), q.rangeInput("to"))
	if rerr != nil {
		d.Components = append(d.Components, views.Notice("error", "service_logbook.reports.bad_range"))
		kind = "week"
		from, to, _ = store.RangeFor(kind, today, "", "")
	}
	rangeOpts := []views.Opt{{Value: "day", LabelKey: "service_logbook.reports.range.day"}, {Value: "week", LabelKey: "service_logbook.reports.range.week"},
		{Value: "month", LabelKey: "service_logbook.reports.range.month"}, {Value: "custom", LabelKey: "service_logbook.reports.range.custom"}}
	d.Components = append(d.Components, views.Form("filter", "service_logbook.reports.apply",
		views.Select("range", "service_logbook.reports.range", rangeOpts, kind),
		views.TextField("from", "service_logbook.reports.from", from),
		views.TextField("to", "service_logbook.reports.to", to)))
	agg, err := a.S.Aggregate(from, to)
	if err != nil {
		return d, err
	}
	all, _ := a.S.ListStaff(true)
	name := map[string]string{}
	for _, s := range all {
		name[s.ID] = s.Label(a.former(q.Locale))
	}
	if len(agg) == 0 {
		d.Components = append(d.Components, views.Empty("service_logbook.reports.empty.title", "service_logbook.reports.empty.body"))
	} else {
		type tot struct{ services, revenue, tips, cash, card int64 }
		totals := map[string]*tot{}
		var curs []string
		rows := [][]any{}
		for _, r := range agg {
			t := totals[r.Currency]
			if t == nil {
				t = &tot{}
				totals[r.Currency] = t
				curs = append(curs, r.Currency)
			}
			t.services += r.Services
			t.revenue += r.Revenue
			t.tips += r.Tips
			t.cash += r.Cash
			t.card += r.Card
			rows = append(rows, []any{name[r.StaffID], r.Currency, r.Services, views.Cash(r.Revenue, r.Currency),
				views.Cash(r.Tips, r.Currency), views.Cash(r.Cash, r.Currency), views.Cash(r.Card, r.Currency)})
		}
		sort.Strings(curs)
		var tiles []any
		for _, c := range curs {
			t := totals[c]
			tiles = append(tiles,
				map[string]any{"label": views.L(T(q.Locale, "service_logbook.reports.tile.revenue") + " " + c), "value": views.Cash(t.revenue, c)},
				map[string]any{"label": views.L(T(q.Locale, "service_logbook.reports.tile.tips") + " " + c), "value": views.Cash(t.tips, c)},
				map[string]any{"label": views.L(T(q.Locale, "service_logbook.reports.tile.services") + " " + c), "value": t.services})
		}
		if len(tiles) > 24 {
			tiles = tiles[:24]
		}
		d.Components = append(d.Components, map[string]any{"type": "stat_tiles", "tiles": tiles},
			views.Table([]map[string]any{views.ColText("service_logbook.reports.col.staff"), views.ColText("service_logbook.reports.col.currency"),
				views.ColNumber("service_logbook.reports.col.services"), views.ColMoney("service_logbook.reports.col.revenue"),
				views.ColMoney("service_logbook.reports.col.tips"), views.ColMoney("service_logbook.reports.col.cash"),
				views.ColMoney("service_logbook.reports.col.card")}, capRows(rows)))
	}
	d.Components = append(d.Components, views.Text("service_logbook.reports.csv_hint"))
	cmp, err := a.S.Compare(shop.BusinessDate)
	if err == nil {
		d.Components = append(d.Components, views.Heading("service_logbook.compare.title"),
			a.deltaTable(q, "service_logbook.compare.month", cmp.Month, name), a.deltaTable(q, "service_logbook.compare.year", cmp.Year, name))
	}
	d.Components = append(d.Components, views.Heading("service_logbook.recon.title"))
	if a.S.MoneyEntry() == "linked" {
		d.Components = append(d.Components, views.Notice("info", "service_logbook.recon.by_construction"))
		return d, nil
	}
	rec, err := a.S.Reconcile(31)
	if err != nil {
		d.Components = append(d.Components, views.Notice("warn", "service_logbook.recon.unavailable"))
		return d, nil
	}
	rows := [][]any{}
	for _, r := range rec {
		rows = append(rows, []any{r.Date, r.Currency, views.Cash(r.LogCash, r.Currency), views.Cash(r.TillCash, r.Currency), views.Cash(r.CashDiff(), r.Currency),
			views.Cash(r.LogCard, r.Currency), views.Cash(r.TillCard, r.Currency), views.Cash(r.CardDiff(), r.Currency)})
	}
	d.Components = append(d.Components, views.Text("service_logbook.recon.help"),
		views.Table([]map[string]any{views.ColText("service_logbook.recon.col.date"), views.ColText("service_logbook.reports.col.currency"),
			views.ColMoney("service_logbook.recon.col.log_cash"), views.ColMoney("service_logbook.recon.col.till_cash"), views.ColMoney("service_logbook.recon.col.diff_cash"),
			views.ColMoney("service_logbook.recon.col.log_card"), views.ColMoney("service_logbook.recon.col.till_card"), views.ColMoney("service_logbook.recon.col.diff_card")}, capRows(rows)))
	return d, nil
}

func capRows(r [][]any) [][]any {
	if len(r) > 500 {
		return r[:500]
	}
	return r
}

func (a *App) deltaTable(q ask, titleKey string, rows []store.DeltaRow, name map[string]string) map[string]any {
	out := [][]any{}
	for _, r := range rows {
		who := name[r.StaffID]
		if r.StaffID == "" {
			who = T(q.Locale, "service_logbook.compare.shop_total")
		}
		pct := T(q.Locale, "service_logbook.compare.na")
		if r.PctOK {
			pct = store.FormatBP(r.PctBP)
		}
		out = append(out, []any{who, r.Currency, views.Cash(r.Cur, r.Currency), views.Cash(r.Prev, r.Currency), views.Cash(r.Abs, r.Currency), pct})
	}
	return views.Table([]map[string]any{views.ColText(titleKey), views.ColText("service_logbook.reports.col.currency"),
		views.ColMoney("service_logbook.compare.col.cur"), views.ColMoney("service_logbook.compare.col.prev"),
		views.ColMoney("service_logbook.compare.col.abs"), views.ColText("service_logbook.compare.col.pct")}, capRows(out))
}
