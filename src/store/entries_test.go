//go:build !wasip1

package store_test

import (
	"errors"
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

var (
	ann   = store.Actor{UserID: "u1"}
	boss  = store.Actor{UserID: "u9", Manager: true}
	other = store.Actor{UserID: "u7"}
)

func free(staff string, amt int64, tip int64) store.FreeInput {
	return store.FreeInput{StaffID: staff, Payment: "cash", TipMinor: tip,
		Lines: []store.LineInput{{ItemID: "i1", Name: "Cut", Qty: 2, AmountMinor: amt}}}
}

func TestCreateFreeMinorUnitsTipSeparate(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	en, err := e.s.CreateFree(ann, free(st.ID, 4000, 250))
	if err != nil {
		t.Fatal(err)
	}
	if en.TotalMinor != 4000 || en.TipMinor != 250 || en.Currency != "GBP" || en.BusinessDate != "2026-11-27" || en.FwdState != "na" {
		t.Fatalf("%+v", en)
	}
	lines, _ := e.s.Lines(en.ID)
	if len(lines) != 1 || lines[0].Qty != 2 || lines[0].AmountMinor != 4000 {
		t.Fatalf("%+v", lines)
	}
}

func TestCreateFreeRefusals(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	bad := []store.FreeInput{
		{StaffID: st.ID, Payment: "cash"}, // no line, no tip
		{StaffID: st.ID, Payment: "cheque", TipMinor: 1},
		{StaffID: st.ID, Payment: "cash", TipMinor: -1},
		{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{{Name: "x", Qty: 0, AmountMinor: 1}}},
		{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{{Name: "x", Qty: 1000, AmountMinor: 1}}},
		{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{{Name: "x", Qty: 1, AmountMinor: -5}}},
		{StaffID: "ghost", Payment: "cash", TipMinor: 1},
	}
	for i, in := range bad {
		if _, err := e.s.CreateFree(ann, in); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if e.s.CountEntries() != 0 {
		t.Fatal("refused input must write nothing")
	}
	if _, err := e.s.CreateFree(ann, store.FreeInput{StaffID: st.ID, Payment: "card", TipMinor: 100}); err != nil {
		t.Fatalf("tip-only entry is allowed: %v", err)
	}
}

func TestDEAndTRCannotUseFree(t *testing.T) {
	for _, c := range []string{"DE", "TR"} {
		e := newEnv(t, c)
		st := mustStaff(t, e, "Ann")
		if _, err := e.s.CreateFree(ann, free(st.ID, 100, 0)); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("%s: %v", c, err)
		}
		if err := e.s.SetMoneyEntry("free"); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("%s must not switch to free: %v", c, err)
		}
		if e.s.MoneyEntry() != "linked" {
			t.Fatal("forced linked")
		}
	}
	e := newEnv(t, "GB")
	if e.s.MoneyEntry() != "free" || e.s.SetMoneyEntry("linked") != nil || e.s.MoneyEntry() != "linked" {
		t.Fatal("GB may choose")
	}
}

func TestEditSupersedesVoidWritesRow(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	en, _ := e.s.CreateFree(ann, free(st.ID, 4000, 0))
	ed, err := e.s.Edit(ann, en.ID, free(st.ID, 5000, 100))
	if err != nil || ed.SupersedesID != en.ID {
		t.Fatalf("%+v %v", ed, err)
	}
	day, _ := e.s.DayList("2026-11-27")
	if len(day) != 1 || day[0].ID != ed.ID {
		t.Fatalf("only the tip of the chain is current: %+v", day)
	}
	if _, err := e.s.Edit(ann, en.ID, free(st.ID, 1, 0)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("superseded row must not be edited again: %v", err)
	}
	v, err := e.s.Void(ann, ed.ID)
	if err != nil || !v.Voided || v.TotalMinor != 0 {
		t.Fatalf("%+v %v", v, err)
	}
	if day, _ := e.s.DayList("2026-11-27"); len(day) != 0 {
		t.Fatalf("voided chain is not current: %+v", day)
	}
	if _, err := e.s.Void(ann, v.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("void of a void: %v", err)
	}
	if e.s.CountEntries() != 3 {
		t.Fatal("append-only: original, edit and void rows all stay")
	}
}

func TestAuthorOrManagerOnly(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	en, _ := e.s.CreateFree(ann, free(st.ID, 4000, 0))
	if _, err := e.s.Void(other, en.ID); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("a stranger: %v", err)
	}
	if _, err := e.s.Edit(other, en.ID, free(st.ID, 1, 0)); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("a stranger: %v", err)
	}
	if _, err := e.s.Void(store.Actor{NoSignIn: true}, en.ID); err != nil {
		t.Fatalf("sign-in off means no role limits: %v", err)
	}
	en2, _ := e.s.CreateFree(ann, free(st.ID, 4000, 0))
	if _, err := e.s.Void(boss, en2.ID); err != nil {
		t.Fatalf("manager: %v", err)
	}
}

func TestReplicaCannotChangeOtherTillRows(t *testing.T) {
	e := newEnv(t, "GB")
	e.f.Role = "replica"
	st := mustStaff(t, e, "Ann")
	// a row that came from another till is only on main; simulate the row
	// existing on a replica anyway: still refused.
	_, _ = e.f.DB().Exec(`INSERT INTO entries (id, origin_till_id, author_user_id, staff_id, business_date, currency, payment_type, total_minor, created_at)
		VALUES ('x1','tillB','u1',?, '2026-11-27','GBP','cash',100,'2026-11-27T08:00:00Z')`, st.ID)
	if _, err := e.s.Void(boss, "x1"); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("%v", err)
	}
	e.f.Role = "main"
	if _, err := e.s.Void(boss, "x1"); err != nil {
		t.Fatalf("main may: %v", err)
	}
}

func TestZeroMoneyDeleteAnyCountry(t *testing.T) {
	for _, c := range []string{"GB", "FR"} {
		e := newEnv(t, c)
		st := mustStaff(t, e, "Ann")
		z, err := e.s.CreateFree(ann, store.FreeInput{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{{Name: "Free sample", Qty: 1, AmountMinor: 0}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.s.DeleteZero(ann, z.ID); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		if e.s.CountEntries() != 0 || e.s.CountLines() != 0 {
			t.Fatal("chain and lines must be gone")
		}
		paid, _ := e.s.CreateFree(ann, free(st.ID, 100, 0))
		if err := e.s.DeleteZero(ann, paid.ID); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("money entries are never hard-deleted this way: %v", err)
		}
		ed, _ := e.s.Edit(ann, paid.ID, store.FreeInput{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{{Name: "x", Qty: 1, AmountMinor: 0}}})
		if err := e.s.DeleteZero(ann, ed.ID); !errors.Is(err, store.ErrForbidden) {
			t.Fatalf("a chain that once carried money stays: %v", err)
		}
	}
}

func TestReplicaForwardsEveryRowAndMainDoesNot(t *testing.T) {
	e := newEnv(t, "GB")
	e.f.Role = "replica"
	st := mustStaff(t, e, "Ann")
	en, err := e.s.CreateFree(ann, free(st.ID, 4000, 100))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.f.Out) != 1 || e.f.Out[0].Collection != "entries" || e.f.Out[0].ID != en.ID {
		t.Fatalf("%+v", e.f.Out)
	}
	got, _ := e.s.GetEntry(en.ID)
	if got.FwdState != "queued" {
		t.Fatalf("%s", got.FwdState)
	}
	ed, _ := e.s.Edit(ann, en.ID, free(st.ID, 5000, 0))
	v, _ := e.s.Void(ann, ed.ID)
	if len(e.f.Out) != 3 {
		t.Fatalf("entry, superseding row and void row are all sent: %d", len(e.f.Out))
	}
	_ = v
	m := newEnv(t, "GB")
	st2 := mustStaff(t, m, "Ann")
	en2, _ := m.s.CreateFree(ann, free(st2.ID, 100, 0))
	if g, _ := m.s.GetEntry(en2.ID); g.FwdState != "na" || len(m.f.Out) != 0 {
		t.Fatal("main forwards nothing")
	}
}

func TestPendingRetriedByMaintenance(t *testing.T) {
	e := newEnv(t, "GB")
	e.f.Role = "replica"
	st := mustStaff(t, e, "Ann")
	en, _ := e.s.CreateFree(ann, free(st.ID, 100, 0))
	_, _ = e.f.DB().Exec("UPDATE entries SET fwd_state='pending' WHERE id=?", en.ID)
	e.f.Out = nil
	if n := e.s.RetryPending(); n != 1 || len(e.f.Out) != 1 {
		t.Fatalf("%d %v", n, e.f.Out)
	}
	if g, _ := e.s.GetEntry(en.ID); g.FwdState != "queued" {
		t.Fatal("must end queued")
	}
}

func TestPickerSnapshotOnReplica(t *testing.T) {
	m := newEnv(t, "GB")
	mustStaff(t, m, "Ann")
	gone := mustStaff(t, m, "Zed")
	_, _ = m.s.RemoveStaff(gone.ID, store.RemoveInput{}) // GB needs confirmation: stays
	r := newEnv(t, "GB")
	r.f.Role = "replica"
	if _, synced, _ := r.s.Picker(); synced {
		t.Fatal("no snapshot yet must say not synced and offer nothing")
	}
	r.f.Snaps["staff"] = m.f.Snaps["staff"]
	list, synced, _ := r.s.Picker()
	if !synced || len(list) != 2 {
		t.Fatalf("%v %v", list, synced)
	}
}

func TestSnapshotHasNoArchivedStaff(t *testing.T) {
	e := newEnv(t, "FR")
	a := mustStaff(t, e, "Ann")
	b := mustStaff(t, e, "Bob")
	_, _ = e.s.RemoveStaff(b.ID, store.RemoveInput{})
	list, _, _ := e.s.Picker()
	if len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("%v", list)
	}
	if string(e.f.Snaps["staff"]) == "" || containsStr(string(e.f.Snaps["staff"]), "Bob") {
		t.Fatalf("archived name must not be shared: %s", e.f.Snaps["staff"])
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
