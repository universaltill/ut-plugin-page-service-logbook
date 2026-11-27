package app

import (
	"encoding/json"
	"time"

	"github.com/universaltill/ut-plugin-page-service-logbook/src/export"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/store"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/views"
)

// View answers ui.view.ask (GET). Anything that fails to read the till's own
// data draws one "can't be shown" notice and stores nothing.
func (a *App) View(raw json.RawMessage) (any, error) {
	q, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return a.render(q, nil), nil
}

// Action answers ui.action.ask (POST): do the action, then draw the view.
func (a *App) Action(raw json.RawMessage) (any, error) {
	q, err := decode(raw)
	if err != nil {
		return nil, err
	}
	switch q.View {
	case ViewAdmin:
		return a.adminAction(q), nil
	case ViewEntry:
		return a.entryAction(q), nil
	case ViewReports:
		return a.render(q, nil), nil // filters re-draw with the posted range
	}
	return unavailable(), nil
}

func (a *App) render(q ask, notice []any) map[string]any {
	var doc views.Doc
	var err error
	switch q.View {
	case ViewAdmin:
		doc, err = a.adminDoc(q, notice, nil)
	case ViewEntry:
		doc, err = a.entryDoc(q, notice, nil)
	case ViewReports:
		doc, err = a.reportsDoc(q, notice)
	default:
		return unavailable()
	}
	if err != nil {
		return unavailable()
	}
	return views.Answer(doc)
}

// RecordsApply is plugin.records.received.ask on the main till. The sender's
// till id is core's from_till_id; the payload's own origin is never read.
func (a *App) RecordsApply(raw json.RawMessage) (any, error) {
	var in struct {
		Collection string     `json:"collection"`
		FromTill   string     `json:"from_till_id"`
		Payload    store.Wire `json:"payload"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Collection != "entries" {
		return map[string]any{"ok": false, "retry": false, "reason": "invalid"}, nil
	}
	if role, _ := a.H.Records().Role(); role == "replica" {
		return map[string]any{"ok": false, "retry": false, "reason": "not_main"}, nil
	}
	if ae := a.S.Apply(in.FromTill, in.Payload); ae != nil {
		return map[string]any{"ok": false, "retry": ae.Retry, "reason": ae.Reason}, nil
	}
	return map[string]any{"ok": true}, nil
}

// Export answers export.requested.ask (core gates Settings > Data to
// managers). entry_key picks the summary or the entries CSV.
func (a *App) Export(raw json.RawMessage) (any, error) {
	var in struct {
		From, To, EntryKey, Locale string
	}
	var w struct {
		From     string `json:"from"`
		To       string `json:"to"`
		EntryKey string `json:"entry_key"`
		Locale   string `json:"locale"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return map[string]any{"ok": false, "error": "invalid"}, nil
	}
	in.From, in.To, in.EntryKey, in.Locale = w.From, w.To, w.EntryKey, w.Locale
	kind := map[string]string{"logbook-summary-csv": "summary", "logbook-entries-csv": "entries"}[in.EntryKey]
	res, err := export.Build(a.S, kind, in.From, in.To, a.former(in.Locale))
	if err != nil {
		return map[string]any{"ok": false, "error": "invalid"}, nil
	}
	return res, nil
}

// TipsSuggest answers tips.suggest.ask for the payout form pre-fill (#7108,
// plugin half). Core keeps only real users and confirms with the manager.
func (a *App) TipsSuggest(raw json.RawMessage) (any, error) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if _, _, err := store.RangeFor("custom", timeZero, in.From, in.To); err != nil {
		return store.Tips{Suggestions: []store.TipSuggestion{}, Unmapped: []store.TipUnmapped{}}, nil
	}
	return a.S.SuggestTips(in.From, in.To)
}

// Maintenance (every 5 minutes): retry unforwarded rows, republish the
// snapshots (main), trim settled local copies (replica), remove lines of
// entries that were never committed.
func (a *App) Maintenance() (any, error) {
	a.S.RetryPending()
	a.S.PublishSnapshots()
	a.S.TrimReplica()
	a.S.SweepOrphans()
	return nil, nil
}

var timeZero = time.Time{}
