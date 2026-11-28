# Changelog

## 0.2.0 (unreleased)

Wired to the real host on ABI 2: `src/host/wasm.go` binds `db_exec` /
`db_query` and the records channel through the Go guest SDK (no more
`ErrUnavailable`). Manifest `wasm_abi` 3 -> 2 (owner order 2026-11-28), adds
`retention.keep_years` 10 (uninstall keeps the data; the host then opens the
DB synchronous=FULL), drops `payout:suggest` and the `tips.suggest.ask` hook
until the core seam (ut-docs#7108) exists (the host refuses an unknown
permission). Reads `currency_decimals` (the real `shop.context.v2` field).
Real-host test `scripts/realhost-test.sh`.

## 0.1.0 (unreleased)

First version. Not publishable until universaltill/ut-docs#3157 slice B and
#3697 are released. Staff admin, free and linked entries, replica forwarding,
reports with CSV exports, comparisons and reconciliation, tips pre-fill.
