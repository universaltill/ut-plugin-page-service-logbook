package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/universaltill/ut-plugin-page-service-logbook/locales"
)

var (
	bundleMu sync.Mutex
	bundles  = map[string]map[string]string{}
)

func bundle(lang string) map[string]string {
	bundleMu.Lock()
	defer bundleMu.Unlock()
	if b, ok := bundles[lang]; ok {
		return b
	}
	var b map[string]string
	if raw, err := locales.FS.ReadFile(lang + ".json"); err == nil {
		_ = json.Unmarshal(raw, &b)
	}
	bundles[lang] = b
	return b
}

// T resolves a key for the cases where the view vocabulary cannot (printf
// arguments in a table cell). Falls back to English, then to the key.
func T(locale, key string, args ...any) string {
	lang := strings.ToLower(strings.SplitN(strings.ReplaceAll(locale, "_", "-"), "-", 2)[0])
	s, ok := bundle(lang)[key]
	if !ok {
		s, ok = bundle("en")[key]
	}
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}
