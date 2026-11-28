//go:build !wasip1

// Package hosttest is the in-memory host for native tests: a real SQLite
// file (modernc) running the plugin's own migration files, scripted core
// views, and an in-memory records outbox/snapshot with the contract's
// validation. Never shipped (excluded from the wasm build).
package hosttest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/universaltill/ut-plugin-page-service-logbook/db"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/host"
)

var (
	collRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	idRe   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
)

// Fake implements host.Host.
type Fake struct {
	mu      sync.Mutex
	sqldb   *sql.DB
	ViewMap map[string]json.RawMessage // view name -> JSON array answer
	ViewErr map[string]error
	Role    string // "main" (default) or "replica"
	RoleErr error  // when set, Role() fails (host busy / unreadable)
	// FailQuery: a Query whose SQL contains this text fails (read-error paths).
	FailQuery string
	Out       []Forwarded
	Snaps     map[string]json.RawMessage
	Status    map[string]host.RecordStatus // by record id
	Time      time.Time
	idN       int
	// Calls to db for write-path assertions.
	WriteStmts []string
	// BeforeExec, when set, runs once before the first Exec whose SQL
	// contains BeforeExecOn: another event's write landing between this
	// event's check and its write (the host runs events concurrently).
	BeforeExec   func()
	BeforeExecOn string
	// RecordsCap, when > 0, is the host's per-event cap on records_* calls:
	// call RecordsCap+1 fails (the real host refuses it). RecordsCalls counts
	// them; a test sets it to 0 at the start of each simulated event.
	RecordsCap   int
	RecordsCalls int
}

// Forwarded is one queued outbox row.
type Forwarded struct {
	Collection, ID string
	Payload        json.RawMessage
}

// New opens a fresh SQLite database with every migration applied.
func New(t testing.TB) *Fake {
	t.Helper()
	d, err := sql.Open("sqlite", fmt.Sprintf("file:%s/logbook.db?_pragma=busy_timeout(5000)", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	f := &Fake{sqldb: d, ViewMap: map[string]json.RawMessage{}, ViewErr: map[string]error{},
		Role: "main", Snaps: map[string]json.RawMessage{}, Status: map[string]host.RecordStatus{},
		Time: time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)}
	if err := f.Migrate(); err != nil {
		t.Fatal(err)
	}
	return f
}

// Migrate applies the migration files in order, one statement per exec, and
// records them in _ut_migrations so a re-run is a no-op (as the host does).
func (f *Fake) Migrate() error {
	if _, err := f.sqldb.Exec(`CREATE TABLE IF NOT EXISTS _ut_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	ents, err := db.Migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range ents {
		var n int
		_ = f.sqldb.QueryRow(`SELECT count(*) FROM _ut_migrations WHERE name=?`, e.Name()).Scan(&n)
		if n > 0 {
			continue
		}
		raw, _ := db.Migrations.ReadFile("migrations/" + e.Name())
		for _, stmt := range strings.Split(string(raw), ";\n") {
			if s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(stmt), ";")); s != "" {
				if _, err := f.sqldb.Exec(s); err != nil {
					return fmt.Errorf("%s: %w", e.Name(), err)
				}
			}
		}
		if _, err := f.sqldb.Exec(`INSERT INTO _ut_migrations(name) VALUES (?)`, e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// SetView scripts a view's JSON array answer.
func (f *Fake) SetView(name string, v any) {
	b, _ := json.Marshal(v)
	f.ViewMap[name] = b
}

func (f *Fake) DB() host.DB           { return fakeDB{f} }
func (f *Fake) Views() host.Views     { return fakeViews{f} }
func (f *Fake) Records() host.Records { return fakeRecords{f} }
func (f *Fake) Clock() host.Clock     { return fakeClock{f} }
func (f *Fake) IDs() host.IDs         { return fakeIDs{f} }

type fakeDB struct{ f *Fake }

func (d fakeDB) Exec(q string, args ...any) (int64, error) {
	low := strings.ToLower(strings.TrimSpace(q))
	for _, bad := range []string{"begin", "commit", "rollback", "pragma", "attach"} {
		if strings.HasPrefix(low, bad) {
			return 0, fmt.Errorf("transaction control / %s refused by the host", bad)
		}
	}
	if strings.Contains(strings.TrimSuffix(strings.TrimSpace(q), ";"), ";") {
		return 0, fmt.Errorf("one statement per db_exec")
	}
	if fn := d.f.BeforeExec; fn != nil && strings.Contains(q, d.f.BeforeExecOn) {
		d.f.BeforeExec = nil
		fn()
	}
	d.f.WriteStmts = append(d.f.WriteStmts, q)
	r, err := d.f.sqldb.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func (d fakeDB) Query(q string, args ...any) ([]host.Row, error) {
	if d.f.FailQuery != "" && strings.Contains(q, d.f.FailQuery) {
		return nil, fmt.Errorf("query refused by the test")
	}
	rows, err := d.f.sqldb.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []host.Row
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		r := host.Row{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				r[c] = string(b)
			} else {
				r[c] = vals[i]
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type fakeViews struct{ f *Fake }

func (v fakeViews) Query(name string, _ any) (json.RawMessage, error) {
	if err := v.f.ViewErr[name]; err != nil {
		return nil, err
	}
	b, ok := v.f.ViewMap[name]
	if !ok {
		return nil, fmt.Errorf("view %s not scripted", name)
	}
	return b, nil
}

type fakeRecords struct{ f *Fake }

func (r fakeRecords) Forward(c, id string, payload any) (bool, error) {
	if err := r.f.recordsCall(); err != nil {
		return false, err
	}
	if !collRe.MatchString(c) || !idRe.MatchString(id) {
		return false, fmt.Errorf("bad input")
	}
	b, err := json.Marshal(payload)
	if err != nil || len(b) > 64<<10 || len(b) == 0 || b[0] != '{' {
		return false, fmt.Errorf("bad payload")
	}
	if r.f.Role != "replica" {
		return false, nil
	}
	for _, o := range r.f.Out {
		if o.Collection == c && o.ID == id {
			return true, nil
		}
	}
	r.f.Out = append(r.f.Out, Forwarded{c, id, b})
	return true, nil
}
func (r fakeRecords) Role() (string, error) {
	if err := r.f.recordsCall(); err != nil {
		return "", err
	}
	if r.f.RoleErr != nil {
		return "", r.f.RoleErr
	}
	return r.f.Role, nil
}
func (r fakeRecords) Status(string) ([]host.RecordStatus, error) {
	if err := r.f.recordsCall(); err != nil {
		return nil, err
	}
	var out []host.RecordStatus
	for _, s := range r.f.Status {
		out = append(out, s)
	}
	// the host answers oldest first and at most StatusLimit rows (ids stand
	// in for age here)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > host.StatusLimit {
		out = out[:host.StatusLimit]
	}
	return out, nil
}
func (r fakeRecords) SnapshotSet(c string, v any) (bool, error) {
	if err := r.f.recordsCall(); err != nil {
		return false, err
	}
	if r.f.Role == "replica" {
		return false, nil
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) > 64<<10 {
		return false, fmt.Errorf("bad snapshot")
	}
	r.f.Snaps[c] = b
	return true, nil
}
func (r fakeRecords) SnapshotGet(c string, out any) (bool, error) {
	if err := r.f.recordsCall(); err != nil {
		return false, err
	}
	b, ok := r.f.Snaps[c]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

type fakeClock struct{ f *Fake }

func (c fakeClock) Now() time.Time { return c.f.Time }

type fakeIDs struct{ f *Fake }

func (i fakeIDs) NewID() string {
	i.f.idN++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", i.f.idN)
}

func (f *Fake) recordsCall() error {
	f.RecordsCalls++
	if f.RecordsCap > 0 && f.RecordsCalls > f.RecordsCap {
		return fmt.Errorf("records: per-event call cap %d reached", f.RecordsCap)
	}
	return nil
}
