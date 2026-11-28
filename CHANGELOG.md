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

Concurrency fixes (the host runs a plugin's events concurrently): migration
0002 adds a unique (sale_ref, line_no) index on `entry_lines` so a sale line
is linked once; the orphan-line sweep keeps lines younger than an hour;
`records_role` is read once per event (the host caps records calls at 64);
`plugin.records.received.ask` applies nothing when the role is unreadable;
`TrimReplica` keeps rows missing from a full (200-row) status answer.

## 0.1.0 (unreleased)

First version. Not publishable until universaltill/ut-docs#3157 slice B and
#3697 are released. Staff admin, free and linked entries, replica forwarding,
reports with CSV exports, comparisons and reconciliation, tips pre-fill.
