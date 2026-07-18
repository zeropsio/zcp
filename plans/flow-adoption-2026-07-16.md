# /flow — ZCP dev-flow adoption plan (Matt Pocock corpus → ultra-customized ZCP flow)

**Date**: 2026-07-16 · **Status**: M1 DONE (package authored + repo edits landed, uncommitted) → M2 owner smoke test
**Codex review**: SOUND-WITH-AMENDMENTS (10 findings, all incorporated below;
review file `/tmp/codex-out-1784214664-72803-7045.md`).
**Sources**: `topics.ai-coding-workflow` (compiled 9-phase package),
`people.matt-pocock` (354 knowledge notes, book, dossiers, claim tensions), plus a
current-state audit of zcp's flow assets (team-analyze/team-execute, 6 skills,
2 agents, 8 hooks, plans/ conventions, test/eval surfaces). 42 extraction agents +
4 Opus synthesis lenses + Codex second opinion.

## Goal

One user-invoked flow carrying every non-trivial zcp change through:
analyze → **live-verified plan** → vertical-slice decomposition with tests →
**AFK** multi-agent TDD implementation → full assembled verification →
**owner retest pack** → spec reconciliation. Token-efficient: nothing always-loaded
beyond a skill description + ≤6 CLAUDE.md pointer lines.

## Canonical vocabulary (all package files use EXACTLY these)

- Phases: **FRAME, PROVE, SHAPE, BUILD, ASSEMBLE, LAND**.
- Plan file `plans/<slug>-<YYYY-MM-DD>.md`, H2 sections in order:
  `## Run State` · `## Frame` · `## Evidence Ledger` · `## Slice Register` ·
  `## Verify Trace` · `## Promotion`.
- Run State fields: `phase:` (frame|prove|shape|awaiting-approval|build|assemble|
  awaiting-retest|land|archived) · `base:` (SHA the plan was approved against) ·
  `integration:` (current integration SHA + landed commit range) · `approved:`
  (Rev-N, date — owner approval; **material edits to Frame or Slice Register after
  approval reset phase to awaiting-approval**) · `codex:` (verdict + review file) ·
  `next:` (single next action, zero-context executable).
- Slice Register columns: `ID | Title | Depends | Files | Layers | Gate | State`;
  Gate ∈ autonomous|review|owner; State ∈ pending|building|landed|blocked.
- Ledger entry fields: `claim / gates / surface / command / observed / verdict /
  promote`; verdict ∈ CONFIRMED|REFUTED|INCONCLUSIVE.
- Retest pack sections: `Run / Drive / What changed / Rollback / Docs`.

## Design decisions (Rev-2)

### D1 — Six phases, two owner gates

| # | Phase | Runs it | Exit gate |
|---|-------|---------|-----------|
| 1 | FRAME | Fable + owner dialogue | Outcome, AC1..n (each with planned evidence), non-goals, risk class, tagged assumption list |
| 2 | PROVE | platform-verifier (+ zerops MCP reads) | every load-bearing assumption resolved: `[VERIFIED]` by repo evidence or CONFIRMED live; REFUTED → back to FRAME |
| 3 | SHAPE | Fable; **codex-brief gate** | slice register + briefs + verify plan; Codex review clean |
| — | **OWNER GATE 1** | Karel approves register; **spec-shaped contracts are promoted to `docs/spec-*.md` immediately upon approval** — BUILD briefs cite the spec §, never the plan (plans are never a source, CLAUDE.md) | |
| 4 | BUILD | Sonnet subagents, AFK, worktree waves | per slice: RED replay passed, layer tests, lint; slice landed on integration branch |
| 5 | ASSEMBLE | fresh verifier session | whole-feature battery green; Verify Trace complete; retest pack written |
| — | **OWNER GATE 2** | Karel runs the retest pack | |
| 6 | LAND | Fable; `/code-review` on the diff | findings dispositioned; **spec reconciled** with shipped behavior; plan archived |

FRAME+PROVE+SHAPE in one owner-present block; BUILD+ASSEMBLE (incl. retest-pack
generation) is the AFK run. Codex/code-review gates are automated, not owner stops.
Matt mapping: Explore+Define→FRAME; Design+Plan+Decompose→SHAPE (Design becomes a
SHAPE step only when a material trade-off appears); Review+Handoff→LAND. PROVE is
the standing phase Matt's package lacks. LAND *reconciles* the spec (promotion
authority sits at GATE 1, per the dataconsole exemplar).

### D2 — Artifact chain: ONE plan file + at most two siblings

Plan file with the six canonical sections (template-enforced). Run State makes the
file resumable by a zero-context session — `/flow resume` reads it; explicit
`/flow build|assemble|land` **revalidates prerequisites, never bypasses gates**.
Owner-facing sibling `…retest.md`; `…handoff.md` only when a run halts mid-wave.
No `.analysis-N/.context/.vN` sibling family — in-place Rev-N edits, git is history.
ACx numbering threads Frame → slices → Verify Trace → retest pack.

### D3 — Router: LITE is an ALL-conditions rule; FULL triggers are risk-typed

- **LITE** only when ALL hold: bounded single seam · existing test oracle covers
  the area · no public wire-contract / security / credential / lifecycle-state /
  concurrency / destructive-action implications · no live-platform mutation ·
  reversible in one session. → `problem-solving` discipline, one vertical slice,
  RED(reproduce)→GREEN→REFACTOR, existing hooks gate. No plan file.
- **FULL** on any hard trigger: load-bearing platform-API assumption · new MCP
  tool · public wire contract or schema change · security/credential surface ·
  destructive/irreversible op or migration · cross-process lifecycle/state ·
  concurrency primitives · multi-session scope · owner asks. Layer count is a
  sizing signal, not a trigger (an ops+tools one-line fix stays LITE; a
  single-layer credential change is FULL).
- Unknown-cause bug: problem-solving root-cause first, deterministic regression
  test first; re-route once the cause names its risk class.
- Router announces verdict + trigger; a skip is justified, never silent.

### D4 — PROVE: assumption-probe protocol (evidence ledger)

Planner tags every material claim `[VERIFIED]` (existing code/test/spec proves it —
cite path) / `[PROBE]` (uncertain AND load-bearing) / `[ASSUMED]` (uncertain, not
load-bearing — stated, not probed). **Budget: ≤3 falsifiable claims probed per
plan** (a claim may need several calls; over budget ⇒ split the plan). Surface
ladder (cheapest that proves it): repo read → zerops MCP read (`zerops_discover`/
`verify`/`env` on `zcp-eval-clean`) → platform-verifier temp live E2E → spike
`_probe_test.go` under `//go:build e2e` (deleted after; assertion promoted to a
permanent test in a BUILD slice). Verdicts: **REFUTED** → back to FRAME.
**INCONCLUSIVE** (infra failure / ambiguous evidence — distinct from refuted):
retry once; still inconclusive → reframe to remove the dependency, split/defer
that part, or halt with a blocked handoff. Both block SHAPE while unresolved.
platform-verifier alignment (M1): map its PARTIAL/UNTESTABLE → INCONCLUSIVE; pin
target project `zcp-eval-clean`; unique `tmpverify-<run>` namespaces; **redact
tokens/credentials before anything enters the ledger** (its Bash token extraction
must never leak into a tracked file). Ledger with all `[PROBE]` rows CONFIRMED is
a hard precondition of the codex-brief gate. PROVE auto-skips when no `[PROBE]`
claims exist (then D1's exit reads "resolved by repo evidence").

### D5 — SHAPE: slice register + tracer bullet + Codex gate

Register per canonical columns; `Files` is the slice's declared write-set —
**overlapping write-sets must not share a wave**. S1 = tracer bullet: thinnest
real end-to-end path (a real tool call against `zcp-eval-clean` where applicable)
proving the route before fan-out. Briefs are self-contained (dna core + BUILD
addendum + slice scope + spec § citations). Whole plan → `codex-brief` (standing
rule). GATE 1: owner approves register; approved contracts promoted to spec
immediately; approval recorded in Run State; **material post-approval edits reset
to awaiting-approval**.

### D6 — BUILD: AFK worktree waves with REPLAYED RED

Wave algorithm (per Codex F4): every wave branches from the **current integration
SHA**; independent slices (disjoint write-sets) run in parallel worktrees; each
slice ends as one atomic commit (`(S<n>)` tag); orchestrator integrates in declared
dependency order — after each integration runs the slice's named tests + `make
lint-fast`; a conflict halts the wave → re-brief from the new integration SHA;
stale worktrees cleaned; landed range recorded in Run State (pins LAND rollback).

RED contract (per Codex F6 — replay, not transcript trust):
- Slice brief names its test(s) `Test{Op}_{Scenario}_{Result}` up front.
- Implementer works RED→GREEN→REFACTOR; reports `-count=1 -v` outputs + exit codes.
- **Orchestrator acceptance = RED replay**: in a scratch worktree at the slice's
  base SHA, apply ONLY the slice's test files (diff-restricted) and run the named
  tests — they must FAIL (assertion or missing-symbol per below); then at the
  slice head the same command must PASS. Fabricated transcripts die here.
- Additive-seam exception: a compile RED is valid ONLY as the exact
  missing-symbol/type error of the new seam; then a minimal compiling skeleton
  must produce an assertion-level RED before behavior lands. Unrelated
  syntax/import failures are invalid REDs.
- Independent oracle: expected values from spec/known-good literals, never
  recomputed the code's way. Tests on public seams (`ops.*`/tool output),
  table-driven. Layer matrix per CLAUDE.md change-impact rule.

AFK stop conditions (halt + handoff): scope drift, material unknown, acceptance
change, repeated unexplained check failure. `task-completed.sh` keeps hard-gating.

### D7 — ASSEMBLE: whole-feature battery (verified-composite)

On the integrated result: `make test-race` · `make lint-local` · `make vet-tags` ·
`make e2e-zcp-fast` (after M1 regex fix) · `make e2e-zcp-deploy` iff the feature
touches deploy/import/export/launch · behavioral eval iff agent-behavior-facing —
**only against the dedicated disposable eval project** (preflight: assert the eval
token's project is NOT `zcp-eval-clean`/the PROVE testbed — `internal/eval/
cleanup.go` deletes all non-system services except `zcp`; pointing it at the
testbed would erase the managed-service fleet; Codex F1) — consumed as signal, not
gate · end-to-end drive of the real binary's headline path against `zcp-eval-clean`.
Verify Trace rows `ACx | check | passed/failed/blocked/not-run | evidence`; any
red blocks owner handoff. Then generate the retest pack.

### D8 — Retest pack (owner gate 2) and LAND

`…retest.md`: Run (exact commands, each with the ONE expected line) / Drive (steps
against `zcp-eval-clean`, expected observations) / What changed (1 line per slice)
/ Rollback (`git revert <range>` from Run State) / Docs (spec §§ touched at GATE 1).
Zero-context executable in minutes; every step ties to an ACx. LAND after owner
confirms: `/code-review` on the branch diff (findings fixed or dispositioned);
**spec reconciliation** (shipped behavior == promoted spec; deltas fixed on
whichever side is wrong); PROVE probes exist as permanent tests; plan (+siblings)
`git mv` → `plans/archive/`.

### D9 — dna.md: flow epistemics ONLY (no CLAUDE.md duplication)

`dna.md` (~30 lines) holds only what CLAUDE.md does NOT: evidence grades
VERIFIED/LOGICAL/UNVERIFIED + citation tags `[KB:]`/`[SELF-VERIFIED:file:line]`/
`[UNVERIFIED]`; fix-upstream-not-downstream; verify-don't-assume (Zerops ≠ K8s);
observed-verification (no check reported without seeing its result);
no-silent-scope; self-contained-brief rule; AFK stop conditions. Project rules
(architecture, TDD, tiers, conventions) are NOT restated — subagents receive
CLAUDE.md automatically; briefs cite specific CLAUDE.md/spec sections when a rule
is load-bearing. The team-* "350-line cap" is dropped (contradicts CLAUDE.md's
cohesion-not-line-count rule).

### D10 — Packaging (12 files, progressive disclosure)

```
.claude/skills/flow/
  SKILL.md              router (LITE/FULL), phase map + gates, load discipline, resume
  dna.md                flow epistemics (D9)
  phases/1-frame.md     task-type routing, complexity→agents, read-only analysis
                        (Explore mode=plan + zerops-knowledge KB), Frame fields
  phases/2-prove.md     D4 protocol, platform-verifier envelope, ledger format
  phases/3-shape.md     slicing rules, write-sets/waves, tracer bullet, briefs,
                        codex-brief gate, GATE 1 + spec promotion
  phases/4-build.md     wave algorithm, RED replay contract, slice report, stops
  phases/5-assemble.md  battery incl. eval-project preflight, Verify Trace, retest pack
  phases/6-land.md      code-review gate, spec reconciliation, archive
  templates/plan.md     6-section skeleton (Run State, ledger + register tables)
  templates/slice-brief.md  self-contained brief skeleton
  templates/retest.md   owner retest pack skeleton
  templates/handoff.md  blocked/multi-wave handoff skeleton
```
Budgets: SKILL.md ≤70 lines; phases ≤85; templates ≤45; dna ≤35. Always-on cost =
the skill description line. Phase files load on phase entry; templates only when
producing that artifact.

### D11 — Existing assets: verdicts

KEEP: audit, problem-solving (LITE owner), plan-summary, commit, release,
code-review/simplify/run/verify built-ins, zerops-knowledge, permissions,
AGENTS.md bridge, CLAUDE.md map discipline, all hard hook gates.
EVOLVE: platform-verifier → standing PROVE executor (keep safety envelope:
`tmpverify*`, `/tmp/zcp-verify/`, read-only SSH, <30-day memory; add D4 alignment).
ABSORB: team-analyze strengths (task-type router, complexity scoring, evidence
grading, adversarial challenge at deep tier, Explore+mode=plan read-only, git
tripwire) → 1-frame.md; team-execute strengths (worktree isolation, waves, two
hard gates, TDD protocol, execution reporting) → 4-build.md.
REPLACE→delete at M4: team-analyze.md + team-execute.md (deprecation pointer at
top of each until then; no shims). Their hardcoded composition-heuristics paths
are dropped — SHAPE greps the live tree.
FIX NOW: `on-failure.sh` auto-memory append (violates global rule) — delete the
append block, keep hints. `Makefile` `e2e-zcp-fast` `-run` regex names dead test
stems (Process|Scaling|LogSearch) — false green (Codex F5); fix to the actual
`TestE2E_*` set before the flow relies on it.

### D12 — Enforcement now vs backlog

Now: no new blocking hooks; all existing gates unchanged. Backlog (each its own
decision after the flow runs): RED-replay automation as a script; ledger-unresolved
block; Stop-vs-TaskCompleted alignment; role-aware SubagentStart; check-claude-md
watch-list refresh; read-only `zerops_*` allowlist; e2e test-selection drift pin;
behavioral-eval project-identity preflight as code.

Owner amendment (2026-07-17): full lint now gates commit via `.githooks/pre-commit`.

### D13 — Matt ideas deliberately NOT adopted

Per-message `Phase:/Mode:` headers (gates live in the plan file); "delete
CLAUDE.md" advice (zcp's map IS the remedy done right); standing prototype phase
(collides with TDD-mandatory; spikes only inside PROVE, deleted after);
per-response mode labels (plan-anchored autonomy); Ralph-style context loops
(worktree slices already reset context).

### D14 — Naming and invocation

Skill `flow`; `/flow <request>` routes LITE/FULL; `/flow resume plans/<file>`
re-enters at Run State's phase; `/flow build|assemble|land plans/<file>`
revalidates that phase's entry conditions (never bypasses). New files use
`SKILL.md`; existing `audit/skill.md` untouched.

## Migration checklist

- M1 (this change): 12-file `.claude/skills/flow/` package · `on-failure.sh` fix ·
  `Makefile` e2e-zcp-fast regex fix (verified against `go test -tags e2e ./e2e
  -list`) · platform-verifier.md D4 alignment · deprecation pointers on both
  team-*.md · CLAUDE.md "Dev flow" pointer (≤6 lines) · AGENTS.md 1 bridge line ·
  this plan.
- M2: owner smoke-tests `/flow`: one LITE fix + one FULL feature with a platform
  assumption (PROVE fires).
- M3: tune wording/budgets from the first run.
- M4: parity checklist passed → `git rm` both team-* commands. Parity = each
  exercised once: LITE route · FULL with PROVE probe · dependent worktree wave ·
  `/flow resume` after interruption · RED replay acceptance · ASSEMBLE battery ·
  both owner gates · LAND reconciliation+archive.
- Backlog: D12 items · `plans/README.md` live-index + archive nudge ·
  zerops-knowledge architecture-block pointer fix.

## Risks (top 5, mitigated)

1. Always-loaded creep → everything behind one skill description; CLAUDE.md gains
   pointer lines only.
2. PROVE burns time/container → ≤3-claim budget, cheapest-surface ladder, <30-day
   verified-fact memory, auto-skip, INCONCLUSIVE retry-once rule.
3. Two flows coexist → deprecation pointers now, M4 deletion on parity checklist.
4. codex gate hangs → helper path, background + timeout, stop gate OFF, no
   parallel Claude sessions in-repo.
5. Behavioral eval erases the testbed → D7 preflight mandate + backlog code guard;
   never point flow-eval at `zcp-eval-clean`.
