# Merge candidates — for Phase 7 review (2026-04-26)

Source: Phase 1 fire-set matrix (`fire-set-matrix.md`), atoms with
fire-set count 1-2 envelopes. Per §7 Phase 1 step 3 these atoms
fire narrowly enough that a Phase 7 axis-overlap merge with a
co-firing atom MAY be viable. Per §7 Phase 7 step 2 RISK CHECK,
each merge requires axis-overlap confirmation BEFORE execution —
disagreeing axes mean the atoms exist BECAUSE different envelopes
need different content, and merging would destroy that filtering.

**Default disposition is KEEP.** A merge happens only when a
specific axis-overlap target is identified and Codex round
confirms preserved filtering.

## Single-fixture (count=1)

| atom | fire-set | axes | possible merge target | risk |
|---|---|---|---|---|
| `export` | export-active/container | `phases:[export-active], environments:[container]` | NONE — only export atom in the corpus; merge would dilute its export-specific content into another atom | KEEP |

## Two-fixture (count=2)

### Bootstrap-active narrow

| atom | fire-set | axes | possible merge target | risk |
|---|---|---|---|---|
| `bootstrap-classic-plan-dynamic` | bootstrap/{env}/classic/discover/svc-dynamic | `phases:[bootstrap-active], routes:[classic], steps:[discover], runtimes:[dynamic]` | `bootstrap-classic-plan-static` (sibling) | **HIGH** — `runtimes:[dynamic]` vs `runtimes:[static]` is precisely the axis distinction that admits each. Merging would require a wildcard runtimes axis, firing on managed/implicit-webserver too. KEEP. |
| `bootstrap-classic-plan-static` | bootstrap/{env}/classic/discover/svc-static | `phases:[bootstrap-active], routes:[classic], steps:[discover], runtimes:[static]` | `bootstrap-classic-plan-dynamic` (sibling) | Same as above. KEEP. |

### Bootstrap recovery / closed-auto

| atom | fire-set | axes | possible merge target | risk |
|---|---|---|---|---|
| `bootstrap-resume` | idle/{env}/incomplete | `phases:[idle], idleScenarios:[incomplete]` | `idle-orphan-cleanup` (idle/orphan) — DIFFERENT scenario | **HIGH** — IdleScenario filter would have to widen, polluting incomplete-recovery guidance into orphan-cleanup envelopes (and vice versa). KEEP. |
| `develop-closed-auto` | develop-closed-auto/{env} | `phases:[develop-closed-auto]` | NONE — only atom for this phase | KEEP. |

### Idle entries

| atom | fire-set | axes | possible merge target | risk |
|---|---|---|---|---|
| `idle-adopt-entry` | idle/{env}/adopt | `phases:[idle], idleScenarios:[adopt]` | other idle-* | **HIGH** — IdleScenario distinction: each entry atom is the orienting frame for its scenario. Merging would force the agent to read all four entries on any idle envelope. KEEP. |
| `idle-bootstrap-entry` | idle/{env}/bootstrapped | `phases:[idle], idleScenarios:[bootstrapped]` | `idle-develop-entry` (also bootstrapped) | **MEDIUM** — both fire on `idle/bootstrapped/{env}`. They're orthogonal in framing (bootstrap vs develop ENTRY), but their fire-sets fully overlap. Phase 7 should investigate whether a single combined "idle/bootstrapped" entry atom makes sense, or whether the framing distinction warrants two atoms. |
| `idle-develop-entry` | idle/{env}/bootstrapped | `phases:[idle], idleScenarios:[bootstrapped]` | `idle-bootstrap-entry` (above) | See above; investigate together. |
| `idle-orphan-cleanup` | idle/{env}/orphan | `phases:[idle], idleScenarios:[orphan]` | other idle-* | KEEP per same logic as idle-adopt-entry. |

### Strategy setup

| atom | fire-set | axes | possible merge target | risk |
|---|---|---|---|---|
| `strategy-push-git-intro` | strategy-setup/{env}/push-git/unset | `phases:[strategy-setup], strategies:[push-git], triggers:[unset]` | `strategy-push-git-trigger-actions`, `-trigger-webhook` (different triggers) | **HIGH** — Trigger-axis distinction is precisely what admits each. KEEP. |
| `strategy-push-git-trigger-actions` | strategy-setup/{env}/push-git/actions | `triggers:[actions]` | siblings (different triggers) | KEEP. |
| `strategy-push-git-trigger-webhook` | strategy-setup/{env}/push-git/webhook | `triggers:[webhook]` | siblings (different triggers) | KEEP. |

## Phase 7 disposition summary

- 11 of 13 marginal atoms have unmergeable axis distinctions —
  default KEEP; Phase 7 confirms.
- 2 atoms warrant Phase 7 investigation: `idle-bootstrap-entry`
  and `idle-develop-entry` fully overlap on `idle/bootstrapped/{env}`.
  The two render orthogonally (bootstrap-section vs develop-section
  framing), so a Phase 7 read of both rendered bodies on the same
  envelope is needed to decide:
  - **Merge**: combined "idle/bootstrapped" entry that orients to
    BOTH bootstrap and develop options?
  - **Keep distinct**: each one's framing is load-bearing in a
    different agent decision branch (e.g. "should I touch
    infrastructure or just code?").

## Cross-reference

- Phase 7 EXIT (§7 step 4): "composition scores documented;
  axis-tightening + merges accompanied by Codex round confirming
  axis-filtering preserved" — re-read this file in Phase 7.
- Phase 7 RISK CHECK (§7 step 2): "confirm the source and target
  atoms have OVERLAPPING axis sets. Merging a `runtimes:[dynamic]`
  atom into a `runtimes:[]` (wildcard) atom changes axis-filtering"
  — applies directly to bootstrap-classic-plan-{dynamic,static}
  and the strategy-trigger atoms.
