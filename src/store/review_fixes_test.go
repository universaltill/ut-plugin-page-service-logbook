//go:build !wasip1

package store_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/host"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

// newReplica is a replica till whose staff and config arrive ONLY through the
// main's snapshots (as the admin pull delivers them): the replica's own staff
// table stays empty, like on a real joined till.
func newReplica(t *testing.T, country string, prep func(m env), names ...string) (env, []store.Staff) {
	t.Helper()
	m := newEnv(t, country)
	var sts []store.Staff
	for _, n := range names {
		sts = append(sts, mustStaff(t, m, n))
	}
	if prep != nil {
		prep(m)
	}
	m.s.PublishSnapshots()
	r := newEnv(t, country)
	r.f.Role = "replica"
	for k, v := range m.f.Snaps {
		r.f.Snaps[k] = v
	}
	return r, sts
}

// B1: a replica validates staff against the snapshot, never its own table.
func TestReplicaLogsForSnapshotStaffOnly(t *testing.T) {
	r, sts := newReplica(t, "GB", nil, "Ann")
	if _, err := r.s.GetStaff(sts[0].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("precondition: the replica's staff table is empty: %v", err)
	}
	en, err := r.s.CreateFree(ann, free(sts[0].ID, 1000, 0))
	if err != nil {
		t.Fatalf("a replica logs for a snapshot member: %v", err)
	}
	if en.StaffID != sts[0].ID {
		t.Fatalf("%+v", en)
	}
	if _, err := r.s.CreateFree(ann, free("ghost", 1000, 0)); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown staff on a replica: %v", err)
	}
	ed, err := r.s.Edit(ann, en.ID, free(sts[0].ID, 2000, 0))
	if err != nil || ed.TotalMinor != 2000 {
		t.Fatalf("a replica edits its own row: %+v %v", ed, err)
	}
	none := newEnv(t, "GB")
	none.f.Role = "replica"
	if _, err := none.s.CreateFree(ann, free(sts[0].ID, 1000, 0)); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("no snapshot = no staff: %v", err)
	}
}

func TestReplicaLinksForSnapshotStaff(t *testing.T) {
	r, sts := newReplica(t, "GB", func(m env) { _ = m.s.SetMoneyEntry("linked") }, "Ann", "Bob")
	r.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 100, cashPay(5000))})
	ens, err := r.s.CreateLinked(ann, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: sts[0].ID, 2: sts[1].ID}, TipTo: sts[1].ID})
	if err != nil || len(ens) != 2 {
		t.Fatalf("%+v %v", ens, err)
	}
	if _, err := r.s.CreateLinked(ann, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: "ghost", 2: "ghost"}}); err == nil {
		t.Fatal("ghost staff must be refused")
	}
}

func TestStaffLabelsFromSnapshotOnReplica(t *testing.T) {
	r, sts := newReplica(t, "GB", nil, "Ann")
	got, err := r.s.StaffForLabels()
	if err != nil || len(got) != 1 || got[0].ID != sts[0].ID || got[0].Name != "Ann" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStaffAdminRefusedOnReplica(t *testing.T) {
	r, sts := newReplica(t, "GB", nil, "Ann")
	if _, err := r.s.AddStaff(store.StaffInput{Name: "Zed"}); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("add: %v", err)
	}
	if _, err := r.s.EditStaff(sts[0].ID, store.StaffInput{Name: "Zed"}); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("edit: %v", err)
	}
	if _, err := r.s.RemoveStaff(sts[0].ID, store.RemoveInput{}); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("remove: %v", err)
	}
	if err := r.s.SetMoneyEntry("linked"); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("mode: %v", err)
	}
	if err := r.s.SetCategories([]string{"c1"}); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("categories: %v", err)
	}
}

// H1: one effective mode, read from the config snapshot on a replica.
func TestEffectiveModeFromSnapshotOnReplica(t *testing.T) {
	r, sts := newReplica(t, "GB", func(m env) { _ = m.s.SetMoneyEntry("linked") }, "Ann")
	if r.s.EffectiveMode() != "linked" {
		t.Fatal("replica must follow the main's config snapshot")
	}
	if _, err := r.s.CreateFree(ann, free(sts[0].ID, 100, 0)); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("free entry on a linked replica: %v", err)
	}
	r2, _ := newReplica(t, "GB", nil, "Ann")
	_, _ = r2.f.DB().Exec("INSERT INTO meta (k, v) VALUES ('money_entry', 'linked')")
	if r2.s.EffectiveMode() != "free" {
		t.Fatal("a stale local meta row must not decide the mode on a replica")
	}
	if store.New(newEnvRole(t, "DE", "replica").f).EffectiveMode() != "linked" {
		t.Fatal("DE is forced linked whatever the snapshot")
	}
}

func newEnvRole(t *testing.T, country, role string) env {
	e := newEnv(t, country)
	e.f.Role = role
	return e
}

// H2: a sale line linked on a replica and again on the main counts once.
func TestApplyRejectsDoubleLinkedSaleLine(t *testing.T) {
	e := newEnv(t, "GB")
	_ = e.s.SetMoneyEntry("linked")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 300, cashPay(5000))})
	a := mustStaff(t, e, "Ann")
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}}); err != nil {
		t.Fatal(err)
	}
	w := linkedWire("b1", a.ID, "r1", 3000, 0, 1)
	ae := e.s.Apply("tillB", w)
	if ae == nil || ae.Retry || ae.Reason != "already_linked" {
		t.Fatalf("a line already carried by a current entry: %v", ae)
	}
	tipOnly := linkedWire("b2", a.ID, "r1", 0, 300)
	if ae := e.s.Apply("tillB", tipOnly); ae == nil || ae.Retry {
		t.Fatalf("the sale's tip is already counted: %v", ae)
	}
	if e.s.CountEntries() != 1 {
		t.Fatalf("nothing stored: %d", e.s.CountEntries())
	}
}

func TestApplyLinkedReplicaFirstThenMainHidesSale(t *testing.T) {
	e := newEnv(t, "GB")
	_ = e.s.SetMoneyEntry("linked")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 0, cashPay(5000))})
	a := mustStaff(t, e, "Ann")
	if ae := e.s.Apply("tillB", linkedWire("b1", a.ID, "r1", 5000, 0, 1, 2)); ae != nil {
		t.Fatal(ae)
	}
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("main must not link a sale the replica already linked: %v", err)
	}
	// a void of the replica's linked row is not a new link
	v := linkedWire("b2", a.ID, "r1", 0, 0)
	v.Entry.SupersedesID, v.Entry.Voided = "b1", true
	if ae := e.s.Apply("tillB", v); ae != nil {
		t.Fatalf("void of a linked row: %v", ae)
	}
}

func linkedWire(id, staff, sale string, total, tip int64, lineNos ...int) store.Wire {
	w := store.Wire{Entry: store.WireEntry{ID: id, Author: "u1", StaffID: staff, BusinessDate: "2026-11-27", Currency: "EUR",
		Payment: "cash", TotalMinor: total, TipMinor: tip, SaleRef: sale, Mode: "linked", CreatedAt: "2026-11-27T09:00:00Z"}}
	for _, n := range lineNos {
		w.Lines = append(w.Lines, store.WireLine{LineNo: n, Name: "Cut", Qty: 1, AmountMinor: total / int64(len(lineNos)), StaffID: staff})
	}
	return w
}

// H3: a GB hard delete never makes an older, superseded row current again.
func TestGBDeleteDoesNotResurrectSupersededRow(t *testing.T) {
	e := newEnv(t, "GB")
	a, b := mustStaff(t, e, "Ann"), mustStaff(t, e, "Bob")
	orig, _ := e.s.CreateFree(ann, free(a.ID, 1000, 0))
	if _, err := e.s.Edit(boss, orig.ID, free(b.ID, 1000, 0)); err != nil { // moved to Bob
		t.Fatal(err)
	}
	if _, err := e.s.RemoveStaff(b.ID, store.RemoveInput{Exported: true, TypedName: "Bob"}); err != nil {
		t.Fatal(err)
	}
	if day, _ := e.s.DayList("2026-11-27"); len(day) != 0 {
		t.Fatalf("the row superseded by Bob's deleted edit must not come back: %+v", day)
	}
	if _, err := e.s.GetEntry(orig.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("superseded ancestor removed with its descendant: %v", err)
	}
	if e.s.CountLines() != 0 {
		t.Fatalf("lines of removed rows go too: %d", e.s.CountLines())
	}
}

// H4: records for unknown or deleted staff are terminal rejections.
func TestApplyRejectsUnknownStaff(t *testing.T) {
	e := newEnv(t, "GB")
	ae := e.s.Apply("tillB", wire("a1", "ghost", 1000, ""))
	if ae == nil || ae.Retry || ae.Reason != "unknown_staff" {
		t.Fatalf("%v", ae)
	}
	st := mustStaff(t, e, "Ann")
	w := wire("a2", st.ID, 1000, "")
	w.Lines[0].StaffID = "ghost"
	if ae := e.s.Apply("tillB", w); ae == nil || ae.Retry {
		t.Fatalf("a line for an unknown member: %v", ae)
	}
	// GB delete, then a late redelivery of that member's record: not resurrected
	if ae := e.s.Apply("tillB", wire("a3", st.ID, 1000, "")); ae != nil {
		t.Fatal(ae)
	}
	if _, err := e.s.RemoveStaff(st.ID, store.RemoveInput{Exported: true, TypedName: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if ae := e.s.Apply("tillB", wire("a3", st.ID, 1000, "")); ae == nil || ae.Retry {
		t.Fatalf("deleted member's record must not come back: %v", ae)
	}
	if e.s.CountEntries() != 0 {
		t.Fatal("nothing stored")
	}
}

func TestApplyAcceptsArchivedStaff(t *testing.T) {
	e := newEnv(t, "FR")
	st := mustStaff(t, e, "Ann")
	_, _ = e.s.RemoveStaff(st.ID, store.RemoveInput{})
	if ae := e.s.Apply("tillB", wire("a1", st.ID, 1000, "")); ae != nil {
		t.Fatalf("an archived member's late record keeps its amounts: %v", ae)
	}
}

func TestTrimReplica(t *testing.T) {
	r, sts := newReplica(t, "GB", nil, "Ann", "Bob")
	a, b := sts[0], sts[1]
	old, _ := r.s.CreateFree(ann, free(a.ID, 100, 0)) // delivered, 40 days old
	pend, _ := r.s.CreateFree(ann, free(a.ID, 100, 0))
	rej, _ := r.s.CreateFree(ann, free(b.ID, 100, 0))
	recent, _ := r.s.CreateFree(ann, free(a.ID, 100, 0))
	bobs, _ := r.s.CreateFree(ann, free(b.ID, 100, 0))
	bobPending, _ := r.s.CreateFree(ann, free(b.ID, 100, 0))
	_, _ = r.f.DB().Exec("UPDATE entries SET created_at = '2026-10-18T09:00:00Z' WHERE id IN (?, ?)", old.ID, pend.ID)
	_, _ = r.f.DB().Exec("UPDATE entries SET fwd_state = 'pending' WHERE id = ?", pend.ID)
	r.f.Status[rej.ID] = host.RecordStatus{ID: rej.ID, Status: "rejected"}
	r.f.Status[bobPending.ID] = host.RecordStatus{ID: bobPending.ID, Status: "pending"}
	// Bob leaves the snapshot (archived or deleted on main)
	r.f.Snaps["staff"] = []byte(fmt.Sprintf(`{"staff":[{"id":%q,"display_name":"Ann"}]}`, a.ID))
	r.f.Time = time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	r.s.TrimReplica()
	gone := func(id string) bool { _, err := r.s.GetEntry(id); return errors.Is(err, store.ErrNotFound) }
	if !gone(old.ID) || gone(pend.ID) || gone(recent.ID) {
		t.Fatalf("delivered rows older than 35 days go, pending and recent stay: old=%v pend=%v recent=%v", gone(old.ID), gone(pend.ID), gone(recent.ID))
	}
	if !gone(bobs.ID) || !gone(rej.ID) || gone(bobPending.ID) {
		t.Fatalf("settled rows of a member who left the snapshot go; undelivered stay: %v %v %v", gone(bobs.ID), gone(rej.ID), gone(bobPending.ID))
	}
	if n := r.s.CountLines(); n != 3 {
		t.Fatalf("lines of trimmed rows go too: %d", n)
	}
	// never on a main till
	m := newEnv(t, "GB")
	st := mustStaff(t, m, "Ann")
	en, _ := m.s.CreateFree(ann, free(st.ID, 100, 0))
	_, _ = m.f.DB().Exec("UPDATE entries SET created_at = '2020-01-01T00:00:00Z' WHERE id = ?", en.ID)
	m.s.TrimReplica()
	if m.s.CountEntries() != 1 {
		t.Fatal("main keeps everything")
	}
	// no snapshot: nothing is trimmed for a staff reason
	r2, sts2 := newReplica(t, "GB", nil, "Ann")
	en2, _ := r2.s.CreateFree(ann, free(sts2[0].ID, 100, 0))
	delete(r2.f.Snaps, "staff")
	r2.s.TrimReplica()
	if _, err := r2.s.GetEntry(en2.ID); err != nil {
		t.Fatal("no snapshot is not 'staff left'")
	}
}

func TestGBDeleteOfArchivedMember(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	_, _ = e.s.CreateFree(ann, free(st.ID, 100, 0))
	e.f.ViewErr["shop.context.v2"] = errors.New("down")
	if res, _ := e.s.RemoveStaff(st.ID, store.RemoveInput{}); res != store.Archived {
		t.Fatal("unreadable country archives (fail closed)")
	}
	delete(e.f.ViewErr, "shop.context.v2")
	former := func(n int) string { return fmt.Sprintf("Former staff #%d", n) }
	if _, err := e.s.RemoveStaff(st.ID, store.RemoveInput{Exported: true, TypedName: "", Former: former}); !errors.Is(err, store.ErrNotConfirmed) {
		t.Fatalf("still needs the typed confirmation: %v", err)
	}
	res, err := e.s.RemoveStaff(st.ID, store.RemoveInput{Exported: true, TypedName: "Former staff #1", Former: former})
	if err != nil || res != store.Deleted {
		t.Fatalf("GB may erase an archived member: %v %v", res, err)
	}
	if e.s.CountEntries() != 0 {
		t.Fatal("entries erased")
	}
	fr := newEnv(t, "FR")
	st2 := mustStaff(t, fr, "Ann")
	_, _ = fr.s.RemoveStaff(st2.ID, store.RemoveInput{})
	if _, err := fr.s.RemoveStaff(st2.ID, store.RemoveInput{Former: former}); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("outside GB an archived member stays archived: %v", err)
	}
}

// M4: a role error never loses a row: it stays pending; the retry settles it.
func TestRoleErrorWritesPending(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	e.f.RoleErr = errors.New("host busy")
	e.s.BeginEvent() // a new event: the role is read again
	en, err := e.s.CreateFree(ann, free(st.ID, 100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := e.s.GetEntry(en.ID); g.FwdState != "pending" {
		t.Fatalf("unknown role must be pending: %s", g.FwdState)
	}
	e.f.RoleErr = nil
	e.s.BeginEvent()
	e.s.RetryPending()
	if g, _ := e.s.GetEntry(en.ID); g.FwdState != "na" || len(e.f.Out) != 0 {
		t.Fatalf("on a main the retry settles it as na: %s %d", g.FwdState, len(e.f.Out))
	}
}

func TestForwardNeedsLines(t *testing.T) {
	r, sts := newReplica(t, "GB", nil, "Ann")
	r.f.FailQuery = "FROM entry_lines"
	en, err := r.s.CreateFree(ann, free(sts[0].ID, 100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.f.Out) != 0 {
		t.Fatal("never forward an entry without its lines")
	}
	if g, _ := r.s.GetEntry(en.ID); g.FwdState != "pending" {
		t.Fatalf("stays pending for the retry: %s", g.FwdState)
	}
	r.f.FailQuery = ""
	r.s.RetryPending()
	if len(r.f.Out) != 1 {
		t.Fatal("retry forwards it")
	}
}

// M5: an edit keeps the original row's business date and currency.
func TestEditKeepsBusinessDateAndCurrency(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	en, _ := e.s.CreateFree(ann, free(st.ID, 1000, 0))
	e.f.SetView("shop.context.v2", []map[string]any{{"currency_code": "EUR", "currency_decimals": 2, "locale": "en",
		"country": "GB", "business_date": "2026-11-28"}})
	ed, err := e.s.Edit(ann, en.ID, free(st.ID, 2000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if ed.BusinessDate != "2026-11-27" || ed.Currency != "GBP" {
		t.Fatalf("edit moved the entry: %+v", ed)
	}
}

// M6: a linked save is all or nothing; negative lines are refused up front.
func TestLinkedNegativeLineRefusedNothingWritten(t *testing.T) {
	e := newEnv(t, "GB")
	_ = e.s.SetMoneyEntry("linked")
	rc := receipt("r1", false, 0, cashPay(5000))
	rc["lines"] = []map[string]any{
		{"line_no": 1, "item_id": "i1", "name": "Cut", "quantity": 1, "total_minor": 3000},
		{"line_no": 2, "item_id": "i2", "name": "Discount", "quantity": 1, "total_minor": -500}}
	e.f.SetView("sales.receipts.v1", []map[string]any{rc})
	a, b := mustStaff(t, e, "Ann"), mustStaff(t, e, "Bob")
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: b.ID}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if e.s.CountEntries() != 0 || e.s.CountLines() != 0 {
		t.Fatalf("no partly linked sale: %d/%d", e.s.CountEntries(), e.s.CountLines())
	}
}

// Low: amounts are bounded (1e11 minor units).
func TestAmountUpperBound(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	const over = 100_000_000_001
	if _, err := e.s.CreateFree(ann, free(st.ID, over, 0)); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("line: %v", err)
	}
	if _, err := e.s.CreateFree(ann, free(st.ID, 0, over)); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("tip: %v", err)
	}
	big := store.FreeInput{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{
		{Name: "a", Qty: 1, AmountMinor: 60_000_000_000}, {Name: "b", Qty: 1, AmountMinor: 60_000_000_000}}}
	if _, err := e.s.CreateFree(ann, big); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("total: %v", err)
	}
	w := wire("a1", st.ID, over, "")
	if ae := e.s.Apply("tillB", w); ae == nil || ae.Retry {
		t.Fatalf("apply: %v", ae)
	}
	if _, err := e.s.CreateFree(ann, free(st.ID, 100_000_000_000, 0)); err != nil {
		t.Fatalf("the bound itself is allowed: %v", err)
	}
}

// Low: the edit form opens only for someone who may change the row.
func TestCanChange(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	en, _ := e.s.CreateFree(ann, free(st.ID, 100, 0))
	if _, err := e.s.CanChange(other, en.ID); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("%v", err)
	}
	if _, err := e.s.CanChange(ann, en.ID); err != nil {
		t.Fatal(err)
	}
}

// records_status answers at most 200 rows (oldest first). With a full answer
// an id missing from it may still be undelivered, so it is never trimmed;
// only rows the answer lists as rejected are settled (fix-plug finding 9).
func TestTrimReplicaFullStatusAnswerTrustsNoAbsence(t *testing.T) {
	r, sts := newReplica(t, "GB", nil, "Ann")
	queued, _ := r.s.CreateFree(ann, free(sts[0].ID, 100, 0))
	rej, _ := r.s.CreateFree(ann, free(sts[0].ID, 100, 0))
	_, _ = r.f.DB().Exec("UPDATE entries SET created_at = '2026-10-18T09:00:00Z' WHERE id IN (?, ?)", queued.ID, rej.ID)
	for i := 0; i < host.StatusLimit; i++ {
		id := fmt.Sprintf("0-older-%03d", i) // sorts before the entry ids: inside the answer
		r.f.Status[id] = host.RecordStatus{ID: id, Status: "pending"}
	}
	r.f.Status[rej.ID] = host.RecordStatus{ID: rej.ID, Status: "rejected"} // past the 200: not answered
	r.f.Time = time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	r.s.BeginEvent()
	r.s.TrimReplica()
	if _, err := r.s.GetEntry(queued.ID); err != nil {
		t.Fatalf("a row missing from a full status answer was trimmed: %v", err)
	}
	if _, err := r.s.GetEntry(rej.ID); err != nil {
		t.Fatalf("a row past the answer was trimmed: %v", err)
	}
	// a rejected row inside a full answer is settled and goes
	delete(r.f.Status, "0-older-000")
	r.f.Status["0-rej"] = host.RecordStatus{ID: rej.ID, Status: "rejected"}
	r.s.BeginEvent()
	r.s.TrimReplica()
	if _, err := r.s.GetEntry(rej.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rejected row in the answer kept: %v", err)
	}
	if _, err := r.s.GetEntry(queued.ID); err != nil {
		t.Fatalf("queued row trimmed: %v", err)
	}
}
