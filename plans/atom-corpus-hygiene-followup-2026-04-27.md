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
> envelope only AND carrying plausible cross-flow training
> reflexes — actually do the wrong thing?

> **Default rule: when uncertain, KEEP.** The cost of regressing
> agent behavior far exceeds the bytes recovered by an
> uncertain DROP. Document the keep rationale in the per-atom
> fact inventory.

**HIGH-risk signals (mandatory KEEP unless Codex per-edit
rejects)** — any of these flags the leak as guardrail-class:

1. **Negation tied to a tool/action**: "Don't run X", "Never use
   Y", "No Z available here". The negation IS the guardrail.
2. **Cross-env contrast as mental-model framing**: "Local mode
   builds from your committed tree — no SSHFS, no dev container"
   couples a positive operational claim to the negation. The
   negation prevents a likely cross-flow reflex.
3. **Tool-selection guidance**: "Use `zerops_deploy` here, not
   `zcli push`"; "Do NOT use `zerops_dev_server` — that tool is
   container-only".
4. **Recovery guidance**: "If X fails, do Y" — the alternative
   path the leak names is the guardrail.
5. **Sentences with "do not" / "never" / "no X" tied to an
   operational choice.

**LOW-risk DROP candidates** (only these, and only when no
HIGH-risk signal applies):

- **Pure implementation trivia, no operational consequence**:
  "`zcli push` under the hood" — agent calls `zerops_deploy`,
  the dispatch path is invisible.
- **Standalone negation with no operational coupling**: "No
  SSHFS mount in local mode" as a bare fact (NOT framed as
  mental-model coupling per signal #2). If the atom anywhere
  couples the negation to a positive operational claim, treat
  as HIGH-risk per signal #2.
- **Comparative diagram of how flows differ in UNRELATED env**:
  the agent in this env doesn't need to compare to others
  unless cross-flow training is a real risk.
- **Historical context**: "this used to be different in v1" —
  no operational consequence.

**Concrete examples (annotated)**:

- KEEP: "Don't run `git init` on the SSHFS mount" — signal #1
  (negation tied to action) + #2 (SSHFS is cross-flow).
- KEEP: "Local mode builds from your committed tree — no SSHFS,
  no dev container." — signal #2 (mental-model framing); the
  no-SSHFS phrase is the guardrail when coupled to "builds from
  committed tree".
- KEEP: "Do NOT use `zerops_dev_server` — that tool is
  container-only." — signal #3 (tool-selection).
- DROP: "`zcli push` under the hood" — pure trivia, no
  signal applies.
- DROP: standalone "No SSHFS in local mode" with no further
  operational framing — signal #2 does NOT apply because
  there's no positive operational claim coupled to it.

**Risk**: dropping a fact that's actually a guardrail can
regress agent behavior. Mitigation, layered:

1. **Per-leak Codex round** mandatory for any leak with a
   HIGH-risk signal (signals #1-5 above).
2. **Axis K DROP ledger** committed alongside Phase 2 work
   (`plans/audit-composition/axis-k-drops-ledger.md`): one row
   per dropped leak — atom, exact pre-edit sentence, classification
   rationale, signal-check, reviewer status.
3. **Phase 2 POST-WORK Codex** samples ALL HIGH/borderline rows
   AND every LOW-risk DROP whose pre-edit sentence contains any
   of: `no `, `never`, `do not`, `SSHFS`, `container`, `local`,
   `SSH`, `git`, `deploy`, or any `zerops_*` tool name.
4. **Borderline cases**: when uncertain → KEEP; document in
   ledger why kept.

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

**Mechanism qualifiers** are also different — qualifiers naming a
load-bearing operational distinction (credentials, tooling,
identity, runtime constraint) carry payload the axis filter does
NOT convey. Those KEEP.

**The judgment test (token-level, not whole-suffix)**: split the
title qualifier on commas, em-dashes, or parentheses. For each
token, ask:

- Is this token an env-only label (`container`, `local`,
  `container env`, `local env`)? → DROP that token.
- Is this token a mode/runtime/strategy distinguisher (`dev mode`,
  `simple mode`, `standard mode`, `dynamic`, `static`, `push-dev`,
  `push-git`, `manual`)? → KEEP.
- Is this token a mechanism payload (`GIT_TOKEN + .netrc`,
  `user's git`, runtime constraint, credential channel)? → KEEP.
- Is this token bare punctuation orphaned by the drop above? →
  Clean up (remove orphaned `, ` / ` — ` / `()`).

**Concrete examples** (from current corpus):

- `"Push-Dev Deploy Strategy — container"` → drop ` — container`
  (only env token).
- `"Push-dev iteration cycle (dev mode, container)"` → drop
  `, container`; keep `dev mode` (mode distinguisher).
- `"Platform rules — container environment"` → drop
  `— container environment` or shorten to `"Platform rules"`.
- `"push-git push setup — container env (GIT_TOKEN + .netrc)"` →
  drop `container env` ONLY; KEEP `(GIT_TOKEN + .netrc)` —
  mechanism payload distinguishing from local-env credential flow.
  Net: `"push-git push setup (GIT_TOKEN + .netrc)"`.
- `"push-git push setup — local env (user's git)"` → drop
  `local env` ONLY; KEEP `(user's git)`. Net:
  `"push-git push setup (user's git)"`.
- `"Mode expansion — add a stage pair"` → KEEP (no env qualifier).

**Risk**: low. Title text is rarely pinned by `MustContain`
phrase pins; the AST atom-ID pins are immune. Mechanism-qualifier
preservation reduces the residual risk further.

### Axis M — TERMINOLOGY-DRIFT (NEW)

**Definition**: same concept written differently in different atoms
costs the agent's parsing budget. The agent has to canonicalise
mentally to map "Zerops container" + "service container" + "dev
container" + "the runtime" to the same referent.

**Drift clusters** (corpus-wide; Codex CORPUS-SCAN enumerates;
canonicals decided BEFORE rewrite):

| # | Concept | Drift seen | Canonical decision |
|---|---|---|---|
| 1 | Container holding user code | "Zerops container", "the container", "service container", "dev container", "runtime container", "build container", "new container" | **Decision table below — per-occurrence review** |
| 2 | Code-change → durable-state action | "deploy", "redeploy" | `deploy` for first-action; `redeploy` for subsequent. Semantically distinct; per-occurrence judgment required (do NOT global replace) |
| 3 | The platform itself | "Zerops", "the platform", "ZCP" | `Zerops` for the platform; `ZCP` for the control-plane / our tool; "the platform" only when context is unambiguous |
| 4 | Agent's tool family | "MCP tool", "zerops_* tool", "the tool" | `zerops_<name>` (specific); `MCP tool` (general protocol context); avoid "the tool" |
| 5 | The agent itself | "you", "the agent", "the LLM" | `you` (atom is direct address); avoid "the agent" / "the LLM" — those are author-perspective |

**Cluster #1 container decision table** (per-occurrence review
mandatory):

| Use this term | When the atom is talking about |
|---|---|
| `dev container` | Mutable push-dev / SSHFS context — the developer-mutable container for dev-mode-dynamic flows. |
| `runtime container` | A running service instance generally. The default for cross-cluster references when no other distinction applies. |
| `build container` | The build-stage filesystem (zbuilder context) before the runtime swap. Only when the atom is explicitly talking about build vs runtime. |
| `Zerops container` | Broad first-introduction framing only — when the atom is orienting an unfamiliar reader. Avoid in detailed operational guidance. |
| `new container` | The replacement container created on each deploy (deploy-replacement semantics specifically). |

**Risk classes for Axis M clusters**:

- **HIGH-risk** (per-occurrence review mandatory; NOT 10%
  sampling): cluster #1 container, cluster #2 deploy/redeploy,
  cluster #3 Zerops/ZCP/platform. Same word can encode distinct
  concepts in adjacent atoms; misclassification regresses agent
  comprehension.
- **MEDIUM-risk** (≥50% sampling): cluster #4 tool-family.
- **LOW-risk** (10% sampling): cluster #5 agent-self.

**Action**: Codex CORPUS-SCAN enumerates drifted terms with one
row per occurrence per cluster (e.g. `axis-m-container-ledger.md`
for cluster #1). HIGH-risk cluster rewrites get per-occurrence
Codex review. MEDIUM-risk cluster rewrites get ≥50% sampling.
LOW-risk cluster rewrites get 10% sampling.

**Risk**: medium. A grep + replace WILL lose nuance for HIGH-risk
clusters. Per-occurrence ledger + Codex review per HIGH-risk
occurrence is the mitigation. Global sed is forbidden for HIGH-risk
clusters.

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
  proceed with content-phase work (variance from MCP framing
  is acceptable for end-to-end function), BUT G5 is NOT marked
  GREEN. G5 stays in NEEDS-ROOT-CAUSE state until either: probe
  is corrected, threshold is widened with explicit evidence, OR
  the variance is downgraded to a documented deferral with the
  user's acceptance. **Final SHIP gate (Phase 7) cannot pass G5
  while it is NEEDS-ROOT-CAUSE.** This was tightened in PRE-WORK
  amendment (Codex C12) — a functional smoke is not the same as
  a green G5.
- If decoded text fails markdown structure check, that's a
  ship-blocker — the corpus broke the rendering pipeline.
- If SSH/zcli access fails, document as infra-blocker; G5
  becomes DEFERRED-WITH-JUSTIFICATION. **Per PRE-WORK amendment
  (Codex C11), this means clean-SHIP target is unreachable.**
  Either fix the infra or downgrade the plan's verdict ambition
  to SHIP-WITH-NOTES before entering Phase 2; the executor must
  surface this decision to the user, not silently proceed under
  the deferred-exit path.

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
- **For clean-SHIP target**: both gates GREEN. Phase 1 may not
  EXIT under DEFERRED-WITH-JUSTIFICATION while the plan's stated
  verdict ambition is clean SHIP — that creates the contradiction
  Codex C11 surfaced. If genuine infra block prevents GREEN, the
  executor MUST either (a) fix the infra, or (b) propose
  downgrading the plan's verdict ambition to SHIP-WITH-NOTES and
  get user acknowledgement before proceeding to Phase 2.
- **Phase 1 establishes baseline G5/G6 only.** Final-shippable
  G5/G6 evidence comes from a Phase 7 re-run on the post-Phase-6
  corpus (per Codex C5 amendment). Phase 1 numbers are stale once
  any content phase commits.
- Tracker `phase-1-tracker-v2.md` committed.

### Phase 2 — Axis K (abstraction leakage)

**ENTRY**: Phase 1 EXIT satisfied.

**WORK-SCOPE**:

1. **Codex CORPUS-SCAN**: Codex reads all 79 atoms, identifies
   each cross-environment leak (atom in container env mentioning
   local; atom in local env mentioning container) and each
   implementation-detail leak (e.g. "zcli push under the hood",
   "zsc noop", "the SDK does X"). Per leak, classify against the
   §3 Axis K HIGH-risk signal list (#1-5):
   - DROP (LOW-risk only — pure trivia, no signal applies)
   - KEEP-AS-GUARDRAIL (any HIGH-risk signal applies; default for
     uncertain cases)
   - REPHRASE (the leak is partly load-bearing but the framing
     is over-explained)
2. **Output 1**: `plans/audit-composition/axis-k-candidates.md`
   ranked by recoverable bytes + risk.
3. **Output 2 — Axis K DROP ledger** (per Codex C8/C9 amendment):
   `plans/audit-composition/axis-k-drops-ledger.md`. One row per
   dropped leak with columns: atom-id, exact pre-edit sentence,
   classification (LOW-risk DROP / REPHRASE), signal-check
   (which §3 HIGH-risk signals were considered and rejected),
   reviewer status (self-verified / Codex-PER-EDIT / borderline
   kept). Codex POST-WORK consumes this ledger.
4. **Codex round per atom** (PER-EDIT) for ANY leak with a §3
   HIGH-risk signal AND for borderline classifications. LOW-risk
   DROP leaks self-verified ONLY when no HIGH-risk signal applies.
5. Apply per atom; commit per concept (one commit per
   "container-mentions-in-local-atoms" pass; another for
   "implementation-detail leaks"; etc.) with §6.1 fact inventory.
6. **Codex POST-WORK** round: re-read every Phase 2 commit AND
   the DROP ledger; sample-audit ALL HIGH/borderline rows AND
   every LOW-risk DROP whose pre-edit sentence contains any of:
   `no `, `never`, `do not`, `SSHFS`, `container`, `local`,
   `SSH`, `git`, `deploy`, `zerops_*` tool name. Flag any
   guardrail accidentally dropped.

**Risk note**: the judgment test is subjective. Erring on
"DROP if uncertain" is wrong (regression risk); erring on "KEEP
if uncertain" is wrong (leaves the leak in place). **The rule
for borderline cases: KEEP, document rationale in the DROP
ledger row "borderline kept".** Per §3 default rule.

**EXIT**:
- All axis-K candidates classified + actioned.
- DROP ledger committed at `axis-k-drops-ledger.md`.
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
   to the same concept AND every occurrence per cluster. Output:
   `plans/audit-composition/axis-m-candidates.md` + per-HIGH-risk
   cluster occurrence ledgers (e.g. `axis-m-container-ledger.md`
   for cluster #1 — one row per occurrence).
2. **Per-cluster decision**: pick canonical term per cluster
   using the §3 axis-M decision tables. Cluster #1 (container)
   uses the §3 sub-table (dev / runtime / build / Zerops / new
   container — context-sensitive). Cluster #2 (deploy/redeploy)
   per-occurrence judgment (semantically distinct).
3. **Apply via per-occurrence judgment** (NEVER global sed for
   HIGH-risk clusters #1, #2, #3). Each replacement reviewed
   against the cluster's decision rule before commit.
4. **Codex sampling per cluster per Codex C13/Amendment 3**:
   - HIGH-risk clusters #1, #2, #3: per-occurrence Codex review
     of EVERY touched occurrence. Not 10% sampling.
   - MEDIUM-risk cluster #4: ≥50% sampling.
   - LOW-risk cluster #5: 10% sampling.

**EXIT**:
- All clusters canonicalised or deferred with reason.
- HIGH-risk cluster occurrence ledgers committed.
- `phase-4-tracker-v2.md` committed.

### Phase 5 — Broad-atom cross-cluster dedup + coverage-gap sub-pass (§15.3 G3 closure)

**ENTRY**: Phase 4 EXIT satisfied.

**WORK-SCOPE**:

This is THE phase that closes the §15.3 G3 first-deploy
strict-improvement gap from the first hygiene cycle. **G3 has
TWO halves** — redundancy AND coverage-gap (per Codex C6/C15
amendment + `final-review.md:21-26`). Phase 5 must close both.

#### 5.1 — Redundancy sub-pass (broad-atom dedup)

The 6 broad atoms causing first-deploy Redundancy=1:

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
   candidate. Codex CORPUS-SCAN may surface additional atoms
   beyond the 6 named — append to the list with rationale per
   inherited §16 amend protocol.
2. **Pick canonical home** per fact. Use original plan's §6.1
   methodology (lowest priority OR broadest axis OR topical
   owner).
3. **Apply dedup** (one commit per fact; fact inventory per
   §6.1).
4. **Codex per-edit round** for each dedup (HIGH-risk because
   broad atoms are foundational).
5. **Re-render fixtures + re-score**: verify Redundancy on
   first-deploy fixtures moves from 1 to ≥ 2 AND simple-deployed
   moves from 2 to ≥ 3 OR holds-flat-at-5 (strict-improvement
   per refined G3 reading).

**Target**: first-deploy Redundancy = 2 or 3 across all 4
fixtures. simple-deployed: target 3 (was 2); flat-at-2 is NOT
strict improvement and fails refined G3.

#### 5.2 — Coverage-gap sub-pass

Coverage-gap on first-deploy fixtures held at 2-3 in the first
cycle's Codex re-score (per `post-hygiene-scores.md:73-80`). The
post-execute-cmds-fix re-run wasn't performed but is expected to
move them to ≥ 3. simple-deployed at 4 already. Phase 5 closes:

1. **Re-score coverage-gap on all 5 fixtures** post-redundancy
   sub-pass. If first-deploy fixtures land at ≥ 3 strictly above
   2 (the prior cycle's score), the execute-cmds fix carried
   through. If still flat at 2, identify the residual gaps
   (likely "what tool to call for X" for at least one likely
   next-action) and add a targeted patch via a Phase 5.3 sub-pass:
   author the missing guidance in the right canonical home (an
   existing atom or a new narrow-axis atom).
2. **simple-deployed coverage-gap target**: hold at 4 (already
   strict-improved from 3 in prior cycle); flat-at-4 is acceptable
   for refined G3 (was 3, now 4 — strict improvement already met).
3. **Per-fixture pass criterion**: redundancy AND coverage-gap
   each strictly improved vs §4.2 baseline OR flat-at-5.

**Risk**: HIGHEST of any phase. Broad atoms are foundational;
trimming them affects every develop-active envelope. Mandatory
per-edit Codex round.

**EXIT**:
- First-deploy fixtures' Redundancy strictly improved (1 → ≥ 2).
- First-deploy fixtures' Coverage-gap strictly improved or
  flat-at-5 vs §4.2 baseline.
- simple-deployed Redundancy strictly improved (2 → ≥ 3) OR
  flat-at-5 (currently 2 → not yet flat).
- §15.3 G3 strict-improvement now MET on all 5 fixtures across
  BOTH redundancy AND coverage-gap.
- Codex per-edit rounds for each dedup APPROVE.
- `phase-5-tracker-v2.md` committed.

### Phase 6 — Phase-6-deferred byte recovery (HIGH/MEDIUM/LOW)

**ENTRY**: Phase 5 EXIT satisfied. **Plus pre-baselining (per
Codex C4 amendment)**: every Phase 6 atom that was also touched
by Phase 5 (notably `develop-verify-matrix`, plus any atom that
ended up in the Phase 5 broad-atom dedup beyond the original 6)
MUST be re-baselined: re-read the post-Phase-5 atom; regenerate
the byte-recovery estimate against current state; treat the
prior cycle's `axis-b-candidates.md` numbers as STALE for those
atoms. Document the re-baseline in
`plans/audit-composition/axis-b-candidates-v2.md`.

Additionally (per Codex C14 amendment), the 14 MEDIUM-risk atoms
were never named in this plan. Phase 6 ENTRY MUST regenerate
`axis-b-candidates-v2.md` as the AUTHORITATIVE work-unit list:
- For HIGH-risk: confirm the 4 atoms below are still HIGH-risk
  post-Phase-5; surface any newly HIGH-risk atom from Phase 5's
  broad-atom dedup if applicable.
- For MEDIUM-risk: enumerate all 14 atoms by name with current
  byte estimates. Use prior cycle's `axis-b-candidates.md` as
  starting input but regenerate against current state.
- For LOW-risk: confirm the 7 atoms below.

**WORK-SCOPE**:

1. **HIGH-risk atoms** (4 from prior cycle's Phase 6, post-Phase-5
   re-baselining):
   - `develop-ready-to-deploy`
   - `develop-first-deploy-write-app`
   - `develop-verify-matrix` (post-Phase-5 — re-baseline
     mandatory; expected smaller delta)
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
   - **Apply**: per-atom mechanical tightening per
     `axis-b-candidates-v2.md`. Codex POST-WORK round per phase
     to catch fact loss.
3. **MEDIUM-risk atoms** (14, named in `axis-b-candidates-v2.md`
   per Phase 6 ENTRY): apply with Codex per-edit round;
   regenerated estimate per the v2 candidates artifact.

**Target**: ≥6 KB additional body recovery. Combined with Phase 5
dedup, this should push first-deploy slice past §9 8 KB target
into the upper part of 8-12 KB band, satisfying §8 binding
target (additional ≥6,000 B + cumulative ≥17,000 B per Codex
C7/Amendment 7).

**EXIT**:
- `axis-b-candidates-v2.md` committed at Phase 6 ENTRY.
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
   for BOTH redundancy AND coverage-gap (not just simple-deployed,
   not just redundancy — per Codex C6/C15 amendments). Phase 5
   should have closed this gap; if any fixture's redundancy or
   coverage-gap is still flat (not strictly improved or
   flat-at-5), re-open Phase 5 with a targeted patch BEFORE
   running G7.
4. **Re-run G5 live smoke** on the post-Phase-6 binary (per
   Codex C5 amendment). Phase 1's G5 result is stale once any
   content phase commits; final-shippable G5 evidence is the
   Phase 7 re-run on the post-followup binary. Save to
   `plans/audit-composition/g5-smoke-test-results-post-followup.md`.
   - If G5 was NEEDS-ROOT-CAUSE coming out of Phase 1 (per Phase 1
     C12 amendment), Phase 7 must either (a) confirm root-cause
     and produce a green re-run with corrected probe/threshold,
     or (b) escalate to user for SHIP-target downgrade.
5. **Re-run G6 eval-scenario regression** on the post-Phase-6
   corpus (per Codex C5 amendment). Save to
   `plans/audit-composition/g6-eval-regression-post-followup.md`.
   Compare both:
   - vs PRE-hygiene baseline (snapshot before first cycle started)
     — should match or improve.
   - vs Phase 1's POST-first-cycle baseline — should match or
     improve.
6. **Final Codex SHIP VERDICT round** per §10.3 + §15.3 G7
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
- Phase 7 G5 + G6 re-runs committed (final-shippable evidence).
- Final commit message cites G1-G8 evidence per §15.3 — including
  the Phase 7 G5 + G6 re-runs as the binding artifacts (NOT the
  Phase 1 baselines).
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
  - G3 strict-improvement met on all 5 fixtures, BOTH redundancy
    AND coverage-gap (Phase 5 closes; Phase 7 verifies).
  - G5 L5 live smoke green: Phase 1 establishes baseline; Phase 7
    re-run on post-followup binary is the binding artifact (per
    Codex C5/C12 amendments).
  - G6 eval-scenario regression run + documented: Phase 1
    baseline + Phase 7 re-run on post-followup corpus is the
    binding artifact.
- Codex final SHIP VERDICT returns clean SHIP.
- **Body-recovery binding target (per Codex C7/Amendment 7)**:
  additional ≥ 6,000 B across 5 fixtures (this cycle alone) AND
  cumulative ≥ 17,000 B across 5 fixtures (first cycle 11,344 B
  + this cycle ≥ 6,000 B). The "13 KB / realistic 14-15 KB"
  numbers from the prior draft are forecast/risk notes only, NOT
  acceptance thresholds.
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
combines both cycles. **The §8 binding targets are these (per
Codex C7/Amendment 7)**:

| Slice | First cycle | This plan target | Cumulative target |
|---|---:|---:|---:|
| 4 first-deploy fixtures | −7,461 B | additional ≥ 5,000 B (Phase 5 broad-atom dedup) | ≥ 12,000 B |
| **5 fixtures aggregate** (binding) | −11,344 B | **additional ≥ 6,000 B** (combined Phases 5 + 6) | **≥ 17,000 B** |
| Off-probe (local + bootstrap) | ~−1,500 B | additional from Phase 2/3/4 axis-K/L/M work | ≥ 3,000 B aggregate |

**Binding numbers** (the only ones that matter for SHIP):
- additional ≥ 6,000 B aggregate across 5 fixtures (this cycle).
- cumulative ≥ 17,000 B aggregate across 5 fixtures (both cycles).

The first-deploy slice (≥ 5,000 B) and off-probe (≥ 3,000 B)
rows are FORECAST sub-targets — useful for tracking but NOT
SHIP-blocking on their own. The aggregate ≥ 17,000 B is the
single binding cumulative number per §8.

**Pre-emptive answer to "did you reach target?"**: track all
three numbers explicitly in `phase-7-tracker-v2.md` but the SHIP
gate cites only the aggregate-5-fixtures cumulative number.

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

## 16. Amendments — Phase 0 PRE-WORK Codex round (2026-04-27)

The PRE-WORK Codex round at Phase 0 returned NEEDS-REVISION with
11 amendments (10 numbered + C12 wire-frame variance). Codex's
verbatim output is preserved at
`plans/audit-composition/codex-round-p0-prework-followup.md`. All
11 amendments were applied in-place to §3, §5, §8, §12 in commit
`<phase-0-amendments-commit>` (filled in at Phase 0 EXIT). This
section catalogs them for fresh-reader traceability.

| # | Concern | Plan section(s) edited | Summary of revision |
|---|---|---|---|
| 1 | Axis K judgment-test ambiguity (C1) | §3 axis K | HIGH-risk signal codification (negation+action, cross-env mental-model framing, tool-selection, recovery, do-not/never/no-X tied to operational choice); "uncertain → KEEP" elevated to default rule next to DROP example; SSHFS example reclassified as KEEP when coupled to positive operational claim. |
| 2 | Axis L compound qualifiers (C2) | §3 axis L | Token-level title edits (split on commas/em-dash/parens; per token: env-only token → DROP; mode/runtime/strategy distinguisher → KEEP; mechanism payload like `GIT_TOKEN + .netrc` → KEEP). Concrete strategy-push-git example added. |
| 3 | Axis M canonical choices (C3, C13) | §3 axis M, §5 Phase 4 | Container concept now uses a 5-row decision sub-table (dev/runtime/build/Zerops/new container — context-sensitive); per-occurrence review mandatory for HIGH-risk clusters #1/#2/#3; ≥50% sampling for cluster #4; 10% sampling only for cluster #5. |
| 4 | Phase 5/6 ordering re-baseline (C4) | §5 Phase 6 ENTRY | Phase 6 atoms also touched by Phase 5 (notably `develop-verify-matrix`) MUST be re-baselined; prior `axis-b-candidates.md` numbers stale; regenerate to `axis-b-candidates-v2.md`. |
| 5 | Phase 1 baselines, Phase 7 binds (C5) | §5 Phase 1 EXIT, §5 Phase 7 work-scope, §8 acceptance | Phase 1 establishes G5/G6 baseline only; Phase 7 re-runs G5 on post-followup binary AND G6 on post-followup corpus; SHIP gate cites Phase 7 re-runs (not Phase 1) as binding. |
| 6 | G3 wording — coverage-gap closure (C6, C15) | §5 Phase 5 (split into 5.1 + 5.2), §5 Phase 7 step 3, §8 acceptance | G3 has TWO halves — redundancy AND coverage-gap. Phase 5.1 covers redundancy; Phase 5.2 covers coverage-gap on all 5 fixtures. simple-deployed Redundancy must move 2 → 3 (flat-at-2 fails refined G3). Phase 7 step 3 re-opens Phase 5 if any fixture's redundancy or coverage-gap is still flat. |
| 7 | Cumulative body target arithmetic (C7) | §8 acceptance, §12 framing | Binding target = additional ≥6,000 B aggregate across 5 fixtures + cumulative ≥17,000 B aggregate across 5 fixtures. The "13 KB / 14-15 KB" numbers downgraded to forecast notes. First-deploy slice + off-probe rows are sub-targets, NOT SHIP-blocking on their own. |
| 8 | Axis K DROP ledger (C8, C9) | §3 axis K (mitigation list), §5 Phase 2 work-scope + EXIT | New artifact `axis-k-drops-ledger.md` — one row per dropped leak with atom, exact pre-edit sentence, signal-check, reviewer status. Codex POST-WORK samples ALL HIGH/borderline rows + every LOW-risk DROP whose pre-edit sentence contains `no `, `never`, `do not`, SSHFS, container, local, SSH, git, deploy, or `zerops_*` tool name. |
| 9 | Phase 1 deferred-exit conflict (C11) | §5 Phase 1 failure handling, §5 Phase 1 EXIT | Phase 1 may not EXIT under DEFERRED-WITH-JUSTIFICATION while plan's verdict ambition is clean SHIP. Genuine infra block forces (a) fix the infra OR (b) propose downgrade to SHIP-WITH-NOTES with user acknowledgement before Phase 2. |
| 10 | Phase 6 MEDIUM list under-specified (C14) | §5 Phase 6 ENTRY | Phase 6 ENTRY MUST regenerate `axis-b-candidates-v2.md` as authoritative work-unit list naming all 14 MEDIUM-risk atoms by current state; prior `axis-b-candidates.md` from first cycle is starting input only. |
| 11 | G5 wire-frame variance handling (C12) | §5 Phase 1 §1.1 failure handling | Variance > 50 bytes: proceed with content phases but G5 stays NEEDS-ROOT-CAUSE; final SHIP gate cannot pass G5 while NEEDS-ROOT-CAUSE; resolution = correct probe / widen threshold with evidence / downgrade to documented deferral. |

**Re-validation requirement**: future fresh sessions reading this
plan should treat §3, §5, §8, §12 as the binding rules. §16
preserves the *why* trail; the in-place edits are the SOURCE OF
TRUTH. Do NOT re-apply amendments — they are already in the body.

**Codex citations spot-checked** (per memory rule
`feedback_codex_verify_specific_claims.md`): 5 out of 5 sampled
file:line claims verified exactly against the live corpus +
plan text. See the verification appendix in the Codex round
artifact for evidence.
