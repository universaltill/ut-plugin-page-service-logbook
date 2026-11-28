//go:build !wasip1

package store_test

import (
	"testing"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/hosttest"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
)

type env struct {
	f *hosttest.Fake
	s *store.Store
}

// newEnv: a main till in country, with two users and one currency.
func newEnv(t *testing.T, country string) env {
	t.Helper()
	f := hosttest.New(t)
	f.SetView("shop.context.v2", []map[string]any{{"currency_code": "GBP", "currency_decimals": 2, "locale": "en",
		"country": country, "business_date": "2026-11-27"}})
	f.SetView("users.list.v1", []map[string]any{
		{"id": "u1", "display_name": "Ann", "active": true},
		{"id": "u2", "display_name": "Bob", "active": false}})
	return env{f, store.New(f)}
}

func mustStaff(t *testing.T, e env, name string) store.Staff {
	t.Helper()
	st, err := e.s.AddStaff(store.StaffInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// seedStaff inserts a member with a fixed id (Apply tests address staff by
// id; the main only accepts records for staff it knows).
func seedStaff(t *testing.T, e env, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := e.f.DB().Exec(`INSERT INTO staff (id, display_name, status, created_at) VALUES (?, ?, 'active', '2026-11-27T00:00:00Z')`, id, "Staff "+id); err != nil {
			t.Fatal(err)
		}
	}
}
