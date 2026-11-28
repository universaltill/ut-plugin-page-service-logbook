# Changelog

## 0.3.0 (unreleased)

- Tips pre-fill is wired (ut-docs#7108, universal-till#2798): permission
  `payout:suggest` and the `tips.suggest.ask` hook are back in the manifest;
  the real host accepts them (realhost test).
- **IDs:** the till's WASI random source is deterministic (wazero default), so
  every event drew the same UUIDs and the second staff member or entry failed
  on the primary key. IDs now mix the wall clock and a sequence into the random
  bytes (found by the real-host test).
- ut-docs#7269: the sale's tip is claimed by the entry INSERT itself
  (`INSERT ... SELECT ... WHERE NOT EXISTS`, one statement), so two forwarded
  entries of one sale that carry different lines cannot both count the tip.
- Real-host tests for country-aware delete, business day from core, replica
  rejection feedback, the tip guard under concurrent applies, the tips answer.

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
