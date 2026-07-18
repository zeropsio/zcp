# R2 single-owner — verification outcome: proven-drift ABSENT, no new machinery

**Date:** 2026-06-04
**Scope:** R2 from `plans/zcp-consolidation-2026-06-04.md`, under the Codex reshape
("lint-first; add narrow tokens ONLY where drift is PROVEN; NOT a broad
OwnerBindings + 80-atom rewrite; {type-form:} from live schema.Cache breaks
golden canonicity — pin canonical if ever added").

## What the reshape gated R2 on
"Add narrow tokens only where drift is PROVEN. The ~80-atom rewrite is scope
inflation until an inventory proves which atoms actually lie." So R2 starts with
an INVENTORY; new machinery is justified only by proven drift.

## Inventory result — drift is absent on the current corpus
The plan's cited drift cases are already resolved by the prior knowledge-arch arc
(tasks P5/P6/P7) + the existing owner-mirror lints:

| Cited drift | Status |
|---|---|
| `develop-checklist` asserts healthCheck **REQUIRED** vs schema optional | FIXED — atoms now say "recommended" (simple), "no healthCheck" (dev keepalive), "real run.start + healthCheck" (stage). Accurate. |
| ~48 atoms ship bare `nodejs@22` vs live composite `alpine/nodejs@22` | NON-DRIFT — `topology.CanonicalBareForm` matching means a bare authored form matches a composite live schema (CLAUDE.md schema invariant). The 28 `@version` tokens that remain are in EXAMPLE scaffolds (`scaffold-zerops-yaml.md` etc.) — the "unless example-marked" exemption; the agent replaces them with its real runtime, and deploy validates live. |
| Stale `action="X"` / `strategy="X"` literals | NETTED — `staleActionViolations` / `staleStrategyViolations` already flag retired values; their accepted-sets are owner-mirrors pinned by `TestAtomLintAcceptedActionsMatchDispatcher` / `…StrategiesMatchGate`. |
| `bootstrap-runtime-classes` hard-codes versions | NON-DRIFT — it DEFERS: "Pick runtime types from the live Zerops catalog (check `zerops_knowledge` for current versions)." Exemplary Information-Contract form. |
| deploy-failure Suggestion/NextActions hand-authored | RE-SOURCED from the classifier owner (P7). |

## Why no new lint / token was added
- A hard-coded-`@version`-in-prose lint would false-positive on all 28 legitimate
  example-scaffold tokens (the reshape's explicit anti-pattern: "scope inflation").
- A `{type-form:}` token would make concrete scaffold EXAMPLES abstract (worse), and
  bare forms already validate live — no drift to fix. Recorded decision if ever
  added (per Karel's lock): pin its goldens against `schema.CanonicalAPIHost`
  (embedded/canonical), never the host-varying live cache.
- `internal/content` is a leaf package (cannot import `internal/schema`); a
  schema-derived lint would need an owner-mirror + sync test — only worth it for a
  PROVEN drift class, and none exists.

## Conclusion
R2's single-owner discipline (the tell derives from the check) is already enforced
for every class that actually drifts: action/strategy enums (lint + owner-sync
test), deploy-failure (classifier owner), tool schemas (owner-extraction), and the
schema is the single client-side source of truth validated live at deploy/import.
The remaining sourced values in atoms are legitimate examples handled by
CanonicalBareForm + live validation. Adding OwnerBindings / a type-form token / an
80-atom rewrite is unjustified by the evidence and would re-introduce the very
drift surface (a hand-maintained canonical mirror) the root exists to remove.

**No code shipped for R2 — by design, per the reshape's prove-drift-first gate.**
