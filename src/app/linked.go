package app

import (
	"fmt"
	"strconv"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/export"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/views"
)

func daysAgo(q ask) int {
	n, err := strconv.Atoi(q.Params["days_ago"])
	if err != nil {
		n, err = strconv.Atoi(q.formStr("days_ago"))
	}
	if err != nil || n < 0 || n > 7 {
		return 0
	}
	return n
}

func (a *App) linkedDoc(d views.Doc, q ask, staff []store.Staff) (views.Doc, error) {
	d.Components = append(d.Components, views.Heading("service_logbook.link.title"), views.Text("service_logbook.link.help"))
	if !a.isMain() {
		d.Components = append(d.Components, views.Notice("info", "service_logbook.link.replica_help"))
	}
	day := daysAgo(q)
	rcs, err := a.S.Receipts(day)
	if err != nil {
		return d, err
	}
	shop, _ := a.S.Shop()
	dayOpts := []views.Opt{}
	for i := 0; i <= 7; i++ {
		key := "service_logbook.link.day." + strconv.Itoa(i)
		dayOpts = append(dayOpts, views.Opt{Value: strconv.Itoa(i), LabelKey: key})
	}
	if len(rcs) == 0 {
		d.Components = append(d.Components, views.Empty("service_logbook.link.empty.title", "service_logbook.link.empty.body"))
	} else {
		if len(rcs) > 100 {
			rcs = rcs[:100]
		}
		opts := []views.Opt{}
		for _, r := range rcs {
			opts = append(opts, views.Opt{Value: r.ID, Label: fmt.Sprintf("%s  %s %s", r.ReceiptNo, export.Decimal(r.TotalMinor, shop.Decimals), r.Currency)})
		}
		d.Components = append(d.Components, views.Form("link_pick", "service_logbook.link.pick",
			views.Select("days_ago", "service_logbook.link.day", dayOpts, strconv.Itoa(day)),
			views.Select("sale", "service_logbook.link.sale", opts, "")))
	}
	return a.dayList(d, q, staff)
}

func (a *App) linkPick(q ask) map[string]any {
	day := daysAgo(q)
	rcs, err := a.S.Receipts(day)
	if err != nil {
		return unavailable()
	}
	var rc *store.Receipt
	for i := range rcs {
		if rcs[i].ID == q.formStr("sale") {
			rc = &rcs[i]
		}
	}
	d := views.Doc{Title: views.K("service_logbook.log.title")}
	if rc == nil {
		doc, err := a.entryDoc(q, []any{views.Notice("error", "service_logbook.err.invalid")}, nil)
		if err != nil {
			return unavailable()
		}
		return views.Answer(doc)
	}
	staff, _, err := a.S.Picker()
	if err != nil || len(staff) == 0 {
		return unavailable()
	}
	shop, _ := a.S.Shop()
	opts := a.staffOpts(staff)
	sel := meStaff(staff, q)
	fields := []map[string]any{
		views.Select("sale", "service_logbook.link.sale", []views.Opt{{Value: rc.ID, Label: rc.ReceiptNo}}, rc.ID),
		views.Select("days_ago", "service_logbook.link.day", []views.Opt{{Value: strconv.Itoa(day), LabelKey: "service_logbook.link.day." + strconv.Itoa(day)}}, strconv.Itoa(day)),
	}
	rows := [][]any{}
	for _, l := range rc.Lines {
		rows = append(rows, []any{l.Name, l.Quantity, views.Cash(l.TotalMinor, rc.Currency)})
	}
	n := 0
	for _, l := range rc.Lines {
		if n >= 40 {
			break
		}
		fields = append(fields, views.Select(fmt.Sprintf("line_%d", l.LineNo), "service_logbook.link.line_staff", opts, sel))
		n++
	}
	if rc.TipMinor > 0 {
		fields = append(fields, views.Select("tip_to", "service_logbook.link.tip_to", opts, sel))
	}
	_ = shop
	d.Components = []any{views.Heading("service_logbook.link.title"),
		views.Table([]map[string]any{views.ColText("service_logbook.link.col.line"), views.ColNumber("service_logbook.link.col.qty"), views.ColMoney("service_logbook.link.col.total")}, rows),
		views.Text("service_logbook.link.assign_help"),
		views.Form("link_save", "service_logbook.link.save", fields...)}
	return views.Answer(d)
}

func (a *App) linkSave(q ask, fail func(error) map[string]any, redraw func(level, key string) map[string]any) map[string]any {
	assign := map[int]string{}
	for k := range q.Form {
		var no int
		if _, err := fmt.Sscanf(k, "line_%d", &no); err == nil && fmt.Sprintf("line_%d", no) == k {
			assign[no] = q.formStr(k)
		}
	}
	// The form carries no money fields; a posted one is simply never read.
	_, err := a.S.CreateLinked(q.actor(), store.LinkInput{SaleID: q.formStr("sale"), DaysAgo: daysAgo(q), Assign: assign, TipTo: q.formStr("tip_to")})
	if err != nil {
		return fail(err)
	}
	return redraw("info", "service_logbook.link.saved")
}
