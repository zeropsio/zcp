# Defect ledger — Phase 2 Step 4

**Status**: Awaiting human approval (Phase 2 Step 5 PAUSE POINT).
**Plan**: `plans/atom-corpus-verification-2026-05-02.md` §2 Step 4-5.
**Reviewer**: LLM (Claude Code single-conversation pass).

## Coverage

This pass read goldens with the following depth:

- **Full body**: `idle/{empty,bootstrapped-with-managed,adopt-only,incomplete-resume}` (4), `bootstrap/{recipe/provision,recipe/close,classic/discover-standard-dynamic,classic/provision-local,adopt/discover-existing-pair}` (5), `develop/first-deploy-dev-dynamic-container` (1), `develop/closed-{auto-complete,iteration-cap}` (2), `strategy-setup/{container-unconfigured,configured-build-integration}` (2), `export/{scope-prompt,variant-prompt}` (2). Total: 16 of 30.
- **Atom-IDs + first-line check**: `develop/{first-deploy-recipe-implicit-standard,standard-auto-pair,git-push-{configured-webhook,unconfigured},post-adopt-standard-unset,multi-service-scope-narrow,mode-expansion-source,steady-dev-auto-container,failure-tier-3}` (9), `export/{scaffold-required,git-push-setup-required,classify-prompt,validation-failed,publish-ready}` (5). Total: 14 of 30 (atom-IDs only — body content not exhaustively reviewed).

The plan's review pass estimate is 15-20 hours of focused work; a single
LLM conversation cannot match that depth. Findings below skew toward
**state-leak / lie-class defects** (the highest-value category per plan
§2 *Why*), **structural redundancy / order issues** spotted at the
atom-fire-set level, and **one-shot reads** of high-complexity
develop scenarios. Fix-class defects (atom prose tweaks, wording
nuances) are likely under-counted.

## Defect format

| ID | Severity | Class | Scenarios | Atom | Claim / observation | Why it matters | Proposed fix |
|---|---|---|---|---|---|---|---|

(Severity legend: **HIGH** = LIE-CLASS; **MED** = redundancy / stale firing; **LOW** = ordering / cosmetic / evidence flag. Class: `lie` / `stale-firing` / `redundancy` / `order` / `evidence` / `cosmetic`.)

## Findings

### LIE-CLASS (HIGH severity)

| ID | Severity | Class | Scenarios | Atom | Claim / observation | Why wrong | Proposed fix |
|---|---|---|---|---|---|---|---|
| L1 | HIGH | lie | `develop/closed-iteration-cap`, `develop/closed-auto-complete` | `develop-closed-auto.md:10` | Body asserts `closeReason: auto-complete` is set | The atom is gated on `phases:[develop-closed-auto]`. That phase covers BOTH `auto-complete` AND `iteration-cap` close reasons. For `develop/closed-iteration-cap` the actual `workSession.closeReason` is `iteration-cap`, but the atom tells the agent it's `auto-complete`. **Canonical example of the LIE-CLASS this plan is designed to catch** (plan §2 *Why* bullet 1). | Rewrite body to inspect `workSession.closeReason` instead of asserting; OR add new axis `closeReasons:[auto-complete, iteration-cap]` and split into two atoms. Procedural-form principle (plan §11.1 bullet 7) — first option preferred since the model already exposes the field. |
| L2 | HIGH | lie | `bootstrap/adopt/discover-existing-pair` | `bootstrap-mode-prompt.md` | "Confirm mode per service ... before submitting the plan." | Adopt route doesn't submit a plan-with-modes — modes are inherited from existing live services. Plan-submission language doesn't apply. The atom over-fires for adopt. | Add `routes:[classic, recipe]` to the atom's frontmatter; OR rewrite body to be route-aware (mention adopt's no-plan path). |

### Redundancy (MED severity)

| ID | Severity | Class | Scenarios | Atom(s) | Observation | Proposed fix |
|---|---|---|---|---|---|---|
| R1 | MED | redundancy | `idle/empty` | `bootstrap-route-options` + `idle-bootstrap-entry` | Both atoms render and both tell the agent to "Start a bootstrap workflow ... action=start workflow=bootstrap intent=...". `bootstrap-route-options` even gives the same example command. Two atoms repeating the imperative. | Drop the redundant `idle-bootstrap-entry` content overlap; OR shorten `bootstrap-route-options` to the routing TABLE without the action call (which `idle-bootstrap-entry` owns). |
| R2 | MED | stale-firing | `idle/bootstrapped-with-managed` | `bootstrap-route-options` | Atom fires for an idle envelope that's ALREADY bootstrapped. The route-options table (recipe/adopt/classic/resume) is irrelevant when services already exist; `idle-develop-entry` (start a develop session) is the correct next action. | Tighten `idle-bootstrap-entry`'s axes to `idleScenarios:[empty, adopt, incomplete]` (drop `bootstrapped`); same for `bootstrap-route-options`. |
| R3 | MED | stale-firing | `bootstrap/recipe/provision`, `bootstrap/recipe/close`, `bootstrap/classic/provision-local`, `bootstrap/adopt/discover-existing-pair` | `bootstrap-intro` | Atom fires across all bootstrap-active steps. Its content (3-route overview) is most useful at `discover` step; reading the same overview at `provision` and `close` adds noise after route is committed. | Add `steps:[discover]` axis to `bootstrap-intro`; route-overview becomes step-specific. |
| R4 | MED | redundancy | `develop/first-deploy-dev-dynamic-container` (and ~9 other develop scenarios) | `develop-platform-rules-common` + `develop-platform-rules-container` | Both atoms render with `### Platform rules` as their section header — composed text shows two consecutive "Platform rules" sections. Common atom carries universal rules, container atom adds env-specific. Reader sees a header, then more rules, then ANOTHER same-named header. | Rename one atom's section header (e.g. container atom → `### Container-only platform additions`); OR merge into one atom rendered conditionally per env. |
| R5 | MED | redundancy | `develop/first-deploy-dev-dynamic-container` (and develop scenarios firing both) | `develop-deploy-modes` + `develop-deploy-files-self-deploy` | Both atoms discuss `deployFiles` semantics — first explains class table (self-deploy vs cross-deploy), second drills into the self-deploy invariant + destruction risk. Similar topic split across two atoms produces interleaved content; reader sees deployFiles table at #4 then deployFiles deep-dive at #11. | Cross-reference instead of dual-coverage — `develop-deploy-modes` carries the high-level table + a pointer to `develop-deploy-files-self-deploy`; the second atom focuses purely on the destruction-risk path with no table re-statement. |
| R6 | MED | redundancy | `export/scope-prompt`, `export/variant-prompt`, all 7 export scenarios | `export-intro` + status-specific atoms | `export-intro` carries subsections "Pick the runtime", "Pick the variant", "What the next calls do" — overlapping content with the status-specific atoms (`export-scope-prompt`, `export-variant-prompt`, etc.) that also explain those steps. Reader gets the runtime/variant explanation twice per render. | Trim `export-intro` to universal framing only (the three-call narrowing intro + bundle structure); move "Pick the runtime" / "Pick the variant" subsections entirely into the status-specific atoms. |
| R7 | MED | redundancy | `bootstrap/recipe/close` | `bootstrap-recipe-close` + `bootstrap-close` | Both atoms describe what happens AFTER close. `bootstrap-recipe-close` says "After close, every service the recipe provisioned appears with `bootstrapped: true`..."; `bootstrap-close` says "After `action=complete step=close`, planned runtimes show `bootstrapped: true`...". Same content from two angles. | Either drop the post-close summary from `bootstrap-close` (since the route-specific atoms each cover their own post-state) OR drop it from `bootstrap-recipe-close` (since `bootstrap-close` is universal). |
| R8 | MED | redundancy | `strategy-setup/configured-build-integration` | `setup-build-integration-actions` + `setup-build-integration-webhook` | Both render side-by-side with no introductory framing as alternatives. Reader may think both must be done. | Add a small framing atom (or extend `setup-build-integration-*` body) that explicitly opens with "Pick ONE: actions OR webhook"; reader reads the chosen one. Or add an intro paragraph at the top of each atom: "This is the *actions* alternative; for webhook see the next section." |

### Order / priority (LOW severity)

| ID | Severity | Class | Scenarios | Atom(s) | Observation | Proposed fix |
|---|---|---|---|---|---|---|
| O1 | LOW | order | All bootstrap + develop scenarios | `develop-api-error-meta` | Atom fires #1-3 in render order across nearly every multi-atom scenario. For `discover` steps and `scope-prompt` envelopes where the agent hasn't submitted anything yet, front-loading API-error guidance puts diagnostic content before action content. | Lower priority (currently looks like 1-2; raise to 6-7) so error-handling renders LAST as reference material. Or gate to `phases` that actually submit (drop bootstrap-discover step). |
| O2 | LOW | order | `develop/first-deploy-dev-dynamic-container`, `develop/failure-tier-3`, `develop/first-deploy-recipe-implicit-standard` | `develop-auto-close-semantics` | Renders BEFORE `develop-first-deploy-execute`. Reader sees "session closes when deploy + verify pass" before being told to actually run the deploy. Closure semantics belong AFTER the actions that produce them. | Lower `develop-auto-close-semantics` priority so it renders after `develop-first-deploy-execute` + `develop-first-deploy-verify`. |

### Evidence-required (LOW severity, per plan §11.x rule)

| ID | Severity | Class | Scenarios | Atom | Claim | Issue | Proposed fix |
|---|---|---|---|---|---|---|---|
| E1 | LOW | evidence | `develop/first-deploy-dev-dynamic-container` | `develop-first-deploy-execute` | "expect 30–90 seconds for dynamic runtimes and longer for `php-nginx` / `php-apache`" | Empirical timing claim with no backing. Per the proposed §11.x evidence rule, non-obvious platform-mechanics claims need either a `references-fields` tie-in, a live-eval comment pointer, or a Zerops docs reference. This claim has none. | Either drop the timing range OR add a references-fields citation OR back it with a live-eval comment. |

### Cosmetic (LOW severity)

| ID | Severity | Class | Scenarios | Atom | Observation | Proposed fix |
|---|---|---|---|---|---|---|
| C1 | LOW | cosmetic | All multi-phase scenarios | `develop-api-error-meta` | Atom name suggests develop-phase only, but it fires across bootstrap + develop. | Rename to `general-api-error-meta` OR `platform-api-error-meta`; OR document the cross-phase fire-set in the atom header. |

## Cycle disposition

The plan's Step 6-9 cycle (triage → batch fix → regenerate → re-review)
applies AFTER human approval. **Cycle 1** would address the HIGH-
severity LIE-CLASS atoms (L1, L2) and the most impactful redundancies
(R3, R5). MED/LOW items can either be batched into Cycle 1 or queued
for Cycle 2.

Cycle cap is 3 batch iterations per plan §2 Step 10. If defects remain
after the 3rd cycle, the convention is to STOP and return to user with
structural-issue analysis.

## Recommendation to human reviewer

1. **Approve L1 + L2 fixes as Cycle 1 priority.** L1 is the canonical
   lie-class example the plan was designed to catch; landing the fix
   validates the verification approach. L2 is a route-targeting bug
   that will recur whenever someone bootstraps via adopt.

2. **Approve a subset of MED items for Cycle 1** based on impact —
   recommend R3 (bootstrap-intro stale-firing — 4 scenarios affected),
   R4 (Platform rules header collision — ~10 develop scenarios), R5
   (deploy modes overlap — high-density develop scenarios).

3. **Defer LOW items to Cycle 2 OR accept as documented.** O1 / O2
   (priority adjustments) require careful reordering across the corpus
   — best done after L/R fixes settle. E1 (evidence rule) is a small
   tweak. C1 (cosmetic rename) is purely editorial.

4. **Acknowledge incomplete review depth.** This pass spent ~30
   minutes per scenario at most; the plan's 15-20-hour estimate
   reflects review depth that single-conversation work cannot reach.
   Phase 5's quarterly live-eval (post-merge) provides additional
   real-world coverage.

5. **Edit / extend this ledger** before approving — additions /
   amendments / removals are how the human reviewer steers what gets
   fixed.

After approval, I'll trigger Cycle 1: batch-fix the approved atoms,
regenerate goldens, re-review the affected scenarios, and either
finalize (strip UNREVIEWED markers, enable golden comparison default-on)
or surface remaining defects for Cycle 2.
