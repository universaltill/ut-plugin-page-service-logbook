// Package locales embeds the plugin's own locale files so the plugin can
// resolve a key itself where the view vocabulary has no printf arguments
// (a table cell such as "Former staff #3").
package locales

import "embed"

// FS holds *.json (flat key -> string).
//
//go:embed *.json
var FS embed.FS
