//go:build !wasip1

package store_test

import (
	"errors"
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

func receipt(id string, signed bool, tip int64, pay ...map[string]any) map[string]any {
	return map[string]any{"id": id, "receipt_no": "R" + id, "sale_type": "sale", "business_date": "2026-11-27",
		"currency": "EUR", "total_minor": 5000, "tip_minor": tip, "fiscal_signed": signed,
		"lines": []map[string]any{
			{"line_no": 1, "item_id": "i1", "name": "Cut", "quantity": 1, "total_minor": 3000},
			{"line_no": 2, "item_id": "i2", "name": "Colour", "quantity": 1, "total_minor": 2000}},
		"payments": pay}
}

func cashPay(a int64) map[string]any { return map[string]any{"method_id": "cash", "amount_minor": a} }
func cardPay(a int64) map[string]any { return map[string]any{"method_id": "stripe", "amount_minor": a} }

func TestLinkedDEOnlySignedSalesNoReturns(t *testing.T) {
	e := newEnv(t, "DE")
	ret := receipt("r3", true, 0, cashPay(5000))
	ret["sale_type"] = "return"
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", true, 0, cashPay(5000)), receipt("r2", false, 0, cashPay(5000)), ret})
	got, err := e.s.Receipts(0)
	if err != nil || len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("DE offers signed sales only: %+v %v", got, err)
	}
	e2 := newEnv(t, "GB")
	e2.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", true, 0, cashPay(1)), receipt("r2", false, 0, cashPay(1)), ret})
	if got, _ := e2.s.Receipts(0); len(got) != 2 {
		t.Fatalf("other countries offer every completed sale: %+v", got)
	}
}

func TestLinkedAmountsComeFromTheSaleOnly(t *testing.T) {
	e := newEnv(t, "DE")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", true, 300, cashPay(1000), cardPay(4000))})
	a, b := mustStaff(t, e, "Ann"), mustStaff(t, e, "Bob")
	ens, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: b.ID}, TipTo: b.ID})
	if err != nil || len(ens) != 2 {
		t.Fatalf("%+v %v", ens, err)
	}
	by := map[string]store.Entry{}
	for _, en := range ens {
		by[en.StaffID] = en
	}
	if by[a.ID].TotalMinor != 3000 || by[b.ID].TotalMinor != 2000 || by[a.ID].TipMinor != 0 || by[b.ID].TipMinor != 300 {
		t.Fatalf("%+v", by)
	}
	if by[a.ID].Payment != "card" || by[a.ID].SaleRef != "r1" || by[a.ID].Mode != "linked" || by[a.ID].Currency != "EUR" {
		t.Fatalf("largest payment is card: %+v", by[a.ID])
	}
	// the same sale cannot be linked twice
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("double link: %v", err)
	}
	if rs, _ := e.s.Receipts(0); len(rs) != 0 {
		t.Fatalf("a fully linked receipt is hidden: %+v", rs)
	}
}

func TestLinkedCashPaymentAndDefaultTipRecipient(t *testing.T) {
	e := newEnv(t, "GB")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 200, cashPay(5000))})
	e.s.SetMoneyEntry("linked")
	a := mustStaff(t, e, "Ann")
	ens, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}})
	if err != nil || len(ens) != 1 || ens[0].Payment != "cash" || ens[0].TipMinor != 200 || ens[0].TotalMinor != 5000 {
		t.Fatalf("%+v %v", ens, err)
	}
}

func TestLinkedRefusals(t *testing.T) {
	e := newEnv(t, "DE")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 0, cashPay(5000))})
	a := mustStaff(t, e, "Ann")
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unsigned receipt in DE: %v", err)
	}
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r2", true, 0, cashPay(5000))})
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r2", Assign: map[int]string{1: a.ID}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("every line needs a staff member: %v", err)
	}
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r2", Assign: map[int]string{1: "ghost", 2: a.ID}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown staff: %v", err)
	}
	if e.s.CountEntries() != 0 {
		t.Fatal("refusals write nothing")
	}
	// linked entries cannot be edited: amounts come from the sale
	ens, _ := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r2", Assign: map[int]string{1: a.ID, 2: a.ID}})
	if _, err := e.s.Edit(boss, ens[0].ID, free(a.ID, 1, 0)); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("linked amounts are not editable: %v", err)
	}
	// but a linked entry can be voided (frees the sale to be linked again)
	if _, err := e.s.Void(boss, ens[0].ID); err != nil {
		t.Fatal(err)
	}
}

func wire(id, staff string, total int64, sup string) store.Wire {
	return store.Wire{Entry: store.WireEntry{ID: id, Author: "u1", StaffID: staff, BusinessDate: "2026-11-27", Currency: "GBP",
		Payment: "cash", TotalMinor: total, SupersedesID: sup, Mode: "free", CreatedAt: "2026-11-27T09:00:00Z"},
		Lines: []store.WireLine{{LineNo: 1, Name: "Cut", Qty: 1, AmountMinor: total}}}
}

func TestApplyIdempotentAndStampsOrigin(t *testing.T) {
	e := newEnv(t, "GB")
	w := wire("a1", "s1", 1000, "")
	if err := e.s.Apply("tillB", w); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Apply("tillB", w); err != nil {
		t.Fatalf("redelivery is ok: %v", err)
	}
	if e.s.CountEntries() != 1 || e.s.CountLines() != 1 {
		t.Fatalf("exactly one row: %d/%d", e.s.CountEntries(), e.s.CountLines())
	}
	got, _ := e.s.GetEntry("a1")
	if got.OriginTill != "tillB" || got.FwdState != "na" {
		t.Fatalf("origin comes from core, not the payload: %+v", got)
	}
}

func TestApplyValidatesEveryField(t *testing.T) {
	e := newEnv(t, "GB")
	mut := []func(*store.Wire){
		func(w *store.Wire) { w.Entry.ID = "bad id!" },
		func(w *store.Wire) { w.Entry.Currency = "GB" },
		func(w *store.Wire) { w.Entry.BusinessDate = "27/11/2026" },
		func(w *store.Wire) { w.Entry.Payment = "crypto" },
		func(w *store.Wire) { w.Entry.TotalMinor = -1 },
		func(w *store.Wire) { w.Lines[0].Qty = 0 },
		func(w *store.Wire) { w.Lines[0].Qty = 1000 },
		func(w *store.Wire) { w.Entry.Mode = "weird" },
	}
	for i, m := range mut {
		w := wire("a1", "s1", 1000, "")
		m(&w)
		err := e.s.Apply("tillB", w)
		if err == nil || err.Retry {
			t.Fatalf("case %d must be a terminal rejection: %v", i, err)
		}
	}
	if e.s.CountEntries() != 0 || e.s.CountLines() != 0 {
		t.Fatal("a rejected record leaves nothing behind")
	}
}

func TestApplyReplicaCannotSupersedeMainRowOrAnotherTill(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	own, _ := e.s.CreateFree(ann, free(st.ID, 1000, 0))
	err := e.s.Apply("tillB", wire("b1", st.ID, 2000, own.ID))
	if err == nil || err.Retry || err.Reason != "not_authorised" {
		t.Fatalf("%v", err)
	}
	if err := e.s.Apply("tillA", wire("a1", "s1", 1000, "")); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Apply("tillB", wire("b2", "s1", 2000, "a1")); err == nil || err.Reason != "not_authorised" {
		t.Fatalf("tillB must not supersede tillA's row: %v", err)
	}
	if err := e.s.Apply("tillA", wire("a2", "s1", 2000, "a1")); err != nil {
		t.Fatalf("same origin may: %v", err)
	}
}

func TestApplyUnknownParentStoredAndConflictMainWins(t *testing.T) {
	e := newEnv(t, "GB")
	if err := e.s.Apply("tillA", wire("a2", "s1", 2000, "a1")); err != nil {
		t.Fatalf("unknown parent is stored (append-only log): %v", err)
	}
	if err := e.s.Apply("tillA", wire("a1", "s1", 1000, "")); err != nil {
		t.Fatal(err)
	}
	day, _ := e.s.DayList("2026-11-27")
	if len(day) != 1 || day[0].ID != "a2" {
		t.Fatalf("chain tip is current: %+v", day)
	}
	// main edits a1 itself after the fact: a conflicting child of the same parent
	if err := e.s.Apply("tillA", wire("a3", "s1", 3000, "a1")); err != nil {
		t.Fatal(err)
	}
	day, _ = e.s.DayList("2026-11-27")
	if len(day) != 1 {
		t.Fatalf("one of two siblings must be shadowed: %+v", day)
	}
	if day[0].ID != "a2" { // earlier (created_at,id) wins between replica rows
		t.Fatalf("earlier wins: %s", day[0].ID)
	}
	// a main-authored child beats a replica child
	e2 := newEnv(t, "GB")
	st := mustStaff(t, e2, "Ann")
	_ = e2.s.Apply("tillB", wire("b1", st.ID, 1000, ""))
	_ = e2.s.Apply("tillB", wire("b2", st.ID, 2000, "b1"))
	mainEdit, err := e2.s.Edit(boss, "b1", free(st.ID, 1500, 0)) // b1 not current any more
	if err == nil {
		t.Fatalf("b1 is superseded: %+v", mainEdit)
	}
}

func TestApplyMainAuthoredWinsOverLaterReplicaChild(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	_ = e.s.Apply("tillB", wire("b1", st.ID, 1000, ""))
	mainEdit, err := e.s.Edit(boss, "b1", free(st.ID, 1500, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.Apply("tillB", wire("b2", st.ID, 2000, "b1")); err != nil {
		t.Fatalf("stored, then shadowed: %v", err)
	}
	day, _ := e.s.DayList("2026-11-27")
	if len(day) != 1 || day[0].ID != mainEdit.ID {
		t.Fatalf("main-authored row wins: %+v", day)
	}
	if g, _ := e.s.GetEntry("b2"); !g.Shadowed {
		t.Fatal("loser kept for audit, marked shadowed")
	}
}
