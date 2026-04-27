# Phase 3 tracker v2 — Axis L (title hygiene) (followup plan)

Started: 2026-04-27
Closed: 2026-04-27

> Phase contract per `plans/atom-corpus-hygiene-followup-2026-04-27.md`
> §5 Phase 3 + §3 Axis L (post-Phase-0 amendment 2). Token-level
> title edits per Codex C2: env-only tokens DROP; mode/runtime/
> strategy distinguishers KEEP; mechanism payload (e.g.
> `GIT_TOKEN + .netrc`) KEEP. Mechanical phase; no mandatory Codex
> per-edit round.

## Codex rounds

(Phase 3 has no mandated Codex rounds per §10.1. Per amendment 2:
"low risk; AST atom-ID pins are immune". Verify gate via probe +
tests.)

## Edits (frontmatter `title:` + H1/H2/H3 headers)

### Frontmatter `title:` field edits (token-level)

| # | atom-id | pre-edit title | post-edit title | rationale |
|---|---|---|---|---|
| T1 | bootstrap-discover-local | "Bootstrap — local-mode discovery addendum" | "Bootstrap — discovery addendum" | drop env-only `local-mode` token (env=[local] axis-implied) |
| T2 | bootstrap-provision-local | "Bootstrap — local-mode provision addendum" | "Bootstrap — provision addendum" | drop env-only `local-mode` |
| T3 | develop-checklist-simple-mode | "Simple-mode checklist extras (container)" | "Simple-mode checklist extras" | drop env-only `(container)`; keep `Simple-mode` (mode distinguisher) |
| T4 | develop-checklist-dev-mode | "Dev-mode checklist extras (container)" | "Dev-mode checklist extras" | drop env-only `(container)`; keep `Dev-mode` |
| T5 | develop-close-push-dev-local | "Close task — push-dev local" | "Close task — push-dev" | drop env-only `local` |
| T6 | develop-close-push-git-container | "Close task — push-git strategy (container)" | "Close task — push-git strategy" | drop env-only `(container)` |
| T7 | develop-close-push-git-local | "Close task — push-git strategy (local)" | "Close task — push-git strategy" | drop env-only `(local)` |
| T8 | develop-dynamic-runtime-start-container | "Dynamic runtime — start dev server via zerops_dev_server (container)" | "Dynamic runtime — start dev server via zerops_dev_server" | drop env-only `(container)`; keep `via zerops_dev_server` (mechanism) |
| T9 | develop-dynamic-runtime-start-local | "Dynamic runtime — start dev server on your machine (local)" | "Dynamic runtime — start dev server on your machine" | drop env-only `(local)`; keep `on your machine` (mechanism distinguisher) |
| T10 | develop-first-deploy-asset-pipeline-container | "Dev + asset pipeline — build assets over SSH before verify" | "Asset pipeline — build assets over SSH before verify" | drop ambiguous `Dev +` prefix; keep `over SSH` (mechanism) |
| T11 | develop-first-deploy-asset-pipeline-local | "Local + asset pipeline — build assets locally before verify" | "Asset pipeline — build assets locally before verify" | drop env-only `Local +`; keep `locally` (mechanism) |
| T12 | develop-platform-rules-local | "Platform rules — local env extras" | "Platform rules" | drop env-only `local env extras` |
| T13 | develop-platform-rules-container | "Platform rules — container extras" | "Platform rules" | drop env-only `container extras` |
| T14 | develop-push-dev-deploy-container | "Push-dev strategy — deploy via zerops_deploy (container)" | "Push-dev strategy — deploy via zerops_deploy" | drop env-only `(container)` |
| T15 | develop-push-dev-deploy-local | "Push-dev strategy — deploy via zerops_deploy (local)" | "Push-dev strategy — deploy via zerops_deploy" | drop env-only `(local)` |
| T16 | develop-push-dev-workflow-simple | "Push-dev iteration cycle (simple mode, container)" | "Push-dev iteration cycle (simple mode)" | drop env-only `, container`; keep `simple mode` (Codex C2 calibration anchor) |
| T17 | develop-push-dev-workflow-dev | "Push-dev iteration cycle (dev mode, container)" | "Push-dev iteration cycle (dev mode)" | drop env-only `, container`; keep `dev mode` (Codex C2 calibration anchor) |
| T18 | strategy-push-git-push-container | "push-git push setup — container env (GIT_TOKEN + .netrc)" | "push-git push setup (GIT_TOKEN + .netrc)" | drop env-only `container env`; keep `(GIT_TOKEN + .netrc)` mechanism payload (Codex C2 calibration anchor) |
| T19 | strategy-push-git-push-local | "push-git push setup — local env (user's git)" | "push-git push setup (user's git)" | drop env-only `local env`; keep `(user's git)` mechanism payload (Codex C2 calibration anchor) |

### H1/H2/H3 header edits (atom body)

| # | atom-id | pre-edit header | post-edit header | rationale |
|---|---|---|---|---|
| H1 | develop-close-push-dev-local | `### Closing the task (local)` | `### Closing the task` | drop env-only; **MustContain pin migration required** (see below) |
| H2 | develop-dynamic-runtime-start-container | `### Dynamic-runtime dev server (container)` | `### Dynamic-runtime dev server` | drop env-only |
| H3 | develop-dynamic-runtime-start-local | `### Dynamic-runtime dev server (local)` | `### Dynamic-runtime dev server` | drop env-only |
| H4 | develop-first-deploy-asset-pipeline-container | `### Dev/simple + frontend asset pipeline (container)` | `### Frontend asset pipeline` | drop env-only and ambiguous mode prefix |
| H5 | develop-first-deploy-asset-pipeline-local | `### Dev/simple + frontend asset pipeline (local)` | `### Frontend asset pipeline` | drop env-only |
| H6 | develop-local-workflow | `### Development workflow (local)` | `### Development workflow` | drop env-only |
| H7 | develop-platform-rules-local | `### Platform rules (local environment)` | `### Platform rules` | drop env-only |
| H8 | develop-platform-rules-container | `### Platform rules (container environment)` | `### Platform rules` | drop env-only |
| H9 | develop-push-dev-deploy-container | `### Push-Dev Deploy Strategy — container` | `### Push-Dev Deploy Strategy` | drop env-only; **MustContain pin migration required** |
| H10 | develop-push-dev-deploy-local | `### Push-Dev Deploy Strategy — local` | `### Push-Dev Deploy Strategy` | drop env-only |
| H11 | strategy-push-git-push-container | `# Push path — container env` | `# Push path (GIT_TOKEN + .netrc)` | drop env-only; rephrase to mechanism |
| H12 | strategy-push-git-push-local | `# Push path — local env` | `# Push path (user's git)` | drop env-only; rephrase to mechanism |

### MustContain pin migrations (per §11.4)

| pin location | pre-edit pin | post-edit pin | uniqueness verified |
|---|---|---|---|
| `corpus_coverage_test.go:407` (develop_close_local_dev_dev) | `"Closing the task (local)"` | `"Local mode builds from your committed tree"` | YES — only in develop-close-push-dev-local.md:14 |
| `corpus_coverage_test.go:425` (develop_close_local_standard) | `"Closing the task (local)"` | `"Local mode builds from your committed tree"` | YES — same atom |
| `corpus_coverage_test.go:582` (develop_simple_deployed_container) | `"Push-Dev Deploy Strategy — container"` | `"The dev container uses SSH push"` | YES — only in develop-push-dev-deploy-container.md:15 |

The `// MustContain pins were grep-verified` calibration comment at
`corpus_coverage_test.go:563-571` updated to reflect the
post-Phase-3 pin phrases.

## Probe re-measurement

| Fixture | Phase 0 baseline | Post-Phase-2 | Post-Phase-3 | Phase 3 Δ | P0→P3 cumulative Δ |
|---|---:|---:|---:|---:|---:|
| standard | 24,347 | 24,145 | 24,109 | −36 B | −238 B |
| implicit-webserver | 26,142 | 25,965 | 25,916 | −49 B | −226 B |
| two-pair | 26,328 | 26,021 | 25,973 | −48 B | −355 B |
| single-service | 24,292 | 24,090 | 24,054 | −36 B | −238 B |
| simple-deployed | 18,435 | 18,435 | 18,397 | −38 B | −38 B |
| **First-deploy slice (4)** | — | — | — | **−169 B** | **−1,057 B** |
| **5-fixture aggregate** | — | — | — | **−207 B** | **−1,095 B** |

Phase 3 incremental gain modest (−207 B) — title edits are
short. The cumulative P0→P3 hits −1,095 B aggregate (probe-only),
toward the §8 binding target additional ≥ 6,000 B.

Phase 3 also touched simple-deployed for the first time (was
unchanged through Phase 2): −38 B from the
develop-platform-rules-container H2 + title edit + the
develop-push-dev-deploy-container title/H2.

## Phase 3 EXIT (§5 Phase 3)

- [x] All axis-L candidates dropped or kept with rationale
  (19 frontmatter title edits + 12 H1/H2/H3 header edits).
- [x] No regressions on `TestCorpusCoverage_RoundTrip` — pin
  migrations applied in same commit.
- [x] Verify gate: `go test ./internal/workflow -count=1 -short` +
  `go test ./internal/content/ -count=1` GREEN.
- [x] `phase-3-tracker-v2.md` committed.

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites Phase 3 EXIT commit `f0893eb2`.
- [x] No mandated Codex rounds; round state n/a.
- [x] `Closed:` 2026-04-27.

## Notes for Phase 4 entry

1. **Phase 4 (axis M terminology)** is the third content-quality
   phase. Per amendment 3 / Codex C3, container concept gets a
   per-occurrence decision sub-table (`dev container` /
   `runtime container` / `build container` / `Zerops container` /
   `new container`) — HIGH-risk cluster requiring per-occurrence
   review. NOT 10% sampling.
2. **Codex CORPUS-SCAN required** for Phase 4 (per §10.1 P4)
   to enumerate drift clusters AND occurrence ledgers per
   HIGH-risk cluster.
3. Two atoms still have informal `(container env)` markdown
   comments embedded in their bodies (NOT the title/H2 headers I
   touched in Phase 3) — `develop-dev-server-triage.md`,
   `develop-manual-deploy.md`. These will be revisited in Phase 4
   if axis-M canonicalization touches them.

Phase 4 (axis M terminology consistency) entry unblocked.
