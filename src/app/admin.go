package app

import (
	"fmt"
	"sort"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/views"
)

// adminStep is the second screen of the remove flow.
type adminStep struct {
	staff store.Staff
	gb    bool
}

type category struct{ id, name string }

// categories lists the catalog's categories (from catalog.items.v1, paged),
// sorted by name, at most 50 (a form holds 50 fields).
func (a *App) categories() ([]category, error) {
	items, err := a.catalog()
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, it := range items {
		if it.CategoryID != "" {
			seen[it.CategoryID] = it.Category
		}
	}
	out := make([]category, 0, len(seen))
	for id, n := range seen {
		out = append(out, category{id, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].id < out[j].id
	})
	if len(out) > 40 {
		out = out[:40]
	}
	return out, nil
}

func (a *App) adminDoc(q ask, notice []any, step *adminStep) (views.Doc, error) {
	if a.S.IsReplica() { // staff and settings live on the main; never edited here
		return views.Doc{Title: views.K("service_logbook.admin.title"),
			Components: []any{views.Notice("info", "service_logbook.admin.replica_notice")}}, nil
	}
	if !q.manager() {
		return views.Doc{Title: views.K("service_logbook.admin.title"),
			Components: []any{views.Notice("error", "service_logbook.err.manager_only")}}, nil
	}
	d := views.Doc{Title: views.K("service_logbook.admin.title")}
	d.Components = append(d.Components, notice...)
	country := a.S.Country()
	forced := store.MoneyEntryForced(country)

	if step != nil {
		return a.removeStepDoc(d, q, step), nil
	}
	staff, err := a.S.ListStaff(true)
	if err != nil {
		return d, err
	}
	users, err := a.S.Users()
	if err != nil {
		return d, err
	}
	uname := map[string]string{}
	userOpts := []views.Opt{{Value: "", LabelKey: "service_logbook.admin.no_user"}}
	for _, u := range users {
		uname[u.ID] = u.Name
		if u.Active {
			userOpts = append(userOpts, views.Opt{Value: u.ID, Label: u.Name})
		}
	}
	d.Components = append(d.Components, views.Heading("service_logbook.admin.staff"))
	if len(staff) == 0 {
		d.Components = append(d.Components, views.Empty("service_logbook.admin.empty.title", "service_logbook.admin.empty.body"))
	} else {
		rows := [][]any{}
		for _, st := range staff {
			status := T(q.Locale, "service_logbook.staff.status."+st.Status)
			rows = append(rows, []any{st.Label(a.former(q.Locale)), status, uname[st.LinkedUserID], st.Colour})
		}
		d.Components = append(d.Components, views.Table([]map[string]any{
			views.ColText("service_logbook.admin.col.name"), views.ColText("service_logbook.admin.col.status"),
			views.ColText("service_logbook.admin.col.user"), views.ColText("service_logbook.admin.col.colour")}, rows))
	}
	d.Components = append(d.Components, views.Heading("service_logbook.admin.add"),
		views.Form("staff_add", "service_logbook.admin.add.submit",
			views.TextField("name", "service_logbook.admin.field.name", ""),
			views.TextField("colour", "service_logbook.admin.field.colour", ""),
			views.Select("user", "service_logbook.admin.field.user", userOpts, "")))
	var active, removable []views.Opt
	for _, st := range staff {
		if !st.Archived() {
			active = append(active, views.Opt{Value: st.ID, Label: st.Name})
			removable = append(removable, views.Opt{Value: st.ID, Label: st.Name})
		} else if country == "GB" { // GB may still erase an archived member
			removable = append(removable, views.Opt{Value: st.ID, Label: st.Label(a.former(q.Locale))})
		}
	}
	if len(removable) > 100 {
		removable = removable[:100]
	}
	if len(active) > 100 {
		active = active[:100]
	}
	if len(active) > 0 {
		d.Components = append(d.Components,
			views.Heading("service_logbook.admin.edit"),
			views.Form("staff_edit", "service_logbook.admin.edit.submit",
				views.Select("staff", "service_logbook.admin.field.staff", active, ""),
				views.TextField("name", "service_logbook.admin.field.name", ""),
				views.TextField("colour", "service_logbook.admin.field.colour", ""),
				views.Select("user", "service_logbook.admin.field.user", userOpts, "")))
	}
	if len(removable) > 0 {
		d.Components = append(d.Components,
			views.Heading("service_logbook.admin.remove"),
			views.Text("service_logbook.admin.remove.help"),
			views.Form("staff_remove", "service_logbook.admin.remove.submit",
				views.Select("staff", "service_logbook.admin.field.staff", removable, "")))
	}
	// Settings.
	d.Components = append(d.Components, views.Heading("service_logbook.admin.settings"))
	modeOpts := []views.Opt{{Value: "free", LabelKey: "service_logbook.mode.free"}, {Value: "linked", LabelKey: "service_logbook.mode.linked"}}
	if forced {
		d.Components = append(d.Components, views.Notice("info", "service_logbook.admin.mode_forced"))
	}
	fields := []map[string]any{}
	if !forced {
		fields = append(fields, views.Select("money_entry", "service_logbook.admin.field.mode", modeOpts, a.S.EffectiveMode()))
	}
	cats, _ := a.categories()
	chosen := map[string]bool{}
	for _, c := range a.S.Categories() {
		chosen[c] = true
	}
	for i, c := range cats {
		fields = append(fields, views.ToggleL(fmt.Sprintf("cat_%d", i), c.name, chosen[c.id]))
	}
	if len(cats) > 0 {
		d.Components = append(d.Components, views.Text("service_logbook.admin.categories.help"))
	}
	if len(fields) > 0 {
		d.Components = append(d.Components, views.Form("settings_save", "service_logbook.admin.settings.submit", fields...))
	}
	return d, nil
}

func (a *App) removeStepDoc(d views.Doc, q ask, st *adminStep) views.Doc {
	d.Components = append(d.Components, views.Heading("service_logbook.admin.remove"))
	label := st.staff.Label(a.former(q.Locale))
	if !st.gb {
		d.Components = append(d.Components, views.Notice("info", "service_logbook.remove.archive_notice"),
			views.Form("staff_remove_confirm", "service_logbook.remove.archive_submit",
				views.Select("staff", "service_logbook.admin.field.staff", []views.Opt{{Value: st.staff.ID, Label: label}}, st.staff.ID)))
		return d
	}
	d.Components = append(d.Components,
		views.Notice("warn", "service_logbook.remove.gb_warning"),
		views.Text("service_logbook.remove.gb_export_hint"),
		views.Form("staff_remove_confirm", "service_logbook.remove.gb_submit",
			views.Select("staff", "service_logbook.admin.field.staff", []views.Opt{{Value: st.staff.ID, Label: label}}, st.staff.ID),
			views.TextField("typed_name", "service_logbook.remove.typed_name", ""),
			views.Toggle("exported", "service_logbook.remove.exported", false)))
	return d
}

func (a *App) adminAction(q ask) map[string]any {
	ok := func(key string) []any { return []any{views.Notice("info", key)} }
	bad := func(key string) map[string]any {
		doc, err := a.adminDoc(q, []any{views.Notice("error", key)}, nil)
		if err != nil {
			return unavailable()
		}
		return views.Answer(doc)
	}
	// re-checked on every action, whatever the page showed: a replica never
	// edits staff or settings, a non-manager never does either.
	if a.S.IsReplica() || !q.manager() {
		doc, _ := a.adminDoc(q, nil, nil)
		return views.Answer(doc)
	}
	redraw := func(n []any) map[string]any {
		doc, err := a.adminDoc(q, n, nil)
		if err != nil {
			return unavailable()
		}
		return views.Answer(doc)
	}
	switch q.Action {
	case "staff_add":
		if _, err := a.S.AddStaff(store.StaffInput{Name: q.formStr("name"), Colour: q.formStr("colour"), LinkedUserID: q.formStr("user")}); err != nil {
			return bad("service_logbook.err.invalid")
		}
		return redraw(ok("service_logbook.admin.saved"))
	case "staff_edit":
		if _, err := a.S.EditStaff(q.formStr("staff"), store.StaffInput{Name: q.formStr("name"), Colour: q.formStr("colour"), LinkedUserID: q.formStr("user")}); err != nil {
			return bad("service_logbook.err.invalid")
		}
		return redraw(ok("service_logbook.admin.saved"))
	case "staff_remove":
		gb := a.S.Country() == "GB"
		st, err := a.S.GetStaff(q.formStr("staff"))
		if err != nil || (st.Archived() && !gb) {
			return bad("service_logbook.err.invalid")
		}
		doc, _ := a.adminDoc(q, nil, &adminStep{staff: st, gb: gb})
		return views.Answer(doc)
	case "staff_remove_confirm":
		res, err := a.S.RemoveStaff(q.formStr("staff"), store.RemoveInput{TypedName: q.formStr("typed_name"), Exported: q.formBool("exported"), Former: a.former(q.Locale)})
		switch {
		case err == store.ErrNotConfirmed:
			st, gerr := a.S.GetStaff(q.formStr("staff"))
			if gerr != nil {
				return bad("service_logbook.err.invalid")
			}
			doc, _ := a.adminDoc(q, []any{views.Notice("error", "service_logbook.remove.not_confirmed")}, &adminStep{staff: st, gb: true})
			return views.Answer(doc)
		case err != nil:
			return bad("service_logbook.err.invalid")
		case res == store.Deleted:
			return redraw(ok("service_logbook.remove.deleted"))
		}
		return redraw(ok("service_logbook.remove.archived"))
	case "settings_save":
		if mode := q.formStr("money_entry"); mode != "" {
			if err := a.S.SetMoneyEntry(mode); err != nil {
				return bad("service_logbook.err.mode_forced")
			}
		}
		cats, err := a.categories()
		if err != nil {
			return unavailable()
		}
		var ids []string
		for i, c := range cats {
			if q.formBool(fmt.Sprintf("cat_%d", i)) {
				ids = append(ids, c.id)
			}
		}
		if err := a.S.SetCategories(ids); err != nil {
			return unavailable()
		}
		a.S.PublishSnapshots()
		return redraw(ok("service_logbook.admin.saved"))
	}
	return bad("service_logbook.err.invalid")
}
