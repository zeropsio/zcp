# Plan: zcp-actionable-analysis

## Run State
- `phase:` executed — owner picks applied 2026-07-17 (run wf_0bc5faae-098), working tree only, nothing committed
- `base:` 3a824246 (analysis); execution landed on top of 45cb6db3 (concurrent dataconsole session advanced the branch mid-day)
- `codex:` SOUND-WITH-AMENDMENTS (4 findings, all incorporated) — /tmp/codex-out-1784309296-70081-845.md
- `run:` wf_53c6ccb9-3a9 — 22 agents, 16 raw findings → 14 survived adversarial verify (2 killed, see appendix)
- `executed:` B1 (toolchain go1.25.12, govulncheck 0) · B4 (tool-call form + new Go-string-literal guard) · B7 (143 files + 8 dirs + nonauthoring + knowledge-arch-unification archived) · B8 owner variant (.githooks/pre-commit full lint; flow-adoption D12 amended) · B9 (all 33 lint issues cleared, lint-local 0) · A1 (TestAnnotations_DeployLocalTool) · A2 (12 worktrees + branches removed, ~617MB; + zcp-capture-raw sibling 207MB) · A3 (CLAUDE.local.md +3 rows; feat/capture-raw-prototype deleted, was fully merged) · A4 (plans/backlog/develop-guidance-cross-call-dedup.md extracted) · A5 (spec-knowledge-architecture Status owns its state) · A6 (hook says Go 1.25)
- `deferred by owner:` B2, B5, B6 (untouched) · Promote #1 (spec-git-delivery-target) not approved, plan retained
- `executed 2026-07-18 (run wf_c569dc58-621):` B3 (CanonicalRepoURL compare + mirror test) · B10 #18 (atom-id headers in Synthesize, 32 goldens regenerated, spec §11.6.6 updated) · B10 #15 (import applies project.envVariables, ProjectEnvsSet in result) · B10 #17 (isPlatformInjected on managed identity/credential keys, keys-only mode) · B10 #14/#13/#7 confirmed landed elsewhere, #8-12 dropped by owner (incl. Next.js gap) — multi-runtime plan archived with disposition note
- `next:` session commits (owner-directed 2026-07-18); remaining open: B2, B5, B6, Promote #1

## Frame — why this analysis is shaped this way

Owner's brief: past "repo analyses" produced sprawling summaries that led to
nothing implementable. The failure mode is findings without oracles, sizing,
and prioritization. This run inverts it:

**Outcome**: a ranked backlog of ≤10 **flow-ready work items**. Every item
carries: claim · repo evidence (file:line / command output) · a falsifiable
oracle (command + expected signal + why it fails if the claim is false) · a
proposed slice · effort (S/M/L) · value tier · its own LITE/FULL routing
verdict. Plus mechanical batch scripts where the fix is a batch (plans
archive, worktree cleanup). Karel processes the backlog by picking items; each
picked item enters `/flow` with its brief already written.

**Anti-sprawl mechanisms** (the design's core):
1. Finders capped at 6 findings each — forced selectivity at the source.
2. Every finding passes an adversarial verifier that INSPECTS the oracle
   (rewrites it if tautological/weak), runs it, and kills non-reproducible /
   non-actionable items. Default to kill when uncertain.
3. Dedup against active work: killed only when an active July plan tracks the
   SAME claim with an adequate acceptance oracle — subsystem overlap alone is
   not dedup (Codex #3).
4. L2 output is a machine-readable disposition manifest, one reason code per
   file; prose only for ≤5 promotion candidates (Codex #2).
5. Final output ≤10 ranked items + one-line appendix; kill-rate reported.

**Non-goals**: no code changes · no live-platform mutation · no full-spec
re-audit (bounded to the CLAUDE.md invariant index) · no re-reporting of
tracked roadmap.

## Recon evidence (inline, VERIFIED)

| observation | evidence |
|---|---|
| `go test ./... -short` GREEN on the dirty tree | baseline run, exit 0 |
| `make lint-local` RED: 33 issues (26 contextcheck, 3 errcheck, 2 goconst, 1 staticcheck SA4004, 1 tparallel), concentrated in internal/dataconsole | baseline run, make Error 1 |
| plans/ holds ~150 active files, archive/ ~30; April–June residue dominates | `ls plans/` |
| ~17k Go files under `.claude/worktrees` — stale agent worktrees with full repo copies; 13+ orphan `worktree-agent-*` branches | `find`, `git worktree list` |
| Dirty tree interleaves two workstreams: flow-adoption assets (.claude, CLAUDE.md, Makefile, hooks) + dataconsole (dist/app.js +284, 9 untracked domtests) | `git status`, plan headers |
| TODO/FIXME total = 8 → TODO lens worthless, dropped | grep count |
| webui dist is no-build (jsdom harness runs `dist/*.js` directly) | webui/package.json |
| Real code is bounded: internal/ 1123 Go files, cmd/ 48, e2e/ 34 | `find` counts |

## Lens register (6 finders, Sonnet, parallel; Codex #1 applied)

| ID | Lens | What it inspects | Deliverable shape |
|---|---|---|---|
| L1 | inflight | Dirty-tree attribution + commit slicing; parked branches vs main; stale worktrees/branches (unlanded commits? dirty?) | commit-slicing proposal · provably-safe cleanup batch · land/kill/keep per parked branch |
| L2 | plans-triage | All active plans → disposition manifest {archive, keep-active, superseded, promote} + one reason code each; "done" claims spot-checked against git log | manifest + git mv batch · promote shortlist ≤5 with target spec § |
| L3 | traps-guard | CLAUDE.md Traps: guard tests exist; NEW violating call sites; guard sweep-scope gaps | violations + guard gaps only |
| L4 | invariants | CLAUDE.md subsystem-invariant index: spec § exists+states it, test exists, code spot-check; budget check (~30 lines) | drift findings only |
| L5 | mech-debt | lint-local RED triage (33 issues: which commits introduced them, why the gate let them land), go vet, deadcode, govulncheck | baseline verdict + ≤6 real violations |
| L6 | tool-safety | Mutating MCP tools: annotations vs handler behavior, ack/override gates, secret exposure across response/state/audit, recovery-path errors, dataconsole AuthorizeWrite route coverage | proven mismatches only, cap 6 |

## Verification protocol (Codex #3 applied)

Per finding: one adversarial Sonnet verifier. It (1) inspects the oracle for
tautology and REWRITES it if weak, (2) runs it and records the exact signal,
(3) states why the oracle fails if the claim is false — cannot state it ⇒ kill,
(4) dedups against the named active plan by claim identity, (5) checks the
proposed slice is executable by a zero-context implementer at the stated
effort. Batch claims are verified by running the batch-check loop.

## Ranking rubric (synthesis, Fable; Codex #4 applied)

Tiers first: T1 security / data-loss / destructive-action / credential risk →
T2 reproducible user/agent-facing correctness → T3 in-flight landing blockers →
T4 compounding maintenance drag → T5 hygiene. Within a tier: likelihood ×
blast radius × agent amplification, then effort and confidence. Each surviving
item gets a LITE/FULL routing verdict with the named trigger.

## Deliverable

`## Backlog` section appended to this file: top ≤10 items (each a flow-entry
brief) + batch scripts + one-line appendix of killed/deferred findings with
kill reasons. Final summary to owner in chat.

---

## Backlog — 2026-07-17 run (deliverable)

Every claim below was reproduced by an independent adversarial verifier that
inspected (and where weak, rewrote) the oracle before running it. B3/B4 were
additionally re-verified by the orchestrator directly. Assets:
`plans/zcp-analysis-2026-07-17.assets/` — `plans-manifest.txt` (152-file
disposition manifest), `archive-plans.sh` (149 git mv, collision-checked),
`cleanup-worktrees.sh` (12 provably-safe removals).

### B1 · T1-safety · S · route: LITE — Pin Go toolchain: 14 reachable stdlib vulns
Local go1.25.6 builds exercise 14 stdlib vulnerabilities govulncheck marks
REACHABLE from production code (capture proxy TLS/x509, dataconsole providers,
schema validation, work-session cleanup); all fixed by go1.25.x patches ≤1.25.12.
**Slice**: add `toolchain go1.25.12` to go.mod; `go build ./...`,
`go test ./... -short`; confirm CI `go-version-file: go.mod` picks it up.
**Oracle**: `go run golang.org/x/vuln/cmd/govulncheck@latest ./... | grep -c '^Vulnerability #'`
→ 14 now, 0 after. Full table in the run output (mech-debt aux).

### B2 · T2+T3 · S · route: LITE (owner decides the invariant) — xss.dom.test.js regression blocks the dataconsole commit
The uncommitted `dist/app.js` (B3 full-cell-view title hint) writes raw cell
text into `td.title`, deterministically failing tracked
`domtest/xss.dom.test.js` (grid-column raw-`<script>`-substring assertion) —
masked by `-short`, so the tree reads green. Commit 4 of the slicing below is
BLOCKED on this. **Decision**: (a) truncate/normalize the title hint like
`truncateForTitle` already does (app.js:962), or (b) declare title-attr an
accepted non-executing sink and add a documented carve-out to
`assertSafelyRendered` (test change → own RED/GREEN). **Oracle**:
`go test ./internal/dataconsole/console/webui/... -run TestDataConsoleSPADOM -v` → FAIL now.

### B3 · T2 · S · route: LITE — deploy_local_git.go:193 violates the CanonicalRepoURL trap
Raw `current != input.RemoteURL` compare false-blocks a legitimate local
git-push deploy on a `.git`-suffix-only difference; the launch-gate path
already fixed+pinned the identical class
(`launch_source_control_gate.go:244`,
`TestValidateLaunchSourceControl_RemoteDotGitDiffersOnly_NoBlock`).
**Slice**: wrap both sides in `topology.CanonicalRepoURL`; RED test
`TestHandleLocalGitPush_RemoteURLDotGitOnlyDiffers_NoMismatch` mirroring the
launch-gate pin. **Oracle**: `sed -n '193p' internal/tools/deploy_local_git.go`
shows the raw compare. Verified twice (verifier + orchestrator).

### B4 · T2 · S · route: LITE — bare `zerops://` in an agent-facing Go string + guard-scope gap
`briefs_design_tokens.go:29` emits a bare backticked
`zerops://themes/design-system` in BuildDesignTokenTable()'s agent-facing
brief; `TestNoBareZeropsURIInAgentContent` scans only git-tracked `.md` files
in 5 dirs, so Go string-literal markdown is structurally invisible to it.
**Slice**: rewrite to `zerops_knowledge uri="..."` form (3 sibling P4 sites
already converted); update the pinned assertion in
`briefs_design_tokens_test.go:46`; optionally extend the guard to scan Go
string literals under internal/authoring/recipe. **Oracle**:
`grep -rn '\`zerops://' --include='*.go' internal/authoring/recipe/ | grep -v _test`.

### B5 · T3 · S · route: owner decision, then mechanical land — feat/vscode-welcome-production is done and invisible
10 ahead / 0 behind main, clean merge-tree, owner-approved per its own plan,
sitting in a locked worktree (keen-strolling-hare) with an idle live session
(pid 28856, ~30h) — tracked by no plan and no CLAUDE.local.md row.
**Slice**: confirm done → close the session, run its battery, land; either way
add/remove the parked-branches row. **Oracle**:
`git rev-list --left-right --count main...feat/vscode-welcome-production` +
`git merge-tree --write-tree main feat/vscode-welcome-production` (no CONFLICT).

### B6 · T3 · S · route: mechanical after B2 — 4-commit slicing of the dirty tree
Attribution verified file-by-file (inflight aux §A): (1) telemetry plan file
alone; (2) `feat(flow): adopt /flow skill package (M1)` — the 9 flow-adoption
files; (3) `test(dataconsole): domtest coverage B3/B4/B6-B9` — 8 new domtests,
independently green; (4) `feat(dataconsole): …` — dist/app.js+style.css,
BLOCKED by B2. No file is shared between workstreams.

### B7 · T4 · S · route: batch after a manifest glance — archive 143 plan files + 8 subdirs
Disposition manifest for all 152 active plans/ files (each DONE-VERIFIED /
DONE-CLAIMED / SUPERSEDED-BY / STALE-90D / ACTIVE / PROMOTE): only 8 stay
active, 143 files + 8 evidence subdirs archive. **Run**:
`bash plans/zcp-analysis-2026-07-17.assets/archive-plans.sh` (collision-checked
against archive/; 2 lines held with DECIDE-FIRST comments → B10, A4).

### B8 · T4 · S · route: owner decision (amends flow-adoption D12) — the agent-visible gate is blind to full lint
The only lint an agent sees mid-session is `--fast-only` (post-edit, async);
the Stop hook runs test+vet but NO lint; the only full-lint gate (pre-push)
has never fired because the branch was never pushed — which is exactly how 33
lint-local issues landed committed. **Slice**: add a bounded, changed-packages
`golangci-lint run` to `.claude/hooks/stop.sh` (blocking, short timeout).
**Oracle**: `grep -c golangci-lint .claude/hooks/stop.sh` → 0 now;
`make lint-fast` → "0 issues." while `make lint-local` → 33.

### B9 · T4 · M · route: LITE — lint-local debt is 100% branch-introduced; fix in 5 mechanical slices
All 33 issues map to feat/managed-data-console commits (none ancestors of
main; per-line blame table in mech-debt aux). Slices: A contextcheck ×26 —
thread `ctx := r.Context()` once per handler in
`dataconsole/console/server/server.go` (M); B errcheck ×3 `require.NoError`
(kv_redis_test); C goconst ×2; D SA4004 restructure `hasChildren` (verified
intentional probe, style-only); E tparallel `t.Cleanup`. **Oracle**:
`make lint-local` → 0 issues after.

### B10 · T4 · M · route: owner decision — multi-runtime-audit-followup: execute or drop
7 fully-specced, test-planned fixes untouched for 82 days (0/37 boxes checked,
5 of 7 spot-verified still unshipped, 1 half-landed by accident). **Decision**:
run as a /flow LITE batch (each fix self-contained per the plan's §0), or
archive with an explicit DROPPED note. The archive batch holds this file until
decided.

### Appendix — smaller verified items (all S)
- **A1** [T4] `annotations_test.go` never exercises the `RegisterDeployLocal`
  registration of `zerops_deploy` (the common laptop path) — its safety
  annotations carry zero test enforcement. Add `TestAnnotations_DeployLocalTool`
  with `sshDeployer=nil`.
- **A2** [T5] Worktree cleanup: `bash plans/zcp-analysis-2026-07-17.assets/cleanup-worktrees.sh`
  (~617MB, 12 provably-safe). NEEDS-HUMAN kept out: agent-af2165… (52 commits
  reachable from NO other branch — inspect before delete),
  frolicking-snacking-sutton (6 untracked guided-mode drafts),
  keen-strolling-hare (B5), zesty-brewing-canyon (multiproject-impl, dirty).
- **A3** [T4] CLAUDE.local.md parked-branches table misses 3 branches with
  unlanded commits (vscode-welcome-production, multiproject-impl,
  anthropic-proxy-capture); `feat/capture-raw-prototype` is fully merged →
  delete.
- **A4** [T5] Extract the "develop-guidance cross-call dedup" idea from
  nonauthoring-context-audit-2026-06-13.md into plans/backlog/ (or mark
  rejected) before archiving; its only mention lives in that file (at line 7 —
  the finder's line-1807 citation was wrong, verifier corrected).
- **A5** [T4] spec-knowledge-architecture.md Status line still names the
  transient unification plan as owner of migration progress — narrowed from a
  KILLED finding: the program is complete per the plan itself; the spec should
  state final state + the gated publish follow-up, then the plan archives.
- **A6** [T5] `.claude/settings.json` SubagentStart hook hardcodes
  "Go 1.24.0 project" vs go.mod `go 1.25.0` — cosmetic staleness.
- **Promote #1** (pairs with B7): the six git-delivery "Fundament" intent
  statements from `plans/spec-git-delivery-target-2026-06-10.md` →
  `docs/spec-workflows.md` new labeled subsection (e.g. §4.4), so
  git-contract phases cite a spec §, not a plan file; then archive the plan.
  (S→M; the promised `docs/spec-git-delivery.md` never materialized — 37 days.)

### Killed by verification (2/16)
- plans-triage F2 (knowledge-arch migration "stalled at phase 1"): the finder
  misread its source — the plan's own tail records the full program complete;
  narrowed remainder became A5.
- invariants L4-1 (DM bullet "lone rule-breaker" for citing no test): false
  premise — 8 of 12 bullets cite no inline test; the real, narrower gap is the
  DM-1..DM-5 spec table lacking "Pinned by" annotations its sibling tables carry.

### Coverage notes (what this run did NOT establish)
- deadcode: ~55 hits from `./cmd/...` roots; 2/2 spot checks were
  test-only-usage false positives — the seed/conformance cluster was NOT
  individually verified; no deletion claims made.
- Traps #1/#4 (deploy gates, subdomain predicate) not deeply re-audited
  (recently touched, scenario-tested); trap #7 has a duplicate mechanism
  (`gitPushErrorDetail`) noted but no proven bug.
- plans/backlog/ (80 files) treated as one ACTIVE unit, not enumerated.
- Sibling worktrees outside .claude/worktrees (zcp-anthropic-proxy 60M,
  zcp-capture-architecture 32M + 1 dirty file, zcp-capture-raw 207M) noted,
  not inventoried.
