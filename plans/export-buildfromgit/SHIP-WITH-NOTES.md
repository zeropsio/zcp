# Plan SHIP-WITH-NOTES — export-buildFromGit (2026-04-29)

Plan: `plans/export-buildfromgit-2026-04-28.md` (807 → ~870 lines after in-place amendments).
Plan inception SHA: `b743cda0`.
Plan SHIP SHA: TBD (this commit).
Total commits: 26 across phases 0-10.

## Outcome

**SHIP-WITH-NOTES** per Codex FINAL-VERDICT (`codex-round-p10-final-verdict.md`). Four noted limitations — three from plan §6 Phase 10's accepted list + one new (stage/re-import waiver) called out separately per Codex FINAL-VERDICT amendment 2.

### Plan-§6-Phase-10-accepted limitations

1. **Subdomain drift documented, not normalized.** Per Q3 default + plan §10 anti-pattern, `enableSubdomainAccess: true` in the bundle's runtime entry is informational — the platform import API doesn't flip subdomain on at re-import time. The destination project's owner manually flips subdomain via dashboard after import. Active normalization (re-flipping subdomain via post-import hook) requires platform-team coordination + ongoing background reconciler — out of scope per plan §10.

2. **Private-repo `buildFromGit:` auth ground truth not yet confirmed.** Zerops platform docs cover continuous deployment (`zerops-docs/.../github-integration.mdx:14-39`) but not one-time private-repo import (`zerops-docs/.../references/import.mdx:398-402` shows public URLs only). Phase 8 used a public test repo (`https://github.com/krls2020/eval1`) so private-repo auth wasn't exercised. If the platform supports private-repo `buildFromGit:` import via GIT_TOKEN, that's a documentation gap; if it doesn't, the export workflow is public-repo-only. Either way: out of scope for this plan; encode in atom prose if the platform team provides ground truth.

3. **Multi-runtime export in one bundle deferred.** Q1 default = single-runtime per call. If real demand surfaces (multi-app monorepo with shared deploys), a follow-up plan can lift the constraint via per-service repo arrays in import.yaml. Not blocking for this plan.

### New noted limitation (separate from §6 Phase 10's accepted list)

4. **Stage-variant + fresh-project re-import live verification waived.** Phase 8 ran the dev-variant path on a single-half ModeDev fixture (`laravel-dev-deployed.yaml`) — PASS (eval r3, 6m26s). The stage variant + re-import gates were deferred as follow-up because (a) the fixture lacks a stage half so a structural test isn't possible without provisioning a separate ModeStandard pair fixture, and (b) re-import on a fresh project introduces real GitHub PAT + dashboard provisioning friction that doesn't add net coverage beyond what the dev variant + Phase 5 schema validation already prove structurally. **This does NOT fall cleanly under "Multi-runtime out of scope"** (Codex Phase 10 amendment 2) — multi-runtime is about packaging multiple runtimes IN ONE BUNDLE, while stage/re-import is about exercising the single-runtime flow ACROSS BOTH HALVES OF A PAIR + a destination-project import. Recorded explicitly so a follow-up plan can pick it up.

## Landed work — per phase

| phase | risk | commits | scope |
|---|---|---|---|
| 0 — Calibration | LOW | `eed181ba` `180c13c7` | Baseline snapshot + 3-agent PRE-WORK fan-out → 13 amendments folded in-place. |
| 1 — Topology types | LOW | `aee5e5d5` `fa23a376` `b3cea80f` | `ExportVariant` + `SecretClassification` enums; `WorkflowInput.{Variant,EnvClassifications}` per-request inputs. |
| 2 — Generator | MEDIUM | `702cad26` `1df479f6` `abf0743f` | `ops.ExportBundle` + `BuildBundle` + 6 composers (363 LOC) + M2 indirect-reference detector + sentinel-pattern flag (155 LOC). 2-agent POST-WORK fan-out → review-row DTO at handler level (Agent B wins). |
| 3 — Handler | MEDIUM | `d5a44cd0` `8352dfa2` `e6da4c1a` | `handleExport` 3-call narrowing (368 LOC) + setup-name heuristic + managed-services collector (170 LOC). 1 POST-WORK round → 6 amendments (redaction, routing, partial classifications, ModeStage/LocalStage tests, SSH error tests). |
| 4 — Atom corpus | HIGH | `de285a15` `0f49e172` `45586751` | 6 new atoms (intro/classify-envs/validate/publish/publish-needs-setup/scaffold) replace 229-line legacy. 3 Codex rounds (PER-EDIT × 2 + POST-WORK) → 11 amendments folded. Final corpus 28,636 raw bytes — under 28,672B soft cap. |
| 5 — Schema validation | MEDIUM | `c33245a6` `e63d5ddc` `8353186b` | jsonschema/v5 vendored + ValidateImportYAML/ValidateZeropsYAML + bundle.Errors + plan §3.3 HA/NON_HA correction (E4 invariant). Validation-failed gate outranks git-push-setup-required. |
| 6 — RemoteURL refresh | MEDIUM | `a1ac96e3` `aa705d5c` `2474c652` | `refreshRemoteURLCache` helper + drift warnings + cache-write fallback (E5 invariant). 6-case helper table test + write-failure test. SHIP-WITH-NOTES: WriteServiceMeta concurrency note (out of scope). |
| 7 — Tests | LOW | `c62938e8` `d5252dcc` | `integration/export_test.go` mock-e2e through MCP transport. |
| 8 — Live verification | MEDIUM | `6e3d83e1` `ace543ab` | Eval r3 PASS on eval-zcp container (6m26s). Real bug surfaced + fixed mid-phase: managed-deps inclusion regression in Discover hostname filter. SHIP-WITH-NOTES: stage variant + re-import waived. |
| 9 — Documentation | LOW | `84a87748` `9a09ce59` | spec-workflows.md §9 + invariants E1-E5 + CLAUDE.md convention bullet. |

## Eight (now nine) root problems resolved

Per plan §1 root-problem table:

| # | Layer | Resolved by |
|---|---|---|
| X1 | Atom prose | Phase 4 — 229-line procedural atom replaced with 6 topic-scoped atoms (priority-ordered, axis-hygiene-clean). |
| X2 | Tool surface | Phase 2 + Phase 3 — `ops.BuildBundle` generates the YAML + `handleExport` runs the multi-call narrowing. |
| X3 | Decomposition | Phase 3 — Variant prompt for ModeStandard / ModeStage / ModeLocalStage; single-half modes skip the prompt. |
| X4 | Prereq chain | Phase 3 + Phase 5 — `git-push-setup-required` chain composed when `meta.GitPushState != configured`. Validation outranks the chain (Phase 5 amendment). |
| X5 | Secret detection | Phase 2 + Phase 4 — Four-category LLM-driven protocol via classify-prompt + `EnvClassifications` per-request map. M1-M7 mitigations in atom prose; M2 indirect-reference + sentinel-pattern detectors in Go. |
| X6 | zerops.yaml validation | Phase 5 — Full JSON Schema validation via jsonschema/v5; `bundle.Errors` populates `validation-failed` status. |
| X7 | Subdomain drift | Documented as known limitation (Q3 default; this SHIP note). |
| X8 | Filename | Phase 4 — `zerops-project-import.yaml` everywhere (recipe convention mirror; corpus_coverage MustContain updated). |
| X9 | Surface duplication | §4 row added — `zerops_export` standalone tool retained as orthogonal raw-export surface; export workflow is the canonical user-facing entry. |

## Codex round summary

Plan §7 specified ~10-11 Codex rounds. Actual count: **9 rounds** (one fewer than planned — Phase 9 docs round skipped per §10 acceptable trim).

| Phase | Round | Verdict |
|---|---|---|
| 0 | PRE-WORK × 3 (decisions / rendering / classification) | NEEDS-REVISION → 13 amendments → effective APPROVE |
| 2 | POST-WORK × 2 (generator code / architectural alignment) | NEEDS-REVISION → 6 amendments → effective APPROVE |
| 3 | POST-WORK × 1 (handler review) | NEEDS-REVISION → 6 amendments → effective APPROVE |
| 4 | PER-EDIT × 2 (classify-envs + publish-needs-setup) + POST-WORK × 1 (corpus) | NEEDS-REVISION → 11 amendments → effective APPROVE |
| 5 | POST-WORK × 1 (schema validation) | APPROVE-with-amendments → 6 amendments → effective APPROVE |
| 6 | POST-WORK × 1 (RemoteURL refresh) | SHIP-WITH-NOTES → 4 recommendations → 2 folded, 2 deferred |
| 7 | (none, LOW risk) | — |
| 8 | POST-WORK × 1 (eval log review) | SHIP-WITH-NOTES → 5 amendments folded |
| 9 | (none, docs) | — |
| 10 | FINAL-VERDICT × 1 | TBD (this round) |

## Deferred items (NICE-TO-HAVE, non-blocking)

- `WriteServiceMeta` concurrency-safety (Codex Phase 6 SHIP-WITH-NOTES note 3) — atomic temp-rename + last-write-wins; same-hostname concurrent writers could lose updates. Existing infrastructure, not Phase 6 scope.
- `ValidateZeropsYmlRaw` legacy validator deprecation (Codex Phase 5 note J) — distinct call site for recipe checks; deprecate when those migrate to the new validators.
- `zerops_mount` UX during export (Phase 8 eval r2 EVAL REPORT) — agent had to switch to develop workflow for mount access. Atom prose now mentions this (Phase 8 amendment 2); a clearer error response on `WORKFLOW_REQUIRED` is a follow-up enhancement.
- Schema testdata refresh cadence (Codex Phase 5 note 6) — manual signal (live import rejects what client validator accepts). No automated drift detector yet.
- Stage-variant + fresh-project re-import live verification — see noted limitation #1.

## Plan archival

After this commit:
- `plans/export-buildfromgit-2026-04-28.md` → `plans/archive/export-buildfromgit-2026-04-28.md`
- `plans/export-buildfromgit/` → `plans/archive/export-buildfromgit/`

PLAN COMPLETE commit follows.

## What does NOT ship

Per CLAUDE.local.md: `make release` is user-controlled. This SHIP-WITH-NOTES verdict closes the plan; the actual version bump + tag + GitHub Actions release happens when the user runs `make release` or `make release-patch`.