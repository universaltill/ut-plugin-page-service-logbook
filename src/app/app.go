// Package app wires the logbook's domain (src/store) to the till's plugin
// seams: ui.view.ask / ui.action.ask, plugin.records.received.ask,
// export.requested.ask, tips.suggest.ask and the maintenance tick.
package app

import (
	"encoding/json"
	"strings"

	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/host"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/views"
)

// PluginID is manifest.json's id; event names in the plugin's own namespace
// start with it (ADR-0121).
const PluginID = "com.universaltill.service-logbook"

// Event names.
const (
	EvView        = "ui.view.ask"
	EvAction      = "ui.action.ask"
	EvRecords     = "plugin.records.received.ask"
	EvExport      = "export.requested.ask"
	EvTips        = "tips.suggest.ask"
	EvMaintenance = PluginID + ".maintenance.tick"
)

// View names (manifest entries).
const (
	ViewAdmin   = "logbook.admin"
	ViewEntry   = "logbook.entry"
	ViewReports = "logbook.reports"
)

// App is the handler set over a host.
type App struct {
	H host.Host
	S *store.Store
}

// New builds the App.
func New(h host.Host) *App { return &App{H: h, S: store.New(h)} }

// Handlers is every event the plugin answers; each has a manifest hook.
func Handlers(h host.Host) plugin.Handlers {
	a := New(h)
	return plugin.Handlers{
		EvView:        func(e plugin.Event) (any, error) { return a.View(e.Payload) },
		EvAction:      func(e plugin.Event) (any, error) { return a.Action(e.Payload) },
		EvRecords:     func(e plugin.Event) (any, error) { return a.RecordsApply(e.Payload) },
		EvExport:      func(e plugin.Event) (any, error) { return a.Export(e.Payload) },
		EvTips:        func(e plugin.Event) (any, error) { return a.TipsSuggest(e.Payload) },
		EvMaintenance: func(e plugin.Event) (any, error) { return a.Maintenance() },
	}
}

type operator struct {
	UserID      string   `json:"user_id"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type ask struct {
	View     string                     `json:"view"`
	Action   string                     `json:"action"`
	Params   map[string]string          `json:"params"`
	Form     map[string]json.RawMessage `json:"form"`
	Locale   string                     `json:"locale"`
	Operator *operator                  `json:"operator"`
}

// actor comes ONLY from the operator core stamped on the ask, never from the
// form. No operator = sign-in off = no role limits (documented core rule).
func (q ask) actor() store.Actor {
	if q.Operator == nil {
		return store.Actor{NoSignIn: true}
	}
	a := store.Actor{UserID: q.Operator.UserID}
	for _, p := range q.Operator.Permissions {
		if p == "reports" {
			a.Manager = true
		}
	}
	return a
}

func (q ask) manager() bool { a := q.actor(); return a.Manager || a.NoSignIn }

func (a *App) former(locale string) func(int) string {
	return func(n int) string { return T(locale, "service_logbook.staff.former", n) }
}

func decode(raw json.RawMessage) (ask, error) {
	var q ask
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &q); err != nil {
			return q, err
		}
	}
	return q, nil
}

// formStr reads a text/select/number form value as a trimmed string.
func (q ask) formStr(k string) string {
	var s string
	if json.Unmarshal(q.Form[k], &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

// formBool reads a toggle.
func (q ask) formBool(k string) bool {
	var b bool
	_ = json.Unmarshal(q.Form[k], &b)
	return b
}

// formMoney reads a money field (minor units); ok=false when absent or
// unreadable. Both {"minor":n,"currency":"X"} and a bare integer are read.
func (q ask) formMoney(k string) (int64, bool) {
	raw, present := q.Form[k]
	if !present {
		return 0, false
	}
	var o struct {
		Minor *int64 `json:"minor"`
	}
	if json.Unmarshal(raw, &o) == nil && o.Minor != nil {
		return *o.Minor, true
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	return 0, false
}

func unavailable() map[string]any {
	return views.Answer(views.Doc{Components: []any{views.Notice("error", "service_logbook.err.unavailable")}})
}
