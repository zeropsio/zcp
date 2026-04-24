# Atom Authoring Contract — Execution Plan

> **Status**: COMPLETE (archived 2026-04-24). All phases shipped:
>   - Demolition (Phase -2), render extension (Phase -1), 27 atom rewrites + 1 delete (Phase 0), parser extension (Phase 1), reference-field + atom integrity tests (Phase 2), unified authoring lint (Phase 3), 2 consolidation atoms + cross-references (Phase 4).
>   - Final corpus: 76 atoms. `go test ./... -count=1` green, `make lint-local` green. Every DoD gate in §10 verified.
>   - Authoritative enforcement now lives in three tests (`TestAtomAuthoringLint`, `TestAtomReferenceFieldIntegrity`, `TestAtomReferencesAtomsIntegrity`) plus `docs/spec-knowledge-distribution.md §11 Authoring Contract`. Per-topic `*_atom_contract_test.go` files are forbidden; extend `internal/content/atoms_lint.go` instead.
> **Companion**: `plans/archive/atom-authoring-contract-rewrites.md` (BEFORE→AFTER prose per atom; preserved for audit).
> **Supersedes**: `plans/friction-root-causes.md §P2.5` and archived `plans/dev-server-canonical-primitive.md` + `plans/deploy-modes-conceptual-fix.md` contract-test portions.
> **Owner**: LLM-only execution. Each session started with §0. No further action required.

---

## 0. Restart Protocol — READ ME FIRST

**60 seconds of attention. Every session. No exceptions.**

1. `git log --grep='atom-contract:' --oneline --max-count=30` — what's been done.
2. Read §6 "Phase Status" below — find the first row that is NOT checked `[x]`.
3. Read that phase's Definition-of-Done in §5.
4. Run `go test ./... -count=1 -short` — baseline green?
   - If NOT green: investigate the red tests FIRST. Do not proceed until baseline is green.
5. If the plan says "confirm with user" (major phase boundary), stop and ask.
6. Otherwise: execute the next commit per plan.
7. After each commit, tick the `[x]` in §6 and, when a phase closes, tick the phase summary row.
8. Never re-litigate §2 "Decisions locked". They are final.
9. Never create new `*_atom_contract_test.go` files. If drift needs a rule, it goes into `TestAtomAuthoringLint` (Phase 3).

**If you are reading this for the first time post-compaction**:
- Your primary artifact is this file + `plans/atom-authoring-contract-rewrites.md`. Everything else is context.
- The corpus lives in `internal/content/atoms/*.md` (75 → 74 → 76 during execution; see §3).
- The correct Go type name is `tools.envChangeResult` (in `internal/tools/env.go:63`) — `envResult` does not exist.
- Per-topic atom contract tests (`subdomain_atom_contract_test.go`, `dev_server_atom_contract_test.go`, `deploy_modes_atom_contract_test.go`) get **deleted** in Phase -2 commit 1. Do not recreate.

---

## 1. Context

Atoms (markdown in `internal/content/atoms/*.md`) are synthesized into the "Guidance" section of `zerops_workflow action=status` and equivalent workflow-lifecycle tool responses. When atom prose makes claims about internal handler behavior or invisible state, the claims drift silently as code evolves.

Prior approach: per-topic phrase-matching contract tests (`TestSubdomainAtomContract`, `TestDevServerAtomContract`, `TestDeployModesAtomContract`). They pin exact phrases in CI, which:
- Couples atom wording to test strings (load-bearing for CI).
- Encourages spec-ID citations in atom prose (DM-2, DM-5, etc. — agent doesn't need them).
- Multiplies with every new drift topic.
- Provides no mechanical check that cited response fields actually exist in Go types.

**Root-cause fix**: eliminate drift surface by disciplining atom content. Atoms describe observable response/envelope state, orchestration, concepts, and pitfalls. The mechanical enforcement is:
- **Test 1** — `TestAtomReferenceFieldIntegrity`: every `references-fields` frontmatter entry resolves to a real Go struct field via AST scan.
- **Test 2** — `TestAtomAuthoringLint`: atom bodies don't contain handler-behavior verbs, spec IDs, or plan-doc cross-references.
- **Test 3** — `TestAtomReferencesAtomsIntegrity`: every `references-atoms` frontmatter entry resolves to an existing atom (prevents rename drift).

Per-topic tests go away. One unified contract covers the corpus.

---

## 2. Decisions locked

These are final. Do not re-open.

| ID | Decision | Rationale |
|---|---|---|
| **R1** | Delete all 3 per-topic atom-contract tests in **Phase -2 commit 1** (one commit). | Pre-production + single-dev → brief window of no-test regression protection is acceptable. Phase 3 `TestAtomAuthoringLint` restores protection with broader coverage. |
| **R2** | **Option A** — extend `renderBootstrappedFields` in `internal/workflow/render.go:187-198` to emit explicit `bootstrapped=true, deployed=true\|false` tokens alongside mode/strategy. ~3 LOC + snapshot regeneration. | 5 rewritten atoms reference these tokens; Option B (verbose envelope-JSON references) was rejected as cognitively heavier and less consistent with the rendered Services-block convention. |
| **R3** | **DELETE** `bootstrap-write-metas.md` entirely. Fold one sentence ("ServiceMeta records are on-disk evidence authored by bootstrap and adoption; projection is ServiceSnapshot with bootstrapped/mode/stage") into `bootstrap-close`. | Atom is 100% invisible-state narration; adds no agent-actionable content. |
| **R4** | **Consolidate** — create 2 new shared atoms: `develop-auto-close-semantics` and `develop-dev-server-reason-codes`. 3 and 4 existing atoms cross-reference them via `references-atoms` instead of duplicating content. | Eliminates 3- and 4-way duplication; renames tracked by `TestAtomReferencesAtomsIntegrity`. |
| **R5** | Add frontmatter field **`references-atoms: [atom-id, …]`**. Validated by Test 3. | Prevents atom rename from silently breaking cross-references baked into prose. |
| **R6** | Authoring lint SPEC-ID regex: `\bDM-[0-9]\|\bDS-0[1-4]\|\bGLC-[1-6]\|\bKD-[0-9]{2}\|\bTA-[0-9]{2}\|\bE[1-8]\b\|\bO[1-4]\b\|\bF#[1-9]\|\bINV-[0-9]+`. Plus handler-behavior patterns. Allowlist mechanism for true-positives. | Prevents re-introduction of spec-ID citations and handler-behavior prose. |
| **R7** | **Inline** `docs/spec-workflows.md §8 O4` — replace the `See plans/dev-server-canonical-primitive.md` pointer with a self-contained DS-01/DS-02/DS-03/DS-04 summary paragraph. | Spec should be self-contained; pointing to an archived plan is an unhealthy pattern. |

---

## 3. Scope

| Artifact | Count | Disposition |
|---|---|---|
| Atom corpus today | 75 | |
| Per-topic atom-contract tests to DELETE | 3 files (345 LOC) | subdomain_atom_contract_test.go, dev_server_atom_contract_test.go, deploy_modes_atom_contract_test.go |
| Atoms with spec-ID citations (Phase -2 strip) | 4 | develop-deploy-modes, develop-deploy-files-self-deploy, develop-first-deploy-scaffold-yaml, develop-push-dev-deploy-container |
| Knowledge-base files with spec-ID citations | 2 | internal/knowledge/bases/static.md, internal/knowledge/recipes/dotnet-hello-world.md |
| HIGH-severity atoms to REWRITE | 27 | See §5 Phase 0 batches (was 28; minus bootstrap-write-metas which is DELETE) |
| Atoms to DELETE | 1 | bootstrap-write-metas |
| New consolidation atoms to CREATE | 2 | develop-auto-close-semantics, develop-dev-server-reason-codes |
| CLAUDE.md bullets to trim | 1 | §Conventions — TestDeployModesAtomContract mention |
| spec-workflows.md sections to inline-rewrite | 1 | §8 O4 (drop plan pointer, inline content) |
| Plans to ARCHIVE | 3 | friction-root-causes.md (banner on §P2.5), dev-server-canonical-primitive.md (git mv), deploy-modes-conceptual-fix.md (git mv) |
| Vestigial code removals | 1 | deploy_validate.go:115 legacy "dev service should use deployFiles: [.]" advisory (superseded by DM-2 hard-error at line 92) |
| Render extension | ~3 LOC + snapshot update | render.go renderBootstrappedFields |
| New tests | 3 | TestAtomReferenceFieldIntegrity, TestAtomAuthoringLint, TestAtomReferencesAtomsIntegrity |

**Corpus size progression**: 75 → 74 (after bootstrap-write-metas delete) → 76 (after 2 consolidation atoms).

---

## 4. Architectural principles

### Atoms MUST describe

1. **Observable response fields** — backticked identifiers that correspond to Go struct fields in `internal/ops`, `internal/tools`, or `internal/platform` types, declared in `references-fields` frontmatter.
2. **Observable envelope fields** — `StateEnvelope`, `ServiceSnapshot`, `WorkSessionSummary`, `Plan`, `BootstrapRouteOption` fields the agent sees via `zerops_workflow action=status`.
3. **Orchestration sequences** — multi-step tool-call flows with ordered commands.
4. **Platform concepts** — mode taxonomy, runtime classes, pair structure, workflow phases.
5. **Preventive rules** — anti-patterns the agent should not attempt (e.g., `git init` on SSHFS mount).
6. **Cross-references to other atoms** — via `references-atoms` frontmatter (for rename tracking).

### Atoms MUST NOT describe

1. **Handler internals** — any verb whose subject is `the X handler`, `the tool auto-…`, `ZCP writes/stamps/activates/enables`. Forbidden by `TestAtomAuthoringLint`.
2. **Invisible state** — ServiceMeta internal fields (`BootstrappedAt`, `FirstDeployedAt`, `BootstrapSession`, `StrategyConfirmed`). Forbidden by authoring lint. Agent sees only derived booleans (`bootstrapped`, `deployed`, `resumable`) via envelope.
3. **Spec invariant IDs** — `DM-*`, `E*`, `O*`, `KD-*`, `DS-*`, `GLC-*`, `F#*`, `TA-*`, `INV-*`. Forbidden by authoring lint regex. These are developer taxonomy; agent has no use for them at runtime.
4. **Plan document paths** — `plans/…`, `plans/archive/…`. Forbidden by authoring lint.
5. **Imperative calls mirroring handler auto-behavior** — if a handler already does X, atom does not tell agent to call X separately.

### Observable/invisible boundary

**Observable by agent**:
- Every field in MCP tool response JSON.
- Every field in `StateEnvelope` and its sub-structs as serialized via envelope JSON.
- Every line in the rendered Services block of `RenderStatus` output (after Phase -1 render extension emits `bootstrapped=`, `deployed=` tokens).

**Invisible to agent**:
- Raw ServiceMeta on disk (`.zcp/state/services/<hostname>.json`).
- WorkSession internals beyond what `RenderStatus` emits (Progress / Blockers lines).
- Bootstrap session ID, first-deploy timestamps.

---

## 5. Execution phases

Phase order is STRICT. Each phase's Definition-of-Done must hold before the next phase starts.

### Phase -2 — Demolition (1 sezení, 7-8 commits)

**Entry**: clean tree, baseline green.

**Commits**:

1. `atom-contract: delete 3 per-topic atom-contract tests`
   - `git rm internal/workflow/subdomain_atom_contract_test.go`
   - `git rm internal/workflow/dev_server_atom_contract_test.go`
   - `git rm internal/workflow/deploy_modes_atom_contract_test.go`
   - Verify: `grep -rn 'TestSubdomainAtomContract\|TestDevServerAtomContract\|TestDeployModesAtomContract' internal/` returns only archived-plan hits (no live Go imports).
   - Run: `go test ./... -count=1 -short` — green.

2. `atom-contract: strip spec-ID citations from 4 atoms`
   - Edit `develop-deploy-modes.md`, `develop-deploy-files-self-deploy.md`, `develop-first-deploy-scaffold-yaml.md`, `develop-push-dev-deploy-container.md`
   - Per-edit details in companion §Phase -2 (P-2.1 through P-2.4).

3. `atom-contract: strip DM-* citations from knowledge bases`
   - Edit `internal/knowledge/bases/static.md:3`
   - Edit `internal/knowledge/recipes/dotnet-hello-world.md:204`
   - Per-edit details in companion §Phase -2 (P-2.5, P-2.6).

4. `atom-contract: CLAUDE.md bullet trim`
   - Edit `CLAUDE.md:168` — remove `and TestDeployModesAtomContract in internal/workflow/` from the Conventions bullet.

5. `atom-contract: inline spec-workflows §8 O4 (R7)`
   - Edit `docs/spec-workflows.md` §8 O4 — replace `See plans/dev-server-canonical-primitive.md` with an inline 2-3 sentence DS-01/DS-02/DS-03/DS-04 summary covering: honest post-deploy message (no runtime-class heuristics), `zerops_dev_server` tool ownership, atom corpus as runtime-class guidance home.

6. `atom-contract: archive superseded plans`
   - Prepend `plans/friction-root-causes.md` §P2.5 with `> **SUPERSEDED BY** `plans/atom-authoring-contract.md`.` banner and obsolete the §A2 agent block.
   - `git mv plans/dev-server-canonical-primitive.md plans/archive/`
   - `git mv plans/deploy-modes-conceptual-fix.md plans/archive/`
   - Prepend each archived file with a SUPERSEDED banner.

7. `atom-contract: delete vestigial deploy_validate.go:115 legacy warning`
   - Remove the `"dev service should use deployFiles: [.] — ensures source files persist across deploys for continued iteration"` advisory (DM-2 hard-error at line 92-97 supersedes it).
   - Update tests that expected the advisory string (grep `dev service should use deployFiles` in test files).

**Definition-of-Done**:
- `go test ./... -count=1` green.
- `make lint-local` green.
- `grep -rn -E '\bDM-[1-5]\b|\bDS-0[1-4]\b|\bGLC-[1-6]\b' internal/content/atoms/` returns zero.
- `find internal -name '*atom_contract*test*.go'` returns zero.
- `ls plans/ | grep -E 'dev-server-canonical-primitive|deploy-modes-conceptual-fix'` returns zero (moved to archive/).

---

### Phase -1 — Render extension (1 sezení, 2 commits)

**Entry**: Phase -2 DoD passes.

**Commits**:

1. `atom-contract: render.go emits bootstrapped/deployed tokens (R2)`
   - Edit `internal/workflow/render.go::renderBootstrappedFields` — add `"bootstrapped=true"` as the first field in the returned slice, and append `"deployed=true"` or `"deployed=false"` based on `svc.Deployed`.
   - Example edit:
     ```go
     func renderBootstrappedFields(svc ServiceSnapshot) string {
         fields := []string{"bootstrapped=true", "mode=" + string(svc.Mode)}
         if svc.Strategy == "" || svc.Strategy == StrategyUnset {
             fields = append(fields, "strategy=unset")
         } else {
             fields = append(fields, "strategy="+string(svc.Strategy))
         }
         if svc.StageHostname != "" {
             fields = append(fields, "stage="+svc.StageHostname)
         }
         if svc.Deployed {
             fields = append(fields, "deployed=true")
         } else {
             fields = append(fields, "deployed=false")
         }
         return strings.Join(fields, ", ")
     }
     ```

2. `atom-contract: update render snapshot tests for new tokens`
   - Regenerate snapshot tests (likely `internal/workflow/render_test.go`).
   - Verify no other tests broke.

**Definition-of-Done**:
- `go test ./internal/workflow/... -count=1` green.
- Sample rendered Services block contains `bootstrapped=true, mode=dev, strategy=push-dev, deployed=false` pattern.

---

### Phase 0 — Rewrite 27 HIGH atoms + DELETE 1 (4 sezení, ~28 commits)

**Entry**: Phase -1 DoD passes.

**Execution**: one commit per atom, following families A/B/C/D from the companion file. Per-atom BEFORE→AFTER prose is frozen in `plans/atom-authoring-contract-rewrites.md`; DO NOT re-derive.

**Family A — bootstrap (7 commits, 1 sezení)**:
Apply companion §B-1 through §B-7 (B-7 is DELETE of bootstrap-write-metas).

**Family B — first-deploy (6 commits, 1 sezení)**:
Apply companion §FD-1 through §FD-6.

**Family C — develop close/push/deploy-modes (8 commits, 1 sezení)**:
Apply companion §C-1 through §C-8. Note C-1, C-2 are NEEDS-REVISION — companion already has edge-case-corrected prose.

**Family D — dynamic/checklist/idle/export/push-git (9 commits, 1 sezení)**:
Apply companion §D-1 through §D-9. Note D-2 is NEEDS-REVISION — companion already has no-HTTP-worker branch.

**Commit format per atom**:
```
atom-contract(phase-0): rewrite <atom-id> to observer form

- Strip <drift category>: "<quote>"
- Replace with observable <field-list>
- No frontmatter changes yet (Phase 1 adds parser; Phase 2 backfills references-fields)
```

**Definition-of-Done**:
- All 27 HIGH atoms rewritten per companion.
- `bootstrap-write-metas.md` deleted.
- `go test ./... -count=1` green (note: `references-fields` frontmatter not yet enforced — it's read-through only after Phase 1).
- Corpus size: 74 (75 - 1 delete).
- Manual review: each rewritten atom matches the companion's proposed prose within reasonable tolerance (atom author may polish wording but must not reintroduce drift).

---

### Phase 1 — Frontmatter parser extension (1 sezení, 3 commits)

**Entry**: Phase 0 DoD passes.

**Commits**:

1. `atom-contract(phase-1): parser reads references-fields + references-atoms + pinned-by-scenario`
   - Edit `internal/workflow/atom.go::ParseAtom` — add 3 new `[]string` fields to `KnowledgeAtom`: `ReferencesFields`, `ReferencesAtoms`, `PinnedByScenarios`.
   - Edit `parseFrontmatter` to collect them via existing key-value mechanism (they're not axis filters, just plain lists).
   - Unit tests: `TestParseAtom_ReferencesFields`, `TestParseAtom_ReferencesAtoms`, `TestParseAtom_PinnedByScenarios`.

2. `atom-contract(phase-1): parser validates frontmatter shapes`
   - `references-fields` entries must match `^[a-z_]+\.[A-Z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*$` (pkg.Type.Field) — parser emits error on malformed.
   - `references-atoms` entries must be non-empty atom-id strings.

3. `atom-contract(phase-1): document new frontmatter keys in spec-knowledge-distribution §4.2`
   - Add `references-fields`, `references-atoms`, `pinned-by-scenario` to the frontmatter field table.

**Definition-of-Done**:
- Parser accepts new keys; malformed values error with a clear message.
- No atom edited yet — parser change is additive.
- `go test ./internal/workflow/... -count=1` green.

---

### Phase 2 — Test 1 (ref-field integrity) + Test 3 (ref-atoms integrity) (1 sezení, 4 commits)

**Entry**: Phase 1 DoD passes.

**Commits**:

1. `atom-contract(phase-2): astFieldExists helper`
   - Implement AST-scan helper that resolves `<pkg>.<Type>.<Field>` against `internal/ops/*.go`, `internal/tools/*.go`, `internal/platform/*.go`, `internal/workflow/*.go`.
   - Reuse pattern from `pair_keyed_contract_test.go`.

2. `atom-contract(phase-2): TestAtomReferenceFieldIntegrity`
   - Iterate corpus; for each atom's `references-fields`, resolve via AST.
   - Fail with `atom %q references missing field %s`.

3. `atom-contract(phase-2): TestAtomReferencesAtomsIntegrity`
   - Iterate corpus; for each atom's `references-atoms`, verify target atom exists.
   - Fail with `atom %q references missing atom %s`.

4. `atom-contract(phase-2): backfill references-fields on all rewritten atoms`
   - Apply frontmatter additions per companion for all 27 rewritten atoms + 2 consolidation atoms (if already created in Phase 4; otherwise in Phase 4).

**Definition-of-Done**:
- Both tests green.
- Every rewritten HIGH atom has `references-fields` where companion specifies.
- `go test ./... -count=1` green.

---

### Phase 3 — Test 2 (authoring lint) (1 sezení, 3 commits)

**Entry**: Phase 2 DoD passes.

**Commits**:

1. `atom-contract(phase-3): atoms_lint.go with forbidden patterns`
   - Create `internal/content/atoms_lint.go` with:
     - Regex list: `\bDM-[0-9]|\bDS-0[1-4]|\bGLC-[1-6]|\bKD-[0-9]{2}|\bTA-[0-9]{2}|\bE[1-8]\b|\bO[1-4]\b|\bF#[1-9]|\bINV-[0-9]+`
     - Regex list: handler-behavior verbs — `\bhandler\b.*\b(automatically|auto-\w+|does|writes|stamps|activates|enables|disables)\b`, `\btool\b.*\bauto(-\|matically)\b`, `\bZCP\s+(writes|stamps|activates|enables|disables)\b`
     - Regex list: invisible-state — `\bFirstDeployedAt\b|\bBootstrapSession\b|\bStrategyConfirmed\b`
     - Regex: plan-doc refs — `\bplans/[a-z][a-z0-9-]+\.md\b`
   - `LintCorpus(atoms) []Violation` function.
   - Allowlist mechanism: `atom-id::exact-phrase` keys for edge cases, empty by default.

2. `atom-contract(phase-3): TestAtomAuthoringLint`
   - Iterate corpus, match patterns, fail with specific violation + line number.
   - Expected initial state: clean (Phase 0 rewrites eliminated all violations).

3. `atom-contract(phase-3): document §11 Authoring Contract in spec-knowledge-distribution`
   - Add §11 "Authoring Contract" section: principles (§4 of this plan), forbidden patterns, allowlist policy.

**Definition-of-Done**:
- `TestAtomAuthoringLint` green on clean corpus.
- Proof-regression: manually add `DM-2` to any atom body → test fails → revert → test passes.
- `go test ./... -count=1` green, `make lint-local` green.

---

### Phase 4 — Consolidation (1 sezení, 5 commits)

**Entry**: Phase 3 DoD passes.

**Commits**:

1. `atom-contract(phase-4): create develop-auto-close-semantics atom`
   - Create `internal/content/atoms/develop-auto-close-semantics.md` per companion §CON-1.
   - Corpus: 74 → 75.

2. `atom-contract(phase-4): create develop-dev-server-reason-codes atom`
   - Create `internal/content/atoms/develop-dev-server-reason-codes.md` per companion §CON-2.
   - Corpus: 75 → 76.

3. `atom-contract(phase-4): atoms referencing auto-close-semantics`
   - Add `references-atoms: [develop-auto-close-semantics]` frontmatter to: `develop-first-deploy-verify`, `develop-first-deploy-promote-stage`, `develop-change-drives-deploy`, `develop-closed-auto`, `idle-develop-entry`.
   - Trim the auto-close paragraphs in each (shortened to "see develop-auto-close-semantics").

4. `atom-contract(phase-4): atoms referencing dev-server-reason-codes`
   - Add `references-atoms: [develop-dev-server-reason-codes]` frontmatter to: `develop-close-push-dev-dev`, `develop-dynamic-runtime-start-container`, `develop-push-dev-workflow-dev`, `develop-dev-server-triage`, `develop-platform-rules-container`.
   - Trim `reason`-code enumeration in each (shortened to "see develop-dev-server-reason-codes").

5. `atom-contract(phase-4): verify all tests green + update evidence index`
   - Run full test suite.
   - Update CLAUDE.md (if needed) to mention the unified authoring contract as canonical pattern.

**Definition-of-Done**:
- 2 new atoms exist and pass Test 1 + Test 3.
- 9 existing atoms have `references-atoms` frontmatter.
- No atom duplicates auto-close or reason-code content.
- `go test ./... -count=1` green.
- Final corpus size: 76.

---

### Phase 5 — MEDIUM polish (optional, 1-2 sezení)

**Entry**: Phase 4 DoD passes. **Optional** — not required for plan completion.

- Backfill `references-fields` on the 8 EXPLICIT-SHAPE atoms not yet covered (per F2 §4).
- Polish ~20 MEDIUM-severity BEHAVIOR-CLAIM atoms to observer form where low-risk.
- Each atom individual commit.

**Definition-of-Done**: optional — declare when the corpus reads uniformly clean.

---

## 6. Phase Status (tick as you go)

**Phase -2 — Demolition**
- [x] commit 1: delete 3 atom-contract tests
- [x] commit 2: strip spec-ID from 4 atoms
- [x] commit 3: strip DM-* from 2 knowledge-base files (static.md tracked; dotnet recipe gitignored — local edit only, upstream push deferred)
- [x] commit 4: CLAUDE.md trim
- [x] commit 5: spec-workflows §8 O4 inline
- [x] commit 6: archive 3 plans
- [x] commit 7: delete deploy_validate.go:115 legacy warning
- [x] **Phase -2 DoD verified** (2026-04-24)

**Phase -1 — Render extension**
- [x] commit 1: renderBootstrappedFields extension (fused with commit 2 — snapshot test lock-in was additive, no regression repair needed)
- [x] commit 2: snapshot test update (see commit 1)
- [x] **Phase -1 DoD verified** (2026-04-24)

**Phase 0 — Atom rewrite**

Family A — bootstrap:
- [x] B-1: bootstrap-adopt-discover
- [x] B-2: bootstrap-close (with bootstrap-write-metas paragraph folded in)
- [x] B-3: bootstrap-mode-prompt
- [x] B-4: bootstrap-recipe-close
- [x] B-5: bootstrap-resume
- [x] B-6: bootstrap-route-options
- [x] B-7: DELETE bootstrap-write-metas

Family B — first-deploy:
- [x] FD-1: develop-first-deploy-execute
- [x] FD-2: develop-first-deploy-verify
- [x] FD-3: develop-first-deploy-intro
- [x] FD-4: develop-first-deploy-promote-stage
- [x] FD-5: develop-first-deploy-write-app
- [x] FD-6: develop-first-deploy-scaffold-yaml (post-Phase -2 polish)

Family C — develop close/push/deploy-modes:
- [x] C-1: develop-change-drives-deploy
- [x] C-2: develop-close-push-dev-dev
- [x] C-3: develop-closed-auto (with corpus_coverage + scenarios_test updates)
- [x] C-4: develop-env-var-channels
- [x] C-5: develop-mode-expansion
- [x] C-6: develop-push-dev-workflow-dev
- [x] C-7: develop-strategy-awareness
- [x] C-8: develop-push-dev-deploy-container (post-Phase -2 polish)

Family D — dynamic/checklist/idle/export/push-git:
- [x] D-1: develop-dynamic-runtime-start-container
- [x] D-2: develop-dev-server-triage
- [x] D-3: develop-checklist-dev-mode
- [x] D-4: develop-platform-rules-container
- [x] D-5: idle-adopt-entry (with scenarios_test + corpus_coverage updates)
- [x] D-6: idle-develop-entry (with corpus_coverage update)
- [x] D-7: export
- [x] D-8: strategy-push-git-push-container
- [x] D-9: strategy-push-git-push-local

- [x] **Phase 0 DoD verified** (2026-04-24, corpus 74, tests + lint green)

**Phase 1 — Parser extension**
- [x] commit 1: parser reads new frontmatter keys
- [x] commit 2: parser validates shapes
- [x] commit 3: spec-knowledge-distribution §4.2 docs
- [x] **Phase 1 DoD verified** (2026-04-24)

**Phase 2 — Tests 1 & 3**
- [x] commit 1: astFieldExists helper (loadAtomReferenceFieldIndex)
- [x] commit 2: TestAtomReferenceFieldIntegrity
- [x] commit 3: TestAtomReferencesAtomsIntegrity
- [x] commit 4: backfill references-fields on all 27 atoms (references-atoms deferred to Phase 4 for CON-1/CON-2 cross-refs)
- [x] **Phase 2 DoD verified** (2026-04-24)

**Phase 3 — Test 2 (authoring lint)**
- [x] commit 1: atoms_lint.go with forbidden patterns
- [x] commit 2: TestAtomAuthoringLint
- [x] commit 3: spec-knowledge-distribution §11 Authoring Contract (+ splitAtomBody lint-fix followup)
- [x] **Phase 3 DoD verified** (2026-04-24, clean corpus, lint green)

**Phase 4 — Consolidation**
- [x] commit 1: create develop-auto-close-semantics
- [x] commit 2: create develop-dev-server-reason-codes
- [x] commit 3: atoms cross-reference auto-close-semantics (5 atoms)
- [x] commit 4: atoms cross-reference dev-server-reason-codes (5 atoms)
- [x] commit 5: final test sweep + CLAUDE.md update + 3 extra cross-refs
- [x] **Phase 4 DoD verified** (2026-04-24, corpus 76, tests + lint green)

**Phase 5 — Optional polish**
- [ ] (TBD if scheduled)

---

## 7. Testing architecture

Three tests collectively enforce the contract. Each has narrow responsibility and clear failure message.

### Test 1 — `TestAtomReferenceFieldIntegrity` (workflow/)
- **Scope**: every `references-fields` frontmatter entry across all atoms.
- **Mechanism**: AST scan of `internal/{ops,tools,platform,workflow}/*.go` — resolve `<pkg>.<Type>.<Field>`.
- **Failure**: `atom %q references missing field %s (expected in %s.%s)`.

### Test 2 — `TestAtomAuthoringLint` (content/)
- **Scope**: every atom body.
- **Mechanism**: regex scan for forbidden patterns:
  - Handler-behavior verbs (handler/tool/ZCP + auto-/activates/writes/…)
  - Spec IDs (`DM-*`, `E*`, `O*`, `KD-*`, `DS-*`, `GLC-*`, `F#*`, `TA-*`, `INV-*`)
  - Invisible-state field names (`FirstDeployedAt`, `BootstrapSession`, `StrategyConfirmed`)
  - Plan-doc paths (`plans/…`)
- **Failure**: `atom %q line %d contains %q — %s`.
- **Allowlist**: `map[string]string` keyed on `atom-id::exact-phrase` for legitimate exceptions with documented rationale.

### Test 3 — `TestAtomReferencesAtomsIntegrity` (workflow/)
- **Scope**: every `references-atoms` frontmatter entry.
- **Mechanism**: verify target atom file exists in corpus.
- **Failure**: `atom %q references missing atom %s`.

### Additional tests that STAY as-is
- `TestParseAtom_*` in `internal/content/atoms_test.go` — frontmatter validation.
- `TestNoInlineManagedRuntimeIndex` in `pair_keyed_contract_test.go` — E8 code invariant, not atom-related.
- `TestSubdomainRobustnessContract` in `ops/subdomain_contract_test.go` — code-level invariant.
- `TestBuildLogsContract` in `ops/build_logs_contract_test.go` — code-level invariant.
- `TestDeployPostMessageHonesty` in `tools/deploy_post_message_honesty_test.go` — DS-01 code invariant.

---

## 8. Rollback strategy

Per-commit rollback via `git revert`. The plan is designed so each commit is independently reversible:

- **Phase -2 commits**: all are prose/file-deletion edits. Revert replaces them; no regression risk.
- **Phase -1**: render extension can be reverted; atoms that rely on `bootstrapped=/deployed=` tokens would be textually wrong but still render, just without the explicit token.
- **Phase 0**: atom rewrites are idempotent-safe reverts.
- **Phase 1**: parser extension is additive; revert leaves old parser.
- **Phase 2**: test additions are reversible; removal of `references-fields` frontmatter would follow.
- **Phase 3**: lint test reversal is clean.
- **Phase 4**: consolidation atom creation is reversible; cross-reference additions are revertible per atom.

**Critical rule**: never leave atoms half-rewritten across a session boundary. If you must abort mid-family, revert the partial commits to the last full atom.

---

## 9. Evidence index

### Code anchors (verified during audit)
| Claim | File:line |
|---|---|
| Atom emission pipeline entry | `internal/tools/workflow.go:750-778`, `workflow_develop.go:150-177` |
| Bootstrap atom synthesis | `internal/workflow/bootstrap_guide_assembly.go:22-84` |
| Strategy atom synthesis | `internal/workflow/strategy_guidance.go:13-48` |
| Immediate workflows | `internal/workflow/synthesize.go:289-305` |
| Atom parser | `internal/workflow/atom.go:39-73` |
| Envelope structure | `internal/workflow/envelope.go:15-92` |
| DeployResult fields | `internal/ops/deploy_common.go:10-40` |
| envChangeResult fields | `internal/tools/env.go:63-70` |
| VerifyResult fields | `internal/ops/verify.go:30-45` |
| DevServerResult fields | `internal/ops/dev_server.go:15-65` |
| ServiceSnapshot fields | `internal/workflow/envelope.go:80-92` |
| renderBootstrappedFields | `internal/workflow/render.go:187-198` |
| BootstrapRouteOption fields | `internal/workflow/route.go:83-93` |
| APIError fields | `internal/platform/types.go:150-160` |
| AST-scan pattern reference | `internal/workflow/pair_keyed_contract_test.go` |

### Audit documents (for reference; archived after Phase 4)
- Third-pass atom classification: in conversation context, preserved here in §6 Phase Status (atom list is complete coverage).
- Per-atom rewrite prose: `plans/atom-authoring-contract-rewrites.md` (companion).

---

## 10. Definition of done (plan-wide)

The plan is complete when ALL of these hold:

- [ ] All checkboxes in §6 "Phase Status" are ticked through Phase 4 DoD.
- [ ] `go test ./... -count=1` green (with `-race` optional sanity run).
- [ ] `make lint-local` green.
- [ ] `grep -rn -E '\bDM-[1-5]\b|\bDS-0[1-4]\b|\bGLC-[1-6]\b|\bKD-[0-9]{2}\b|\bTA-[0-9]{2}\b|\bE[1-8]\b|\bO[1-4]\b|\bF#[1-9]\b|\bINV-[0-9]+\b' internal/content/atoms/` returns zero.
- [ ] `grep -rn '\bhandler\b.*\b(automatically|auto-)' internal/content/atoms/` returns zero.
- [ ] `find internal -name '*atom_contract*test*.go'` returns zero (only `TestAtomAuthoringLint` + `TestAtomReferenceFieldIntegrity` + `TestAtomReferencesAtomsIntegrity` remain).
- [ ] `bootstrap-write-metas.md` does not exist.
- [ ] `develop-auto-close-semantics.md` and `develop-dev-server-reason-codes.md` exist and are referenced by ≥3 atoms each.
- [ ] `spec-workflows.md §8 O4` contains no `plans/` pointer.
- [ ] `plans/friction-root-causes.md §P2.5` carries SUPERSEDED banner.
- [ ] `plans/archive/` contains `dev-server-canonical-primitive.md` and `deploy-modes-conceptual-fix.md`.
- [ ] This plan file moves to `plans/archive/atom-authoring-contract.md` with final commit.

**Explicit non-goals** (do not expand scope):
- Recipe-authoring workflow atoms (zcprecipator2 atom tree under `internal/content/workflows/recipe/`) — separate corpus, separate plan later.
- Migration of existing ops-level code invariants (those tests stay; only per-topic atom-content tests are removed).
- Full MEDIUM-severity polish (Phase 5 is optional).

---

## 11. Session restart — detailed checklist

When resuming work after any break (compaction, new session, pause):

### Step 1 — Orient (2 minutes)
- Open this file; read §0 and §6 top-to-bottom.
- `git log --grep='atom-contract:' --oneline --max-count=30` — last 30 commits.
- `git status` — clean tree?

### Step 2 — Verify baseline (2 minutes)
- `go test ./... -count=1 -short` — green?
- If not: investigate red tests before any further work.

### Step 3 — Identify next action (1 minute)
- §6 "Phase Status": find the first unchecked `[ ]` row.
- If it's a phase boundary (e.g., "Phase 0 DoD verified") — verify DoD manually before ticking.
- Otherwise: read the commit's description in §5.

### Step 4 — Execute (variable)
- For atom rewrites: open `plans/atom-authoring-contract-rewrites.md` at the corresponding §-id (B-1, FD-2, etc.).
- Apply the BEFORE→AFTER edit exactly as specified.
- Commit with `atom-contract(phase-N): <description>` prefix.

### Step 5 — Tick (10 seconds)
- Edit §6 Phase Status to change `[ ]` → `[x]` for the commit you just made.
- If phase just completed, tick the phase DoD row.

### Step 6 — Decide continuation
- More to do in this session? Back to Step 3.
- End of session? Commit any plan-file updates with `atom-contract: tick phase status`. Clean exit.

**Never**:
- Skip to a later phase without verifying earlier DoDs.
- Re-open §2 "Decisions locked".
- Create new per-topic `*_atom_contract_test.go` files.
- Write atoms with spec-ID citations, handler-behavior prose, or plan-doc paths.
- Tick `[x]` without the commit actually landing.
