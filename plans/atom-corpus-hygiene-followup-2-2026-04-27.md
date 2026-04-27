# Plan: Atom corpus hygiene follow-up #2 — content-quality refinements (2026-04-27)

> **Reader contract.** Self-contained for a fresh Claude session.
> Sister plans:
> - `plans/atom-corpus-hygiene-2026-04-26.md` (cycle 1; SHIP-WITH-NOTES)
> - `plans/archive/atom-corpus-hygiene-followup-2026-04-27.md`
>   (cycle 2; SHIP-WITH-NOTES, archived; both pre-authorized
>   notes documented).
> This plan addresses 5 user-surfaced findings from a 2026-04-27
> post-SHIP-WITH-NOTES review of the post-cycle-2 corpus. The
> 6th finding (auto-handle orphan metas) is engine work — see
> `plans/engine-atom-rendering-improvements-2026-04-27.md`.

## 1. Problem

After cycle 2 SHIPped SHIP-WITH-NOTES (cumulative −29,231 B
across both cycles; 4/5 G3 PASS), the user audited specific
atoms in the post-cycle-2 corpus and surfaced 5 content-quality
findings (Finding 6 is engine work — separate plan):

1. **F1 — `develop-first-deploy-scaffold-yaml` content-root tip**:
   tilde-extract / preserve guidance is a runtime-corner-case
   (ASP.NET wwwroot, Java WAR) that already cross-links to
   `develop-deploy-modes`. The duplicate forward-pointer + the
   trailing schema-fetch line are corpus space the atom doesn't
   need.

2. **F3 — `zcli push` mechanism leakage** across 8 atoms:
   the agent calls `zerops_deploy`; the dispatch-layer detail
   (`zcli push under the hood`) is mostly noise. 5 atoms
   benefit from DROP/REPHRASE; 3 atoms (push-git Actions
   config + trigger-distinguisher) genuinely need the
   `zcli push` mention.

3. **F4 — `develop-push-dev-workflow-dev` cycle correctness**:
   "After each edit, run `action=restart`" is over-eager —
   most modern dev runners (`npm run dev`, `vite`, `nodemon`,
   `air`, `fastapi --reload`) auto-watch the SSHFS mount.
   Plus the atom assumes prior `action=start` happened in a
   different atom (`develop-dynamic-runtime-start-container`)
   without explaining the connection.

4. **F5 — `develop-static-workflow` per-env detail leak**:
   step 1 "Edit files locally, or on the SSHFS mount in container
   mode" duplicates per-env detail already established by
   `develop-platform-rules-{local,container}` (which always
   co-fire). This surfaces a NEW corpus-wide axis (Axis N) —
   universal atoms shouldn't carry per-env detail. Audit
   corpus-wide.

5. **F1 (continued) — schema-fetch line at end of scaffold-yaml**:
   "fetch zerops.yaml JSON Schema via zerops_knowledge if
   unsure" — generic advice the agent already has via tool
   surface.

(F2 + F6 omitted — F2 KEEP-AS-IS; F6 engine work in separate
plan.)

## 2. Goal

Apply the 5 atom-level findings as a tight follow-up cycle:

1. **F1**: drop content-root tip + schema-fetch line from
   `develop-first-deploy-scaffold-yaml`.
2. **F3**: revise 5 atoms with DROP/REPHRASE; KEEP `zcli push`
   in 3 (push-git Actions config + trigger-distinguisher).
3. **F4**: rewrite `develop-push-dev-workflow-dev` cycle to be
   auto-reload aware + clarify start/restart split.
4. **F5 + Axis N**: drop per-env detail from
   `develop-static-workflow`; codify Axis N
   (`docs/spec-knowledge-distribution.md §11.6` new); corpus-wide
   Codex CORPUS-SCAN for Axis N candidates; apply.
5. **F5 inverse**: any env-split atoms differing only in
   marginalia → unification + cross-link to per-env
   platform-rules atoms.

Estimated recovery: **~1,500-3,000 B aggregate** (most
concentrated on F1 because the scaffold atom fires on every
first-deploy envelope; F4 is net-neutral but clarity-positive).

## 3. Mental model — Axis N (NEW; "env detail leakage")

### Axis N — UNIVERSAL ATOM CARRIES PER-ENV DETAIL

**Definition**: an atom WITHOUT `environments:` axis restriction
(or with both env values implicitly) carries env-specific
edit-location, runtime-shell, or storage-layer detail. The
per-env context is already established by
`develop-platform-rules-local` and `develop-platform-rules-
container` (which always co-fire on develop-active envelopes).
Universal atoms should write at the universal-truth layer; let
per-env atoms fill the where-to-edit / how-to-run gaps.

**Distinction from Axis K**:
- Axis K = atom mentions OUTSIDE-its-envelope flow detail
  (cross-flow leakage). E.g., container atom mentions local mode.
- Axis N = atom WITHOUT env axis carries env-specific detail
  that belongs in per-env atoms (within-flow over-specification).

**Judgment test (per phrase)**: would an agent on EITHER env
benefit from this phrase, or does it hard-code one env's
mental model?

**Examples** (post-cycle-2 state):

- `develop-static-workflow.md:13` "Edit files locally, or on the
  SSHFS mount in container mode." → drop the environment split;
  rewrite "Edit files." (per-env edit location is in
  platform-rules-{local,container}).
- `develop-strategy-review.md:15` "`zerops_deploy` (zcli push
  from your workspace: dev container → stage, or local CWD →
  stage)." → drop the parenthetical (per-env shape in
  push-dev-deploy-{container,local}).

**Inverse rule (UNIFICATION candidate)**:

> If two env-split atoms differ ONLY in env-marginal phrasing
> (e.g. one says "edit files locally", the other says "edit
> files on SSHFS mount") and the per-env detail is already in
> platform-rules atoms, the two are candidates for UNIFICATION
> into a single env-agnostic atom. Resolution: merge atoms +
> cross-link to per-env platform-rules atoms; drop the env axis
> on the merged atom.

**DO-NOT-UNIFY exception**: if the env split itself encodes a
tool-selection (signal #3), recovery (#4), or do-not (#5)
guardrail — e.g., `develop-platform-rules-local` L14-L24 (use
`Bash run_in_background=true` harness; `zerops_dev_server` is
container-only) vs `develop-platform-rules-container` L20-L24
(use `zerops_dev_server`; do NOT hand-roll
`ssh <host> "cmd &"` backgrounding) — the env split IS the
load-bearing signal. Such atoms are NEVER unification candidates
regardless of marginal phrasing similarity. Phase 4 CORPUS-SCAN
classifier MUST apply this exception before flagging a pair as
UNIFICATION-CANDIDATE.

**Risk**: low. If the agent reading a universal atom needs the
per-env detail and the platform-rules cross-link doesn't render
(e.g. axis-mismatch), it would surface as missing-information.
Mitigation: corpus-coverage tests + manual fixture re-render
per Phase 4 sub-pass.

## 4. Baseline (snapshot 2026-04-27 post-cycle-2 SHIP)

### 4.1 Corpus state

```
79 atom files
~ aggregate corpus body bytes: ~95 KB (post-cycle-2)
Per-fixture body sizes (post-cycle-2 SHIP):
  develop_first_deploy_standard_container          20,643 B
  develop_first_deploy_implicit_webserver_standard 21,947 B
  develop_first_deploy_two_runtime_pairs_standard  22,394 B
  develop_first_deploy_standard_single_service     20,588 B
  develop_simple_deployed_container                16,085 B
```

### 4.2 Composition scores post-cycle-2

| Fixture | Coh | Den | Red | Cov-gap | Task-rel |
|---|---|---|---|---|---|
| standard | 4 | 3 | 2 | 4 | 4 |
| implicit-webserver | 3 | 3 | 2 | 3 | 4 |
| two-pair | (per artifact) | (per artifact) | 1* | 4 | (per artifact) |
| single-service | (per artifact) | (per artifact) | 2 | 4 | (per artifact) |
| simple-deployed | (per artifact) | (per artifact) | 3 | 4 | 4 |

*two-pair Redundancy held at 1 — STRUCTURAL per-service render
duplication; engine-level fix per
`engine-atom-rendering-improvements-2026-04-27.md`.

### 4.3 Findings backlog (this plan)

- F1: develop-first-deploy-scaffold-yaml content-root + schema → DROP both. Est ~270 B per render × 4 first-deploy fixtures = ~1,080 B.
- F3: 8 atoms with `zcli push`; 5 DROP/REPHRASE; 3 KEEP. Est ~400-600 B aggregate.
- F4: develop-push-dev-workflow-dev rewrite. Net-neutral; clarity positive.
- F5 + Axis N: corpus-wide audit + apply. Est ~200-1,000 B aggregate.

Total estimated recovery: ~1,500-3,000 B aggregate.

## 5. Phased execution

> **Pacing rule**: 5 phases (0-4) + Phase 5 EXIT. Each phase
> ENTRY/WORK-SCOPE/EXIT criteria. Codex protocol per cycle 2 §10
> (inherited). Trackers per cycle 2 §15.1 schema with `-v3`
> suffix.

> **Inherited machinery**: this plan REUSES cycle 2's §6
> methodology, §10 Codex protocol, §15 completeness machinery,
> §11 atom-consistency guardrails. Read those sections during
> Phase 0.

### Phase 0 — Calibration

**ENTRY**: working tree clean; HEAD = `281fb79f` (post-cycle-2
PLAN COMPLETE).

**WORK-SCOPE**:

1. §17 prereq P1-P11 (cycle 1 §17). Probe binaries no longer
   exist (cycle 2 G8 cleanup); recreate from git history if
   needed (`git show 3725157e:cmd/atomsize_probe/main.go`).
2. Re-baseline: re-create probe binary; render 5 fixtures via
   `PROBE_DUMP_DIR=plans/audit-composition-v3/rendered-fixtures-baseline /tmp/probe`.
3. Init tracker dir `plans/audit-composition-v3/`; write empty
   `phase-0-tracker-v3.md` per §15.1 schema.
4. **Phase 0 PRE-WORK Codex round**: validate the 5 findings'
   per-atom proposed actions against current corpus state. Catch
   any atom that's already at proposed-state.

**EXIT**:
- Probe baseline output committed to
  `plans/audit-composition-v3/probe-baseline-2026-04-27-v3.txt`.
- Phase 0 PRE-WORK Codex round APPROVE.
- Tracker `phase-0-tracker-v3.md` committed.
- Verify gate green.

### Phase 1 — F1: develop-first-deploy-scaffold-yaml drops

**ENTRY**: Phase 0 EXIT satisfied.

**WORK-SCOPE**:

1. Drop content-root tip block (current L41-45) — tilde-extract
   / preserve detail; cross-link to `develop-deploy-modes`
   already exists at L24 + L39.
2. Drop trailing schema-fetch line (current L47) — generic
   advice; agent has `zerops_knowledge` tool.
3. **Codex per-edit round** OPTIONAL — risk LOW; signal-check
   confirms no HIGH-risk Axis K signal in dropped lines.
4. Verify gate after commit.
5. Re-run probe; measure aggregate impact.

**EXIT**:
- F1 atom edits committed.
- Probe re-run shows expected ~1,000-1,200 B aggregate first-
  deploy slice reduction (atom fires once per first-deploy
  fixture; 4 fixtures + simple-deployed unaffected).
- `phase-1-tracker-v3.md` committed.

### Phase 2 — F3: zcli push refs cleanup

**ENTRY**: Phase 1 EXIT satisfied.

**WORK-SCOPE**:

1. **Codex CORPUS-SCAN** (verification): re-grep corpus for
   `zcli push` references. Cross-reference against the F3 table
   below. Confirm uniqueness of each occurrence.

2. **Per-atom action table**:

| atom | line | action | rationale |
|---|---:|---|---|
| `develop-push-dev-deploy-local` | L13 | DROP "`zcli push`" mention; rewrite without dispatch detail | Implementation detail; agent calls `zerops_deploy` |
| `develop-deploy-files-self-deploy` | L23 | REPHRASE — preserve recovery guardrail (signal #4) without `zcli push` mechanism | Keep "subsequent self-deploys would have no source to upload" form |
| `strategy-push-git-trigger-actions` | L12, L75, L112 | KEEP all | Actions trigger model (L12); literal config (L75); error context (L112) |
| `strategy-push-git-intro` | L22 | KEEP | trigger-distinguishing in actions row |
| `develop-platform-rules-local` | L31 | REPHRASE row — drop `zcli push` mechanism mention; PRESERVE push-dev-vs-git-push uncommitted-tree distinction (push-dev ships uncommitted edits; git-push needs commits). | The `zcli push` mechanism phrase duplicates push-dev-deploy-local L13, but the strategy-uncommitted-tree guardrail is unique to this row (Codex round 1 catch). |
| `develop-first-deploy-asset-pipeline-container` | L17 | REPHRASE — `zcli push` → `zerops_deploy` | Agent's command is `zerops_deploy` |
| `develop-strategy-review` | L15 | DROP parenthetical "(zcli push from your workspace…)" | Mechanism detail |
| `develop-first-deploy-asset-pipeline-local` | L32 | REPHRASE — `zcli push` → `zerops_deploy` | Agent's command |

3. Apply per atom; commit per concept (one commit for "zcli
   push DROP/REPHRASE" covering 5 atoms).
4. **Codex POST-WORK round** (sample): verify no Axis K signal
   loss across the 5 touched atoms. The recovery guardrail in
   `develop-deploy-files-self-deploy` is the critical signal to
   verify.

**EXIT**:
- F3 atom edits committed (one commit covering 5 atoms;
  fact-inventory in commit message per §6.1).
- Codex POST-WORK APPROVE.
- Probe re-run.
- `phase-2-tracker-v3.md` committed.

### Phase 3 — F4: develop-push-dev-workflow-dev cycle rewrite

**ENTRY**: Phase 2 EXIT satisfied.

**WORK-SCOPE**:

This atom is HIGH-risk per cycle-2 axis-b classification. The
rewrite touches dev-mode iteration cycle semantics — the most
common develop-mode flow. **Mandatory Codex per-edit round**.

**Issues**:

1. "After each edit, run `action=restart`" — wrong framing;
   most modern dev runners auto-watch.
2. First-time `action=start` lives in a different atom
   (`develop-dynamic-runtime-start-container`); this atom
   doesn't explain the connection.
3. Restart-vs-start semantics need clarification.

**Proposed rewrite** (~net-neutral bytes; clarity-positive):

```markdown
### Development workflow

Edit code on `/var/www/{hostname}/`. The dev process is already
running (see `develop-dynamic-runtime-start-container` for
first-time start). **Code-only edits never trigger
`zerops_deploy`** — deploy is for `zerops.yaml` changes only
(see "**`zerops.yaml` changes**" below).

**Code-only edit cycle**:
- Dev runners with file-watch (`npm run dev`, `vite`, `nodemon`,
  `air`, `fastapi --reload`) pick up edits **only when configured
  for polling** — SSHFS does not surface inotify events. Set
  `CHOKIDAR_USEPOLLING=1` (vite/webpack), `--poll` (nodemon), or
  the runner's equivalent.
- Otherwise (non-watching runner, polling not configured, OR the
  process died), `zerops_dev_server action=restart hostname="{hostname}"
  command="{start-command}" port={port} healthPath="{path}"`.
  The response carries `running`, `healthStatus`, `startMillis`,
  and on failure a `reason` code (see
  `develop-dev-server-reason-codes`) — read it before issuing
  another call.

**`zerops.yaml` changes** (env vars, ports, run-block fields):
`zerops_deploy` first; container is replaced; on the rebuilt
runtime container use `action=start` (NOT restart). See
`develop-platform-rules-common` for the deploy=new-container
rule.

**Diagnostic**: tail the log ring with
`zerops_dev_server action=logs hostname="{hostname}"
logLines=60`. `reason` classifies the failure (connection
refused, HTTP 5xx, spawn timeout, worker exit) without a
follow-up call.
```

Round-1 PER-EDIT Codex round flagged two defects in the prior
proposal (now resolved above):

- **No-redeploy guardrail lost** — current atom L25 says
  "Code-only changes: `action=restart` is enough — no redeploy"
  (signal #5 do-not). The first proposal dropped it. Revised
  rewrite restores via the leading "**Code-only edits never
  trigger `zerops_deploy`**" sentence and the explicit cross-ref
  to the `zerops.yaml` changes section.
- **False auto-watch claim** — "auto-watch the SSHFS mount — no
  action needed" is wrong: SSHFS does not surface inotify events;
  watchers need polling mode. Revised rewrite says runners with
  file-watch "pick up edits only when configured for polling"
  and lists concrete env vars / flags (`CHOKIDAR_USEPOLLING=1`,
  `--poll`).

1. **Codex per-edit round (MANDATORY per HIGH-risk classification)**:
   review the rewrite + verify all Axis K signals preserved
   (signal #3 tool-selection, signal #4 recovery, signal #5
   do-not).
2. Apply edit.
3. **MustContain pin migration check** — grep for any pinned
   phrase in `corpus_coverage_test.go` referencing this atom's
   body. If pinned phrase is dropped, migrate.
4. Verify gate.

**EXIT**:
- F4 atom rewrite committed.
- Codex per-edit round APPROVE.
- Pin migration handled (if any).
- Probe re-run; confirm `develop_push_dev_dev_container` fixture
  byte size shifts as expected.
- `phase-3-tracker-v3.md` committed.

### Phase 4 — F5 + Axis N corpus-wide

**ENTRY**: Phase 3 EXIT satisfied.

**WORK-SCOPE**:

1. **Codify Axis N in spec**: add §11.6 to
   `docs/spec-knowledge-distribution.md` with the rule + the
   example + the inverse rule (unification candidate). Mirror
   shape of the existing §11.5 (axes K/L/M).

2. **Codex CORPUS-SCAN for Axis N candidates** corpus-wide. For
   each atom WITHOUT `environments:` axis restriction (or
   with both env values), grep body for env-specific tokens:
   - "locally", "your machine", "your editor", "your IDE"
   - "SSHFS", "/var/www/{hostname}", "container env", "local env"
   - "on your CWD", "on the mount", "via SSH"

   Per match: classify as
   - **DROP-LEAK** (atom is universal; per-env detail belongs
     in platform-rules-{local,container}; drop and rely on
     per-env atoms to fill).
   - **KEEP-LOAD-BEARING** (the per-env detail IS the
     guardrail; can't be dropped without losing operational
     guidance).
   - **SPLIT-CANDIDATE** (atom genuinely needs per-env split;
     candidate for axis-restriction tightening).
   - **UNIFICATION-CANDIDATE** (atom is currently env-split but
     marginalia is env-irrelevant; merge candidate).

3. Output `plans/audit-composition-v3/axis-n-candidates.md` —
   mirror axis-k-candidates.md shape from cycle 2 Phase 2.

4. Apply per-atom: F5 work units in `develop-static-workflow`:
   (a) **L13** "Edit files locally, or on the SSHFS mount in
       container mode." → "Edit files."
   (b) **L27-L28** "`push-dev` for fast iteration on a dev
       container over SSH." → drop the "on a dev container over
       SSH" qualifier (env-leak; per-env edit-location detail
       lives in `develop-platform-rules-{local,container}`).
       Codex round 1 catch.
   Apply F5 first; corpus-wide drops from CORPUS-SCAN follow.

5. **Per-edit Codex round** for any DROP-LEAK that touches a
   broad atom (priority 1-3). LOW-risk for narrow atoms
   (priority 4-7).

6. **POST-WORK Codex round**: verify no platform-rules
   cross-link broken (the universal atom now relies on
   platform-rules atoms ALWAYS firing on develop-active
   envelopes — confirm via fire-audit-style check).

**EXIT**:
- §11.6 Axis N added to spec.
- `axis-n-candidates.md` committed.
- F5 atom edits + corpus-wide Axis N applies committed (one
  commit per concept; fact-inventory per §6.1).
- Codex POST-WORK APPROVE.
- Probe re-run; aggregate impact measured.
- `phase-4-tracker-v3.md` committed.

### Phase 5 — Final composition re-score + SHIP

**ENTRY**: Phase 4 EXIT satisfied.

**WORK-SCOPE**:

1. Re-render 5 fixtures via probe.
2. **Codex composition re-score** (CORPUS-SCAN per cycle 1
   §10.1 P7 row 2). Compare to §4.2 baseline (post-cycle-2)
   AND original cycle 1 §4.2 baseline.
3. Verify §15.3 G3 strict-improvement holds — not regressing
   from post-cycle-2 PASS state on the 4 PASS fixtures.
4. **G5 binding re-run** (idle envelope smoke on post-cycle-3
   binary). Compare to cycle-2 G5.
5. **G6 binding re-run** (`develop-add-endpoint` scenario
   on eval-zcp). Compare PASS verdict + duration.
6. **Final Codex SHIP VERDICT round** per cycle 1 §10.3.

**Target SHIP outcome**: clean SHIP for the 4 PASS fixtures;
two-pair STRUCTURAL note inherited from cycle 2 (engine work
unchanged); no NEW notes. Cycle 3 ships clean OR
SHIP-WITH-NOTES (inheriting cycle 2 notes, no new notes).

**EXIT**:
- Codex SHIP VERDICT returns SHIP / SHIP-WITH-NOTES.
- `final-review-v3.md` committed.
- Plan archived to `plans/archive/`.
- `phase-5-tracker-v3.md` committed.

## 6. Codex collaboration protocol

Inherit cycle 1 §10 + cycle 2 §6 amendments. Same patterns:
CORPUS-SCAN per finding (when corpus-wide), PER-EDIT
mandatory for HIGH-risk (F4 develop-push-dev-workflow-dev),
POST-WORK per phase, FINAL-VERDICT at Phase 5.

Estimated Codex rounds budget:
- Phase 0: 1 PRE-WORK
- Phase 1: 0 (LOW-risk DROP)
- Phase 2: 1 CORPUS-SCAN + 1 POST-WORK
- Phase 3: 1 PER-EDIT (mandatory)
- Phase 4: 1 CORPUS-SCAN + 1 POST-WORK
- Phase 5: 1 CORPUS-SCAN composition + 1 FINAL-VERDICT

Total: ~7 Codex rounds. Mostly compact (axis-N CORPUS-SCAN may
be larger if many candidates surface).

## 7. Acceptance criteria

- All 6 phases (0-5) closed per §15.2 trackers.
- §15.3 G1-G8 satisfied (inheriting cycle 2's SHIP-WITH-NOTES
  on G3 two-pair; no new G3 regressions).
- Codex final SHIP VERDICT: SHIP or SHIP-WITH-NOTES (no NEW
  notes vs cycle 2).
- Body recovery: ~1,500-3,000 B aggregate this cycle (combined
  cumulative across 3 cycles: ~30,500-32,000 B from cycle 1+2+3).
- Axis N documented in `docs/spec-knowledge-distribution.md
  §11.6`.

## 8. Out of scope (engine work — separate plan)

Per `plans/engine-atom-rendering-improvements-2026-04-27.md`:

1. **Multi-service single-render atom support** (consolidates
   cycle 2's two-pair structural fix + Finding 2 1-line `-cmds`
   atoms).
2. **`zerops_deploy` error-response enrichment** (so corpus
   doesn't need to teach the agent zcli internals;
   complements cycle-3 Phase 2 `zcli push` cleanup).
3. **Auto-handle orphan metas** (drops
   `idle-orphan-cleanup` atom; engine-side compute_envelope or
   bootstrap-start side-effect).
4. **K/L/M (and N) lint enforcement in
   `internal/content/atoms_lint.go`** (cycle 2's Phase 8+
   ticket #2; lint-rule additions to catch drift in future
   atom edits).

These engine tickets are NOT prerequisites for cycle 3; cycle 3
ships independent. Engine work proceeds on its own cadence per
the engine plan.

## 9. Provenance

Drafted 2026-04-27 after the user's post-cycle-2 corpus audit
surfaced 6 specific findings. 5 are atom-content concerns
addressed by this plan; 1 (auto-handle orphan metas) is engine
work in `plans/engine-atom-rendering-improvements-2026-04-27.md`.

The cycle-3 mini-plan is intentionally tight (5 phases, ~7
Codex rounds) — by-design smaller than cycle 2 because the
findings are surgical, not corpus-wide-systematic. Cycle 1
established the machinery; cycle 2 applied breadth-first axes
K/L/M; cycle 3 is finishing-touch refinement.

## 10. First moves for the fresh instance

**Step 0 — prereq verification (MANDATORY)**: walk every row of
`atom-corpus-hygiene-2026-04-26.md` §17 prereq checklist (P1-P11).

**Step 0.5 — verify corpus baseline matches §4.1**: 79 atoms;
post-cycle-2 5-fixture sizes per §4.1 line numbers. If sizes
differ, corpus shifted post-cycle-2; re-baseline this plan §4.1.

**Step 1 — read context**:

1. This plan end-to-end.
2. `plans/atom-corpus-hygiene-2026-04-26.md` §6, §10, §15, §17.
3. `plans/archive/atom-corpus-hygiene-followup-2026-04-27.md`
   §3 axes K/L/M, §16 amendments.
4. `plans/audit-composition/final-review-v2.md` (cycle 2's
   verbatim ship-verdict trail).
5. `plans/engine-atom-rendering-improvements-2026-04-27.md`
   (engine ticket cross-reference).
6. CLAUDE.md + CLAUDE.local.md — project conventions + auth.

**Step 2 — corpus baseline check**: re-create probe from
`3725157e`; render 5 fixtures; confirm baseline sizes match
§4.1 within ±2%.

**Step 3 — initialize tracker dir**: `plans/audit-composition-v3/`
with `phase-0-tracker-v3.md` per §15.1 schema.

**Step 4 — Phase 0 PRE-WORK Codex round** validating the 5
findings + the new Axis N rule. Cycle-3 starts only after
APPROVE.

**Step 5 — Begin Phase 1 work** (F1 drops; smallest blast
radius first).

## 11. Open questions for first reviewer

1. **Should the schema-fetch line at end of scaffold-yaml stay**
   if the agent has `zerops_knowledge`? Default DROP per F1
   above. Override only if there's evidence the agent fails to
   discover the tool without the prompt.

2. **F4 cycle rewrite — is the auto-reload assumption universal**
   across all dynamic runtimes ZCP supports? Default YES (npm,
   nodemon, vite, air, fastapi --reload, php artisan serve all
   auto-watch). If a specific runtime doesn't, the rewrite's
   "OR if the process died" clause covers it.

3. **Axis N corpus-wide audit — what's the false-positive risk**
   on env-implicit phrases like "your code" (which subtly maps
   to either env)? Default: only flag explicit env tokens
   (locally / SSHFS / etc.); leave ambiguous phrases.

## 12. Anti-patterns + risks

(Inherits cycle 1 §12 plus:)

- **Don't over-drop in F3 zcli-push cleanup**. The 3 KEEP atoms
  (push-git Actions config + trigger-distinguisher) genuinely
  need `zcli push` because the agent writes it literally in CI
  YAML or chooses strategy based on it.

- **Don't unify env-split atoms whose split IS the guardrail**.
  Some atoms split per env precisely because the operational
  rule differs (e.g. dev-server start: container uses
  `zerops_dev_server`; local uses `Bash run_in_background=true`).
  Unification would lose the distinction.

- **F4 rewrite must preserve the `zerops_dev_server` tool family**
  references (start/restart/logs) — these are signal #3
  tool-selection guardrails.

- **Axis N must NOT regress agent comprehension on env-specific
  guidance**. The platform-rules-{local,container} atoms ALWAYS
  fire on develop-active envelopes — but verify this via
  fire-audit before relying on the cross-link.
