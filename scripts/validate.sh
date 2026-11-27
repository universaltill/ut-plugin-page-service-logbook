#!/usr/bin/env bash
# Manifest sanity for the Service Staff Logbook WASM plugin (ADR-0149):
# marketplace-required fields, ABI 3 + plugin SQLite, the exact permission
# set (least privilege), views_used vs the permissions that open them, hooks
# vs src/app, the migrations, and every key the manifest names.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import json, os, re, sys, glob
m = json.load(open("manifest.json"))
errs = []
if m.get("id") != "com.universaltill.service-logbook": errs.append("id must be com.universaltill.service-logbook")
for f in ("name", "version", "description"):
    if not m.get(f): errs.append(f"missing {f}")
if not re.match(r'^\d+\.\d+\.\d+', m.get("version", "")): errs.append("bad version")
if m.get("canonical_type") != "page": errs.append("canonical_type must be 'page' (ADR-0002)")
if m.get("runtime") != "wasm": errs.append("runtime must be 'wasm' (ADR-0001)")
if m.get("wasm_abi") != 3: errs.append("wasm_abi must be 3")
if m.get("device_arch") != "any": errs.append("device_arch must be 'any'")
ep = (m.get("entrypoint") or "").lstrip("./")
if not ep.endswith(".wasm"): errs.append("wasm runtime needs a .wasm entrypoint")
elif not os.path.isfile(ep): errs.append(f"module not found: {ep} (run scripts/build.sh)")
want_perms = {"events:receive","db:own","ui:page","ui:slot:admin.pages","ui:slot:reports.panels","view:inventory",
              "view:users","view:sales","view:shop","records:sync","payout:suggest","schedule"}
perms = set(m.get("permissions", []))
if perms != want_perms:
    errs.append(f"permissions must be exactly the reviewed set; extra={sorted(perms-want_perms)} missing={sorted(want_perms-perms)}")
for p in perms:
    if p.startswith(("net:", "tcp:", "http", "write:", "ledger:")) or p.endswith(":write"):
        errs.append(f"no egress or core-write permission allowed: {p}")
if "retention" in m: errs.append("no retention.keep_years: a GB delete must stay possible (ADR-0149 §4)")
views = {"catalog.items.v1": "view:inventory", "users.list.v1": "view:users", "sales.receipts.v1": "view:sales", "shop.context.v2": "view:shop"}
if set(m.get("views_used", [])) != set(views): errs.append("views_used must be exactly " + ", ".join(sorted(views)))
for v, p in views.items():
    if p not in perms: errs.append(f"{v} needs {p}")
if (m.get("db") or {}).get("migrations") != "db/migrations": errs.append("db.migrations must be db/migrations")
mig = sorted(glob.glob("db/migrations/*.sql"))
if not mig: errs.append("no migrations")
for f in mig:
    if not re.match(r'^db/migrations/\d{4}_[a-z0-9_]+\.sql$', f): errs.append(f"bad migration name {f}")
    t = open(f).read()
    if re.search(r'(?im)^\s*(PRAGMA|BEGIN|COMMIT|ROLLBACK|ATTACH)\b', t): errs.append(f"{f}: PRAGMA/BEGIN/COMMIT/ATTACH are refused by the host")
rf = m.get("records_forward") or {}
for k in ("collections", "snapshots"):
    for n in rf.get(k, []):
        if not re.match(r'^[a-z][a-z0-9_]{0,31}$', n): errs.append(f"records_forward.{k}: bad name {n}")
    if len(rf.get(k, [])) > 8: errs.append(f"records_forward.{k}: at most 8")
hooks = {h.get("event") for h in m.get("hooks", [])}
src = "".join(open(f).read() for f in glob.glob("src/app/*.go") if not f.endswith("_test.go"))
for ev in ("ui.view.ask", "ui.action.ask", "plugin.records.received.ask", "export.requested.ask", "tips.suggest.ask", m["id"] + ".maintenance.tick"):
    if ev not in hooks: errs.append(f"missing hook {ev}")
for h in hooks:
    if h.startswith(m["id"] + ".") and not re.match(r'^[a-z0-9_-]+(\.[a-z0-9_-]+)+$', h): errs.append(f"job event {h} must be lower-case dotted segments")
    if h.split(".")[0] not in ("ui", "plugin", "export", "tips", "com") : errs.append(f"unexpected hook {h}")
for s in m.get("schedules", []):
    if s.get("event") not in hooks: errs.append(f"schedule {s.get('event')} has no hook")
en = json.load(open("locales/en.json"))
for e in m.get("entries", []):
    if e.get("label") not in en: errs.append(f"entry {e.get('key')} label {e.get('label')!r} missing from locales/en.json")
# Every key the Go code names exists (static ones; dynamic suffixes checked below).
for k in set(re.findall(r'"(service_logbook\.[a-z0-9_.]+)"', "".join(open(f).read() for f in glob.glob("src/**/*.go", recursive=True) if not f.endswith("_test.go")))):
    if k.endswith("."): continue
    if k not in en: errs.append(f"key {k} used in code but missing from locales/en.json")
mpv = tuple(int(x) for x in re.findall(r'\d+', m.get("min_pos_version", "0"))[:3])
if mpv < (0, 40, 0): errs.append("min_pos_version must name a till with the records channel, plugin SQLite and shop.context.v2.country")
if errs:
    print("FAIL: " + "; ".join(errs)); sys.exit(1)
print(f"ok {m['id']} v{m['version']}")
PY
