//go:build !wasip1

package store_test

import "testing"

func sumTips(t *testing.T, e env, sale string) int64 {
	t.Helper()
	rows, err := e.f.DB().Query(`SELECT COALESCE(SUM(tip_minor), 0) AS t FROM entries WHERE sale_ref = ? AND voided = 0 AND shadowed = 0`, sale)
	if err != nil {
		t.Fatal(err)
	}
	return rows[0]["t"].(int64)
}

// ut-docs#7269: the "tip not counted yet" read and the entry write are two
// host calls, so another event can land its tip between them. Two forwarded
// entries of one sale that carry different lines both pass the read; the
// write itself must refuse the second tip.
func TestApplyTipCountedOnceWhenAnotherTipLandsBetweenCheckAndWrite(t *testing.T) {
	e := newEnv(t, "GB")
	_ = e.s.SetMoneyEntry("linked")
	e.f.SetView("sales.receipts.v1", []map[string]any{receipt("r1", false, 300, cashPay(5000))})
	a := mustStaff(t, e, "Ann")
	x := linkedWire("x1", a.ID, "r1", 3000, 300, 1)
	y := linkedWire("y1", a.ID, "r1", 2000, 300, 2)
	e.f.BeforeExecOn = "INTO entries"
	e.f.BeforeExec = func() {
		if ae := e.s.Apply("tillC", x); ae != nil {
			t.Fatalf("the concurrent apply: %v", ae)
		}
	}
	ae := e.s.Apply("tillB", y)
	if ae == nil || ae.Retry || ae.Reason != "already_linked" {
		t.Fatalf("the second tip of a sale must be refused for good: %v", ae)
	}
	if got := sumTips(t, e, "r1"); got != 300 {
		t.Fatalf("tip counted %d, want 300 once", got)
	}
	if n := e.s.CountLines(); n != 1 {
		t.Fatalf("the refused entry's line must not stay behind: %d lines", n)
	}
}
