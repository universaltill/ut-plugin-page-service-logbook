//go:build !wasip1

package store_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

func TestMigrationsReapplyIsNoop(t *testing.T) {
	e := newEnv(t, "GB")
	if err := e.f.Migrate(); err != nil {
		t.Fatal(err)
	}
}

func TestAddEditStaffValidation(t *testing.T) {
	e := newEnv(t, "GB")
	if _, err := e.s.AddStaff(store.StaffInput{Name: "  "}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := e.s.AddStaff(store.StaffInput{Name: strings.Repeat("x", 81)}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("long name: %v", err)
	}
	if _, err := e.s.AddStaff(store.StaffInput{Name: "A", Colour: "red"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("bad colour: %v", err)
	}
	if _, err := e.s.AddStaff(store.StaffInput{Name: "A", LinkedUserID: "nobody"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown user: %v", err)
	}
	if _, err := e.s.AddStaff(store.StaffInput{Name: "A", LinkedUserID: "u2"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("inactive user: %v", err)
	}
	st, err := e.s.AddStaff(store.StaffInput{Name: "Ann", Colour: "#a1b2c3", LinkedUserID: "u1"})
	if err != nil || st.Status != "active" || st.LinkedUserID != "u1" {
		t.Fatalf("%+v %v", st, err)
	}
	st, err = e.s.EditStaff(st.ID, store.StaffInput{Name: "Anna", Colour: "", LinkedUserID: ""})
	if err != nil || st.Name != "Anna" || st.LinkedUserID != "" {
		t.Fatalf("%+v %v", st, err)
	}
	list, _ := e.s.ListStaff(false)
	if len(list) != 1 {
		t.Fatalf("list %v", list)
	}
}

func TestArchiveErasesNameKeepsAmounts(t *testing.T) {
	e := newEnv(t, "FR")
	st := mustStaff(t, e, "Ann")
	en, err := e.s.CreateFree(store.Actor{UserID: "u1"}, store.FreeInput{StaffID: st.ID, Payment: "cash",
		Lines: []store.LineInput{{Name: "Cut", Qty: 1, AmountMinor: 3000}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.s.RemoveStaff(st.ID, store.RemoveInput{})
	if err != nil || res != store.Archived {
		t.Fatalf("%v %v", res, err)
	}
	got, _ := e.s.GetStaff(st.ID)
	if got.Name != "" || got.Status != "archived" || got.PseudoNo != 1 || got.LinkedUserID != "" {
		t.Fatalf("%+v", got)
	}
	if n := e.s.CountEntries(); n != 1 {
		t.Fatalf("entries kept: %d", n)
	}
	_ = en
	if act, _ := e.s.ListStaff(false); len(act) != 0 {
		t.Fatal("archived staff must leave the picker")
	}
	st2 := mustStaff(t, e, "Bob")
	if _, err := e.s.RemoveStaff(st2.ID, store.RemoveInput{}); err != nil {
		t.Fatal(err)
	}
	if g, _ := e.s.GetStaff(st2.ID); g.PseudoNo != 2 {
		t.Fatalf("pseudonym numbers increment: %d", g.PseudoNo)
	}
}

func TestUnknownCountryFailsClosedToArchive(t *testing.T) {
	e := newEnv(t, "")
	st := mustStaff(t, e, "Ann")
	if res, _ := e.s.RemoveStaff(st.ID, store.RemoveInput{TypedName: "Ann", Exported: true}); res != store.Archived {
		t.Fatal("empty country must archive")
	}
	e2 := newEnv(t, "GB")
	e2.f.ViewErr["shop.context.v2"] = errors.New("boom")
	st2 := mustStaff(t, e2, "Ann")
	if res, _ := e2.s.RemoveStaff(st2.ID, store.RemoveInput{TypedName: "Ann", Exported: true}); res != store.Archived {
		t.Fatal("unreadable country must archive")
	}
}

func TestGBDeleteNeedsConfirmationThenRemovesOnlyThatStaff(t *testing.T) {
	e := newEnv(t, "GB")
	ann, bob := mustStaff(t, e, "Ann"), mustStaff(t, e, "Bob")
	a := store.Actor{UserID: "u1"}
	mk := func(id string) {
		if _, err := e.s.CreateFree(a, store.FreeInput{StaffID: id, Payment: "card", TipMinor: 100,
			Lines: []store.LineInput{{Name: "Cut", Qty: 2, AmountMinor: 4000}}}); err != nil {
			t.Fatal(err)
		}
	}
	mk(ann.ID)
	mk(ann.ID)
	mk(bob.ID)
	if _, err := e.s.RemoveStaff(ann.ID, store.RemoveInput{TypedName: "Ann"}); !errors.Is(err, store.ErrNotConfirmed) {
		t.Fatalf("must need the exported toggle: %v", err)
	}
	if _, err := e.s.RemoveStaff(ann.ID, store.RemoveInput{Exported: true, TypedName: "ann?"}); !errors.Is(err, store.ErrNotConfirmed) {
		t.Fatalf("must need the typed name: %v", err)
	}
	if e.s.CountEntries() != 3 {
		t.Fatal("nothing may be deleted before confirmation")
	}
	res, err := e.s.RemoveStaff(ann.ID, store.RemoveInput{Exported: true, TypedName: "Ann"})
	if err != nil || res != store.Deleted {
		t.Fatalf("%v %v", res, err)
	}
	if e.s.CountEntries() != 1 || e.s.CountLines() != 1 {
		t.Fatalf("entries %d lines %d", e.s.CountEntries(), e.s.CountLines())
	}
	if _, err := e.s.GetStaff(ann.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("staff row must be gone: %v", err)
	}
}

func TestPluginWritesOnlyItsOwnTables(t *testing.T) {
	e := newEnv(t, "GB")
	st := mustStaff(t, e, "Ann")
	_, _ = e.s.CreateFree(store.Actor{UserID: "u1"}, store.FreeInput{StaffID: st.ID, Payment: "cash", Lines: []store.LineInput{{Name: "Cut", Qty: 1, AmountMinor: 1}}})
	_, _ = e.s.RemoveStaff(st.ID, store.RemoveInput{Exported: true, TypedName: "Ann"})
	own := regexp.MustCompile(`(?i)^\s*(INSERT INTO|UPDATE|DELETE FROM)\s+(staff|entries|entry_lines|meta)\b`)
	if len(e.f.WriteStmts) == 0 {
		t.Fatal("no writes recorded")
	}
	for _, q := range e.f.WriteStmts {
		if !own.MatchString(q) {
			t.Fatalf("write outside the plugin's own tables: %s", q)
		}
	}
}
