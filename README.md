# Service Staff Logbook (`ut-plugin-page-service-logbook`)

A Universal Till plugin for service shops (hairdressers, nail bars, barbers):
a logbook of **who did which service, the tips they got, and what they took in
cash and card**. Staff do not need a till login. ADR-0149, epic
universaltill/ut-docs#3691.

> **Status: cannot run yet.** The plugin needs the plugin-SQLite host
> functions (universaltill/ut-docs#3157 slice B: `db_exec`, `db_query`,
> `db:own`, manifest `db.migrations`) and the core plugin-records channel
> (universaltill/ut-docs#3697). Neither is released. All domain code is built
> and tested natively against ports (`src/host`); the real binding in
> `src/host/wasm.go` fails closed (`ErrUnavailable`) until then.
> `min_pos_version` in `manifest.json` is a placeholder (`0.40.0`) that must be
> raised to the first till release holding all of that.
>
> **Not published.** The release workflows are gated on the repo variable
> `PUBLISH_ENABLED == 'true'` (unset by default): a merge cuts no tag and
> uploads nothing until the owner turns it on.
>
> **Nothing is installed or enabled by default.** A shop installs it on an
> explicit click; without the install there are no hooks, no tables and no
> records sent.

## What it does

| Card (universaltill/ut-docs) | Part |
|---|---|
| #3700 | repo, manifest, schema, staff admin page, country-dependent delete |
| #3701 | entry popup on the sell screen (free money entry) |
| #3702 | linked money entry: pick a till sale (forced in DE and TR) |
| #3703 | replica tills forward entries to the main till |
| #3704 | per-staff reports panel and CSV exports |
| #3705 | month/year comparisons and logbook-vs-till reconciliation |
| #7108 | tips pre-fill: the plugin half (`tips.suggest.ask`) |

* **Delete rule (ADR-0149 §4).** Country `GB`: the person and all their entries
  are erased after a typed-name confirmation. Any other or unknown country:
  the person is archived and shown as "Former staff #N", amounts stay. The
  country comes from `shop.context.v2.country` (core card pending) and fails
  closed to *archive*.
* **Money.** Integer minor units plus an ISO currency everywhere; totals never
  mix currencies. The tip is its own column, not part of the service total.
* **No write path to core.** The plugin holds no write or egress permission
  (`scripts/validate.sh` enforces the exact permission set) and never writes
  till sales or `worker_allocations`.
* **Offline-first.** Every action touches only local SQLite and the local
  records outbox; nothing waits on the network.
* **CSV is not a link on a page.** The view vocabulary has no download
  component. Totals and entries are exported through **Settings > Data >
  Export** (`export` entries, manager-gated by core).

## Layout

```
manifest.json            ABI 3 page plugin, db.migrations, records_forward
db/migrations/           0001_init.sql (one statement per db_exec)
locales/en.json          English only; de/tr arrive from Weblate (ADR-0160)
src/host/                ports: DB, Views, Records, Clock, IDs (+ wasm.go binding)
src/hosttest/            in-memory host for native tests (real SQLite via modernc)
src/store/               staff, entries, linked, apply-on-main, reports, tips
src/export/              CSV
src/views/  src/app/     view documents, ui.view/ui.action routing, handlers
```

## Develop

```
go vet ./... && GOOS=wasip1 GOARCH=wasm go vet ./...
gofmt -l .            # no output
go test ./...         # native, no till needed
scripts/build.sh && scripts/validate.sh && scripts/guard-plugin-i18n.sh
shellcheck scripts/*.sh && scripts/check-version-bump.test.sh
```

See `CLAUDE.md` for the rules of this repo. Legal-sensitive texts (the GB delete
warning, the archive notice) must pass legal review before the plugin is
published.

## License

MIT, see `LICENSE`.
