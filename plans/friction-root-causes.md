# Friction Root-Cause Elimination — Implementation Plan

> **Scope**: Eliminate three structural root causes of agent-visible friction in ZCP: dishonest runtime-state messaging, atom ↔ code contract drift, and static guidance verbosity. Touches deploy post-processing, deploy/verify side-effects, the atom corpus, envelope maturity tracking, the import pre-validation layer, and the eval framework.
>
> **Already shipped** (v8.114.0, 2026-04-23) — the pair-keyed meta consolidation that F#2 depended on landed as a dedicated fundamentals plan (`plans/archive/pair-keyed-meta-invariant.md`). Outcomes folded into this plan:
> - F#2 scope rejection — fixed via `ManagedRuntimeIndex`; no further work here.
> - Strategy-handler stage-hostname fix — shipped as bonus; no further work here.
> - Spec invariant E8 pinned in `spec-workflows.md §8`.
> - `TestNoInlineManagedRuntimeIndex` guards against future inline drift; its AST-scan skeleton is the reference pattern for P2.5 below.
>
> **Companion specs** (authoritative):
> - `docs/spec-workflows.md` — §1.3–§1.6 pipeline, §1.4 Plan dispatch, §4 develop flow, §8 invariants (incl. **E8 pair-keyed meta**).
> - `docs/spec-knowledge-distribution.md` — §2 envelope, §3 axes, §5 synthesizer, §10 invariants.
> - `docs/spec-work-session.md` — §3 data model, §7 tool side-effects.
> - `CLAUDE.md` + `CLAUDE.local.md` — TDD gates, single-path engineering, fix-at-source, no fallbacks, 350 LOC per file cap.

---

## 0. Non-Negotiable Constraints

Violating any of these fails the plan.

1. Every workflow-aware response flows through `ComputeEnvelope → BuildPlan → Synthesize` (invariant KD-01, P1).
2. `BuildPlan` and `Synthesize` stay pure — byte-identical output for byte-equal envelopes (KD-02/03).
3. `ServiceMeta` is the single persistent per-service state (KD-11).
4. No backwards-compat shims. One canonical path per behavior.
5. TDD at every layer a change touches — RED test before GREEN code.
6. MCP tool annotations (`ReadOnlyHint`, `IdempotentHint`) are protocol contracts. They cannot be silently violated.
7. Every `importPreValidationRule` carries empirical evidence inline (live test result or Zerops docs citation).

---

## 1. Context — Three Structural Root Causes

### RC-A — ZCP asserts runtime state without checking it

Post-deploy messaging derives claims about process state from static heuristics. Build-warning fetching returns stale entries from prior deploys.

Two specific expressions:

- **F#3**: `internal/tools/deploy_poll.go:44` branches on `ops.NeedsManualStart(serviceType)` (`internal/ops/deploy_validate.go:378-387`). The branch sets `result.Message` to *"Dev server NOT running (idle start). Open new SSH to start server."* for any non-webserver service type, regardless of what `run.start` actually declares. A service with `run.start: gunicorn foo` gets the message; the server is actually running.

- **F#10**: `internal/ops/build_logs.go:29-57` — `FetchBuildWarnings` queries logs from `event.Build.ServiceStackID` with severity filter only. The build service-stack is a persistent Zerops entity; its log accumulates warnings from every historical build. Warnings from a fixed-and-redeployed build surface in the next successful deploy's result.

Brand damage: in an LLM harness, the agent treats MCP output as authoritative. False positives trigger remedial actions for problems that don't exist.

### RC-B — Atom claims and code behavior have no enforced contract

Atoms and Go code are authored in isolation. Drift is silent. Five specific expressions:

- **F#2**: atom `develop-first-deploy-promote-stage.md:14` promises *"both halves are in scope for auto-close"*. Code at `internal/tools/workflow_develop.go:73-82` builds `runtimeMetas` by iterating meta files. Stage hostname has no standalone meta file in container mode (spec §2.7: *"Container: hostname = devHostname"*). `validateDevelopScope` at `workflow_develop.go:194-201` rejects `appstage` in scope with `"scope contains unknown or non-deployable hostnames"`.

- **F#6**: atom `develop-first-deploy-verify.md:11-13` instructs the agent to call `zerops_subdomain action=enable` manually before `zerops_verify`. Code at `internal/ops/verify.go:151-167` returns `CheckSkip` with *"subdomain not enabled — call zerops_subdomain action=enable first"* when subdomain is off. The first verify on every new service always skips.

- **F#7**: Zerops API rejects certain service configurations with `{Code: "projectImportInvalidParameter", Message: "Invalid parameter provided."}` — no field context. The SDK's `APIError` carries only these two fields (`internal/platform/zerops_search.go:64-69`). ZCP has the import schema but does not pre-validate cross-field rules, forcing the agent into trial-and-error.

- **F#8**: `internal/ops/deploy_ssh.go:175` auto-runs `(test -d .git || git init -q -b main)` over SSH inside the target container for push-dev — this is correct. But no atom warns the agent against running `git init` from the ZCP mount side. A pre-created `.git` on the SSHFS mount breaks ownership and the push fails.

- **F#9**: the `dotnet-hello-world` recipe declares `deployFiles: - ./out`. ASP.NET with `UseStaticFiles()` needs `wwwroot/` at `ContentRootPath`; without the `~` suffix, artifacts land at `/var/www/out/wwwroot/` and static serving fails. Tilde semantics are documented for static frontends only; no recipe or atom note covers dynamic-runtime publish-output extraction.

### RC-C — Guidance envelope does not adapt to session maturity

`StateEnvelope` (`internal/workflow/envelope.go:15-92`) carries `Phase`, `Environment`, per-service `Mode`/`Strategy`/`Deployed` flags — but no per-service signal for "successfully verified in the current work session." Consequence: atoms cannot distinguish first-turn guidance from tenth-turn guidance on the same service.

Measurement: for a develop-active envelope with `{Phase=develop-active, Environment=container, service={mode=dev, runtime=dynamic, strategy=push-dev, deployed=true}}`, 17 atoms match, total guidance body is 17 059 bytes, rendered output is ~18 KB. The same content renders every turn.

Heaviest atoms rendered in this shape:
- `develop-verify-matrix.md` — 3 399 B (84 lines, contains a full sub-agent prompt).
- `develop-ready-to-deploy.md` — 1 668 B.
- `develop-http-diagnostic.md` — 1 588 B.
- `develop-platform-rules-common.md` — 1 247 B.
- `develop-platform-rules-container.md` — 1 161 B.
- `develop-env-var-channels.md` — 1 132 B.

Several of these are load-bearing per turn. One is not: `develop-verify-matrix.md`'s sub-agent prompt is a reference manual the agent absorbs once and recalls from context afterward.

---

## 2. Workstreams

Four workstreams aligned one-to-one with the root causes plus an enabling prerequisite.

```
                       ┌─── A1 (P1)
         A0 (W0) ──────┤
                       └─── A6 (P3)
                       
         A2 (P2.5 + P2.1)
              │
       ┌──────┼──────┐
       │      │      │
      A3    A4     A5     ← P2.2 / P2.3 / P2.4
       │      │      │
       └──────┼──────┘
              │
            A7 (integration + release)
```

W0, P1, P2 run in parallel. P3 depends on W0 (size assertion available) and on P1 merged (shared surfaces).

Each workstream below is structured identically: problem statement → design → invariants preserved / established → TDD plan → file budget.

---

### W0 — Eval framework size assertion support

**Problem**: `internal/eval/scenario.go::Expectation` (lines 35-46) accepts only substring-based assertions (`requiredPatterns`, `forbiddenPatterns`, tool call counts). There is no mechanism to bound response size. P3 regression testing requires it.

**Design**:

1. Extend `Expectation`:
   ```go
   type Expectation struct {
       // ... existing fields ...
       MaxBriefBytes    int      `yaml:"maxBriefBytes,omitempty"`
       MaxAtomsRendered int      `yaml:"maxAtomsRendered,omitempty"`
       BriefToolName    string   `yaml:"briefToolName,omitempty"` // e.g. "zerops_workflow"
       BriefActions     []string `yaml:"briefActions,omitempty"`  // e.g. ["start","status"]
   }
   ```
2. Add `checkBriefSize` in `internal/eval/grade.go`:
   - For tool calls matching `BriefToolName` + `BriefActions`, extract guidance body from the response.
   - Compute byte length; fail when greater than `MaxBriefBytes`.
   - Count rendered atoms (heading or delimiter count); fail when greater than `MaxAtomsRendered`.
   - Failure message reports actual vs. cap for both metrics.

**Invariants preserved**: scenarios that omit the new fields (defaults = 0) have identical behavior to today.

**TDD**:
1. RED: `internal/eval/scenario_test.go` — parse a scenario with the new fields; verify YAML round-trip.
2. RED: `internal/eval/grade_test.go` — mock tool calls above and below thresholds; verify pass/fail.
3. GREEN: implement.

**File budget**: ~60 LOC across `scenario.go` + `grade.go` + tests.

---

### P1 — Remove dishonest runtime-state claims

**Goal**: post-deploy messaging no longer asserts process state or surfaces stale warnings.

#### P1.1 — Delete the "Dev server NOT running" branch

**Why delete, not refactor**: runtime-start guidance is already atom-owned (`develop-dynamic-runtime-start-container.md:11-21`, `-local.md:11-19`, `develop-implicit-webserver.md:9-14`, `develop-checklist-dev-mode.md:12-13`, `bootstrap-runtime-classes.md:15-19`). Deploy-tool code copies are drift. `NeedsManualStart` has zero non-UX consumers — only `deploy_poll.go:44` and `next_actions.go:32`. Parsing `zerops.yaml` to improve accuracy adds an edge-case surface for no benefit when the whole claim is redundant with atoms and with `zerops_verify`.

**Changes**:

- `internal/tools/deploy_poll.go:42-50` — collapse the three-branch switch:
  ```go
  result.Message = fmt.Sprintf("Successfully deployed to %s. Run zerops_verify for runtime state.", result.TargetService)
  if result.SourceService == result.TargetService {
      // Strategy-agnostic fact: push-dev replaces the container, killing prior SSH sessions.
      // Runtime-start guidance (dynamic SSH start, implicit-webserver auto) lives in atoms.
      result.Message += " New container replaced old — prior SSH sessions are gone."
  }
  ```
- `internal/tools/next_actions.go:28-48` — collapse `deploySuccessNextActions` to unconditionally return `nextActionDeploySuccess`. The runtime-class-specific branches duplicate atom content.
- `internal/ops/deploy_validate.go:378-387` — delete `NeedsManualStart`.
- `internal/ops/deploy_validate_test.go:592-620` — delete corresponding tests.
- **Keep** `IsImplicitWebServerType` — it has legitimate non-UX consumers at `internal/tools/workflow_checks_generate.go:103` (port/healthCheck validation) and `internal/ops/deploy_validate.go:41` (`run.start` requirement validation).
- **Keep** `runtimePHPApach` / `PHPNginx` / `Nginx` / `Static` constants — consumed by `IsImplicitWebServerType`.

**Invariants preserved**: O1 (deploy blocks until build completes), O4 (manual SSH start for container+dynamic — atoms own this unchanged). Single-path engineering per CLAUDE.md — eliminates code ↔ atom duplication.

**Invariants newly established**:
- **TA-01**: post-deploy `result.Message` is strategy-agnostic and runtime-class-agnostic. Enforced by `deploy_post_message_contract_test.go` (AST scan for forbidden branch patterns).

**TDD**:
1. RED: `deploy_poll_test.go` `TestPollDeployBuild_ActiveStatus_NeutralMessage` — mock ACTIVE; assert message contains `"Successfully deployed"` AND does NOT contain `"NOT running"`, `"idle start"`, `"auto-starts"`.
2. RED: `deploy_post_message_contract_test.go` (new) — AST-scan `deploy_poll.go` for switch/if branches referencing `NeedsManualStart` or `IsImplicitWebServerType` inside message construction. Expect zero hits.
3. GREEN: delete + simplify.
4. Scenario snapshot updates (`scenarios_test.go` S6/S8/S9) — update expected `Message` text.

**File delta**: –50 LOC net.

#### P1.2 — Filter stale build warnings by pipeline start time ✅ SUPERSEDED by `plans/logging-refactor.md`

**Outcome**: The narrow P1.2 patch described below was abandoned mid-session in favour of a fundamental refactor that solves the same symptom more durably. Live probes (2026-04-23) revealed that:

- The current client-side `Since` filter is lex-compare, broken at sub-second boundaries (ASCII `Z` > `.` ordering); so P1.2 as originally specified would land on top of a silently-wrong comparator.
- The Zerops log backend exposes a cleaner primitive: `tags=zbuilder@<appVersionId>` filters exactly this build's entries server-side. No time window needed.
- Our queries never set `facility`, so daemon noise (sshfs, systemd) leaks into build warnings regardless of any time filter.

The refactor (`plans/logging-refactor.md`, shipped 2026-04-23) delivers a stronger guarantee than P1.2 would have: `FetchBuildWarnings` now scopes by tag identity, not time window. Stale warnings from prior builds are physically excluded, not probabilistically filtered. The sub-second lex bug is fixed for every path that still uses `Since` (`zerops_logs` MCP tool, `FetchRuntimeLogs`). The `deploy-warnings-fresh-only` scenario is authored at `internal/eval/scenarios/deploy-warnings-fresh-only.md` as planned.

**Frozen original text preserved for audit**:

> Why: `event.Build.ServiceStackID` identifies a persistent build service-stack; its log accumulates warnings across all historical builds of the target service. A fixed-and-redeployed build's warning response currently includes entries from prior builds.
>
> Key fact: `platform.LogFetchParams.Since` field already exists and the client-side time filter is implemented but never populated. The fix is to populate it. The anchor is `event.Build.PipelineStart`.
>
> File delta: +15 LOC.

**Actual delivered changes**: see `plans/logging-refactor.md` §5 Phases 1–7. Rough file deltas: +logfetcher pipeline rewrite, +MockLogFetcher filter fidelity, +tag-identity in `FetchBuildWarnings`/`FetchBuildLogs`, +container-creation-start anchor in `FetchRuntimeLogs`, +AST contract test, +2 CLAUDE.md conventions, +eval scenario.

---

### P2 — Guidance ↔ code contract

**Goal**: atom claims about workflow behavior have corresponding code paths. Mismatches fail at test time.

#### P2.1 — Scope accepts stage hostname in standard-mode pair ✅ SHIPPED in v8.114.0

Delivered via a broader consolidation — see `plans/archive/pair-keyed-meta-invariant.md`. The problem was a symptom of an unnamed invariant (one ServiceMeta file represents a whole dev/stage pair, keyed by `m.Hostname`, with the other half in `m.StageHostname`). Four consumers respected it correctly, two violated it, four duplicated the dual-key logic inline with subtle divergences.

The shipped consolidation:

- Introduces `workflow.ManagedRuntimeIndex(metas) map[string]*ServiceMeta` in `internal/workflow/service_meta.go` as the canonical hostname→meta helper on top of the existing `m.Hostnames()` primitive.
- Exports `findMetaForHostname` → `FindServiceMeta` as the disk-backed counterpart for tool-layer callers.
- Migrates all nine relevant consumers: `handleDevelopBriefing` (F#2 root fix), `handleStrategy` set path (bonus fix — stage hostname now resolves), `buildStrategySetupSnapshots`, `trackTriggerMissingWarning`, `enrichWithMetaStatus`, `handleRoute` (metaMap + stageOf collapsed), `buildServiceSnapshots`, `adoptableServices`, `resumableServices`.
- Adds `TestNoInlineManagedRuntimeIndex` — AST scan that blocks any future inline reimplementation (whitelist is service_meta.go and its tests).
- Promotes the invariant to **spec-workflows.md §8 E8** as a normative rule, threaded through `spec-knowledge-distribution.md §8.2` and `CLAUDE.md` Conventions.

**What this means for the friction workstream**: F#2 (scope rejection) and a bonus bug (strategy set for stage hostname) are both fixed. `TestHandleDevelopBriefing_StandardPair_StageInScope_Accepted` and `TestScenario_StandardPair_FirstDeploy_PromoteToStage` pin the behavior. No additional work here.

**Downstream dependency**: the contract-test pattern from `pair_keyed_contract_test.go` (AST scan + citation-of-invariant failure message + tight whitelist) is the template P2.5's atom-code contract framework below can reuse.

#### P2.2 — Deploy handler auto-enables subdomain on first deploy ✅ SHIPPED in v9.2.0 (robustness fundament in v9.1.0)

Delivered as two sequenced plans rather than the single P2.2 drop above. Live-platform probing uncovered that the original "just call EnableSubdomainAccess after deploy" design would have landed on top of three pre-existing bugs in the subdomain path itself:

1. **Redundant enable produces a garbage FAILED process** — the platform accepts a second enable on an already-enabled service, then immediately fails the resulting Process with `error.code=noSubdomainPorts`. ZCP reported success; the GUI showed a red error event on every redeploy.
2. **`pollManageProcess` silently discarded its timeout bool** — a 10-minute poll timeout returned as if enable had succeeded, leaving callers to race the L7 balancer with no signal.
3. **L7 propagation is 440ms–1.3s after Process FINISHED** — the first `zerops_verify` after enable hit the old backend and reported 502 roughly half the time, which the agent mis-diagnosed as an app crash.

Auto-enable built on top of those bugs would have amplified them across every first deploy. So the work shipped in two plans:

**Plan 1 — `plans/archive/subdomain-robustness.md` (v9.1.0)**: fixed the fundament.

- `ops.Subdomain` reads `SubdomainAccess` via a REST-authoritative `GetService` call before invoking `EnableSubdomainAccess` and short-circuits when already on, eliminating the garbage-FAILED-process pattern (check-before-enable, mirrored for disable).
- `pollManageProcess` timeout now surfaces into `SubdomainResult.Warnings` instead of being discarded via `_ :=`.
- `ops.WaitHTTPReady` (new primitive) waits for each subdomain URL to respond with <500 after a fresh enable, gated on the empirically-measured 440ms–1.3s propagation window.
- Platform `FailReason` is preserved in Warnings instead of being silently dropped.
- Dead `SubdomainAccessAlreadyEnabled`/`Disabled` error-code branches were removed — the platform doesn't emit those codes anymore.
- Contract test `TestSubdomainRobustnessContract` (AST scan of `ops/subdomain.go` + `tools/subdomain.go`) pins all four fixes so any regression surfaces as a failing unit test, not a latent production bug.
- Normative rule added as **CLAUDE.md Conventions** bullet *"Check-before-mutate for platform APIs with no idempotent response"*, threaded through **spec-workflows.md §8 O3**.

**Plan 2 — `plans/archive/subdomain-auto-enable.md` (v9.2.0)**: built the auto-enable on top of the robust base.

- `maybeAutoEnableSubdomain` helper (in `internal/tools/deploy_subdomain.go`) wired into all three deploy handlers (`deploy_ssh.go`, `deploy_local.go`, `deploy_batch.go`). Gate is platform-side via `ops.Subdomain`'s internal check-before-enable, **not** meta-side `FirstDeployedAt` — because `FirstDeployedAt` is pair-keyed (E8) and the dev half's stamp also covers stage, so using it would skip the stage cross-deploy's auto-enable. `modeEligibleForSubdomain` allow-lists `dev/stage/simple/standard/local-stage` explicitly; production and unknown modes default to no auto-enable.
- Fresh-enable HTTP readiness wait fires via `ops.WaitHTTPReady`; already-enabled short-circuits skip the probe (the route has been live since the earlier enable, so probing just adds latency).
- `DeployResult.SubdomainAccessEnabled` + `SubdomainURL` surface in the tool response so the agent sees the auto-enable happened without checking discover afterwards.
- Atoms updated: `develop-first-deploy-{intro,execute,verify,promote-stage}` + `develop-implicit-webserver` no longer tell the agent to run `zerops_subdomain action=enable` — the deploy handler owns it.
- Contract test `TestSubdomainAtomContract` (`internal/workflow/subdomain_atom_contract_test.go`) scans the embedded corpus and blocks any of those atoms from regaining an imperative enable phrase, keeping the atom-side and handler-side of the contract locked together.
- Normative rule added as **CLAUDE.md Conventions** bullet *"Subdomain L7 activation is a deploy-handler concern"*, anchored to **spec-workflows.md §4.8** and threaded through spec-knowledge-distribution.md §8.2.

**Design divergences from the original P2.2 draft above**:

- **Gate is platform-side, not meta-side.** The original used `meta.FirstDeployedAt == ""` as the first-deploy signal. That breaks for stage cross-deploy in standard mode because the dev half's stamp covers the whole pair (E8). The delivered gate checks `detail.SubdomainAccess` on the target service via `GetService` — authoritative, pair-correct, and free of meta-lifecycle coupling.
- **Mode allow-list expanded.** The original listed `dev/stage/simple`. The delivered allow-list adds `standard` (dev half explicitly) and `local-stage` (stage half of a local standard pair), matching the full set of modes where a Zerops runtime serves HTTP.
- **Two atoms cleaned, not one.** The original mentioned `develop-first-deploy-verify` only. In practice five atoms referenced the imperative enable pattern; all five were cleaned and all five are pinned by the atom contract test.
- **No special-case for `SUBDOMAIN_ALREADY_ENABLED` error code.** The original draft anticipated one. Empirical probing showed the platform doesn't emit that code anymore — redundant enable surfaces as HTTP 200 with a FAILED Process, handled by the check-before-enable gate plus a belt-and-suspenders TOCTOU normalization at the tool layer.

**Where to look**:
- Live code — `internal/ops/subdomain.go`, `internal/ops/http_ready.go`, `internal/tools/subdomain.go`, `internal/tools/deploy_subdomain.go`, the three deploy handlers, atom bodies listed above.
- Contract pins — `internal/ops/subdomain_contract_test.go` (Plan 1), `internal/workflow/subdomain_atom_contract_test.go` (Plan 2).
- Archived plan docs — `plans/archive/subdomain-robustness.md`, `plans/archive/subdomain-auto-enable.md`.

#### P2.3 — Import pre-validation with empirical-evidence discipline (SUPERSEDED)

> **Superseded** by `plans/api-validation-plumbing.md` (shipped
> 2026-04-23). The pre-validation rule-table approach was retired in
> favor of plumbing the Zerops API's structured `apiMeta` to the LLM
> surface. F#7 is closed: every 4xx error from the import endpoint now
> carries field-level detail in the MCP response's `apiMeta` JSON key.
> The deploy flow additionally calls `ValidateZeropsYaml` pre-push so
> zerops.yaml errors surface before a build cycle is wasted. See
> `plans/api-validation-plumbing.md §12` for the full list of
> supersessions. Content below is historical only — do not implement.

**Problem**: Zerops API returns `projectImportInvalidParameter` with no field context. Some constraints are deterministic and verifiable; others are ambiguous anecdotes.

**Design** — two tables with distinct authority:

1. **Pre-validation (blocks before API call)** — reserved for rules with empirical proof:
   ```go
   // internal/ops/import_validation.go
   //
   // Pre-validation blocks requests. Each rule requires empirical evidence
   // (live API test result or Zerops docs citation) inline. Wrong rule = blocked
   // valid config = broken feature. When in doubt, add to importPostHocHints
   // instead.
   type importPreRule struct {
       applies func(ImportService) bool
       message string
   }

   var importPreValidationRules = []importPreRule{
       {
           // Evidence: live API test. `mode: NON_HA` and `mode: HA` on object-storage
           // both return projectImportInvalidParameter. Object-storage scaling is
           // platform-fixed; no HA/NON_HA variant.
           applies: func(s ImportService) bool {
               return strings.HasPrefix(s.Type, "object-storage") && s.Mode != ""
           },
           message: "object-storage type does not accept the `mode` field. Remove `mode: ...` — object-storage scaling is fixed by the platform.",
       },
   }
   ```

2. **Post-hoc hints (advisory, non-blocking)** — surfaced alongside API errors when a pattern matches:
   ```go
   type importHintRule struct {
       applies func(ImportService) bool
       hint    string
   }

   // importPostHocHints — advisory only. Attached to projectImportInvalidParameter
   // responses when the rule matches. Wrong hint = confusing message alongside
   // real API error, never broken feature. Adding requires only "this confusion
   // has been observed".
   var importPostHocHints = []importHintRule{
       // empty; seed as field reports arrive
   }
   ```

**Wiring** in `internal/ops/import.go`:

```go
if violations := ValidateImportConstraints(parsed); len(violations) > 0 {
    return platform.NewPlatformError(platform.ErrInvalidParameter, violations[0], ...)
}
// ... existing client.ImportServices call ...
if ss.Error != nil && ss.Error.Code == "projectImportInvalidParameter" {
    ss.Error.Hints = matchPostHocHints(parsed, ss.Name)
}
```

**Extend** `APIError` with a `Hints []string` field. Existing consumers that ignore it are unaffected.

**Invariants preserved**: O2 (import blocks until complete) — pre-validation happens earlier in the same call. API error mapping for non-violating imports unchanged.

**TDD**:
1. RED: `import_validation_test.go` — table-driven: `object-storage + mode=NON_HA` → error; `object-storage + mode=HA` → error; `object-storage` no mode → nil; non-object-storage → nil.
2. RED: `import_test.go` integration — `ops.Import` with violating YAML returns `ErrInvalidParameter` and never reaches `client.ImportServices`.
3. RED: `TestImport_PostHocHintsFoldedIntoError` with empty rule table — assert `Hints` is nil (no crash, no spurious hints).
4. GREEN: implement both tables and wiring.

**File delta**: +80 LOC (new file + test); small edit in `import.go` and error struct.

#### P2.4 — Content fixes

**Git-init guidance** — in the first-deploy atom (`internal/content/atoms/develop-first-deploy-execute.md` or `-intro.md`; pick whichever fires at code-write phase for container environment), add:

> **Git** — don't run `git init` from the ZCP mount side (`/var/www/{hostname}/`). Push-dev initializes the repo inside the target container over SSH automatically. Pre-creating `.git` on the SSHFS mount breaks ownership for the target container and the first push fails.

This aligns guidance with the existing behavior at `internal/ops/deploy_ssh.go:175` (`(test -d .git || git init -q -b main)` over SSH).

**.NET recipe** — via the sync flow (`zcp sync pull recipes dotnet-hello-world` → edit → `zcp sync push`):

- `deployFiles: - ./out` → `deployFiles: - ./out/~`
- `run.start: dotnet ./out/app/App.dll` → `run.start: dotnet App.dll`
- Add a note block after `deployFiles`:
  > **Publish output extraction** — `./out` preserves the directory (artifacts land at `/var/www/out/`). `./out/~` extracts contents (artifacts land at `/var/www/`). Choose extraction when the runtime expects assets like `wwwroot/` at the application's ContentRootPath.

**No new runtime-specific atom.** The axis model has no runtime-version granularity — a `runtimes: [dynamic]` atom would fire dotnet advice for every nodejs, python, go, and rust deploy. Runtime-specific quirks beyond recipe scope are tracked for a future axis-model extension.

**Invariants preserved**: atom frontmatter format (`internal/content/atoms_test.go` validates); corpus coverage invariants (`corpus_coverage_test.go`).

**File delta**: +5 LOC in one atom; upstream recipe PR.

#### P2.5 — Atom ↔ code contract test framework

**Design**: `internal/workflow/atom_contract_test.go` defines a table binding atom IDs to behavioral phrases and corresponding code paths:

```go
type atomContract struct {
    atomID         string   // atom's frontmatter id
    phraseRequired []string // substrings that must appear in body
    phraseAbsent   []string // substrings that must NOT appear
    testName       string   // a Go test that exercises the behavior
    codeAnchor     string   // file:symbol[#subsymbol] located via AST
}

var atomContractTable = []atomContract{
    // P2.1 is already shipped via v8.114.0; behavior is pinned by
    // TestHandleDevelopBriefing_StandardPair_StageInScope_Accepted and
    // TestScenario_StandardPair_FirstDeploy_PromoteToStage. The atom
    // phrase "both halves" is still worth an entry here so atom edits
    // that drop it fail the build even though code behavior is already
    // correct.
    {
        atomID:         "develop-first-deploy-promote-stage",
        phraseRequired: []string{"both halves"},
        testName:       "TestHandleDevelopBriefing_StandardPair_StageInScope_Accepted",
    },
    // P2.2
    {
        atomID:       "develop-first-deploy-verify",
        phraseAbsent: []string{"zerops_subdomain"},
        testName:     "TestDeployHandler_FirstDeploy_AutoEnablesSubdomain",
    },
    // P2.4
    {
        atomID:         "<first-deploy-atom-chosen-by-A5>",
        phraseRequired: []string{"don't run `git init` from the ZCP mount side"},
        codeAnchor:     "internal/ops/deploy_ssh.go#buildSSHCommand",
    },
}
```

`TestAtomContract` iterates the table: loads atoms via `content.ReadAllAtoms()`, checks phrase presence/absence, AST-resolves test names and code anchors. A failing row lists atom + missing phrase / missing symbol together so one run surfaces all drift.

Header comment pins the authoring policy:

> Adding or rewording an atom's behavioral promise → update this table. Removing an entry because a test reworded is correct; deleting an entry to make tests pass while the behavior is still asserted is a regression.

**Invariants newly established**:
- **TA-02**: every atom making a behavioral claim has a contract entry. The framework is additive — it does not gate atoms without entries, it gates the entries themselves against atom and code drift.

**TDD**: seed test per CLAUDE.md pattern; table-driven; three initial entries above.

**File budget**: ~120 LOC.

**Pattern reference**: `internal/workflow/pair_keyed_contract_test.go` (shipped in v8.114.0) demonstrates the AST-scan skeleton this framework should extend — whitelist-by-exact-file-path, tight failure message citing the invariant's spec anchor, zero dependencies beyond `go/parser` + `go/ast`. The P2.5 test differs in that it also loads atom bodies via `content.ReadAllAtoms()` and runs phrase presence/absence assertions, but the scanning half is directly reusable.

---

### P3 — Adaptive guidance envelope

**Goal**: the develop-active brief scales down for services that have already verified successfully in the current work session.

**Target sizes** (measured baselines):
- First-run brief (any service never-verified): ≤ 20 KB sanity cap. Today: ~18 KB.
- Edit-loop brief (at least one service verified this session): ≤ 14 KB. Today: ~18 KB.

A smaller target is not achievable without breaking content currently asserted by `corpus_coverage_test.go:207-209` (`MustContain` for `develop-env-var-channels` and `develop-platform-rules-common` bodies). A radical "brief-on-demand" reduction is a separate effort tracked in §10.

**Design**:

1. Add `ServiceMaturity` to `ServiceSnapshot` in `internal/workflow/envelope.go`:
   ```go
   type ServiceMaturity string
   const (
       MaturityFirstRun ServiceMaturity = "first-run" // no verify history OR last verify failed
       MaturityEditLoop ServiceMaturity = "edit-loop" // last verify passed
   )
   ```

2. Derive in `ComputeEnvelope` from `WorkSession.Verifies[hostname]`:
   ```go
   history := ws.Verifies[svc.Hostname]
   if len(history) > 0 && history[len(history)-1].Success {
       snap.Maturity = MaturityEditLoop
   } else {
       snap.Maturity = MaturityFirstRun
   }
   ```

   **Why last-verify, not any-historic-pass**: a service that passed then regressed needs the full diagnostic set again. "Last verify" responds to current service health; "any historic" would leave a broken service thin-briefed precisely when recovery guidance is most useful.

3. Add service-scoped axis `maturity: [first-run, edit-loop]` (empty = either) to `atom.go::AxisVector` and `synthesize.go::atomMatches`. Follows KD-16 conjunction semantics: service-scoped axes combine per service.

4. Migrate `develop-verify-matrix.md` to `maturity: [first-run]`. The body (the sub-agent prompt, 3 399 B) is reference material the agent absorbs once and can re-fetch on demand afterward.

5. Create pointer atom `develop-verify-matrix-pointer.md` with `maturity: [edit-loop]`:
   ```markdown
   ---
   id: develop-verify-matrix-pointer
   priority: 4
   phases: [develop-active]
   deployStates: [deployed]
   maturity: [edit-loop]
   title: "Verify matrix — on demand"
   ---
   Need the full verify diagnostic matrix? Call `zerops_knowledge key="verify-matrix"`.
   ```

6. Extend `internal/tools/knowledge.go` with mode 5: `key=<atom-id>` retrieves the full atom body by id with placeholder substitution applied. Respects the existing mode-exclusivity rule (`knowledge.go:115-120`) — mixing with other modes is rejected.

**Atoms explicitly NOT migrated** (load-bearing in edit-loop):
- `develop-http-diagnostic.md` — active recovery path when verify fails.
- `develop-env-var-channels.md` — `MustContain`-tested; auto-restart behavior and shadow-loop pitfall are recurring failure modes.
- `develop-platform-rules-common.md` — `MustContain`-tested base vocabulary.
- `develop-first-deploy-*` atoms — already gated by `deployStates: [never-deployed]`; they don't render in edit-loop briefs.

**A secondary migration candidate** (reserved for in-workstream decision): `develop-platform-rules-container.md` (1 161 B) mixes per-turn rules with first-run deep-reference. A body audit may produce a clean split worth an additional ~600 B saving. A6 decides based on the actual content.

**Invariants preserved**:
- KD-02 / KD-03 — `ServiceMaturity` is deterministic; synthesize output remains byte-identical for byte-equal envelopes.
- KD-04 — all atoms retain non-empty `phases`.
- KD-11 — maturity derives from WorkSession on the fly; nothing new persists per service.
- KD-12 — corpus coverage expands to include both maturity values.
- KD-16 — maturity is service-scoped; conjunction with other service-scoped axes is per-service.

**TDD**:
1. RED: `envelope_test.go` `TestServiceMaturity_FromLastVerify` — verify history `[pass]` → edit-loop; `[pass, fail]` → first-run; `[]` → first-run.
2. RED: `synthesize_test.go` — axis filter for each maturity value.
3. RED: `corpus_coverage_test.go` — matrix extended with both maturity values across existing fixtures.
4. RED: `brief_size_test.go` (new) — first-run envelope ≤ 20 KB; edit-loop envelope ≤ 14 KB.
5. RED: new scenario `develop-verified-service-shorter-brief.md` uses `MaxBriefBytes: 14336` from W0.
6. GREEN: envelope field, synthesize filter, atom frontmatter edit, pointer atom, knowledge mode 5.
7. Scenario snapshot updates for any develop-active scenario whose expected brief content changes.

**File budget**:
- `envelope.go` +8 LOC.
- `compute_envelope.go` +15 LOC.
- `atom.go` +10 LOC.
- `synthesize.go` +20 LOC.
- `knowledge.go` +60 LOC (mode 5).
- `brief_size_test.go` +80 LOC.
- Atom corpus: one frontmatter edit, one new pointer atom.

---

## 3. New Scenario Coverage

Six scenarios in `internal/eval/scenarios/` provide regression coverage for every friction point addressed. Each is authored by the workstream's agent as part of their deliverable.

| File | Workstream | Key assertions |
|---|---|---|
| `develop-scope-stage-accepted.md` | P2.1 | `scope=["appdev","appstage"]` passes; `forbiddenPatterns`: `"non-deployable hostnames"` |
| `deploy-honest-runtime-state.md` | P1.1 | Dynamic runtime with real `run.start`; `forbiddenPatterns`: `"NOT running"`, `"idle start"` |
| `verify-auto-enables-subdomain.md` | P2.2 | First-deploy, subdomain off; agent makes zero `zerops_subdomain` calls; `http_root` passes |
| `import-cross-field-validate.md` | P2.3 | `object-storage + mode: NON_HA` rejected pre-flight with hostname and `"mode"` in error |
| `deploy-warnings-fresh-only.md` | P1.2 | Two deploys, first with warning condition, second clean; second result contains no warning string from first |
| `develop-verified-service-shorter-brief.md` | P3 + W0 | After one passing verify, next `zerops_workflow action="start"` response satisfies `MaxBriefBytes: 14336` |

---

## 4. Team — Opus agent assignments

All agents run on Opus. Each briefing is self-contained — paste-ready into the Agent tool prompt.

### A0 — Eval framework size assertion (W0)

**Mission**: implement W0 per §2 W0.

**Files authorized**: `internal/eval/scenario.go`, `internal/eval/grade.go`, `internal/eval/scenario_test.go`, `internal/eval/grade_test.go`.

**Must not touch**: runner, probe, orchestration.

**Briefing**:

> You are implementing W0 of `plans/friction-root-causes.md`. Read §2 W0 for the full design.
>
> This is infrastructure. No P3 scenario can be written without it.
>
> Order:
> 1. Read `internal/eval/scenario.go::Expectation` and the corresponding `grade.go` check functions. Understand the current pattern.
> 2. RED: add a scenario parse round-trip test asserting `MaxBriefBytes`, `MaxAtomsRendered`, `BriefToolName`, `BriefActions` survive YAML marshal/unmarshal.
> 3. RED: add grade tests with mock tool calls (guidance body above and below `MaxBriefBytes`; atom counts above and below `MaxAtomsRendered`).
> 4. GREEN: implement `checkBriefSize`. Keep defaults (field = 0) behaving as "skip check" so existing scenarios are unaffected.
> 5. Run `go test ./internal/eval/... -count=1`. Green.
>
> Commit RED then GREEN on `friction-w0-eval-size`. Deliver: branch, commit hashes, brief-check invocation example.

### A1 — Runtime-state honesty + stale warnings (P1)

**Mission**: implement P1.1 and P1.2 per §2 P1.

**Files authorized**: `internal/tools/deploy_poll.go`, `internal/tools/next_actions.go`, `internal/ops/deploy_validate.go`, `internal/ops/deploy_validate_test.go`, `internal/ops/build_logs.go`, `internal/ops/build_logs_test.go`, `internal/tools/deploy_poll_test.go`, `internal/tools/deploy_post_message_contract_test.go` (create).

**Must not touch**: `workflow/`, `content/atoms/`, `tools/workflow_*.go`.

**Invariants**: O1, O4 (atoms own), `IsImplicitWebServerType` — do not delete; has legitimate non-UX consumers.

**Briefing**:

> You are implementing P1 of `plans/friction-root-causes.md`. Read §2 P1 fully.
>
> P1.1 is a deletion: `NeedsManualStart` has zero logic consumers; runtime-start guidance lives in atoms. P1.2 uses a field (`LogFetchParams.Since` at `internal/platform/types.go:167`) that already exists and is wired client-side (`internal/platform/logfetcher.go:134-144`) but never populated. The fix is six LOC.
>
> RED (commit as `red: P1 ground-truth assertion tests` on branch `friction-p1-honest-state`):
>
> 1. `internal/tools/deploy_poll_test.go` — `TestPollDeployBuild_ActiveStatus_NeutralMessage`: mock ACTIVE; assert `result.Message` contains `"Successfully deployed"` AND does NOT contain `"NOT running"`, `"idle start"`, `"auto-starts"`.
> 2. `internal/tools/deploy_post_message_contract_test.go` (new) — AST-scan `deploy_poll.go` for switch or if statements whose condition references `NeedsManualStart` or `IsImplicitWebServerType` within message-building code. Expect zero matches.
> 3. `internal/ops/build_logs_test.go` — `TestFetchBuildWarnings_FiltersStaleByPipelineStart`: mock three log entries at `t0`, `t0+1h`, `t0+2h`; set `event.Build.PipelineStart = t0+30m`; assert only the last two returned. Also `TestFetchBuildWarnings_NilPipelineStart_NoFilter` with `PipelineStart = nil`: all three returned.
>
> GREEN (commit as `green: remove dishonest runtime-state claims + anchor warning filter to PipelineStart`):
>
> 4. Edit `deploy_poll.go:42-50` — replace the three-branch switch with the neutral message + strategy-agnostic SSH-sessions-dead post-script. Exact code in §2 P1.1.
> 5. Edit `next_actions.go:28-48` — collapse `deploySuccessNextActions` to unconditionally return `nextActionDeploySuccess`.
> 6. Delete `NeedsManualStart` (`deploy_validate.go:378-387`) and its test table (`deploy_validate_test.go:592-620`).
> 7. Keep `IsImplicitWebServerType` — it has consumers at `workflow_checks_generate.go:103` and `deploy_validate.go:41`.
> 8. Edit `build_logs.go::FetchBuildWarnings` — populate `params.Since` from `event.Build.PipelineStart` (RFC3339Nano parse); widen `Limit` from 20 to 100.
> 9. Update scenario snapshots (`scenarios_test.go` S6/S8/S9) where expected `Message` content no longer matches.
>
> Run `go test ./... -count=1 -short`. Green.
>
> STOP and report if `event.Build.PipelineStart` is not populated by the platform event mapper (check `internal/platform/zerops_event_mappers.go`), if any test asserted `NeedsManualStart` for logic rather than UX, or if scenario snapshots break in unexpected ways beyond message text.
>
> Deliver: branch `friction-p1-honest-state` with RED + GREEN commits.

### A2 — Atom-code contract framework (P2.5)

**Mission**: implement the atom-contract test framework (§2 P2.5). P2.1 is already shipped in v8.114.0 via the pair-keyed-meta consolidation — no scope work remains in this agent.

**Files authorized**: `internal/workflow/atom_contract_test.go` (create).

**Must not touch**: `content/atoms/` (other agents populate entries via edits), `ops/`, `tools/`.

**Invariants**: none broken by a read-only test file.

**Pattern reference**: `internal/workflow/pair_keyed_contract_test.go` is the AST-scan skeleton your framework extends.

**Briefing**:

> You are implementing P2.5 of `plans/friction-root-causes.md`. Read §2 P2.5 fully.
>
> P2.1 is already shipped in v8.114.0 (see `plans/archive/pair-keyed-meta-invariant.md`). Your job is the contract-framework half (P2.5) only.
>
> Order:
>
> 1. Read `internal/workflow/pair_keyed_contract_test.go` — it's the AST-scan skeleton your framework extends. Note the whitelist pattern, failure message with spec anchor, and zero-dependency design.
>
> 2. Write `internal/workflow/atom_contract_test.go`:
>    - Define the `atomContract` struct and `atomContractTable` with three initial entries (text in §2 P2.5).
>    - Implement `TestAtomContract`: iterate the table, load atoms via `content.ReadAllAtoms()`, check phrase presence/absence on body, AST-resolve `testName` via `go/parser` + `go/ast` walks across `internal/`.
>    - Header comment stating authoring policy (same spirit as `pair_keyed_contract_test.go`'s comment).
>    - No `t.Parallel()` — corpus load is cheap, parallelism isn't worth it.
>
> 3. Initial expected state: P2.1 entry passes (atom body has "both halves", `TestHandleDevelopBriefing_StandardPair_StageInScope_Accepted` resolves). P2.2 entry fails (test not yet written — A3 fills it). P2.4 entry fails (atom not yet edited — A5 fills it). Document this staged completion in the table comment so A3 / A5 aren't surprised.
>
> 4. Commit on `friction-p2-contract`. Publish the framework's public API (struct name, table name, file path) so A3 and A5 can reference it.

### A3 — Subdomain auto-enable in deploy (P2.2)

**Mission**: implement auto-enable in the deploy handler per §2 P2.2.

**Files authorized**: whichever file holds the MCP `zerops_deploy` handler (locate via `grep -rn '"zerops_deploy"' internal/server/ internal/tools/`), `internal/ops/subdomain.go` (read-only), `internal/content/atoms/develop-first-deploy-verify.md`, new test files.

**Must not touch**: `internal/tools/verify.go` — verify's `ReadOnlyHint: true` is a protocol contract.

**Invariants**: O3, MCP `ReadOnlyHint` on verify.

**Briefing**:

> You are implementing P2.2 of `plans/friction-root-causes.md`. Read §2 P2.2 fully.
>
> The design decision is locked: auto-enable goes in the deploy handler post-step, not verify pre-step. Verify declares `ReadOnlyHint: true, IdempotentHint: true` in MCP annotations (`internal/ops/verify.go:35-36`). Breaking those is a protocol regression.
>
> Wait for A2's contract framework to land on main (check `git log --oneline friction-p2-contract-scope`).
>
> Order:
>
> 1. Locate the deploy handler: find where `recordDeployAttemptToWorkSession` is called. That site has `stateDir` access — the correct seam.
>
> 2. RED on branch `friction-p2-subdomain`:
>    - `TestDeployHandler_FirstDeploy_AutoEnablesSubdomain` — meta `{FirstDeployedAt="", Mode=dev}`, runtime=dynamic, `SubdomainAccess=false`. Call handler after mock ACTIVE deploy. Assert `EnableSubdomainAccess` called exactly once.
>    - `TestDeployHandler_SecondDeploy_NoAutoEnable` — meta `{FirstDeployedAt=<set>}`. Assert NOT called.
>    - `TestDeployHandler_ManagedOrProductionMode_NoAutoEnable` — meta with `Mode` outside {dev, stage, simple}. Assert NOT called.
>    - Update `atomContractTable` in `internal/workflow/atom_contract_test.go`: the P2.2 entry's `testName` should resolve to `TestDeployHandler_FirstDeploy_AutoEnablesSubdomain`. The `phraseAbsent: ["zerops_subdomain"]` assertion will fail until the atom is edited.
>
> 3. GREEN:
>    - Wire auto-enable in the handler per the gate in §2 P2.2. Idempotency via the `!svc.SubdomainAccess` check (combined with `FirstDeployedAt == ""`) — subsequent deploys skip.
>    - Edit `develop-first-deploy-verify.md:11-13` — remove the `zerops_subdomain ... action="enable"` line. Keep the `zerops_verify` call. Keep explanatory prose.
>
> 4. Scenario updates: S6 and S8 (or wherever first-deploy flows are exercised) — expected subdomain tool call count drops; first verify `http_root` now passes.
>
> Run `go test ./... -count=1 -short`. Green.
>
> Commit RED and GREEN on `friction-p2-subdomain`. Commit body explains the MCP contract preservation argument and cites `verify.go:35-36` annotations.

### A4 — Import pre-validation + hint surface (P2.3)

**Mission**: implement the pre-validation table seeded with the `object-storage + mode` rule, plus the post-hoc hint surface per §2 P2.3.

**Files authorized**: `internal/ops/import.go`, `internal/ops/import_test.go`, new `internal/ops/import_validation.go` and test.

**Must not touch**: API client, schema validation layer.

**Briefing**:

> You are implementing P2.3 of `plans/friction-root-causes.md`. Read §2 P2.3 fully.
>
> **Authoring policy is load-bearing**: pre-validation rules require empirical evidence (live API test or Zerops docs citation) inline as a comment above each rule. Wrong pre-rule = blocked valid config = broken feature. Post-hoc hints require only "this confusion has been observed" — wrong hints = confusing message, never broken feature.
>
> The seed rule has verified evidence: `object-storage + mode: NON_HA` and `object-storage + mode: HA` both return `projectImportInvalidParameter`; `object-storage` with no `mode` is accepted regardless of `objectStoragePolicy`. Record this in the comment above the rule.
>
> Order (branch `friction-p2-import-validate`):
>
> 1. RED: `internal/ops/import_validation_test.go` — table-driven: `object-storage + mode=NON_HA` → violation; `object-storage + mode=HA` → violation; `object-storage` without mode, policy=`private` → nil; `object-storage` without mode, policy=`public-read` → nil; `nodejs@22` with any mode → nil (rule doesn't apply).
>
> 2. RED: `internal/ops/import_test.go` integration — call `ops.Import` with violating YAML; assert `client.ImportServices` is NEVER invoked AND the returned error has `platform.ErrInvalidParameter` code AND the error message contains both `"object-storage"` and `"mode"`.
>
> 3. RED: `TestImport_PostHocHintsFoldedIntoError_EmptyTable` — with `importPostHocHints` empty, assert that API error responses have `Hints == nil` (the feature exists but emits nothing).
>
> 4. GREEN:
>    - Create `internal/ops/import_validation.go` with the two tables and `ValidateImportConstraints(parsed []ImportService) []string` returning violation messages.
>    - Extend `APIError` with `Hints []string`.
>    - Edit `import.go`: call `ValidateImportConstraints` after YAML parse, before `client.ImportServices`. On `projectImportInvalidParameter` API response, populate `Hints` by matching `importPostHocHints`.
>    - Include the authoring-policy header comment at the top of `import_validation.go`.
>
> Run `go test ./... -count=1 -short`. Green.
>
> Commit RED and GREEN on `friction-p2-import-validate`. Commit body explains the evidence chain for the seed rule.

### A5 — Atom and recipe content edits (P2.4)

**Mission**: per §2 P2.4 — add the git-init paragraph to the correct first-deploy atom, edit the dotnet recipe, wire the contract entry.

**Files authorized**: `internal/content/atoms/develop-first-deploy-execute.md` or `-intro.md` (pick whichever fires at code-write phase for container); recipe sync flow for `dotnet-hello-world`; `internal/workflow/atom_contract_test.go` (edit one entry).

**Must not touch**: ops/, tools/ (beyond contract test entry).

**Briefing**:

> You are implementing P2.4 of `plans/friction-root-causes.md`. Read §2 P2.4 fully.
>
> Wait for A2's contract framework to land on main.
>
> Order (branch `friction-p2-content`):
>
> 1. Identify the first-deploy atom that fires for the code-write phase in a container environment. Inspect `internal/content/atoms/develop-first-deploy-execute.md` first; if absent or not the right fit, fall back to `develop-first-deploy-intro.md`. The atom must be rendered during the phase when the agent writes initial code, before the first push.
>
> 2. Add this paragraph, preserving existing content:
>
>    > **Git** — don't run `git init` from the ZCP mount side (`/var/www/{hostname}/`). Push-dev initializes the repo inside the target container over SSH automatically. Pre-creating `.git` on the SSHFS mount breaks ownership for the target container and the first push fails.
>
> 3. In `atomContractTable` (`atom_contract_test.go`), replace the placeholder `<first-deploy-atom-chosen-by-A5>` with the chosen atom ID, and set `codeAnchor` to `"internal/ops/deploy_ssh.go#buildSSHCommand"`.
>
> 4. Run `go test ./internal/workflow/... ./internal/content/... -count=1`. The contract test for this entry must pass.
>
> 5. Recipe edit (separate upstream PR):
>    - `zcp sync pull recipes dotnet-hello-world`
>    - In the recipe's import YAML / zerops.yaml: change `deployFiles: - ./out` to `./out/~`. Change `run.start: dotnet ./out/app/App.dll` to `run.start: dotnet App.dll`.
>    - Add the note block (text in §2 P2.4) explaining extraction semantics.
>    - `zcp sync push recipes dotnet-hello-world --dry-run` to preview, then push for real.
>
> If recipe sync credentials (`STRAPI_API_TOKEN`) are unavailable, do NOT block the atom commit. File a tracking issue for the recipe update.
>
> Commit atom edit on `friction-p2-content`. Deliver: branch with one commit, recipe PR URL (or tracking issue URL).

### A6 — Adaptive guidance envelope (P3)

**Mission**: implement `ServiceMaturity` axis with last-verify semantics; migrate `develop-verify-matrix`; add pointer atom; extend `zerops_knowledge` with mode 5 per §2 P3.

**Files authorized**: `internal/workflow/envelope.go`, `compute_envelope.go`, `atom.go`, `synthesize.go`, `envelope_test.go`, `synthesize_test.go`, `corpus_coverage_test.go`, `brief_size_test.go` (create), `scenarios_test.go`, `internal/content/atoms/develop-verify-matrix.md`, new `develop-verify-matrix-pointer.md`, `internal/tools/knowledge.go`.

**Depends on**: A0, A1, A2 merged.

**Invariants**: KD-02/03, KD-04, KD-12, KD-16.

**Briefing**:

> You are implementing P3 of `plans/friction-root-causes.md`. Read §2 P3 fully.
>
> Target: edit-loop brief ≤ 14 KB (today ~18 KB). This is intentionally modest; a more aggressive target breaks content asserted by existing `MustContain` tests.
>
> Branch from main once A0 + A1 + A2 are merged: `friction-p3-maturity`.
>
> Order:
>
> 1. Read `docs/spec-knowledge-distribution.md` §3 (axes). You are adding a service-scoped axis; KD-16 conjunction semantics apply.
>
> 2. RED:
>    - `envelope_test.go` `TestServiceMaturity_FromLastVerify`: verify history `[{Success:true}]` → edit-loop; `[{Success:true},{Success:false}]` → first-run; `[]` → first-run.
>    - `synthesize_test.go` — axis filter tests for each maturity value, respecting conjunction with other service-scoped axes.
>    - `corpus_coverage_test.go` — expand the phase/environment matrix to include both maturity values.
>    - `brief_size_test.go` (new) — fixture envelope with three services all `MaturityEditLoop`, mode=dev, runtime=dynamic, strategy=push-dev, container; synthesize; assert byte length ≤ 14336. Second fixture with `MaturityFirstRun` same shape ≤ 20480 (sanity cap).
>    - New scenario file `internal/eval/scenarios/develop-verified-service-shorter-brief.md` with `maxBriefBytes: 14336` (field from W0).
>
> 3. GREEN:
>    - Add `ServiceMaturity` type + `MaturityFirstRun` / `MaturityEditLoop` constants to `envelope.go`.
>    - Add `Maturity` field to `ServiceSnapshot`.
>    - In `compute_envelope.go`, derive maturity from `ws.Verifies[hostname]` last entry.
>    - Parse `maturity:` in `atom.go::AxisVector`; filter in `synthesize.go::atomMatches` under service-scoped conjunction.
>    - Edit `develop-verify-matrix.md` frontmatter: add `maturity: [first-run]`.
>    - Create `develop-verify-matrix-pointer.md` with `maturity: [edit-loop]` and body pointing to `zerops_knowledge key="verify-matrix"`.
>    - Extend `internal/tools/knowledge.go` with mode 5 `key=<atom-id>`. Enforce existing mode exclusivity — `key` combined with any other mode field is rejected.
>    - Scenario snapshot updates for any develop-active scenario whose brief content changes.
>
> 4. Body audit of `develop-platform-rules-container.md` (1 161 B): split if clear common-operations vs deep-reference separation exists. If a clean split saves ≥ 500 B for edit-loop with no regression in `MustContain` asserts, migrate the deep-reference portion to `maturity: [first-run]`. Otherwise leave it; report the decision in the commit body.
>
> 5. Run `go test ./... -count=1 -short -race`. Green.
>
> STOP and report if the edit-loop brief still exceeds 14 KB after verify-matrix migration (indicates another atom needs attention), or if KD-16 conjunction makes any migrated atom fire in unintended service combinations (enumerate affected atom IDs).
>
> Commit RED and GREEN on `friction-p3-maturity` with before/after brief-size measurements in the GREEN commit body.

### A7 — Integration and release

**Mission**: merge branches in dependency order; run full eval; release.

**Files authorized**: any during merge conflict resolution; release tooling.

**Briefing**:

> You are coordinating merges for `plans/friction-root-causes.md`.
>
> Pre-merge gate for every branch:
>
> 1. `git pull --rebase origin main`.
> 2. `go test ./... -count=1 -race`. Green.
> 3. `make lint-local`. Green.
> 4. Scenario tests updated where behavior changed.
>
> Merge order (strict — each rebases the next):
>
> 1. `friction-w0-eval-size` (A0).
> 2. `friction-p1-honest-state` (A1).
> 3. `friction-p2-contract-scope` (A2) — rebase after W0 + P1.
> 4. `friction-p2-subdomain` (A3) — rebase after P2.5 framework merged.
> 5. `friction-p2-import-validate` (A4) — independent; merge any time after P1.
> 6. `friction-p2-content` (A5) — rebase after P2.5 framework.
> 7. `friction-p3-maturity` (A6) — rebase after all above.
>
> After all merged, run `zcp eval scenario --file internal/eval/scenarios/<id>.md` for all six new scenarios plus representative regression scenarios (`develop-first-deploy-branch`, `develop-add-endpoint`, `greenfield-nodejs-todo`, `greenfield-laravel-weather`). Every new scenario must pass; every regression scenario must continue to pass.
>
> Release: `git pull --rebase origin main`, then `make release` (minor bump).
>
> STOP and report if two branches conflict irreconcilably or if any eval scenario regresses unexpectedly.

---

## 5. Execution Order

- W0 and P1 run in parallel. P2.5+P2.1 starts once its own dependencies (none — it can start immediately) allow.
- P2.2, P2.3, P2.4 start once the P2.5 framework lands on main.
- P3 starts once W0, P1, and P2.5 are all merged.
- A7 coordinates serial merges and the release.

Estimated wall-clock: five to seven development days.

---

## 6. Verification — "Done" Definition

**Automated gates**:
1. `go test ./... -count=1 -race`: green.
2. `make lint-local`: green.
3. `TestAtomContract` green with at least three entries (P2.1 scope, P2.2 subdomain absence, P2.4 git-init).
4. `TestDeployPostMessageContract` green — zero runtime-class branches in deploy post-message code.
5. `TestBriefSize` green — first-run ≤ 20 KB, edit-loop ≤ 14 KB.
6. `TestFetchBuildWarnings_FiltersStaleByPipelineStart` green.

**Scenario evaluation** (run in the ZCP container via `zcp eval scenario --file ...`):

| Scenario | Expected outcome |
|---|---|
| `develop-scope-stage-accepted` | `scope=["appdev","appstage"]` accepted; no `"non-deployable hostnames"` in output |
| `deploy-honest-runtime-state` | No `"NOT running"` / `"idle start"` in any tool call result |
| `verify-auto-enables-subdomain` | Zero `zerops_subdomain` tool calls; first verify passes `http_root` |
| `import-cross-field-validate` | `object-storage + mode` rejected pre-flight; error message contains `"object-storage"` and `"mode"` |
| `deploy-warnings-fresh-only` | Second deploy's `BuildLogs` contains none of the first deploy's warnings |
| `develop-verified-service-shorter-brief` | Second `zerops_workflow action="start"` body ≤ 14 336 bytes |

**Regression**: every existing eval scenario passes unchanged except for deliberate snapshot updates documented in commits.

---

## 7. Cross-Cutting Concerns

### Commit hygiene

Each agent splits work into logical commits (RED → GREEN → refactor if needed). Commit bodies explain WHY with references to spec sections, invariants, and root causes. No `Co-Authored-By` trailers.

### Atomic consistency

Every commit compiles, runs, passes tests, and makes sense on its own. No intermediate broken states. When a planned change would require one, agents stop and report rather than ship an incoherent intermediate.

### Single path

No dual paths gated by feature flags or "legacy vs new" branches. Migrations are hard-cuts. Existing callers of modified interfaces are updated or removed in the same commit as the interface change.

### MCP annotation contract

`ReadOnlyHint`, `IdempotentHint` are honored as protocol contracts. If a feature requires mutation, it belongs in a tool without these annotations (P2.2 places subdomain mutation in deploy, not verify, for this reason).

### Rule authoring discipline

Any addition to `importPreValidationRules` requires empirical evidence inline (live test result or cited docs). Any `atomContractTable` entry rewording reflects a real behavior change, not a rename to make tests pass.

---

## 8. Rollback Strategy

Per-workstream non-squash merges preserve clean revert ranges. If post-merge eval regresses:

| Workstream | Rollback impact |
|---|---|
| W0 | Size fields unused (defaults); revert is cosmetic |
| P1.1 | False `"NOT running"` messages return |
| P1.2 | Stale warnings return |
| P2.1 | Stage hostnames rejected in scope; atom promise briefly stale |
| P2.2 | `develop-first-deploy-verify.md` restored; agent re-instructed to call subdomain manually |
| P2.3 | Pre-validation off; raw `projectImportInvalidParameter` returned; post-hoc hints empty |
| P2.4 | Git-init advice absent; dotnet recipe reverts |
| P3 | `ServiceMaturity` field retained but atoms un-gated; brief returns to ~18 KB |

Each merge commit retains its full range for trivial `git revert`.

---

## 9. Evidence Index (file:line and measured baselines)

### Code anchors

- `internal/tools/workflow.go:653-657` — bootstrap `plan` rejection at `action="start"`.
- `internal/tools/workflow_develop.go:73-82` — `runtimeMetas` construction filter.
- `internal/tools/workflow_develop.go:194-201` — scope rejection error text.
- `internal/tools/deploy_poll.go:42-50` — post-deploy message branching.
- `internal/ops/deploy_validate.go:378-387` — `NeedsManualStart` definition.
- `internal/ops/deploy_validate.go:41` — `IsImplicitWebServerType` consumer (`run.start` validation).
- `internal/tools/workflow_checks_generate.go:103` — `IsImplicitWebServerType` consumer (port/healthCheck validation).
- `internal/ops/verify.go:35-36` — `ReadOnlyHint: true, IdempotentHint: true` annotations.
- `internal/ops/verify.go:151-167` — subdomain-off `CheckSkip`.
- `internal/content/atoms/develop-first-deploy-verify.md:11-13` — manual subdomain instruction.
- `internal/content/atoms/develop-first-deploy-promote-stage.md:14` — "both halves in scope" promise.
- `internal/ops/build_logs.go:29-57` — `FetchBuildWarnings` implementation.
- `internal/platform/types.go:167` — `LogFetchParams.Since` field.
- `internal/platform/types.go:197-215` — `AppVersionEvent` + `BuildInfo` with `PipelineStart` field.
- `internal/platform/logfetcher.go:134-144` — client-side `Since` filter.
- `internal/platform/zerops_search.go:64-69` — `APIError` struct (Code + Message only).
- `internal/ops/deploy_ssh.go:164-198` — `buildSSHCommand` with auto `git init`.
- `internal/workflow/envelope.go:15-92` — `StateEnvelope` definition.
- `internal/workflow/compute_envelope.go:205-214` — virtual-stage-hostname precedent (`buildServiceSnapshots`).
- `internal/workflow/service_meta.go:268-287` — `findMetaForHostname` dual-hostname resolver.
- `internal/workflow/bootstrap_outputs.go:29-61` — single meta file per container+standard pair.
- `internal/eval/scenario.go:35-46` — `Expectation` schema.
- `internal/eval/grade.go:140-187` — current pattern-based checks.
- `internal/tools/knowledge.go:115-120` — mode exclusivity validation.

### Measured baselines

- 72 atoms in `internal/content/atoms/*.md` total; `develop-*` subset 1350 lines.
- Representative develop-active (container, dev, dynamic, push-dev, deployed) envelope: 17 atoms match, 17 059 bytes of atom body, ~18 KB rendered.
- Heaviest atom: `develop-verify-matrix.md` — 3 399 B / 84 lines.
- Edit-loop realistic floor after migrating `develop-verify-matrix` only: ~14.5 KB. Target: ≤ 14 KB.
- Load-bearing atoms for edit-loop (not migratable): `develop-http-diagnostic` (1 588 B), `develop-env-var-channels` (1 132 B), `develop-platform-rules-common` (1 247 B) — all assert-referenced in `corpus_coverage_test.go:207-209`.

### API behavior

- `object-storage + mode: NON_HA` → `projectImportInvalidParameter` (no field info).
- `object-storage + mode: HA` → `projectImportInvalidParameter` (no field info).
- `object-storage` with no `mode`, any `objectStoragePolicy` in {`private`, `public-read`, `public-objects-read`, `public-write`, `public-read-write`} → accepted.
- The Zerops log API does not support server-side `since=` filter; client-side filter exists at `logfetcher.go:134-144`.
- Build service-stack (`event.Build.ServiceStackID`) is persistent per target service — logs accumulate across builds until time-filtered.

---

## 10. Open Questions

1. **P3 secondary migration** — A6 audits `develop-platform-rules-container.md` for a common-ops vs deep-reference split. If the body supports a clean split yielding ≥ 500 B edit-loop savings, migrate the deep-reference portion to `maturity: [first-run]`. If not, leave it. A6 decides based on body audit and reports the decision.

2. **Recipe upstream PR credentials** — A5 requires `STRAPI_API_TOKEN` for `zcp sync push`. If unavailable, A5 ships the atom commit and files a tracking issue for the recipe update.

3. **Eval cadence during merges** — run eval after every workstream merge, or batch at A7? Trade-off: per-merge catches regressions earlier; batch at A7 minimizes eval time. A7 defaults to batch unless otherwise instructed.

4. **`zerops_knowledge` mode 5 parameter name** — `key=` is the working name. Alternatives: `atomId=`, `atom=`. The parameter is a stable atom identifier; `key=` aligns with generic knowledge-lookup framing.

5. **Tracked for future plans** — extend the atom axis model with runtime-type granularity (resolves the runtime-specific-quirks gap currently absorbed by recipe coverage only); full atom-corpus audit to populate `atomContractTable` beyond the three seed entries; brief-on-demand architecture for edit-loop below 8 KB.
