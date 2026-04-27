# Plan: Atom corpus hygiene follow-up — content-quality + verification (2026-04-27)

> **Reader contract.** Self-contained for a fresh Claude session. The
> sister plan `plans/atom-corpus-hygiene-2026-04-26.md` shipped
> SHIP-WITH-NOTES at commit `5a85fba0`; this plan addresses every
> deferred item in `plans/audit-composition/deferred-followups.md`
> PLUS three new content-quality axes (K, L, M) the user surfaced
> 2026-04-27 after reading the simple-deployed render of
> `develop-push-dev-deploy-container.md`.

## 1. Problem

The first hygiene cycle (`atom-corpus-hygiene-2026-04-26.md`) recovered
~11.3 KB across 5 baseline fixtures and resolved several axis +
content bugs, but shipped SHIP-WITH-NOTES because:

- **§15.3 G3 strict-improvement** was met only on simple-deployed
  (the user-test target). First-deploy fixtures held flat on
  Redundancy + Coverage-gap — broad atoms (`api-error-meta`,
  `env-var-channels`, `verify-matrix`, `platform-rules-common`,
  `auto-close-semantics`, `change-drives-deploy`) co-render with
  cross-atom paraphrased restatements above the "7+" rubric
  threshold.
- **G5 L5 live smoke test** deferred (eval-zcp infra-dependent).
- **G6 eval-scenario regression** deferred (eval scenarios for
  simple-deployed envelope not authored).
- **~11 KB additional body recovery deferred**: 4 HIGH-risk atoms
  (mandatory per-edit Codex rounds, context-budgeted out) + 7
  LOW-risk atoms (off-probe local-env) + 14 MEDIUM-risk atoms.

Beyond the deferred-followups, **2026-04-27 user audit identified
three NEW content-quality axes** that the first cycle missed:

1. **Abstraction leakage**. Atoms render `(container)` mode tell
   the local-flow agent "there is no SSHFS mount" — but the local
   agent never had any reason to think SSHFS exists. Anti-information.
2. **Title over-qualification**. `### Push-Dev Deploy Strategy — container`
   tells the agent "this is the container variant" — but they
   only RECEIVE this variant on container envelopes. The
   `— container` is bytes the axis filter already pays for.
3. **Terminology drift**. Same concept written in different ways
   across atoms (e.g. "Zerops container" / "the dev container" /
   "service container"; "deploy" vs "redeploy"; "platform" vs
   "Zerops" vs "ZCP"). Costs the agent's parsing budget.

These are PHASE-1-CLASS concerns the original plan's axes A-J
missed because they're about CONTENT QUALITY at a level deeper
than redundancy/density.

## 2. Goal

Close the SHIP-WITH-NOTES gaps and apply the new axes:

1. **Close §15.3 G3 strict-improvement** on first-deploy fixtures
   via broad-atom cross-cluster dedup (so all 5 fixtures strictly
   improve, not just simple-deployed).
2. **Execute deferred verification** (G5 + G6) so the final SHIP
   verdict isn't dependent on infra deferrals.
3. **Apply axes K + L + M** across the corpus.
4. **Resume Phase-6 deferred work** (4 HIGH-risk + 7 LOW-risk +
   14 MEDIUM-risk atoms) so the ~11 KB additional recoverable
   lands.
5. **Re-run §15.3 ship gate** for a clean SHIP verdict (not
   SHIP-WITH-NOTES).

## 3. Mental model — three new content-quality axes

### Axis K — ABSTRACTION-LEAK (NEW; "concept leakage")

**Definition**: an atom mentions flows, mechanisms, or
implementation details from OUTSIDE the envelope it fires on.
The agent reading the atom never had a reason to know about
those things; the mention either gives them anti-information
("there's no X here") or implementation detail they shouldn't
care about ("Y runs under the hood").

**Author bias**: the original atom author thought in terms of the
WHOLE PLATFORM. They explained how things work, what's different
between flows, what's hidden. **The agent doesn't share that
context**. The agent receives ONE envelope's atoms and acts. They
don't need a comparative diagram of how-flows-differ.

**The judgment test** (apply per leak, NOT per atom):

> Without this sentence, would the agent — operating on this
> envelope only — actually do the wrong thing?

- **YES** → KEEP as a guardrail. (Examples: "Don't run `git
  init` on the SSHFS mount" — real foot-gun the agent might
  trigger from training. "Don't hand-roll `ssh cmd &`" — same.)
- **NO** → DROP or REPHRASE. (Examples: "No SSHFS mount in local
  mode" — agent in local mode doesn't know SSHFS is a thing.
  "`zcli push` under the hood" — implementation detail; agent
  calls `zerops_deploy`, doesn't need to know what it dispatches.)

**Risk**: dropping a fact that's actually a guardrail can
regress agent behavior. Mitigation: per-leak Codex round is
SKIPPED only when the leak clearly satisfies "agent without
this would not consider the wrong path". Borderline cases get
a Codex round.

### Axis L — TITLE-OVER-QUALIFIED (NEW)

**Definition**: atom title contains env qualifiers (`(container)`,
`(local)`, `— container`, etc.) that the axis filter already
implies. The agent only RECEIVES this atom on envelopes matching
the axis; the qualifier conveys nothing the framing-context
doesn't already convey.

**Mode/runtime/strategy qualifiers** are different — those
distinguish sibling atoms (e.g. `(dev mode)` distinguishes
`develop-push-dev-workflow-dev` from `-simple` and `-standard`
siblings). Those KEEP.

**The judgment test**: drop a qualifier when its information is
already in the axis filter (env: container/local — drop). Keep
when it disambiguates from sibling atoms in the rendered output
(mode: dev/simple/standard — keep).

**Concrete examples** (from current corpus):

- `"Push-Dev Deploy Strategy — container"` → drop ` — container`
- `"Push-dev iteration cycle (dev mode, container)"` → drop
  `, container`; keep `dev mode` (distinguishes from
  workflow-simple sibling)
- `"Platform rules — container environment"` → drop
  `— container environment` or shorten to `"Platform rules"`
- `"Mode expansion — add a stage pair"` → KEEP (no env qualifier)

**Risk**: low. Title text is rarely pinned by `MustContain`
phrase pins; the AST atom-ID pins are immune.

### Axis M — TERMINOLOGY-DRIFT (NEW)

**Definition**: same concept written differently in different atoms
costs the agent's parsing budget. The agent has to canonicalise
mentally to map "Zerops container" + "service container" + "dev
container" + "the runtime" to the same referent.

**Drift examples** (corpus-wide; Codex CORPUS-SCAN enumerates):

| Concept | Drift seen | Canonical (proposed) |
|---|---|---|
| Container holding the user's code | "Zerops container", "the container", "service container", "dev container" | context-dependent: `dev container` for dev-mode-dynamic; `runtime container` for general; `Zerops container` for cross-flow framing |
| Code-change → durable-state action | "deploy", "redeploy" | `deploy` is first-action; `redeploy` is subsequent. They have different meaning and `redeploy` is correct after first deploy |
| The platform itself | "Zerops", "the platform", "ZCP" | `Zerops` for the platform; `ZCP` for the control-plane / our tool; "the platform" only when context is unambiguous |
| The agent's tool family | "MCP tool", "zerops_* tool", "the tool" | `zerops_<name>` (specific); `MCP tool` (general protocol context); avoid "the tool" |
| The agent itself | "you", "the agent", "the LLM" | `you` (atom is direct address); avoid "the agent" / "the LLM" — those are author-perspective |

**Action**: Codex CORPUS-SCAN identifies drifted terms; per-term
canonical chosen; corpus-wide rewrite.

**Risk**: medium. A grep + replace can lose nuance. Per-term
Codex sampling round catches over-aggressive replacement.

## 4. Empirical baseline (snapshot 2026-04-27)

### 4.1 Corpus state post first hygiene cycle

```
79 atom files (unchanged from prior cycle)
~110 KB total (down from ~115 KB)
Per-fixture body sizes (post-Phase-7):
  develop_first_deploy_standard_container          24,347 B (was 26,145 B; −1,798 B)
  develop_first_deploy_implicit_webserver_standard 26,142 B (was 27,752 B; −1,610 B)
  develop_first_deploy_two_runtime_pairs_standard  26,328 B (was 28,636 B; −2,308 B)
  develop_first_deploy_standard_single_service     24,292 B (was 26,037 B; −1,745 B)
  develop_simple_deployed_container                18,435 B (was 22,424 B; −3,989 B)
```

### 4.2 Composition scores post first cycle (Codex re-score)

| Fixture | Coherence | Density | Redundancy | Coverage-gap | Task-relevance |
|---|---|---|---|---|---|
| standard | 1 | 3 | 1 | 3 | 3 |
| implicit-webserver | 1 | 3 | 1 | 2 | 3 |
| two-pair | 1 | 2 | 1 | 2 | 3 |
| push-dev-dev | 1 | 3 | 1 | 3 | 3 |
| simple-deployed | 2 | 3 | 2 | 4 | **4** |

**Headline gap**: first-deploy fixtures Redundancy = 1 because of
broad-atom cross-restatements. Phase 5 of THIS plan addresses.

### 4.3 Deferred backlog (verbatim from `deferred-followups.md`)

- G5 L5 live smoke test (eval-zcp infra)
- G6 eval-scenario regression
- §15.3 G3 first-deploy strict-improvement (broad-atom dedup)
- Phase 6 deferred:
  - 4 HIGH-risk atoms (per-edit Codex mandatory)
  - 7 LOW-risk atoms (mostly off-probe)
  - 14 MEDIUM-risk atoms
  - ~6.5 KB additional recoverable
- Phase 2 deferred dedups (~1.5 KB)

Total deferred body recovery: ~11 KB.

## 5. Phased execution

> **Pacing rule**: 8 phases (0-7). Each phase has explicit ENTRY,
> WORK-SCOPE, EXIT criteria. Codex rounds per `atom-corpus-hygiene-
> 2026-04-26.md` §10 protocol — same shape, same work-economics
> rules (§10.5). Trackers per §15.1 schema. Verify gate per §6.5.

> **Inherited machinery**: this plan REUSES the original plan's
> §6 (methodology), §8 (test guardrails), §10 (Codex protocol),
> §15 (completeness machinery), §17 (prereq checklist) — read
> the original sections during Phase 0 calibration.

### Phase 0 — Calibration

**ENTRY**: working tree clean; latest commit `5a85fba0` (PLAN
COMPLETE marker for first hygiene cycle).

**WORK-SCOPE**:

1. **§17 prereq verification** (P1-P11 from original plan §17).
   Note P4 — `cmd/atomsize_probe/main.go` was deleted in the
   first cycle's Phase 8 G8. To recreate: `git show 3725157e:cmd/
   atomsize_probe/main.go` (the original recreation commit).
2. **Recreate measurement infrastructure**: re-recreate
   `cmd/atomsize_probe/main.go` + `cmd/atom_fire_audit/main.go`
   from git history. Required for byte-recovery measurement +
   composition re-score in later phases.
3. **Initialize tracker dir**: `mkdir -p plans/audit-composition`
   (already exists from prior cycle; this plan WRITES TO THE
   SAME dir).
4. **Phase 0 PRE-WORK Codex round** per §10.1 P0 row 1 from the
   original plan. Validate this plan's approach + new axes
   K/L/M definitions + the §15.3 G3 closure path.

**EXIT**:
- Probe binaries re-created and run (output committed as
  `plans/audit-composition/probe-baseline-2026-04-27.txt`).
- Phase 0 PRE-WORK Codex round APPROVE (NEEDS-REVISION → revise
  plan; do NOT enter Phase 1 until APPROVE).
- Tracker `phase-0-tracker-v2.md` (or similar) committed.
- Verify gate green (`go test ./... -short -race -count=1` +
  `make lint-local`).

### Phase 1 — Live smoke + eval regression (G5 + G6 closure)

**ENTRY**: Phase 0 EXIT satisfied.

**WORK-SCOPE**:

#### 1.1 — L5 live smoke (G5)

**Infrastructure verified 2026-04-27**: `make linux-amd` exists
(Makefile:156); eval-zcp SSH/zcli authorization in
`CLAUDE.local.md`; binary path on zcp container is
`~/.local/bin/zcp` (per same auth section).

Per original plan §6.6 L5 procedure:

1. `make linux-amd` → produces `builds/zcp-linux-amd64`. Confirm
   build success (binary size > 20 MB; `--version` returns
   non-empty).
2. `scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
   builds/zcp-linux-amd64 zcp:/tmp/zcp-followup` (per
   CLAUDE.local.md SSH flag mandate).
3. `ssh zcp 'cp /tmp/zcp-followup ~/.local/bin/zcp'`.
4. Issue MCP STDIO call (initialize → status) for an idle
   envelope and a develop-active envelope. Capture decoded
   `text` content. Use the snippet in original plan §6.6 verbatim.
5. **Assertion 1 (relaxed)**: wire-frame size matches the probe
   number ± 5 % OR ± 50 bytes (whichever is larger). The original
   plan's "± 1 byte" is too strict — production MCP transport may
   add framing overhead that the probe doesn't model. **If
   variance > 50 bytes**, root-cause: probe lying, or transport
   adds bytes systematically. Document the variance + investigate.
6. **Assertion 2**: decoded `text` parses as valid markdown +
   contains the expected structure (`## Status`, services, plan,
   guidance). Use a markdown parser via `goldmark` (already in
   go.mod) OR a regex check for the canonical structure.
7. **For develop-active envelope**: provision a fresh runtime
   service via `ssh zcp "zcli serviceImport --projectId
   i6HLVWoiQeeLv8tV0ZZ0EQ <yaml-path>"`. CLAUDE.local.md
   eval-zcp section explicitly authorizes this. Use small
   `nodejs@22` with `startWithoutCode: true, minContainers: 1`
   per the same authorization. Delete after the smoke test.
8. Document outcome in `plans/audit-composition/g5-smoke-test-results.md`.

**Failure handling**:
- If wire-frame variance is large (> 50 bytes), document AND
  proceed (variance from MCP framing is acceptable; the smoke
  test's purpose is "does it work end-to-end", not "exact byte
  match").
- If decoded text fails markdown structure check, that's a
  ship-blocker — the corpus broke the rendering pipeline.
- If SSH/zcli access fails, document as infra-blocker; G5
  becomes DEFERRED-WITH-JUSTIFICATION (only acceptable if VPN
  is genuinely down).

#### 1.2 — Eval-scenario regression (G6)

**Architecture confirmed via audit 2026-04-27**: scenarios are
MD files in `internal/eval/scenarios/` (e.g. `develop-add-endpoint.md`,
`greenfield-nodejs-todo.md`); fixtures are YAML in
`internal/eval/scenarios/fixtures/`; runner is real LLM-driven
via `internal/eval/runner.go::spawnClaude` (invokes `claude` CLI
in headless mode at `internal/eval/runner.go:191`). Default model
`claude-opus-4-6[1m]` per `internal/eval/eval.go:23`.

**Step 1 — Survey existing scenarios** for simple-mode-deployed
shape:

```bash
grep -lE "^seed: deployed" internal/eval/scenarios/*.md
```

Audit 2026-04-27 already enumerated:
- `develop-add-endpoint.md` (Laravel + Postgres; **fixture is
  `laravel-dev-deployed.yaml`** — single `app` hostname,
  php-nginx@8.4, deployed, no ServiceMeta seeded → adopt route).
- `develop-strategy-unset-regression.md` (deployed strategy=unset).
- `develop-pivot-auto-close.md` (deployed pivot scenario).
- `develop-ambiguous-state.md`.

**Step 2 — Pick or author**:

- **DEFAULT (preferred)**: REUSE `develop-add-endpoint.md`
  as the regression baseline. It tests adopt-then-develop on a
  deployed implicit-webserver service — close enough to the
  user-test target's "deployed-edit-task" shape for regression
  verification. Fixture already seeds a deployed service.
- **AUTHOR-NEW (fallback)**: only if `develop-add-endpoint`'s
  agent moves don't surface hygiene-touched atoms (e.g. the
  scenario stays out of dev-server-triage / mode-expansion
  territory).
  - Template from `develop-add-endpoint.md` shape.
  - New fixture: `go-simple-deployed.yaml` — single `weatherdash`
    hostname, `go@1.22`, simple mode, ServiceMeta seeded
    (`bootstrapped: true, deployed: true, mode: simple,
    strategy: push-dev`).
  - Edit-task intent: `"add a /healthz endpoint to weatherdash
    and redeploy"`.

**Step 3 — Run PRE- and POST-hygiene**:

```bash
# POST-hygiene (at HEAD):
go run ./cmd/eval-runner ./internal/eval/scenarios/develop-add-endpoint.md \
  --output plans/audit-composition/g6-post-hygiene.log

# PRE-hygiene (snapshot before first cycle started):
git worktree add /tmp/pre-hygiene 96b9bab7
cd /tmp/pre-hygiene
go run ./cmd/eval-runner ./internal/eval/scenarios/develop-add-endpoint.md \
  --output /tmp/g6-pre-hygiene.log
cd -
git worktree remove /tmp/pre-hygiene
```

**Note**: the actual eval-runner CLI invocation may differ from
the snippet above. Check `internal/eval/eval.go` + `cmd/` for the
binary name. If no standalone runner exists, the scenarios may
run via `go test ./internal/eval -run TestEval...` — discover the
right invocation in Phase 0 calibration.

**Step 4 — Compare**:

- `mustCallTools` should match in both runs (or post-hygiene
  uses FEWER tools — that's OK).
- `requiredPatterns` should all match in post-hygiene; if any
  fails, that's a regression.
- `workflowCallsMin` should hold (post-hygiene shouldn't need
  MORE workflow calls).
- Agent's `assessment` text should reflect the same task
  understanding (extract via `internal/eval/extract.go::ExtractAssessment`).

**Step 5 — Document** in `plans/audit-composition/g6-eval-regression.md`.

**Failure handling**: if post-hygiene shows REGRESSION (agent
gets confused / takes wrong moves), the hygiene cycle introduced
a bug. Triage to specific phase / commit / atom edit; revert
and re-do.

**Cost budget**: each `claude` CLI run for a scenario takes
~2-5 minutes. Pre + post = ~10 minutes per scenario. Budget
~30 minutes total for G6 if running 2-3 scenarios.

**EXIT**:
- G5 smoke-test results + G6 eval-regression results committed.
- Both gates either GREEN or explicitly DOCUMENTED as deferred
  with infra-blocker rationale (only acceptable if a CI/test
  infra reason genuinely blocks; user has authorized eval-zcp
  use, so default expectation is GREEN).
- Tracker `phase-1-tracker-v2.md` committed.

### Phase 2 — Axis K (abstraction leakage)

**ENTRY**: Phase 1 EXIT satisfied.

**WORK-SCOPE**:

1. **Codex CORPUS-SCAN**: Codex reads all 79 atoms, identifies
   each cross-environment leak (atom in container env mentioning
   local; atom in local env mentioning container) and each
   implementation-detail leak (e.g. "zcli push under the hood",
   "zsc noop", "the SDK does X"). Per leak, classify:
   - DROP (anti-information; agent has no reason to know)
   - KEEP-AS-GUARDRAIL (without the negation, agent might do the
     wrong thing)
   - REPHRASE (the leak is partly load-bearing but the framing
     is over-explained)
2. **Output**: `plans/audit-composition/axis-k-candidates.md`
   ranked by recoverable bytes + risk.
3. **Codex round per atom** (PER-EDIT) for the HIGH-risk leaks
   (those classified REPHRASE or borderline DROP). LOW-risk
   leaks (clear DROP per the judgment test) self-verified.
4. Apply per atom; commit per concept (one commit per
   "container-mentions-in-local-atoms" pass; another for
   "implementation-detail leaks"; etc.) with §6.1 fact inventory.
5. **Codex POST-WORK** round: re-read every Phase 2 commit; flag
   any guardrail accidentally dropped.

**Risk note**: the judgment test is subjective. Erring on
"DROP if uncertain" is wrong (regression risk); erring on "KEEP
if uncertain" is wrong (leaves the leak in place). The rule for
borderline cases: KEEP, document rationale in the fact inventory.

**EXIT**:
- All axis-K candidates classified + actioned.
- Codex POST-WORK clean (or all findings restored).
- Probe re-run shows monotone or improved body-join.
- `phase-2-tracker-v2.md` committed.

### Phase 3 — Axis L (title hygiene)

**ENTRY**: Phase 2 EXIT satisfied.

**WORK-SCOPE**:

1. Walk every atom's H1/H2/title field. Identify env-qualifier
   suffixes (`(container)`, `(local)`, `— container`,
   `(dev mode, container)`, `(container environment)`, etc.).
2. For each: drop env qualifier; keep mode/runtime/strategy
   qualifiers when they distinguish from sibling atoms.
3. Check whether the title text is referenced anywhere
   (frontmatter `title:` field, MustContain pins, prose
   mentions). If pinned, migrate the pin in the same commit.
4. Codex round NOT mandatory for axis L — it's mechanical drop.
   Verify via probe + tests.

**EXIT**:
- All axis-L candidates dropped or kept with rationale.
- No regressions on `TestCorpusCoverage_RoundTrip`.
- `phase-3-tracker-v2.md` committed.

### Phase 4 — Axis M (terminology consistency)

**ENTRY**: Phase 3 EXIT satisfied.

**WORK-SCOPE**:

1. **Codex CORPUS-SCAN** to enumerate inconsistent terms:
   walking all 79 atoms, list each cluster of terms that refer
   to the same concept. Output:
   `plans/audit-composition/axis-m-candidates.md`.
2. **Per-cluster decision**: pick canonical term per cluster.
   The §3 axis-M section above seeds the choices, but Codex's
   findings may surface clusters not anticipated.
3. **Apply via grep + targeted edit** (NOT global sed — context
   matters per occurrence).
4. **Per-atom Codex sampling round** (NOT per atom; sample
   ~10 % of touched atoms): verify the canonical term reads
   correctly in context.

**EXIT**:
- All clusters canonicalised or deferred with reason.
- `phase-4-tracker-v2.md` committed.

### Phase 5 — Broad-atom cross-cluster dedup (§15.3 G3 closure)

**ENTRY**: Phase 4 EXIT satisfied.

**WORK-SCOPE**:

This is THE phase that closes the §15.3 G3 first-deploy
strict-improvement gap from the first hygiene cycle. The 6
broad atoms causing first-deploy Redundancy=1:

- `develop-api-error-meta`
- `develop-env-var-channels`
- `develop-verify-matrix`
- `develop-platform-rules-common`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`

Each individually reasonable. Collectively, they restate facts
across atoms that co-render on every develop-active envelope.

**Resolution path** (Codex CORPUS-SCAN identifies; executor
applies):

1. **Identify cross-atom restated facts** in the 6 broad
   atoms. Each fact appearing in 2+ of these atoms is a dedup
   candidate.
2. **Pick canonical home** per fact. Use original plan's §6.1
   methodology (lowest priority OR broadest axis OR topical
   owner).
3. **Apply dedup** (one commit per fact; fact inventory per
   §6.1).
4. **Codex per-edit round** for each dedup (HIGH-risk because
   broad atoms are foundational).
5. **Re-render fixtures + re-score**: verify Redundancy on
   first-deploy fixtures moves from 1 to ≥ 2.

**Target**: first-deploy Redundancy = 2 or 3 across all 4
fixtures. simple-deployed already at 2; should hold or improve.

**Risk**: HIGHEST of any phase. Broad atoms are foundational;
trimming them affects every develop-active envelope. Mandatory
per-edit Codex round.

**EXIT**:
- First-deploy fixtures' Redundancy strictly improved (1 → ≥ 2).
- §15.3 G3 strict-improvement now MET on all 5 fixtures.
- Codex per-edit rounds for each dedup APPROVE.
- `phase-5-tracker-v2.md` committed.

### Phase 6 — Phase-6-deferred byte recovery (HIGH/MEDIUM/LOW)

**ENTRY**: Phase 5 EXIT satisfied.

**WORK-SCOPE**:

1. **HIGH-risk atoms** (4 from prior cycle's Phase 6):
   - `develop-ready-to-deploy`
   - `develop-first-deploy-write-app`
   - `develop-verify-matrix`
   - `develop-deploy-files-self-deploy`
   - **Per atom**: mandatory per-edit Codex round per §10.1 P6
     row 2 from the original plan. Codex reads the diff, lists
     facts missing/mutated. Iterate until APPROVE.
2. **LOW-risk atoms** (7 from prior cycle's Phase 6 deferred):
   - `develop-first-deploy-asset-pipeline-local`
   - `develop-first-deploy-asset-pipeline-container`
   - `develop-dynamic-runtime-start-local`
   - `develop-dev-server-triage`
   - `develop-implicit-webserver`
   - `bootstrap-provision-local`
   - `develop-manual-deploy`
   - **Apply**: per-atom mechanical tightening per Codex's
     `axis-b-candidates.md` from prior cycle. Codex POST-WORK
     round per phase to catch fact loss.
3. **MEDIUM-risk atoms** (14 from prior cycle): apply with
   Codex per-edit round; estimated ~4.6 KB recoverable.

**Target**: ~6.5 KB additional body recovery. Combined with
Phase 5 dedup, this should push first-deploy slice past §9
8 KB target into the upper part of 8-12 KB band.

**EXIT**:
- All HIGH-risk atom rewrites APPROVE per per-edit Codex rounds.
- All LOW-risk + MEDIUM-risk atoms tightened.
- Probe re-run shows aggregate body recovery ≥ 6 KB additional.
- `phase-6-tracker-v2.md` committed.

### Phase 7 — Final composition re-score + clean SHIP gate

**ENTRY**: Phase 6 EXIT satisfied.

**WORK-SCOPE**:

1. **Re-render fixtures** (build helper binary; render 5
   fixtures; output to
   `plans/audit-composition/rendered-fixtures-post-followup/`).
2. **Codex CORPUS-SCAN composition cross-validation** per
   original plan §10.1 P7 row 2. Score all 5 fixtures using
   the refined §6.2 rubric. Output
   `plans/audit-composition/post-followup-scores.md`.
3. **Verify §15.3 G3** strict-improvement on ALL 5 fixtures
   (not just simple-deployed). Phase 5 should have closed this
   gap.
4. **Final Codex SHIP VERDICT round** per §10.3 + §15.3 G7
   from the original plan. Read all phase trackers; verify
   G1-G8; return SHIP / NO-SHIP / SHIP-WITH-NOTES.

**Target SHIP outcome**: every G1-G8 satisfied; no SHIP-WITH-
NOTES disposition. The first cycle ended SHIP-WITH-NOTES; this
cycle should end CLEAN SHIP.

**Failure handling**: if Codex returns NO-SHIP or
SHIP-WITH-NOTES, re-open the relevant phase, address the
finding, re-run G7. NEVER claim "PLAN COMPLETE" without explicit
SHIP verdict.

**EXIT**:
- Codex SHIP VERDICT returns SHIP (clean).
- Final commit message cites G1-G8 evidence per §15.3.
- `final-review-v2.md` committed (verbatim Codex output +
  executor disposition).
- `phase-7-tracker-v2.md` committed.

## 6. Codex collaboration protocol

Reference `atom-corpus-hygiene-2026-04-26.md` §10 (full protocol)
+ §10.5 (work-economics rules). Same patterns apply:

- **CORPUS-SCAN** rounds for each new axis (K, L, M) and for
  Phase 5 broad-atom dedup.
- **PER-EDIT** rounds MANDATORY for Phase 5 + Phase 6 HIGH-risk;
  SKIPPABLE per §10.5 rule #3 for LOW-risk mechanical edits.
- **POST-WORK** rounds per phase to catch silent fact loss.
- **FINAL-VERDICT** round per §10.3 at Phase 7 EXIT.

**Critical lessons from the first cycle**:

1. **Codex sandbox blocks artifact writes**. Default protocol:
   "Codex returns text; Claude saves to the named path." Do not
   assume Codex can write files.
2. **`run_in_background=true` agent invocations may delegate to
   async codex CLI without returning analysis text**. Avoid for
   Codex rounds. Use synchronous invocation.
3. **`isPlaceholderToken` trap**: any literal `{word}` or
   `{word:value}` in atom body that's not in the replacer's
   substitutions OR `allowedSurvivingPlaceholders` allowlist
   triggers Synthesize error. Pre-commit grep:
   `grep -nE "\{[a-z][a-z0-9_-]*[:}]" internal/content/atoms/*.md`
   → review every match.
4. **MustContain pin migration**: before any atom edit that
   touches a phrase pinned by `coverageFixtures().MustContain`,
   migrate the pin in the same commit. Find existing pins:
   `grep -nE "MustContain" internal/workflow/corpus_coverage_test.go`.
5. **Test pin contradictions**: before any axis-tightening,
   grep for atom-specific tests:
   `grep -lrE "atom-id-here|TestModeExpansion|...etc" internal/workflow/*_test.go`.
   Run the test before AND after the change.
6. **Memory rule** (`feedback_codex_verify_specific_claims.md`):
   Codex's CURRENT-code citations are reliable; pre-baked claims
   need grep before trust. Verify every "X exists at Y" claim.

## 7. Test guardrails

Reference original plan §8 + §15.2. The pin-density gate
(`corpus_pin_density_test.go`) ENFORCES every atom is pinned
forward; if Phase 2/3/4/5/6 work creates new atoms or renames
existing ones, the bulk-pin
`TestScenario_PinCoverage_AllAtomsReachable` in scenarios_test.go
must be updated in the same commit.

## 8. Acceptance criteria

- All 8 phases (0-7) closed per §15.2 trackers.
- §15.3 G1-G8 ALL satisfied (no DEFERRED-WITH-JUSTIFICATION):
  - G3 strict-improvement met on all 5 fixtures (Phase 5 closes).
  - G5 L5 live smoke green (Phase 1 closes).
  - G6 eval-scenario regression run + documented (Phase 1 closes).
- Codex final SHIP VERDICT returns clean SHIP.
- Cumulative body recovery ≥ 13 KB across 5 fixtures (combining
  first cycle's 11.3 KB + this cycle's ~6.5 KB Phase 6 = ~17 KB
  upper end; realistic target ~14-15 KB).
- New axes K + L + M documented in atom-authoring contract per
  original plan §11 (`docs/spec-knowledge-distribution.md` if
  that's the authoritative spec).

## 9. Out of scope

- Recipe atoms (`internal/recipe/atoms/*.md` if any) — owned by
  recipe team; separate plan.
- `claude_shared.md` / `claude_container.md` / `claude_local.md`
  edits beyond what axis-K work explicitly authorises.
- Tool descriptions (`internal/tools/*.go::Description`) —
  drift-tested separately.
- Recipe authoring scenarios.

### Strategy/export atom scope

Same rule as original plan §11: in-scope for Phase 0 + Phase 1;
in-scope for axes K + L + M proactively (those axes apply
corpus-wide). Out-of-scope for Phase 5 broad-atom dedup unless
they happen to restate one of the 6 broad atoms' facts.

## 10. Anti-patterns + risks

(Inherits original plan §12 plus:)

- **Don't over-drop concept-leakage**. The judgment test is
  conservative: when uncertain, KEEP. A real foot-gun guarded
  by an axis-K candidate is more expensive to lose than
  recovering its bytes.
- **Don't replace terminology globally without per-occurrence
  judgment**. "deploy" vs "redeploy" have semantically distinct
  meanings; a global replace is a regression.
- **Don't merge atoms across axes during Phase 5 dedup**. The 6
  broad atoms each have a distinct conceptual scope; collapsing
  them into one atom destroys the axis distinctions that admit
  each.
- **Don't claim PLAN COMPLETE without Codex SHIP verdict**.
  First cycle ended SHIP-WITH-NOTES; this cycle aims clean SHIP.

## 11. Pre-flight checks per atom edit (atom-consistency guardrail)

The first hygiene cycle hit several "I changed an atom and
broke a pinned test" or "I added a cross-link to a non-co-firing
atom" issues. This plan codifies the checks that catch these
BEFORE commit, not after.

### 11.1 — Before changing atom frontmatter axes (axis-tightening)

**Mandatory pre-flight grep**:

```bash
# 1. Find atom-specific tests pinning expected behavior
grep -lrE "<atom-id>|TestModeExpansion|TestAtomFires|TestAxis" \
  internal/workflow/*_test.go internal/content/*_test.go

# 2. Run those tests BEFORE the edit to capture green baseline
go test ./internal/workflow/ -run "<TestPattern>" -v
```

**If any atom-specific test exists**: read its assertions; the
test pin may encode an architectural truth your axis-tightening
contradicts. Check before changing. The `develop-mode-expansion`
revert in the first cycle (commit `1c93a215`) was the lesson.

**If no atom-specific test exists**: proceed. Run the full test
suite after the change; if `TestCorpusCoverage_RoundTrip` fails,
inspect the failing fixture and migrate `MustContain` pins or
revert.

### 11.2 — Before adding `references-atoms` cross-links

**Mandatory check**: does the cross-link target co-fire on at
least one envelope where the source atom fires?

```bash
# Read source atom's axes
head -10 internal/content/atoms/<source>.md

# Read target atom's axes
head -10 internal/content/atoms/<target>.md

# Mentally compute: is there ANY envelope where both fire?
# (intersection of phases × modes × runtimes × strategies × ...)
```

**Test the intersection** by running `Synthesize` on a synthetic
envelope that should fire both:

```go
// in a one-shot scratch program OR via cmd/atom_fire_audit
env := workflow.StateEnvelope{ /* shape that satisfies both */ }
matches, _ := workflow.Synthesize(env, corpus)
// confirm both atom IDs in matches
```

**Examples of WRONG cross-links** (caught in the first cycle):

- `develop-close-push-dev-standard` (axes: `deployStates:[deployed]
  / modes:[standard]`) → linking to `develop-first-deploy-promote-stage`
  (axes: `envelopeDeployStates:[never-deployed] / modes:[standard]`):
  these envelopes NEVER co-fire (deployed vs never-deployed are
  mutually exclusive). The agent reading close-push-dev-standard
  follows the link and finds promote-stage NOT in the rendered
  output.

**Resolution**: either (a) inline the fact in the source atom,
or (b) link to a canonical that DOES co-fire (e.g.
`develop-auto-close-semantics` co-fires with both close-* and
first-deploy-promote-stage).

### 11.3 — Before any atom body edit

**Mandatory pre-commit grep** for the `isPlaceholderToken` trap:

```bash
# Find any literal {word} or {word:value} that's not in the
# replacer's substitutions or allowedSurvivingPlaceholders
grep -nE "\{[a-z][a-z0-9_-]*[:}]" internal/content/atoms/<edited>.md
```

For each match: confirm it's one of:
- `{hostname}`, `{stage-hostname}`, `{project-name}` (replaced by
  Synthesize)
- One of the `allowedSurvivingPlaceholders` from
  `internal/workflow/synthesize.go:316-343`
- Otherwise: escape with `<` `>` (e.g. `<hostname>.mode`) or
  enclose in quotes (e.g. `"hostname":"value"`) to defang.

The first cycle hit this trap TWICE:
- F0-DEAD-1 sidecar (`bootstrap-recipe-close.md:25` had
  `{hostname:value}` literal — every recipe/close envelope errored).
- Phase 6 `develop-api-error-meta.md` table cells with
  `{host}.mode` literal (caught immediately by tests; fixed in
  same edit).

### 11.4 — Before any `MustContain` pin migration

**Mandatory pre-flight grep**:

```bash
# Find every fixture that pins phrases from the atom you're editing
grep -nE "MustContain" internal/workflow/corpus_coverage_test.go | head
grep -B 2 -A 30 "<fixture-name>" internal/workflow/corpus_coverage_test.go
```

If your atom edit drops or rewrites a phrase that's pinned, you
MUST migrate the pin in the same commit:

1. Pick a NEW unique phrase from the atom's post-edit body
   (verify uniqueness via `grep -lrn "<phrase>" internal/content/atoms/`).
2. Replace the dropped pin with the new phrase.
3. Verify `TestCorpusCoverage_RoundTrip` passes.

The first cycle had this pattern with `"edit → deploy"` →
`"persistence boundary"` migration (commit `27e82976`) and
`"Read and edit directly on the mount"` → `"Mount caveats"`
migration (commit `053af563`).

### 11.5 — After any phase commit

**Mandatory verify gate**:

```bash
go test ./... -short -count=1 -race
make lint-fast
```

Must be GREEN before next phase work. The original plan §6.5
mandates this; this plan reinforces it.

### 11.6 — Before declaring SHIP

**Final ship gate** (Phase 7) requires:

- All 8 phase trackers have `Closed: <date>` headers (not "open").
- All `<pending>` placeholders in tracker rows are replaced with
  commit hashes.
- `make lint-local` (full lint, not lint-fast) passes 0 issues.
- `go test ./... -short -count=1 -race` passes ALL packages.
- `knownUnpinnedAtoms` is empty (already from first cycle).
- Codex final SHIP VERDICT round APPROVE.

The first cycle's Codex round-1 surfaced "tracker headers `Closed:
open`" and "missing `post-hygiene-scores.md`" as G1/G3 blockers.
This plan's Phase 7 must NOT replicate those.

## 12. Cumulative-vs-additional body recovery framing

To avoid confusion: this plan's body recovery target is
**ADDITIONAL** to the first cycle's 11.3 KB. **Cumulative target**
combines both cycles.

| Slice | First cycle | This plan target | Cumulative target |
|---|---:|---:|---:|
| 4 first-deploy fixtures | −7,461 B | additional ≥ 5,000 B (Phase 5 broad-atom dedup) | ≥ 12,000 B |
| 5 fixtures aggregate | −11,344 B | additional ≥ 6,000 B (combined Phases 5 + 6) | ≥ 17,000 B |
| Off-probe (local + bootstrap) | ~−1,500 B | additional from Phase 2/3/4 axis-K/L/M work | ≥ 3,000 B aggregate |

**Pre-emptive answer to "did you reach target?"**: track these
three numbers explicitly in `phase-7-tracker-v2.md`. Cumulative
≥ 17 KB across 5 fixtures means the §9 acceptance criterion
"8-12 KB body" is comfortably met.

## 13. First moves for the fresh instance

**Step 0 — prereq verification (MANDATORY)**: walk every row of
`atom-corpus-hygiene-2026-04-26.md` §17 prereq checklist. P4 now
references commit `3725157e` (the probe) AND `55a9fbdf` (the
fire-audit) — both reachable via `git show <commit>:cmd/<bin>/main.go`.

**Step 0.5 — verify infrastructure** (audited 2026-04-27):

```bash
# Make targets
grep -E "^linux-amd|^linux-amd64" Makefile
# Eval scenarios
ls internal/eval/scenarios/*.md
ls internal/eval/scenarios/fixtures/*.yaml
# Eval runner
grep -nE "spawnClaude|exec.CommandContext.*claude" internal/eval/runner.go
# Probe + fire-audit re-buildable
git show 3725157e:cmd/atomsize_probe/main.go | head -3
git show 55a9fbdf:cmd/atom_fire_audit/main.go | head -3
# eval-zcp authorization (CLAUDE.local.md)
grep -A 3 "eval-zcp" CLAUDE.local.md
```

If any of these fail, STOP and ask the user. The audit on
2026-04-27 confirmed all of them present at HEAD.

**Step 1 — read context**:

1. This plan end-to-end.
2. Sister plan `atom-corpus-hygiene-2026-04-26.md` (especially
   §6 methodology + §10 Codex protocol + §15 completeness machinery).
3. `plans/audit-composition/deferred-followups.md` — the input
   backlog.
4. `plans/audit-composition/final-review.md` — Codex's
   round-3 NO-SHIP analysis (preserved verbatim) + executor's
   SHIP-WITH-NOTES disposition.
5. `CLAUDE.md` + `CLAUDE.local.md` — project conventions + auth.

**Step 2 — corpus baseline check**: same as original plan §13
Step 2. Confirm 79 atoms; per-fixture bytes per §4.1 of THIS plan.

**Step 3 — initialize tracker dir**: same dir as first cycle
(`plans/audit-composition/`). Use `-v2` suffix on tracker
filenames to distinguish from first-cycle trackers.

**Step 4 — Phase 0 PRE-WORK Codex round** per §10.1 P0 row 1
from original plan. Validate this plan's approach + axes K/L/M
definitions + the §15.3 G3 closure path.

**Step 5 — Begin Phase 0 work**: build probe + fire-audit;
re-derive corpus baseline from current state.

**Step 6 — Phase 0 EXIT verification**: same as original plan
§13 Step 6. Only when fully green: enter Phase 1.

**Pause/resume**: per original plan §16.2. Trackers are the
system of record.

## 14. Provenance

Drafted 2026-04-27 after the `atom-corpus-hygiene-2026-04-26.md`
hygiene cycle shipped SHIP-WITH-NOTES. User audit identified
three new content-quality axes (K abstraction-leak, L title-
over-qualified, M terminology-drift) that the first cycle
missed.

Drafted as a SELF-CONTAINED plan a fresh Claude session can
execute end-to-end. Reuses original plan's machinery (§6 / §10 /
§15 / §17) by reference rather than re-stating; new content is
the three axes' definitions + the closure path for §15.3 G3 +
the Phase 1 G5/G6 verification work.

This plan does NOT supersede the first hygiene cycle plan; the
two are complementary. The first cycle established the
infrastructure and the bytes-focused axes; this cycle closes
the gaps + applies content-quality refinement.

## 15. Open question for first reviewer

If the new axes K/L/M conflict with one another (e.g. removing a
title qualifier under L creates a terminology-drift case under
M), what's the resolution order? **Default**: process axes in
plan order (K → L → M). Each phase's edits are committed before
the next phase's CORPUS-SCAN, so each later phase sees the prior
phase's results. Conflicts are resolved at the per-edit level
during the later phase, not at plan-design level.
