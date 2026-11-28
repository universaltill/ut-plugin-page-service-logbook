// Package store is the logbook's domain layer (ADR-0149): staff, entries,
// apply-on-main, reports. All I/O goes through src/host ports, so it is
// tested natively; SQL targets only this plugin's own tables.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/host"
)

// Errors callers map to notices.
var (
	ErrInvalid      = errors.New("invalid input")
	ErrNotFound     = errors.New("not found")
	ErrNotConfirmed = errors.New("deletion not confirmed")
	ErrForbidden    = errors.New("not allowed")
	ErrConflict     = errors.New("conflict")
)

// LocalTill is origin_till_id of a row authored on the till holding it.
const LocalTill = "local"

// Store is the domain handle.
type Store struct {
	h host.Host
	// The till's role, read at most once per event (BeginEvent clears it):
	// the host caps records_* calls at 64 per event, and a role read past
	// the cap fails closed to replica, so a per-row read turned a main till
	// into a replica half-way through a long list.
	roleRead bool
	role     string
	roleErr  error
}

// BeginEvent forgets what was read for the previous event (the role). The
// app calls it at the start of every event it handles.
func (s *Store) BeginEvent() { s.roleRead, s.role, s.roleErr = false, "", nil }

// Role is records_role, read once per event (see Store).
func (s *Store) Role() (string, error) {
	if !s.roleRead {
		s.role, s.roleErr = s.h.Records().Role()
		s.roleRead = true
	}
	return s.role, s.roleErr
}

// New returns a Store over h.
func New(h host.Host) *Store { return &Store{h: h} }

// ShopCtx is the part of shop.context.v2 the logbook needs (view:shop).
type ShopCtx struct {
	Currency     string `json:"currency_code"`
	Decimals     int    `json:"currency_decimals"`
	Locale       string `json:"locale"`
	Country      string `json:"country"`
	BusinessDate string `json:"business_date"`
}

// Shop reads shop.context.v2. An error is returned as is: callers that only
// need the country use Country, which fails closed.
func (s *Store) Shop() (ShopCtx, error) {
	raw, err := s.h.Views().Query("shop.context.v2", nil)
	if err != nil {
		return ShopCtx{}, err
	}
	var rows []ShopCtx
	if err := json.Unmarshal(raw, &rows); err != nil || len(rows) != 1 {
		return ShopCtx{}, fmt.Errorf("shop.context.v2: unexpected answer")
	}
	c := rows[0]
	c.Country = strings.ToUpper(strings.TrimSpace(c.Country))
	return c, nil
}

// Country is the shop's ISO country, "" when unknown or unreadable (every
// country rule fails closed on "").
func (s *Store) Country() string {
	c, err := s.Shop()
	if err != nil {
		return ""
	}
	return c.Country
}

// MoneyEntryForced reports whether the country forces linked money entry
// (DE, TR: ADR-0149 §5).
func MoneyEntryForced(country string) bool { return country == "DE" || country == "TR" }

func str(r host.Row, k string) string {
	switch v := r[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func num(r host.Row, k string) int64 {
	switch v := r[k].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

// CountEntries and CountLines are for tests and the maintenance sweep.
func (s *Store) CountEntries() int { return s.count("SELECT count(*) AS n FROM entries") }
func (s *Store) CountLines() int   { return s.count("SELECT count(*) AS n FROM entry_lines") }

func (s *Store) count(q string, args ...any) int {
	rows, err := s.h.DB().Query(q, args...)
	if err != nil || len(rows) == 0 {
		return -1
	}
	return int(num(rows[0], "n"))
}

// Meta keys.
const (
	MetaMoneyEntry = "money_entry"
	MetaCategories = "service_categories"
)

// GetMeta returns "" when unset.
func (s *Store) GetMeta(k string) string {
	rows, err := s.h.DB().Query("SELECT v FROM meta WHERE k = ?", k)
	if err != nil || len(rows) == 0 {
		return ""
	}
	return str(rows[0], "v")
}

// SetMeta upserts a setting.
func (s *Store) SetMeta(k, v string) error {
	_, err := s.h.DB().Exec("INSERT INTO meta (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v", k, v)
	return err
}

// IsReplica reports a joined (replica) till. An unreadable role counts as a
// replica: admin writes are then refused and new rows stay pending until the
// maintenance retry learns the real role (fail closed, never lose a row).
func (s *Store) IsReplica() bool {
	r, err := s.Role()
	return err != nil || r == "replica"
}

// EffectiveMode is the one money-entry mode the UI and the store both obey:
// forced linked in DE/TR; on a replica the main's config snapshot (the
// replica's own meta is never consulted); on the main the stored setting.
// Default free. Read per call, never cached (country can change).
func (s *Store) EffectiveMode() string {
	if MoneyEntryForced(s.Country()) {
		return "linked"
	}
	if s.IsReplica() {
		var c SnapConfig
		if ok, err := s.h.Records().SnapshotGet("config", &c); err == nil && ok && c.MoneyEntry == "linked" {
			return "linked"
		}
		return "free"
	}
	if s.GetMeta(MetaMoneyEntry) == "linked" {
		return "linked"
	}
	return "free"
}

// SetMoneyEntry refuses "free" where the country forces linked, no matter
// what the form posted. Settings live on the main only.
func (s *Store) SetMoneyEntry(mode string) error {
	if s.IsReplica() {
		return ErrForbidden
	}
	if mode != "free" && mode != "linked" {
		return ErrInvalid
	}
	if mode == "free" && MoneyEntryForced(s.Country()) {
		return ErrForbidden
	}
	return s.SetMeta(MetaMoneyEntry, mode)
}

// Categories are the chosen service categories; empty = all active items.
func (s *Store) Categories() []string {
	var out []string
	_ = json.Unmarshal([]byte(s.GetMeta(MetaCategories)), &out)
	return out
}

// EffectiveCategories: the config snapshot on a replica, else Categories.
func (s *Store) EffectiveCategories() []string {
	if s.IsReplica() {
		var c SnapConfig
		if ok, err := s.h.Records().SnapshotGet("config", &c); err == nil && ok {
			return c.Categories
		}
		return nil
	}
	return s.Categories()
}

// SetCategories stores the chosen category ids (main only).
func (s *Store) SetCategories(ids []string) error {
	if s.IsReplica() {
		return ErrForbidden
	}
	b, _ := json.Marshal(ids)
	return s.SetMeta(MetaCategories, string(b))
}
