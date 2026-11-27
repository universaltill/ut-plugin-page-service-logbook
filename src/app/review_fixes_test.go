//go:build !wasip1

package app_test

import (
	"strings"
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/app"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

// replicaRig: a replica whose staff and config come only from a main's
// snapshots (its own staff table stays empty, as on a real joined till).
func replicaRig(t *testing.T, country string, prep func(m rig), names ...string) (rig, []store.Staff) {
	t.Helper()
	m := newRig(t, country)
	var sts []store.Staff
	for _, n := range names {
		sts = append(sts, m.staff(t, n))
	}
	if prep != nil {
		prep(m)
	}
	m.a.S.PublishSnapshots()
	r := newRig(t, country)
	r.f.Role = "replica"
	for k, v := range m.f.Snaps {
		r.f.Snaps[k] = v
	}
	return r, sts
}

// B1: a replica logs for snapshot staff and names them in the day list.
func TestReplicaLogsAndNamesSnapshotStaff(t *testing.T) {
	r, sts := replicaRig(t, "GB", nil, "Ann")
	d := docOf(t)(r.a.Action(ask(app.ViewEntry, "log_save", cash, map[string]any{"staff": sts[0].ID, "payment": "cash", "item_0": "i1"})))
	if !strings.Contains(text(d), "log.saved") {
		t.Fatalf("%s", text(d))
	}
	if r.a.S.CountEntries() != 1 {
		t.Fatal("stored")
	}
	if !strings.Contains(text(d), `"Ann"`) {
		t.Fatalf("day list names the snapshot member: %s", text(d))
	}
}

// H1: the popup follows the config snapshot on a replica (the store too).
func TestReplicaPopupFollowsSnapshotMode(t *testing.T) {
	r, sts := replicaRig(t, "GB", func(m rig) { _ = m.a.S.SetMoneyEntry("linked") }, "Ann")
	d := docOf(t)(r.a.View(ask(app.ViewEntry, "", cash, nil)))
	if strings.Contains(text(d), "log_save") || !strings.Contains(text(d), "link.title") {
		t.Fatalf("%s", text(d))
	}
	docOf(t)(r.a.Action(ask(app.ViewEntry, "log_save", cash, map[string]any{"staff": sts[0].ID, "payment": "cash", "item_0": "i1"})))
	if r.a.S.CountEntries() != 0 {
		t.Fatal("a forged free save on a linked replica is refused")
	}
}

// H5: the admin page on a replica only points to the main till.
func TestAdminOnReplicaPointsToMain(t *testing.T) {
	r, sts := replicaRig(t, "GB", nil, "Ann")
	d := docOf(t)(r.a.View(ask(app.ViewAdmin, "", mgr, nil)))
	if !strings.Contains(text(d), "service_logbook.admin.replica_notice") || strings.Contains(text(d), `"type":"form"`) {
		t.Fatalf("%s", text(d))
	}
	for _, act := range []string{"staff_add", "staff_edit", "staff_remove", "staff_remove_confirm", "settings_save"} {
		d := docOf(t)(r.a.Action(ask(app.ViewAdmin, act, mgr, map[string]any{"name": "Zed", "staff": sts[0].ID,
			"typed_name": "Ann", "exported": true, "money_entry": "linked"})))
		if !strings.Contains(text(d), "admin.replica_notice") || strings.Contains(text(d), `"type":"form"`) {
			t.Fatalf("%s: %s", act, text(d))
		}
	}
	if l, _ := r.a.S.ListStaff(true); len(l) != 0 {
		t.Fatal("nothing written on the replica")
	}
	if r.a.S.GetMeta(store.MetaMoneyEntry) != "" {
		t.Fatal("no settings written on the replica")
	}
}

// H4: Maintenance trims a replica's settled copies.
func TestMaintenanceTrimsReplica(t *testing.T) {
	r, sts := replicaRig(t, "GB", nil, "Ann")
	e, _ := r.a.S.CreateFree(store.Actor{UserID: "u1"}, store.FreeInput{StaffID: sts[0].ID, Payment: "cash", TipMinor: 1})
	r.f.DB().Exec("UPDATE entries SET created_at = '2026-01-01T00:00:00Z' WHERE id = ?", e.ID)
	if _, err := r.a.Maintenance(); err != nil {
		t.Fatal(err)
	}
	if r.a.S.CountEntries() != 0 {
		t.Fatal("a delivered row older than 35 days is trimmed")
	}
}

// H4: GB may erase an archived member from the admin page.
func TestGBAdminRemovesArchivedMember(t *testing.T) {
	r := newRig(t, "FR")
	st := r.staff(t, "Ann")
	_, _ = r.a.S.RemoveStaff(st.ID, store.RemoveInput{})
	r.f.SetView("shop.context.v2", []map[string]any{{"currency_code": "GBP", "decimals": 2, "locale": "en", "country": "GB", "business_date": "2026-11-27"}})
	d := docOf(t)(r.a.Action(ask(app.ViewAdmin, "staff_remove", mgr, map[string]any{"staff": st.ID})))
	if !strings.Contains(text(d), "remove.gb_warning") {
		t.Fatalf("%s", text(d))
	}
	docOf(t)(r.a.Action(ask(app.ViewAdmin, "staff_remove_confirm", mgr, map[string]any{"staff": st.ID, "typed_name": "Former staff #1", "exported": true})))
	if _, err := r.a.S.GetStaff(st.ID); err == nil {
		t.Fatal("archived member erased in GB")
	}
}

// Low: edit_start applies the author-or-manager rule.
func TestEditStartNeedsAuthorOrManager(t *testing.T) {
	r := newRig(t, "GB")
	st := r.staff(t, "Ann")
	e, _ := r.a.S.CreateFree(store.Actor{UserID: "u9"}, store.FreeInput{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{{Name: "Secret", Qty: 1, AmountMinor: 777}}})
	d := docOf(t)(r.a.Action(ask(app.ViewEntry, "edit_start", cash, map[string]any{"entry": e.ID})))
	if strings.Contains(text(d), "edit_save") || !strings.Contains(text(d), "err.forbidden") {
		t.Fatalf("a stranger must not open the edit form: %s", text(d))
	}
	d = docOf(t)(r.a.Action(ask(app.ViewEntry, "edit_start", mgr, map[string]any{"entry": e.ID})))
	if !strings.Contains(text(d), "edit_save") {
		t.Fatalf("manager may: %s", text(d))
	}
}
