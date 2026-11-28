//go:build !wasip1

package app_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/app"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/hosttest"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

type rig struct {
	f *hosttest.Fake
	a *app.App
}

func newRig(t *testing.T, country string) rig {
	f := hosttest.New(t)
	f.SetView("shop.context.v2", []map[string]any{{"currency_code": "GBP", "currency_decimals": 2, "locale": "en", "country": country, "business_date": "2026-11-27"}})
	f.SetView("users.list.v1", []map[string]any{{"id": "u1", "display_name": "Ann", "active": true}})
	f.SetView("catalog.items.v1", []map[string]any{
		{"id": "i1", "name": "Cut", "category_id": "c1", "category": "Hair", "price_minor": 2500, "active": true},
		{"id": "i2", "name": "Gel nails", "category_id": "c2", "category": "Nails", "price_minor": 4000, "active": true},
		{"id": "i3", "name": "Old", "category_id": "c1", "category": "Hair", "price_minor": 100, "active": false}})
	f.SetView("sales.receipts.v1", []map[string]any{})
	return rig{f, app.New(f)}
}

var (
	mgr  = map[string]any{"user_id": "u9", "role": "manager", "permissions": []string{"reports", "sale"}}
	cash = map[string]any{"user_id": "u1", "role": "cashier", "permissions": []string{"sale"}}
)

func ask(view, action string, op map[string]any, form map[string]any) json.RawMessage {
	m := map[string]any{"view": view, "action": action, "locale": "en", "form": form, "params": map[string]string{}}
	if op != nil {
		m["operator"] = op
	}
	b, _ := json.Marshal(m)
	return b
}

func docOf(t *testing.T) func(v any, err error) map[string]any {
	return func(v any, err error) map[string]any { return docCheck(t, v, err) }
}

func docCheck(t *testing.T, v any, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	d, ok := out["document"].(map[string]any)
	if !ok {
		t.Fatalf("no document: %s", b)
	}
	checkDoc(t, d)
	return d
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

// checkDoc enforces the view-vocabulary rules the till would refuse a
// document for, and that every key resolves in locales/en.json.
func checkDoc(t *testing.T, d map[string]any) {
	t.Helper()
	raw, _ := os.ReadFile("../../locales/en.json")
	var en map[string]string
	_ = json.Unmarshal(raw, &en)
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if k, ok := x["key"].(string); ok {
				if _, has := en[k]; !has {
					t.Fatalf("key %q missing from en.json", k)
				}
			}
			if x["type"] == "table" {
				cols := x["columns"].([]any)
				for _, r := range x["rows"].([]any) {
					if len(r.([]any)) != len(cols) {
						t.Fatalf("row/column mismatch: %v", r)
					}
					for i, c := range r.([]any) {
						kind := cols[i].(map[string]any)["kind"]
						_, isObj := c.(map[string]any)
						if (kind == "money") != isObj {
							t.Fatalf("cell kind mismatch col %d: %v", i, c)
						}
						if isObj && len(c.(map[string]any)["currency"].(string)) != 3 {
							t.Fatalf("money cell needs a currency: %v", c)
						}
					}
				}
			}
			if x["type"] == "form" {
				fs := x["fields"].([]any)
				if len(fs) > 50 {
					t.Fatalf("form has %d fields", len(fs))
				}
				seen := map[string]bool{}
				for _, f := range fs {
					fm := f.(map[string]any)
					n := fm["name"].(string)
					if !nameRe.MatchString(n) || seen[n] {
						t.Fatalf("bad or duplicate field name %q", n)
					}
					seen[n] = true
					if fm["kind"] == "select" && len(fm["options"].([]any)) > 100 {
						t.Fatal("more than 100 options")
					}
					if fm["kind"] == "secret" || fm["kind"] == "file" {
						t.Fatal("no secret/file fields")
					}
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(d)
}

func (r rig) staff(t *testing.T, name string) store.Staff {
	st, err := r.a.S.AddStaff(store.StaffInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func text(d map[string]any) string { b, _ := json.Marshal(d); return string(b) }

func TestAdminOnlyForManagersOnViewAndEveryAction(t *testing.T) {
	r := newRig(t, "GB")
	d := docOf(t)(r.a.View(ask(app.ViewAdmin, "", cash, nil)))
	if !strings.Contains(text(d), "err.manager_only") || strings.Contains(text(d), `"type":"form"`) {
		t.Fatalf("cashier must get no admin forms: %s", text(d))
	}
	r.a.Action(ask(app.ViewAdmin, "staff_add", cash, map[string]any{"name": "Sneaky"}))
	if l, _ := r.a.S.ListStaff(true); len(l) != 0 {
		t.Fatal("a cashier's forged POST must not add staff")
	}
	docOf(t)(r.a.Action(ask(app.ViewAdmin, "staff_add", mgr, map[string]any{"name": "Ann", "user": "u1"})))
	if l, _ := r.a.S.ListStaff(true); len(l) != 1 {
		t.Fatal("manager adds staff")
	}
	// sign-in off: no operator, no limits
	docOf(t)(r.a.Action(ask(app.ViewAdmin, "staff_add", nil, map[string]any{"name": "Bob"})))
	if l, _ := r.a.S.ListStaff(true); len(l) != 2 {
		t.Fatal("no operator = no role limits")
	}
}

func TestOperatorNeverTakenFromForm(t *testing.T) {
	r := newRig(t, "GB")
	st := r.staff(t, "Ann")
	docOf(t)(r.a.Action(ask(app.ViewEntry, "log_save", cash, map[string]any{"staff": st.ID, "payment": "cash",
		"item_0": "i1", "qty_0": "1", "operator": "u9", "author_user_id": "u9", "user_id": "u9"})))
	day, _ := r.a.S.DayList("2026-11-27")
	if len(day) != 1 || day[0].AuthorUserID != "u1" {
		t.Fatalf("author is the stamped operator: %+v", day)
	}
}

func TestLogSaveFillsAmountFromPriceAndTipAndMinorUnits(t *testing.T) {
	r := newRig(t, "GB")
	st := r.staff(t, "Ann")
	docOf(t)(r.a.Action(ask(app.ViewEntry, "log_save", cash, map[string]any{"staff": st.ID, "payment": "card",
		"item_0": "i1", "qty_0": "2", "item_1": "i2", "amount_1": map[string]any{"minor": 3500, "currency": "GBP"},
		"tip": map[string]any{"minor": 250, "currency": "GBP"}})))
	day, _ := r.a.S.DayList("2026-11-27")
	if len(day) != 1 || day[0].TotalMinor != 5000+3500 || day[0].TipMinor != 250 || day[0].Payment != "card" {
		t.Fatalf("%+v", day)
	}
	// negative, bad qty and inactive item are refused with a notice and write nothing
	for _, f := range []map[string]any{
		{"staff": st.ID, "payment": "cash", "item_0": "i1", "qty_0": "0"},
		{"staff": st.ID, "payment": "cash", "item_0": "i3"},
		{"staff": st.ID, "payment": "cash", "item_0": "i1", "amount_0": -5},
		{"staff": st.ID, "payment": "cash"},
	} {
		d := docOf(t)(r.a.Action(ask(app.ViewEntry, "log_save", cash, f)))
		if !strings.Contains(text(d), "err.invalid") {
			t.Fatalf("%v: %s", f, text(d))
		}
	}
	if r.a.S.CountEntries() != 1 {
		t.Fatal("refusals write nothing")
	}
}

func TestCategoriesFilterItemsEmptyMeansAll(t *testing.T) {
	r := newRig(t, "GB")
	r.staff(t, "Ann")
	d := docOf(t)(r.a.View(ask(app.ViewEntry, "", cash, nil)))
	if !strings.Contains(text(d), "Gel nails") || !strings.Contains(text(d), "Cut") || strings.Contains(text(d), `"Old"`) {
		t.Fatalf("all active items by default: %s", text(d))
	}
	r.a.S.SetCategories([]string{"c1"})
	d = docOf(t)(r.a.View(ask(app.ViewEntry, "", cash, nil)))
	if strings.Contains(text(d), "Gel nails") || !strings.Contains(text(d), "Cut") {
		t.Fatalf("only the chosen category: %s", text(d))
	}
}

func TestMeIsDefaultWhenLinked(t *testing.T) {
	r := newRig(t, "GB")
	r.staff(t, "Zed")
	me, _ := r.a.S.AddStaff(store.StaffInput{Name: "Ann", LinkedUserID: "u1"})
	d := docOf(t)(r.a.View(ask(app.ViewEntry, "", cash, nil)))
	if !strings.Contains(text(d), `"value":"`+me.ID+`","`) && !strings.Contains(text(d), `"kind":"select","label":{"key":"service_logbook.log.staff"},"name":"staff"`) {
		// the select's pre-selected value is the linked staff member
		t.Log(text(d))
	}
	if !strings.Contains(text(d), `"value":"`+me.ID+`"}`) && !strings.Contains(text(d), `"value":"`+me.ID+`",`) {
		t.Fatalf("staff select defaults to the linked member: %s", text(d))
	}
}

func TestDEPopupIsLinkedAndForgedFreeSaveRefused(t *testing.T) {
	r := newRig(t, "DE")
	st := r.staff(t, "Ann")
	d := docOf(t)(r.a.View(ask(app.ViewEntry, "", mgr, nil)))
	if strings.Contains(text(d), "log_save") || !strings.Contains(text(d), "service_logbook.link.title") {
		t.Fatalf("DE shows the linked flow only: %s", text(d))
	}
	docOf(t)(r.a.Action(ask(app.ViewEntry, "log_save", mgr, map[string]any{"staff": st.ID, "payment": "cash", "item_0": "i1"})))
	if r.a.S.CountEntries() != 0 {
		t.Fatal("a forged free POST in DE must be refused server-side")
	}
	// admin cannot switch to free either
	docOf(t)(r.a.Action(ask(app.ViewAdmin, "settings_save", mgr, map[string]any{"money_entry": "free"})))
	if r.a.S.EffectiveMode() != "linked" {
		t.Fatal("DE stays linked")
	}
}

func TestLinkedFlowIgnoresPostedMoney(t *testing.T) {
	r := newRig(t, "DE")
	st := r.staff(t, "Ann")
	r.f.SetView("sales.receipts.v1", []map[string]any{{"id": "s1", "receipt_no": "R1", "sale_type": "sale", "business_date": "2026-11-27",
		"currency": "EUR", "total_minor": 3000, "tip_minor": 100, "fiscal_signed": true,
		"lines":    []map[string]any{{"line_no": 1, "name": "Cut", "quantity": 1, "total_minor": 3000}},
		"payments": []map[string]any{{"method_id": "cash", "amount_minor": 3000}}}})
	d := docOf(t)(r.a.Action(ask(app.ViewEntry, "link_pick", mgr, map[string]any{"sale": "s1", "days_ago": "0"})))
	if !strings.Contains(text(d), "link_save") || strings.Contains(text(d), `"name":"amount`) {
		t.Fatalf("the assign form has no money fields: %s", text(d))
	}
	docOf(t)(r.a.Action(ask(app.ViewEntry, "link_save", mgr, map[string]any{"sale": "s1", "days_ago": "0", "line_1": st.ID,
		"total_minor": 99999, "amount_0": map[string]any{"minor": 1, "currency": "EUR"}})))
	day, _ := r.a.S.DayList("2026-11-27")
	if len(day) != 1 || day[0].TotalMinor != 3000 || day[0].TipMinor != 100 || day[0].SaleRef != "s1" {
		t.Fatalf("amount comes from the sale: %+v", day)
	}
}

func TestGBRemoveFlowTwoStepAndArchiveElsewhere(t *testing.T) {
	r := newRig(t, "GB")
	st := r.staff(t, "Ann")
	d := docOf(t)(r.a.Action(ask(app.ViewAdmin, "staff_remove", mgr, map[string]any{"staff": st.ID})))
	if !strings.Contains(text(d), "remove.gb_warning") || !strings.Contains(text(d), "typed_name") {
		t.Fatalf("%s", text(d))
	}
	d = docOf(t)(r.a.Action(ask(app.ViewAdmin, "staff_remove_confirm", mgr, map[string]any{"staff": st.ID, "typed_name": "ann", "exported": true})))
	if !strings.Contains(text(d), "remove.not_confirmed") {
		t.Fatal("wrong typed name keeps the confirmation screen")
	}
	if _, err := r.a.S.GetStaff(st.ID); err != nil {
		t.Fatal("nothing deleted yet")
	}
	docOf(t)(r.a.Action(ask(app.ViewAdmin, "staff_remove_confirm", mgr, map[string]any{"staff": st.ID, "typed_name": "Ann", "exported": true})))
	if _, err := r.a.S.GetStaff(st.ID); err == nil {
		t.Fatal("deleted")
	}
	fr := newRig(t, "FR")
	st2 := fr.staff(t, "Bob")
	d = docOf(t)(fr.a.Action(ask(app.ViewAdmin, "staff_remove", mgr, map[string]any{"staff": st2.ID})))
	if !strings.Contains(text(d), "archive_notice") || strings.Contains(text(d), "typed_name") {
		t.Fatalf("%s", text(d))
	}
	docOf(t)(fr.a.Action(ask(app.ViewAdmin, "staff_remove_confirm", mgr, map[string]any{"staff": st2.ID})))
	g, _ := fr.a.S.GetStaff(st2.ID)
	if g.Status != "archived" {
		t.Fatal("archived")
	}
	d = docOf(t)(fr.a.View(ask(app.ViewAdmin, "", mgr, nil)))
	if !strings.Contains(text(d), "Former staff #1") || strings.Contains(text(d), "Bob") {
		t.Fatalf("the pseudonym replaces the name: %s", text(d))
	}
}

func TestReportsManagersOnlyAndContent(t *testing.T) {
	r := newRig(t, "GB")
	st := r.staff(t, "Ann")
	r.a.S.CreateFree(store.Actor{UserID: "u1"}, store.FreeInput{StaffID: st.ID, Payment: "cash", TipMinor: 100,
		Lines: []store.LineInput{{Name: "Cut", Qty: 1, AmountMinor: 2500}}})
	d := docOf(t)(r.a.View(ask(app.ViewReports, "", cash, nil)))
	if !strings.Contains(text(d), "manager_only") || strings.Contains(text(d), "Ann") {
		t.Fatalf("a cashier sees no totals: %s", text(d))
	}
	d = docOf(t)(r.a.View(ask(app.ViewReports, "", mgr, nil)))
	for _, want := range []string{"Ann", `"minor":2500`, "compare.title", "recon.title", "csv_hint"} {
		if !strings.Contains(text(d), want) {
			t.Fatalf("missing %s in %s", want, text(d))
		}
	}
	d = docOf(t)(r.a.Action(ask(app.ViewReports, "filter", mgr, map[string]any{"range": "custom", "from": "2020-01-01", "to": "2026-11-27"})))
	if !strings.Contains(text(d), "bad_range") {
		t.Fatal("a custom range over 366 days says so")
	}
}

func TestRecordsApplyUsesCoreTillIDAndIsIdempotent(t *testing.T) {
	r := newRig(t, "GB")
	st := r.staff(t, "Ann")
	w := map[string]any{"entry": map[string]any{"id": "e1", "author_user_id": "u1", "staff_id": st.ID, "business_date": "2026-11-27", "currency": "GBP",
		"payment_type": "cash", "total_minor": 100, "mode": "free", "created_at": "2026-11-27T09:00:00Z", "origin_till_id": "forged"},
		"lines": []map[string]any{{"line_no": 1, "name": "Cut", "qty": 1, "amount_minor": 100}}}
	ev, _ := json.Marshal(map[string]any{"collection": "entries", "id": "e1", "from_till_id": "tillB", "payload": w})
	for i := 0; i < 2; i++ {
		out, err := r.a.RecordsApply(ev)
		b, _ := json.Marshal(out)
		if err != nil || string(b) != `{"ok":true}` {
			t.Fatalf("%s %v", b, err)
		}
	}
	e, _ := r.a.S.GetEntry("e1")
	if e.OriginTill != "tillB" || r.a.S.CountEntries() != 1 {
		t.Fatalf("%+v", e)
	}
	bad, _ := json.Marshal(map[string]any{"collection": "staff", "from_till_id": "tillB", "payload": w})
	out, _ := r.a.RecordsApply(bad)
	if b, _ := json.Marshal(out); !strings.Contains(string(b), `"retry":false`) {
		t.Fatalf("%s", b)
	}
	r.f.Role = "replica"
	out, _ = r.a.RecordsApply(ev)
	if b, _ := json.Marshal(out); !strings.Contains(string(b), "not_main") {
		t.Fatalf("a replica never applies: %s", b)
	}
}

func TestReplicaWithoutSnapshotOffersNothing(t *testing.T) {
	r := newRig(t, "GB")
	r.f.Role = "replica"
	d := docOf(t)(r.a.View(ask(app.ViewEntry, "", cash, nil)))
	if !strings.Contains(text(d), "not_synced") || strings.Contains(text(d), `"type":"form"`) {
		t.Fatalf("%s", text(d))
	}
}

func TestExportAndTipsHandlers(t *testing.T) {
	r := newRig(t, "GB")
	st, _ := r.a.S.AddStaff(store.StaffInput{Name: "Ann", LinkedUserID: "u1"})
	r.a.S.CreateFree(store.Actor{UserID: "u1"}, store.FreeInput{StaffID: st.ID, Payment: "cash", TipMinor: 300, Lines: []store.LineInput{{Name: "Cut", Qty: 1, AmountMinor: 2500}}})
	out, _ := r.a.Export(json.RawMessage(`{"from":"2026-11-01","to":"2026-11-30","entry_key":"logbook-summary-csv","locale":"en"}`))
	if b, _ := json.Marshal(out); !strings.Contains(string(b), `"ok":true`) || !strings.Contains(string(b), "content_b64") {
		t.Fatalf("%s", b)
	}
	out, _ = r.a.Export(json.RawMessage(`{"from":"2026-11-01","to":"2026-11-30","entry_key":"bogus"}`))
	if b, _ := json.Marshal(out); !strings.Contains(string(b), `"ok":false`) {
		t.Fatalf("%s", b)
	}
	out, err := r.a.TipsSuggest(json.RawMessage(`{"from":"2026-11-01","to":"2026-11-30","locale":"en"}`))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(out); !strings.Contains(string(b), `"user_id":"u1","amount_minor":300,"currency":"GBP"`) {
		t.Fatalf("%s", b)
	}
	out, _ = r.a.TipsSuggest(json.RawMessage(`{"from":"bad","to":"2026-11-30"}`))
	if b, _ := json.Marshal(out); !strings.Contains(string(b), `"suggestions":[]`) {
		t.Fatalf("a bad range answers nothing: %s", b)
	}
}

func TestMaintenanceForwardsPendingAndSweeps(t *testing.T) {
	r, sts := replicaRig(t, "GB", nil, "Ann")
	st := sts[0]
	e, _ := r.a.S.CreateFree(store.Actor{UserID: "u1"}, store.FreeInput{StaffID: st.ID, Payment: "cash", TipMinor: 1})
	r.f.DB().Exec("UPDATE entries SET fwd_state='pending' WHERE id=?", e.ID)
	r.f.DB().Exec(`INSERT INTO entry_lines (entry_id, line_no, name, qty, amount_minor) VALUES ('ghost', 1, 'x', 1, 1)`)
	r.f.Out = nil
	if _, err := r.a.Maintenance(); err != nil {
		t.Fatal(err)
	}
	if len(r.f.Out) != 1 || r.a.S.CountLines() != 0 {
		t.Fatalf("out %d lines %d", len(r.f.Out), r.a.S.CountLines())
	}
}

func TestEveryStaticKeyHasAnEnglishString(t *testing.T) {
	// Dynamic keys built in code (suffix concatenation).
	raw, _ := os.ReadFile("../../locales/en.json")
	var en map[string]string
	_ = json.Unmarshal(raw, &en)
	for _, k := range []string{"service_logbook.staff.status.active", "service_logbook.staff.status.archived",
		"service_logbook.link.day.0", "service_logbook.link.day.7", "service_logbook.staff.former"} {
		if en[k] == "" {
			t.Fatalf("%s", k)
		}
	}
	if app.T("de-DE", "service_logbook.staff.former", 3) != "Former staff #3" {
		t.Fatal("falls back to English until the translation step adds a locale")
	}
	if app.T("en", "no.such.key") != "no.such.key" {
		t.Fatal("unknown key returns the key")
	}
}
