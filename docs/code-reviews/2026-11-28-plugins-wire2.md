# Review: logbook 0.3.0, tips guard and payout wiring (plugins-wire2, 2026-11-28)

Part of the plugins-wire2 combined review (full record: universal-till
`docs/code-reviews/2026-11-28-plugins-wire2.md`). Reviewer: Fable (code built
by a Sonnet agent). PR #2, not merged by this review, nothing published.

## What shipped
`payout:suggest` + `tips.suggest.ask` back in the manifest; the sale's tip
claimed by the entry INSERT itself (ut-docs#7269); ids mixed with the clock
and a sequence (deterministic WASI random on tills before ut-docs#7280);
real-host tests.

## Findings
| Sev | Finding | Result |
|---|---|---|
| important | `Apply`: when the guarded insert wrote nothing, a failed read-back was answered as terminal `already_linked`, so a replica showed "Not accepted by the main till" for an entry the main till holds | fixed: a read error is retried. `TestApplyUnreadableOutcomeAfterSkippedInsertIsRetried` failed before the fix |

## Checked and clean
The guard is one statement (`INSERT ... SELECT ... WHERE NOT EXISTS`), all
values bound; mutation (`tipGuarded` off) makes
`TestApplyTipCountedOnceWhenAnotherTipLandsBetweenCheckAndWrite` fail with the
tip counted twice. `validate.sh` pins the exact permission set including
`payout:suggest`. `wasm_abi` 2. `PUBLISH_ENABLED` gates stay as they were.

## Deferred
`min_pos_version` (0.40.0) is a placeholder: raise it to the first till
release that ships `shop_types` (universal-till #2809) and the tips seam
before any publish. DE/TR bundles via Weblate.

Pre-flight: CHANGELOG and README updated by the PR; no UI text change in the
fix; ADR-0149 notes in ut-docs #7281/#7282.

## Verdict
Approve after the fix.
