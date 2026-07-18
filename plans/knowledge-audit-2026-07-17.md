# Plan: knowledge-audit

## Run State
- `phase:` awaiting-approval
- `base:` 844bc62c (dirty working tree — unrelated dataconsole edits; this plan adds only plans/ files)
- `integration:` — (no BUILD started)
- `approved:` — (Owner Gate 1 pending)
- `codex:` register reviewed, resequenced into 20 slices + 2 external queues, 9 reshape/drop calls — plans/knowledge-audit-2026-07-17.assets/codex-review.md
- `next:` Karel reviews the Slice Register + Codex reshape calls; approves scope (esp. P0 band, corpus-integrity chassis, fragment tier) or trims it
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: knowledge subsystem carries no confirmed agent-facing falsehood, every authored content unit is deliverable by a production code path, and an enforcement ratchet (corpus-integrity chassis) prevents recurrence of the audited defect classes.

| obs | evidence |
|---|---|
| 95-agent audit, 12 dimensions, per-finding adversarial verify: 70 confirmed / 12 refuted | workflow run wf_63f56038-a63; full digest with file:line cites + verifier verdicts → assets/audit-digest.md |
| Register of 6 themes (T1 falsehoods · T2 reachability · T3 lint holes · T4 duplication · T5 spec drift · T6 coverage) | assets/register.md |
| Codex second opinion: taxonomy sound, priority order wrong; band P0-P3; 20-slice cut | assets/codex-review.md |

- AC1: P0 harmful falsehoods corrected (T1.1 dirty-tree deploy, T1.9 READY_TO_DEPLOY recovery ownership, T1.7 git-push-setup contract, T2.2 launch source-control catalog reachable) — planned evidence: updated atoms + re-blessed goldens + per-slice regression tests
- AC2: every atom is deliverable by a production code path — planned evidence: production delivery witnesses (per content unit), replacing fixture-only scenario assurance; the 4 stranded launch atoms wired via `launchStatus` envelope axis
- AC3: corpus-integrity chassis (CorpusSnapshot + named validators, structured diagnostics) covers the invariants of the 7 proposed bespoke guards — planned evidence: chassis test suite green; bespoke-guard proposals retired
- AC4: lint holes closed (spec-ID families, substring→set membership, fires-on-fixture for all 16 rule families, suppression liveness) — planned evidence: fixture rows fail before/pass after; 7 dead allowlist entries removed
- AC5: specs reconciled inside each owning slice (recipe-active ghosts, §11 documents all rule families, five-bucket §9.3) — planned evidence: spec-drift dimension re-run clean

**Non-goals**: cross-call token economics / dedup (existing backlog plan), authoring-side generator audit, AGENTS.md standing-instruction tier, error-path guidance corpus, eval-efficacy loop, sync/Strapi provenance (all = audit blind spots, logged below for a future frame) · payload-budget tuning beyond removing contradictions.
**Constraints**: Strapi + zerops-docs fixes need credentials/PR authority → separate owner-gated queues, never in an unattended run · recipe .md content ships via `zcp sync push recipes <slug>` · no platform mutations anywhere in this plan.
**Risk class**: medium — trigger: owner asked (orchestrated audit); BUILD touches production handler output (launch delivery) and lint/test infrastructure, no live-platform ops.
**Assumptions**:
- [VERIFIED] all 70 register findings — each independently re-verified against cited file:line by an adversarial agent (verdict notes in assets/audit-digest.md)
- [VERIFIED] `LaunchStatus` typed enum already exists and anticipates atom filtering — internal/topology/types.go:239 (Codex cite)
- [ASSUMED] Strapi/zerops-docs upstream copies match the local snapshot (audit ran on pulled state; provenance = known blind spot)

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| 70 findings hold at cited file:line | AC1-AC5 | repo | workflow wf_63f56038-a63 (12 auditors + 70 adversarial verifiers) | 70 CONFIRMED / 12 refuted | CONFIRMED | per-slice tests |
| register shape + slice cut sound | Slice Register | codex | codex-helper run 1784324320-12477-13099 | resequenced; 9 reshape/drop calls | CONFIRMED | — |
| lint substring hole fires | AC4 | repo | probe test in audit (modes:[stage]+configured fixture) | axis-conjunction did NOT fire | CONFIRMED | fires-on-fixture row |
| TestAtomAuthoringLint green with live P-PROD-2 violation | AC4 | repo | `go test ./internal/content/ -run TestAtomAuthoringLint` | PASS (violation present) | CONFIRMED | extended spec-ID regex + fixture |

## Slice Register
Codex cut — slice by mechanism and authority boundary, spec updates travel with their owning slice, each slice starts with its failing invariant or production-response test. Bands: P0 = harmful-truth containment, P1 = enforcement + delivery seams, P2 = remaining correctness, P3 = composition/hygiene.

| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | local-deploy-source-truth (T1.1 + allowlist text + local golden) [P0] | — | atoms/develop-local-workflow.md, atoms_lint_seed_allowlist.go, goldens | unit | autonomous | pending |
| S2 | ready-to-deploy-recovery-owner (T1.9 single-owner + failed-history coverage) [P0] | — | atoms/bootstrap-env-var-discovery.md, atoms/develop-ready-to-deploy.md, goldens | unit+tool | review | pending |
| S3 | axis-contract-registry (reflection completeness + corpus-integrity chassis) [P1] | — | workflow/synthesize*, content/ (new test chassis) | unit | autonomous | pending |
| S4 | launch-status-plumbing (LaunchStatus → envelope/axis/parser/matcher) [P1] | S3 | workflow/envelope.go, atom.go, synthesize.go | unit | autonomous | pending |
| S5 | launch-production-delivery (T2.1/T2.2, static guidance → Synthesize, witnesses) [P1] | S4 | tools/workflow_launch_production.go, launch atoms | unit+tool+integration | review | pending |
| S6 | canonical-knowledge-resolver (kind dispatch; uri=/recipe= equivalence) [P1] | — | tools/knowledge.go, knowledge/briefing.go | unit+tool | autonomous | pending |
| S7 | knowledge-mode-contract (mode= recipe-only, honest schema; T1.8 atom) [P2] | S6 | tools/knowledge.go, knowledge/engine.go, atoms/develop-knowledge-pointers.md | unit+tool | autonomous | pending |
| S8 | knowledge-search-contract (structured zero-hit recovery, five-mode description) [P2] | S6 | tools/knowledge.go | unit+tool | autonomous | pending |
| S9 | recipe-discovery-index (delete runtimeRecipeHints, derive from metadata; composite-type Decision Hints) [P1] | — | knowledge/engine.go, sections.go, briefing.go | unit | autonomous | pending |
| S10 | recipe-matcher-metadata (canonical framework aliases + real-corpus intent coverage) [P1] | — | knowledge/recipe_matcher.go (+ Strapi queue for tags) | unit | autonomous | pending |
| S11 | recipe-publishability (viable-body/YAML requirements, import.yml validation, no self-skip) [P1] | — | knowledge/recipe_lint_test.go, recipes_viability.go | unit | autonomous | pending |
| S12 | atom-lint-registry (T3.1-4, T3.8-10: rule teeth, suppression liveness) [P1] | — | content/atoms_lint*.go, launch-post-checklist.md | unit | autonomous | pending |
| S13 | atom-dependency-graph (co-render enforcement, no refs from reference sources, fix dangling edges) [P1] | S3 | workflow/atom_crossref_contract_test.go, affected atoms | unit | autonomous | pending |
| S14 | learned-once-reachability (http-diagnostic, steady-state env pointers, stranded content) [P2] | S13 | affected develop atoms, goldens | unit | autonomous | pending |
| S15 | git-push-setup-contract (T1.7 across export/launch/setup — probe-first, service-scope) [P0] | — | atoms/export-publish-needs-setup.md, export-publish.md, launch-source-control-required.md | unit | review | pending |
| S16 | bootstrap-composition (route-scoped verify, close ordering, same-render dedup) [P3] | S3 | bootstrap atoms, goldens | unit | autonomous | pending |
| S17 | develop-close-composition (broken/unconfigured wording, retired close-mode, implicit-webserver, dup variants) [P3] | S3 | develop close atoms, goldens | unit | autonomous | pending |
| S18 | classification-protocol (fragment tier, executable platform-env policy, five-bucket reconcile incl. spec §9.3) [P2] | S3 | new fragment mechanism, export/launch classify atoms, spec-workflows §9.3 | unit+tool | review | pending |
| S19 | micro-slices ×4: hostname-from-ValidateHostname · export zcli examples · ES-lag agent surface · .git-suffix agent surface [P2] | — | scattered, tiny | unit | autonomous | pending |
| S20 | spec-hygiene (residual dead enums, archived-plan cites, stale signatures/line-ranges) [P3] | S1-S19 | docs/spec-knowledge-*.md, workflow/atom.go | unit | autonomous | pending |
| Q1 | Strapi queue: nestjs Valkey auth + execOnce, matcher tags, nextjs-ssr truncation, solidstart H2 [P0 content] | — | recipes via `zcp sync push` | — | owner | pending |
| Q2 | zerops-docs queue: choose-database mutability, core-cap contradiction, grammar.md refs [P0 content] | — | ../zerops-docs PR | — | owner | pending |

Codex reshape calls folded in: T1.4→route-model fix not prose; T1.13→derive from ValidateHostname; T2.7→delete map, don't guard it; T2.10→narrow contract, don't implement stage; T4.7→delete runtime_resources.go unless a real consumer lands; T3.9 dropped as churn; byte-parity dropped for resolver-routing assertions; SingleOwnerFacts NOT expanded into a numeric-fact DB; pull-doc repetition deduped only where copies co-render or already drifted.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | P0 slice tests + re-blessed goldens | not-run | — |
| AC2 | production delivery witnesses over full atom corpus | not-run | — |
| AC3 | chassis validators green; 7 bespoke guards retired | not-run | — |
| AC4 | fires-on-fixture 16/16; allowlist liveness | not-run | — |
| AC5 | spec-drift re-audit clean | not-run | — |
| — | negative: modes:[stage]+configured fixture FAILS lint | not-run | — |
| — | negative: truncated recipe (no zerops.yaml + import.yml present) FAILS recipe lint | not-run | — |

## Promotion
- Contracts → corpus-integrity chassis architecture → `docs/spec-knowledge-distribution.md` (new §; witnesses/validators/fragment tier); launchStatus axis → spec-knowledge-distribution §2/§3 envelope tables
- Invariants → per slice (each starts with its failing test); key ones: delivery-witness reachability, uri=/recipe= resolver equivalence, axis classification registry, suppression liveness
- CLAUDE.md trap line (≤1): none new expected — candidates fold into chassis-enforced tests
- This plan → `plans/archive/` on LAND close

## Audit blind spots (logged for a future frame, out of scope here)
Strapi sync round-trip provenance · authoring-side generator (briefs*.go, record_fact loop) · cross-call token economics (knowledge_tracker only in bootstrap; ~5,250 tokens re-emitted per develop status call — existing plans/backlog/develop-guidance-cross-call-dedup.md) · AGENTS.md standing-instruction tier + refresh markerless-stale semantics · inline error-path guidance corpus (next_actions.go, convert.go) · eval harness as efficacy loop. Codex adds: tool-call example conformance lint · trust-boundary/prompt-injection review of Strapi-inserted content · URI stability (aliases/tombstones) · corpus revision manifest · transition-level reachability · persisted-state compat for new axes.
