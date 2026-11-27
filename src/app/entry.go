package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/export"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/views"
)

// formRows is the number of service lines the free form offers: three keeps
// the phone screen short; most services are one line (<= 3 taps: staff is
// preselected for the linked user, pick the item, save).
const formRows = 3

// maxItemOptions: a select holds at most 100 options.
const maxItemOptions = 100

type item struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CategoryID string `json:"category_id"`
	Category   string `json:"category"`
	PriceMinor int64  `json:"price_minor"`
	Weighed    bool   `json:"weighed"`
	Active     bool   `json:"active"`
}

// catalog pages catalog.items.v1 (500 per page, local DB: never the network).
func (a *App) catalog() ([]item, error) {
	var all []item
	for off := 0; off < 20000; off += 500 {
		raw, err := a.H.Views().Query("catalog.items.v1", map[string]int{"offset": off, "limit": 500})
		if err != nil {
			return nil, err
		}
		var page []item
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < 500 {
			break
		}
	}
	return all, nil
}

// offered are the active, non-weighed items in the chosen categories (none
// chosen = all). Weighed items are out of scope (no fractional quantity).
func (a *App) offered() (list []item, truncated bool, err error) {
	items, err := a.catalog()
	if err != nil {
		return nil, false, err
	}
	cats := a.S.EffectiveCategories()
	chosen := map[string]bool{}
	for _, c := range cats {
		chosen[c] = true
	}
	for _, it := range items {
		if !it.Active || it.Weighed || (len(chosen) > 0 && !chosen[it.CategoryID]) {
			continue
		}
		list = append(list, it)
	}
	if len(list) > maxItemOptions {
		return list[:maxItemOptions], true, nil
	}
	return list, false, nil
}

func (a *App) staffOpts(staff []store.Staff) []views.Opt {
	o := make([]views.Opt, 0, len(staff))
	for _, s := range staff {
		o = append(o, views.Opt{Value: s.ID, Label: s.Name})
	}
	return o
}

// meStaff is the picker default: the staff member linked to the operator.
func meStaff(staff []store.Staff, q ask) string {
	if q.Operator == nil {
		return ""
	}
	for _, s := range staff {
		if s.LinkedUserID != "" && s.LinkedUserID == q.Operator.UserID {
			return s.ID
		}
	}
	return ""
}

type editPrefill struct {
	entry store.Entry
	lines []store.Line
}

func (a *App) entryDoc(q ask, notice []any, edit *editPrefill) (views.Doc, error) {
	d := views.Doc{Title: views.K("service_logbook.log.title")}
	d.Components = append(d.Components, notice...)
	staff, synced, err := a.S.Picker()
	if err != nil {
		return d, err
	}
	if !synced {
		d.Components = append(d.Components, views.Notice("warn", "service_logbook.log.not_synced"))
		return d, nil
	}
	if len(staff) == 0 {
		d.Components = append(d.Components, views.Empty("service_logbook.log.nostaff.title", "service_logbook.log.nostaff.body"))
		return d, nil
	}
	if a.S.EffectiveMode() == "linked" { // the same rule the store enforces
		return a.linkedDoc(d, q, staff)
	}
	return a.freeDoc(d, q, staff, edit)
}

func (a *App) freeDoc(d views.Doc, q ask, staff []store.Staff, edit *editPrefill) (views.Doc, error) {
	items, truncated, err := a.offered()
	if err != nil {
		return d, err
	}
	itemOpts := []views.Opt{{Value: "", LabelKey: "service_logbook.log.item_none"}}
	for _, it := range items {
		itemOpts = append(itemOpts, views.Opt{Value: it.ID, Label: it.Name})
	}
	if truncated {
		d.Components = append(d.Components, views.Notice("info", "service_logbook.log.items_truncated"))
	}
	sel := meStaff(staff, q)
	action, submit := "log_save", "service_logbook.log.save"
	pre := map[int]store.Line{}
	var tip int64
	payment := "cash"
	if edit != nil {
		action, submit = "edit_save", "service_logbook.log.edit_save"
		sel, tip, payment = edit.entry.StaffID, edit.entry.TipMinor, edit.entry.Payment
		for i, l := range edit.lines {
			if i < formRows {
				pre[i] = l
			}
		}
		d.Components = append(d.Components, views.Heading("service_logbook.log.edit"))
	} else {
		d.Components = append(d.Components, views.Heading("service_logbook.log.new"))
	}
	fields := []map[string]any{}
	if edit != nil {
		fields = append(fields, views.Select("entry", "service_logbook.log.entry", []views.Opt{{Value: edit.entry.ID, Label: edit.entry.ID[:8]}}, edit.entry.ID))
	}
	fields = append(fields, views.Select("staff", "service_logbook.log.staff", a.staffOpts(staff), sel))
	for i := 0; i < formRows; i++ {
		l := pre[i]
		fields = append(fields,
			views.Select(fmt.Sprintf("item_%d", i), "service_logbook.log.item", itemOpts, l.ItemID),
			views.Number(fmt.Sprintf("qty_%d", i), "service_logbook.log.qty", qtyStr(l.Qty)),
			views.Money(fmt.Sprintf("amount_%d", i), "service_logbook.log.amount", l.AmountMinor, l.Name != ""),
			views.TextField(fmt.Sprintf("desc_%d", i), "service_logbook.log.description", l.Description))
	}
	fields = append(fields,
		views.Money("tip", "service_logbook.log.tip", tip, tip > 0),
		views.Select("payment", "service_logbook.log.payment", []views.Opt{
			{Value: "cash", LabelKey: "service_logbook.pay.cash"}, {Value: "card", LabelKey: "service_logbook.pay.card"}}, payment))
	d.Components = append(d.Components, views.Text("service_logbook.log.amount_help"), views.Form(action, submit, fields...))
	return a.dayList(d, q, staff)
}

func qtyStr(q int) string {
	if q < 1 {
		return ""
	}
	return strconv.Itoa(q)
}

// dayList draws today's current entries and the change forms.
func (a *App) dayList(d views.Doc, q ask, staff []store.Staff) (views.Doc, error) {
	shop, err := a.S.Shop()
	if err != nil {
		return d, err
	}
	entries, err := a.S.DayList(shop.BusinessDate)
	if err != nil {
		return d, err
	}
	// Names: the snapshot on a replica (its own staff table is empty), every
	// member on the main. A member who left the snapshot is "Former staff".
	name := map[string]string{}
	all, _ := a.S.StaffForLabels()
	for _, s := range all {
		name[s.ID] = s.Label(a.former(q.Locale))
	}
	unknown := T(q.Locale, "service_logbook.staff.unknown")
	label := func(id string) string {
		if n, ok := name[id]; ok {
			return n
		}
		return unknown
	}
	d.Components = append(d.Components, views.Heading("service_logbook.day.title"))
	if len(entries) == 0 {
		d.Components = append(d.Components, views.Empty("service_logbook.day.empty.title", "service_logbook.day.empty.body"))
		return d, nil
	}
	act := q.actor()
	rows := [][]any{}
	var changeable []views.Opt
	chips := a.fwdChips(q.Locale)
	for _, e := range entries {
		hh := e.CreatedAt
		if len(hh) >= 16 {
			hh = hh[11:16]
		}
		rows = append(rows, []any{hh, label(e.StaffID), views.Cash(e.TotalMinor, e.Currency), views.Cash(e.TipMinor, e.Currency), chips(e)})
		if (act.Manager || act.NoSignIn || (act.UserID != "" && act.UserID == e.AuthorUserID)) && (e.OriginTill == store.LocalTill || a.isMain()) {
			changeable = append(changeable, views.Opt{Value: e.ID, Label: fmt.Sprintf("%s %s %s %s", hh, label(e.StaffID), export.Decimal(e.TotalMinor, shopDecimals(shop)), e.Currency)})
		}
	}
	d.Components = append(d.Components, views.Table([]map[string]any{views.ColText("service_logbook.day.col.time"),
		views.ColText("service_logbook.day.col.staff"), views.ColMoney("service_logbook.day.col.total"),
		views.ColMoney("service_logbook.day.col.tip"), views.ColText("service_logbook.day.col.state")}, rows))
	if len(changeable) > 0 {
		if len(changeable) > 100 {
			changeable = changeable[:100]
		}
		d.Components = append(d.Components, views.Heading("service_logbook.day.change"),
			views.Form("edit_start", "service_logbook.day.edit", views.Select("entry", "service_logbook.log.entry", changeable, "")),
			views.Form("void", "service_logbook.day.void", views.Select("entry", "service_logbook.log.entry", changeable, "")))
	}
	return d, nil
}

func shopDecimals(s store.ShopCtx) int { return s.Decimals }

func (a *App) isMain() bool { return !a.S.IsReplica() }

// fwdChips: the state chip of a row. Never colour alone: always text.
func (a *App) fwdChips(locale string) func(store.Entry) string {
	rejected := map[string]string{}
	if sts, err := a.H.Records().Status("entries"); err == nil {
		for _, s := range sts {
			rejected[s.ID] = s.Status
		}
	}
	return func(e store.Entry) string {
		switch {
		case e.OriginTill != store.LocalTill:
			return T(locale, "service_logbook.state.received")
		case rejected[e.ID] == "rejected":
			return T(locale, "service_logbook.state.rejected")
		case e.FwdState == "pending" || rejected[e.ID] == "pending":
			return T(locale, "service_logbook.state.pending")
		}
		return T(locale, "service_logbook.state.ok")
	}
}

// parseFree reads the free form. Amounts are minor units from the money
// field; an empty amount is filled from the catalog price x qty here.
func (a *App) parseFree(q ask) (store.FreeInput, error) {
	in := store.FreeInput{StaffID: q.formStr("staff"), Payment: q.formStr("payment")}
	if t, ok := q.formMoney("tip"); ok {
		in.TipMinor = t
	}
	items, err := a.catalog()
	if err != nil {
		return in, err
	}
	byID := map[string]item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for i := 0; i < formRows; i++ {
		id := q.formStr(fmt.Sprintf("item_%d", i))
		desc := q.formStr(fmt.Sprintf("desc_%d", i))
		amt, hasAmt := q.formMoney(fmt.Sprintf("amount_%d", i))
		if id == "" && !hasAmt && desc == "" {
			continue // an untouched row
		}
		qty := 1
		if s := strings.TrimSpace(q.formStr(fmt.Sprintf("qty_%d", i))); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				return in, store.ErrInvalid
			}
			qty = n
		}
		l := store.LineInput{ItemID: id, Qty: qty, Description: desc, AmountMinor: amt}
		if id != "" {
			it, ok := byID[id]
			if !ok || !it.Active {
				return in, store.ErrInvalid
			}
			l.Name = it.Name
			if !hasAmt {
				l.AmountMinor = it.PriceMinor * int64(qty)
			}
		} else {
			l.Name = desc
			if l.Name == "" {
				l.Name = T(q.Locale, "service_logbook.log.other_service")
			}
		}
		in.Lines = append(in.Lines, l)
	}
	return in, nil
}

func (a *App) entryAction(q ask) map[string]any {
	act := q.actor()
	redraw := func(level, key string) map[string]any {
		doc, err := a.entryDoc(q, []any{views.Notice(level, key)}, nil)
		if err != nil {
			return unavailable()
		}
		return views.Answer(doc)
	}
	fail := func(err error) map[string]any {
		switch err {
		case store.ErrForbidden:
			return redraw("error", "service_logbook.err.forbidden")
		case store.ErrConflict:
			return redraw("error", "service_logbook.err.conflict")
		case store.ErrInvalid:
			return redraw("error", "service_logbook.err.invalid")
		}
		return unavailable()
	}
	switch q.Action {
	case "log_save":
		in, err := a.parseFree(q)
		if err != nil {
			return fail(err)
		}
		if _, err := a.S.CreateFree(act, in); err != nil {
			return fail(err)
		}
		return redraw("info", "service_logbook.log.saved")
	case "edit_start": // the form opens only for who may save it
		e, err := a.S.CanChange(act, q.formStr("entry"))
		if errors.Is(err, store.ErrNotFound) {
			return fail(store.ErrInvalid)
		}
		if err != nil {
			return fail(err)
		}
		lines, _ := a.S.Lines(e.ID)
		doc, err := a.entryDoc(q, nil, &editPrefill{e, lines})
		if err != nil {
			return unavailable()
		}
		return views.Answer(doc)
	case "edit_save":
		in, err := a.parseFree(q)
		if err != nil {
			return fail(err)
		}
		if _, err := a.S.Edit(act, q.formStr("entry"), in); err != nil {
			return fail(err)
		}
		return redraw("info", "service_logbook.log.saved")
	case "void":
		if _, err := a.S.Void(act, q.formStr("entry")); err != nil {
			return fail(err)
		}
		return redraw("info", "service_logbook.log.voided")
	case "link_pick":
		return a.linkPick(q)
	case "link_save":
		return a.linkSave(q, fail, redraw)
	}
	return fail(store.ErrInvalid)
}
