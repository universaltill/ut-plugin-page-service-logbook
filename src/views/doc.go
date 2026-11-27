// Package views builds view documents (ut-docs reference/plugin-views.md):
// data only, no HTML/JS/CSS. Every text is a locale key; the only literals
// are user data (names, item names, notes).
package views

// Doc is a version-1 view document.
type Doc struct {
	Version    int   `json:"version"`
	Title      any   `json:"title,omitempty"`
	Components []any `json:"components"`
}

// Answer wraps a document as the ask answer.
func Answer(d Doc) map[string]any { d.Version = 1; return map[string]any{"document": d} }

// K is a key text; L a literal text (user data only).
func K(key string) map[string]string { return map[string]string{"key": key} }
func L(s string) map[string]string   { return map[string]string{"literal": s} }

type m = map[string]any

func Heading(key string) m { return m{"type": "heading", "text": K(key)} }
func Text(key string) m    { return m{"type": "text", "text": K(key)} }
func Notice(level, key string) m {
	return m{"type": "notice", "level": level, "text": K(key)}
}
func LiteralNotice(level, s string) m { return m{"type": "notice", "level": level, "text": L(s)} }
func Empty(titleKey, bodyKey string) m {
	return m{"type": "empty_state", "title": K(titleKey), "body": K(bodyKey)}
}
func Button(action, labelKey, style string) m {
	return m{"type": "button", "action": action, "label": K(labelKey), "style": style}
}

// Opt is a select option; Label is user data (literal) unless LabelKey is set.
type Opt struct{ Value, Label, LabelKey string }

func (o Opt) json() m {
	if o.LabelKey != "" {
		return m{"value": o.Value, "label": K(o.LabelKey)}
	}
	return m{"value": o.Value, "label": L(o.Label)}
}

// Field helpers (label is always a key).
func Select(name, labelKey string, opts []Opt, value string) m {
	o := make([]any, 0, len(opts))
	for _, x := range opts {
		o = append(o, x.json())
	}
	f := m{"name": name, "label": K(labelKey), "kind": "select", "options": o}
	if value != "" {
		f["value"] = value
	}
	return f
}
func TextField(name, labelKey, value string) m {
	f := m{"name": name, "label": K(labelKey), "kind": "text"}
	if value != "" {
		f["value"] = value
	}
	return f
}
func Number(name, labelKey, value string) m {
	f := m{"name": name, "label": K(labelKey), "kind": "number"}
	if value != "" {
		f["value"] = value
	}
	return f
}
func Money(name, labelKey string, minor int64, set bool) m {
	f := m{"name": name, "label": K(labelKey), "kind": "money"}
	if set {
		f["value"] = minor
	}
	return f
}
func Toggle(name, labelKey string, on bool) m {
	return m{"name": name, "label": K(labelKey), "kind": "toggle", "value": on}
}

// Form is a form component.
func Form(action, submitKey string, fields ...m) m {
	f := make([]any, len(fields))
	for i, x := range fields {
		f[i] = x
	}
	return m{"type": "form", "action": action, "submit": K(submitKey), "fields": f}
}

// Col kinds.
func ColText(key string) m   { return m{"label": K(key), "kind": "text"} }
func ColNumber(key string) m { return m{"label": K(key), "kind": "number"} }
func ColMoney(key string) m  { return m{"label": K(key), "kind": "money"} }

// Table builds a table; rows are [][]any of already-typed cell values.
func Table(cols []m, rows [][]any) m {
	c := make([]any, len(cols))
	for i, x := range cols {
		c[i] = x
	}
	if rows == nil {
		rows = [][]any{}
	}
	return m{"type": "table", "columns": c, "rows": rows}
}

// Cash is a money cell value.
func Cash(minor int64, currency string) m { return m{"minor": minor, "currency": currency} }

// ToggleL is a toggle whose label is user data (a category name).
func ToggleL(name, label string, on bool) m {
	return m{"name": name, "label": L(label), "kind": "toggle", "value": on}
}
