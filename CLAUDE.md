# ut-plugin-page-service-logbook: rules for working in this repo

WASM plugin (ADR-0009 one repo per plugin, ADR-0001, ADR-0121, ADR-0149) on the
official guest SDK `github.com/universaltill/universal-till/sdk/plugin`; never
hand-rolled `//go:wasmimport`. Staff logbook for service shops. Rules binding
here:

- **Ports first.** Domain code (`src/store`, `src/app`, `src/export`) talks only
  to `src/host` interfaces. The only file that binds the real till is
  `src/host/wasm.go`; capabilities that do not exist yet (plugin SQLite,
  records channel) return `ErrUnavailable` so actions fail closed. Tests run
  natively on `src/hosttest` (`*_test.go` and the fake are `!wasip1`).
  TDD: failing test first.
- **No write path to core, no egress.** The exact permission set is pinned in
  `scripts/validate.sh`; widening it is a reviewed change. Never write till sales
  or `worker_allocations`. Core data only through read views.
- **Operator comes only from the `operator` core stamps** on `ui.view.ask` /
  `ui.action.ask`; never from `form`. Manager = holds `reports`. No operator =
  sign-in off = no role limits. Re-check on every action, not only on the page.
- **Money is int64 minor units + currency**, tip separate from the service total,
  per-currency sums, no floats (percent change is integer basis points).
- **Country rules fail closed** (empty or unreadable country = archive, not
  delete; DE/TR forced linked). Read the country per call, never cache it.
- **Migrations are append-only once released** (ADR-0100): add `000N_*.sql`.
  One statement per `db_exec`; no PRAGMA/BEGIN. Entry writes: lines first, the
  `entries` row last (the commit point).
- **i18n:** every user-facing string is a `service_logbook.*` key in
  `locales/en.json` (English only; de/tr come from Weblate, ADR-0160). View text
  is `{key}` or a literal of user data only. `scripts/guard-plugin-i18n.sh`.
- **CSV is never a page link**; exports answer `export.requested.ask`.

## Before committing

`gofmt -l .` (no output), `go vet ./...`, `GOOS=wasip1 GOARCH=wasm go vet ./...`,
`go test ./...`, `scripts/build.sh`, `scripts/validate.sh`,
`scripts/guard-plugin-i18n.sh`, `shellcheck scripts/*.sh`,
`scripts/check-version-bump.test.sh`. Feature branch, never `main`.

## Releases

Tag `v<version>` matching `manifest.json`, cut by `auto-tag-release.yml` on
merge. Any PR that changes what ships (`manifest.json`, `README.md`, `LICENSE`,
`locales/`, `src/`, `db/`, `go.mod`/`go.sum`, `scripts/build.sh`,
`scripts/package.sh`) must bump `manifest.json`'s `version`
(`scripts/check-version-bump.sh`). **Do not publish** until #3157 slice B and
the records channel are released and the legal texts are reviewed. Enforced:
`auto-tag-release.yml` and `release.yml` run only when the repo variable
`PUBLISH_ENABLED` is `'true'` (unset by default = no tag, no release, no
marketplace upload). Setting it is an owner decision.
