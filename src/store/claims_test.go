//go:build !wasip1

package store_test

import (
	"errors"
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

// Two events link the same sale line at once: both pass the "not linked yet"
// read, the unique (sale_ref, line_no) index on entry_lines (migration 0002)
// refuses the second write, which maps to the existing "already linked"
// answer and leaves nothing behind (fix-plug finding 6).
func TestConcurrentLinkOfOneSaleLine_LocalLoses(t *testing.T) {
	e := newEnv(t, "GB")
	_ = e.s.SetMoneyEntry("linked")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 0, cashPay(5000))})
	a := mustStaff(t, e, "Ann")
	e.f.BeforeExecOn = "INSERT INTO entry_lines"
	e.f.BeforeExec = func() {
		if ae := e.s.Apply("tillB", linkedWire("b1", a.ID, "r1", 5000, 0, 1, 2)); ae != nil {
			t.Fatalf("the other till's link: %v", ae)
		}
	}
	_, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second link of the same lines: %v", err)
	}
	if n := e.s.CountEntries(); n != 1 {
		t.Fatalf("entries = %d, want 1", n)
	}
	if rows, _ := e.f.DB().Query("SELECT entry_id FROM entry_lines WHERE entry_id <> 'b1'"); len(rows) != 0 {
		t.Fatalf("lines left behind: %v", rows)
	}
}

func TestConcurrentLinkOfOneSaleLine_ApplyLoses(t *testing.T) {
	e := newEnv(t, "GB")
	_ = e.s.SetMoneyEntry("linked")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 0, cashPay(5000))})
	a := mustStaff(t, e, "Ann")
	e.f.BeforeExecOn = "INSERT"
	e.f.BeforeExec = func() {
		if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}}); err != nil {
			t.Fatalf("main's link: %v", err)
		}
	}
	ae := e.s.Apply("tillB", linkedWire("b1", a.ID, "r1", 5000, 0, 1, 2))
	if ae == nil || ae.Retry || ae.Reason != "already_linked" {
		t.Fatalf("apply of a line linked meanwhile: %v", ae)
	}
	if n := e.s.CountEntries(); n != 1 {
		t.Fatalf("entries = %d, want 1", n)
	}
	if rows, _ := e.f.DB().Query("SELECT entry_id FROM entry_lines WHERE entry_id = 'b1'"); len(rows) != 0 {
		t.Fatalf("b1 lines left behind: %v", rows)
	}
	// a redelivery of the same record is still refused, never half-applied
	if ae := e.s.Apply("tillB", linkedWire("b1", a.ID, "r1", 5000, 0, 1, 2)); ae == nil || ae.Retry {
		t.Fatalf("redelivery: %v", ae)
	}
}

// A void frees the sale lines: linking them again works (the claim of the
// voided entry is released).
func TestVoidedLinkCanBeLinkedAgain(t *testing.T) {
	e := newEnv(t, "GB")
	_ = e.s.SetMoneyEntry("linked")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 0, cashPay(5000))})
	a := mustStaff(t, e, "Ann")
	ens, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Void(boss, ens[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: a.ID, 2: a.ID}}); err != nil {
		t.Fatalf("relink after void: %v", err)
	}
	// and a replica's void of its own link frees the line on the main too
	e2 := newEnv(t, "GB")
	_ = e2.s.SetMoneyEntry("linked")
	e2.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 0, cashPay(5000))})
	b := mustStaff(t, e2, "Bob")
	if ae := e2.s.Apply("tillB", linkedWire("b1", b.ID, "r1", 5000, 0, 1, 2)); ae != nil {
		t.Fatal(ae)
	}
	v := linkedWire("b2", b.ID, "r1", 0, 0)
	v.Entry.SupersedesID, v.Entry.Voided = "b1", true
	if ae := e2.s.Apply("tillB", v); ae != nil {
		t.Fatal(ae)
	}
	if _, err := e2.s.CreateLinked(boss, store.LinkInput{SaleID: "r1", Assign: map[int]string{1: b.ID, 2: b.ID}}); err != nil {
		t.Fatalf("relink after the replica's void: %v", err)
	}
}

// The index itself: a second current claim of one sale line is refused by
// the database, free (unlinked) lines are never claimed.
func TestSaleLineUniqueIndex(t *testing.T) {
	e := newEnv(t, "GB")
	db := e.f.DB()
	ins := `INSERT INTO entry_lines (entry_id, line_no, name, qty, amount_minor, sale_ref, created_at) VALUES (?, 1, 'Cut', 1, 1, ?, '2026-11-27T09:00:00Z')`
	if _, err := db.Exec(ins, "x1", "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ins, "x2", "s1"); err == nil {
		t.Fatal("second claim of s1 line 1 accepted")
	}
	if _, err := db.Exec(ins, "x3", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ins, "x4", nil); err != nil {
		t.Fatalf("unlinked lines are not unique: %v", err)
	}
}
