#!/usr/bin/env bash
# Real-host test: builds the REAL plugin.wasm and runs it in a universal-till
# checkout's wasm runtime against the real host functions (plugin SQLite
# migrations, db_exec/db_query, records channel). The host packages are
# internal to universal-till, so the test template is copied into the
# checkout (internal/plugins/logbook_realguest_test.go) for the run and
# removed again. Usage: scripts/realhost-test.sh <universal-till checkout>
# The checkout must be at or after the plugin-DB slice B merge (0e049b0) and
# carry shop.context.v2 country + business_date (universal-till#2800, ut-docs#7231).
set -euo pipefail
core="${1:?usage: realhost-test.sh <universal-till checkout>}"
here="$(cd "$(dirname "$0")/.." && pwd)"
[ -d "$core/internal/plugins" ] || { echo "not a universal-till checkout: $core" >&2; exit 2; }
dst="$core/internal/plugins/logbook_realguest_test.go"
cp "$here/realhost/logbook_realguest_test.go.tmpl" "$dst"
trap 'rm -f "$dst"' EXIT
cd "$core"
LOGBOOK_DIR="$here" go test ./internal/plugins -run 'TestLogbookRealGuest' -count=1 -v
