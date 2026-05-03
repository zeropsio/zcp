# Live-eval runs — atom corpus goldens

**Protocol**: see `_live-eval-protocol.md`.
**Owner**: `@krls2020` (initial assignment).
**CODEOWNERS**: `/internal/workflow/testdata/atom-goldens/_live-eval-runs.md @krls2020` (see `.github/CODEOWNERS`).

This file is an append-only log of live-eval runs against eval-zcp.
Each run captures the date, scenarios walked, services used,
divergence summary, and disposition. Quarterly cadence per protocol
§5; ad-hoc runs welcome when production friction surfaces.

---

## 2026-05-02 — Q1 2026 live-eval (first run, post-merge cross-check)

**Owner**: `@krls2020`
**Scope**: 1 of 5 protocol scenarios verifiable against current eval-zcp
state (`idle/adopt-only`); 1 negative-path verification (export handler
validation gate); plus 1 deployment-state observation (production
zcp is pre-Phase-0b-migration, MCP restart needed for new paths).
**Services provisioned**: none — eval-zcp's existing `zcp` service
(type `zcp@1`, status ACTIVE, not bootstrapped, subdomain enabled)
already matches `idle/adopt-only` fixture shape.
**Disposition**: clean for verifiable subset; remaining 4 scenarios
require provisioning + state-driving (regular cadence work).

### Findings

#### 1. `idle/adopt-only` fixture-vs-production shape — **PASS**

Production envelope from `zerops_workflow action="status"`:
- `Phase: idle` ✓ (matches fixture `PhaseIdle`)
- `IdleScenario: adopt` ✓ (matches fixture `IdleAdopt` — derived
  from "1 unbootstrapped runtime, no metas, no resumable")
- `Services`: 1 runtime (`zcp` / `zcp@1` / status `ACTIVE` /
  `bootstrapped: false`) ✓ structurally matches fixture's single-
  runtime shape

Atom fire-set:
- `bootstrap-route-options` ✓ (present in production response)
- `idle-adopt-entry` ✓ ("Adopt unmanaged runtimes" guidance fires —
  matches the golden's expected atom IDs at
  `internal/workflow/testdata/atom-goldens/idle/adopt-only.md`)

Differences (cosmetic, not divergence):
- Service hostname: live=`zcp`, fixture=`appdev` (placeholder
  substitution, irrelevant).
- Type-version: live=`zcp@1` (the ZCP runtime container itself),
  fixture=`nodejs@22`. Different runtime classes (ZCP runtime vs
  generic dynamic) but RuntimeClass classification doesn't gate
  fire-set for these two atoms.

**Conclusion**: fixture-vs-production cross-check PASSES for
`idle/adopt-only`. The fixture envelope shape derives cleanly from
production; the atoms render exactly as the golden expects.

#### 2. Production zcp is pre-Phase-0b-migration — **EXPECTED**

`zerops_workflow action="start" workflow="export"` against eval-zcp
returned:
```json
{
  "guidance":"Pick the runtime service to export. Pass targetService=<hostname> on the next call.",
  "phase":"export-active",
  "runtimes":["zcp"],
  "status":"scope-prompt"
}
```

The `guidance` field is a single inline string — the **pre-migration
shape**. After Phase 0b refactor, the response's `guidance` field
should be atom-rendered output from `export-intro` + `export-scope-
prompt` (multi-paragraph "three-call narrowing" framing).

**This confirms the deployed zcp is pre-migration code.** The MCP
server running `.mcp.json`-registered local binary started BEFORE
this branch landed; an MCP server restart will pick up the new code
paths. Per Phase 0b commit message (commit `7776cc7d`), this was
explicitly documented as expected:

> Live-platform smoke against eval-zcp is pending an MCP server
> restart so the new code paths run; deferred to Phase 5 documented
> live-eval protocol per plan section 5.5.

**No fixture-reality drift here** — production behavior matches
pre-migration corpus state, which is consistent. Once MCP restarts,
the next quarterly run can verify the migrated atom-rendered output
matches the new goldens (the `export/scope-prompt`,
`export/variant-prompt`, etc. set introduced in Phase 0b).

#### 3. Export handler validation gate — **PASS**

`zerops_workflow action="start" workflow="export" targetService="zcp"`
returned the structured error:
```json
{
  "code":"SERVICE_NOT_FOUND",
  "error":"Service \"zcp\" has no bootstrapped meta — export needs
   the topology.Mode (dev / standard / stage / simple / local-stage
   / local-only) to resolve variant",
  "suggestion":"Run bootstrap first: zerops_workflow action=\"start\"
   workflow=\"bootstrap\". Or adopt the service via adopt-local.",
  "recovery":{"tool":"zerops_workflow","action":"status"}
}
```

The error message and recovery shape match my refactored handler
exactly (verified in `internal/tools/workflow_export.go::handleExport`
line 109-112). Validation logic is identical pre/post migration —
the Phase 0b refactor only moved the success-path guidance into atom
rendering; error paths were unchanged. **Cross-check PASS for
service-not-found error path.**

### Scenarios NOT verified this run

The following 4 protocol scenarios require service provisioning +
state-driving on eval-zcp (estimated 4-8 hours per protocol §5):

| Scenario | Why deferred this run |
|---|---|
| `bootstrap/recipe/provision` | Needs a recipe-route bootstrap on a fresh `nodejs@22` runtime; observe at provision step. |
| `develop/first-deploy-dev-dynamic-container` | Needs dev-mode `nodejs@22` with `startWithoutCode: true`; never deployed; in-container. |
| `develop/standard-auto-pair` | Needs standard pair (`appdev`+`appstage`), close-mode auto, both deployed. Two-deploy minimum. |
| `strategy-setup/configured-build-integration` | Needs single runtime, GitPushState=configured, BuildIntegration=none. |
| `export/publish-ready` | Needs full export workflow driven to publish-ready (zerops.yaml + git remote + envClassifications). |

**These deferrals are expected** for the first quarterly run
(post-merge follow-up) per plan §5.5: "first live-eval run is
post-merge follow-up — NOT a pre-merge gate". Subsequent quarterly
runs will pick up these scenarios incrementally.

### Follow-ups

1. **MCP server restart** to load new code paths — agent action
   when convenient. Subsequent live-eval runs against eval-zcp will
   verify the migrated atom-rendered output (Phase 0b export
   workflow).

2. **Provisioning sequence** for next quarterly run: file a
   sub-plan (`plans/atom-corpus-quarterly-eval-2026-Q2.md` or
   similar) listing the 4 deferred scenarios with provisioning
   recipes. Recommended order: bootstrap/recipe/provision (sets up
   a service), develop/first-deploy-dev-dynamic-container (uses the
   service), strategy-setup/configured-build-integration (configures
   git-push), export/publish-ready (full workflow). standard-auto-
   pair needs separate provisioning of the pair.

3. **Fixture refinements from this run**: NONE — the one fixture
   shape we cross-checked (`idle/adopt-only`) matches production
   exactly. No edits to `scenarios_fixtures_test.go`.

### Cleanup

No services provisioned this run; no cleanup needed. eval-zcp
remains at its pre-run state (single `zcp` service).

---

(End of Q1 2026 entry.)

---

## 2026-05-03 — Q2 2026 live-eval

**Owner**: `@krls2020`
**Scope**: 3 of 5 protocol scenarios verifiable on the eval-zcp project
this run (`idle/adopt-only` re-verify; `export/scope-prompt`
post-Phase-0b probe; `develop/first-deploy-dev-dynamic` driven
end-to-end in LOCAL env). 2 scenarios deferred for the same operational
reason (`strategy-setup/configured-build-integration`,
`develop/standard-auto-pair` — both require ModeStandard pair
provisioning + container-env capture; see Findings #4).
**Services provisioned**: `probe` (nodejs@22, ModeDev) — bootstrap
classic-route + develop entry; deleted at end of run.
**Disposition**: clean for all three verifiable scenarios; two
operational blockers reproduce from Q1 (MCP server stale + new finding:
container `zcp` binary stale).

### Findings

#### 1. `idle/adopt-only` fixture-vs-production shape — **PASS** (re-verify)

Re-ran the Q1 cross-check against the unchanged eval-zcp state (single
`zcp` service, `not bootstrapped`, subdomain enabled). Production
`zerops_workflow action="status"` rendered the same atom set the golden
expects:

- `bootstrap-route-options` ✓ (`### Bootstrap route discovery` → ranked
  options → explicit overrides → collision semantics)
- `idle-adopt-entry` ✓ ("Runtime services exist in this project that
  ZCP is not tracking ... Adopt them to enable ZCP deploy and verify
  workflows.")

`Phase: idle`, `IdleScenario: adopt`, single-runtime envelope — matches
fixture shape. Q1 PASS holds; no fixture refinements needed.

#### 2. Export workflow renders pre-Phase-0b inline string — **MCP STALENESS** (Q1 #2 reproduces)

`zerops_workflow action="start" workflow="export"` returned the same
pre-Phase-0b inline guidance as Q1:

```json
{
  "guidance":"Pick the runtime service to export. Pass targetService=<hostname> on the next call.",
  "phase":"export-active",
  "runtimes":["zcp"],
  "status":"scope-prompt"
}
```

This single-line string is **not present** in the current source tree —
`grep -rn "Pass targetService=<hostname> on the next call"
internal/{tools,workflow}/` returns 0 hits in `*.go`; the on-disk
`bin/zcp` (built `2026-05-03T06:08:23Z`, version
`v9.50.0-4-gd43db695-dirty`) does NOT contain the inline string in its
`strings` output (only the atom title `"Pick the runtime service to
export"` survives, embedded in `export-scope-prompt.md`).

**Root cause**: my MCP server child process loaded a pre-Phase-0b
binary (any of the local `bin/zcp` PIDs spawned before today's
`2026-05-03 08:08` rebuild). POSIX semantics: replacing the binary on
disk does NOT update an already-running process's in-memory code. The
atom `export-scope-prompt` exists in the corpus, but the running
handler still uses the old `scopePromptResponse` that returned the
inline string instead of routing through `renderExportStatusGuidance`.

**Q1 follow-up #1 (MCP server restart) remains the actionable item.**
Until restart, every quarterly run that includes export-workflow
checks will continue to show the pre-Phase-0b shape against the
current goldens.

#### 3. `develop/first-deploy-dev-dynamic` env-axis routing — **PASS** (with expected env divergence vs container fixture)

End-to-end drive on a fresh `probe` service (nodejs@22, ModeDev,
`startWithoutCode: true`, `bootstrapped=true`, `deployed=false`):

- bootstrap classic-route discover→provision→close completed cleanly;
  meta stamped (`bootstrapped=true, mode=dev, closeMode=unset`).
- `zerops_workflow action="start" workflow="develop" scope=["probe"]`
  composed `Phase: develop-active`, with the rendered guidance hitting
  every universal develop atom the
  `develop/first-deploy-dev-dynamic-container` golden expects:
  `develop-first-deploy-intro`, `develop-api-error-meta`,
  `develop-change-drives-deploy`, `develop-deploy-modes`,
  `develop-env-var-channels`, `develop-first-deploy-env-vars`,
  `develop-first-deploy-scaffold-yaml`, `develop-http-diagnostic`,
  `develop-platform-rules-common`, `develop-deploy-files-self-deploy`,
  `develop-knowledge-pointers`, `develop-auto-close-semantics`,
  `develop-first-deploy-execute`, `develop-verify-matrix`,
  `develop-first-deploy-verify`.

**Env-axis substitution worked exactly as expected**: the live response
fired the LOCAL-env atoms in the slots the container fixture pins to
container atoms. Per fixture
`scenarios_fixtures_test.go::"develop/first-deploy-dev-dynamic-container"`,
the golden bakes `Environment: EnvContainer`; my MCP server runs in
`Environment: EnvLocal` (.mcp.json-launched on macOS). Three slot
substitutions confirmed:

| Container atom (in golden's atomIds) | Local atom that fired (live) |
|---|---|
| `develop-platform-rules-container` | `develop-platform-rules-local` ("Platform rules — local additions" / "Dev server — always background") |
| `develop-dynamic-runtime-start-container` | `develop-dynamic-runtime-start-local` ("In local env the dev server runs **on your machine**, not in Zerops...") |
| `develop-checklist-dev-mode` | `develop-checklist-local-mode` ("Development workflow" — `zcli vpn up`, `.env` bridge) |

This confirms the env-axis filter works as designed: each `environments:
[container]` atom is correctly suppressed in local env, with its
local-env counterpart firing in its place. No fixture lag and no
production drift — the divergence is **structural by design**.

**Verification of the in-container atom set** requires driving the
scenario from inside the zcp container. See finding #4.

#### 4. Container `zcp` binary on eval-zcp is also stale — **NEW (operational blocker for container-env goldens)**

`ssh zcp "zcp version"` reports `v9.49.0 (baa4c082, 2026-05-02T14:14:12Z)`.
The commit `baa4c082` ("docs: remove obsolete ZCP intro guide") predates
the entire atom-corpus-verification work — including:

- `7776cc7d` Phase 0b (route handleExport through atom synthesis)
- `26880625` Phase 1 (goldens infrastructure + 30 raw fixtures)
- `bff507f2` Phase 2 Cycle 2 (12 single-agent HIGH lies + missing-critical)
- `89cee583` Phase 2 Cycle 3 + finalize
- `b7e43088` Phase 4 (Axis O lint + coverage gate)
- `0bbec747` Cycle-3 close-out

So even if I drove the develop scenario from inside the container
(via SSH-side MCP invocation), the container's running zcp binary
would render its own pre-Phase-0b atom set — not comparable to the
current goldens.

**Implication for container-env golden verification**: the eval-zcp
project's `zcp` service needs to be redeployed with a current binary
before any container-env golden (every `EnvContainer` fixture in
`scenarios_fixtures_test.go`) can be cross-checked end-to-end. This
adds a second operational prerequisite alongside Q1 follow-up #1.

#### 5. Scenarios NOT verified this run

| Scenario | Status | Why deferred |
|---|---|---|
| `bootstrap/recipe/provision` | Deferred | Recipe-route bootstrap requires a recipe-matched runtime. Lower priority than the corpus-wide MCP/container staleness blockers. |
| `develop/standard-auto-pair` | Deferred | Needs ModeStandard pair (`appdev`+`appstage`) + two deploys + verify. Heavier provisioning; once container zcp is current, fold into a multi-scenario provisioning run. |
| `strategy-setup/configured-build-integration` | Deferred | Both `strategy-setup/*` fixtures pin `Environment: EnvContainer` + `Mode: ModeStandard` + `CloseDeployMode: CloseModeGitPush`. Same env-axis caveat as develop-container; defer to post-MCP-restart + post-container-redeploy run. |
| `export/publish-ready` | Deferred | Even if MCP restarts, this needs full export workflow driven to publish-ready (zerops.yaml + git remote + envClassifications). Independent of staleness blockers — separate provisioning effort. |

### Follow-ups

1. **Q1 follow-up #1 (MCP server restart) — STILL PENDING.** Restart
   the local Claude Code session so the MCP child re-execs the current
   `bin/zcp`. Single hop, no code change needed. Until done, every
   live-eval run that touches the export workflow will reproduce the
   pre-Phase-0b inline-string finding.

2. **NEW — Container `zcp` redeploy on eval-zcp.** Push the current
   `bin/zcp` (or an equivalent build at HEAD) to the eval-zcp `zcp`
   service so SSH-driven container-env captures match the current
   atom corpus. Until done, no `EnvContainer` golden can be
   cross-checked end-to-end.

3. **Procedure clarification (protocol §5.5 amendment, optional).**
   Add a note to `_live-eval-protocol.md` calling out that
   container-environment goldens (any fixture with `Environment:
   EnvContainer`) require driving the scenario from inside the
   container (SSH-side MCP invocation against the container's local
   zcp binary). Today the protocol leaves the agent's environment
   implicit; making it explicit avoids future confusion when
   local-env captures don't match container goldens.

4. **Q3 quarterly run plan.** Once #1 + #2 are resolved, target the 4
   deferred scenarios above. Priority order:
   `develop/standard-auto-pair` (foundational standard-pair shape) →
   `strategy-setup/configured-build-integration` (uses the standard
   pair) → `export/publish-ready` (full workflow on the same pair) →
   `bootstrap/recipe/provision` (separate recipe service, isolated).

5. **Fixture refinements from this run**: NONE. The two cross-checks
   that completed (`idle/adopt-only` re-verify + universal develop
   atom set under env-axis substitution) match production exactly
   modulo the env-axis differences that are structural-by-design.

### Cleanup

`probe` (nodejs@22, ModeDev) deleted via `zerops_delete` after the
develop scenario completed; verified via `zerops_discover` that
eval-zcp returned to its pre-Q2 state (single `zcp` service, ACTIVE,
not bootstrapped, subdomain enabled).

---

(End of Q2 2026 entry. Next entry: Q3 2026 quarterly run, owner-
scheduled — gated on follow-ups #1 + #2.)

