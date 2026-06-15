# Verification Verdict: oss-recipe-port-flow-2026-06-09.md

**Date**: 2026-06-09
**Method**: 3 parallel codebase investigations (code-reference claims, schema/platform
claims, architecture-fit claims) + codex validation pass over all contested findings.
**Subject**: `plans/oss-recipe-port-flow-2026-06-09.md` (autonomous OSS port flow).

---

## Verdict

The plan is unusually well-grounded. Nearly every file:line citation checks out exactly,
and the three platform-feasibility claims that drive the ambition level are all
**confirmed**. The verification surfaced **two compile-level breaks, three design
omissions, and one impossible fix-class action** that need correcting before Phase 0.
None are fatal — the feasibility thesis survives intact.

---

## Confirmed (load-bearing claims hold)

- `build_plan.go:41-46` fall-through for export/launch-production is exact; a
  `PhasePortActive` slots into the same case.
- All seven `topology.FailureClass` values match
  ({build, start, verify, network, config, credential, other}).
- `EmitDeliverableYAML` (`yaml_emitter.go:67`), `AssembleRootREADME` (`assemble.go:64`),
  `writeRuntimeBuildFromGit` hardcode (`yaml_emitter.go:460-461`, no override mechanism
  exists today), `CloseReasonIterationCap`, `Deploys[]` cap = 10, per-PID
  `.zcp/state/work/{pid}.json` sidecar convention — all verified.
- All ~14 reused `internal/ops` functions exist (Import, EnvSet, DeploySSH, PollBuild,
  PollProcess, FetchBuildLogs, FetchRuntimeLogs, ExecSSH, Start, Restart, Verify,
  VerifyAll, Scale, Subdomain).
- **Managed catalog**: clickhouse@25.3 and kafka@{3.8,3.9} ARE managed (plus
  postgresql@{14,16,17,18}, valkey, keydb, elasticsearch, meilisearch, qdrant,
  typesense, nats, rabbitmq@3.9, object-storage, shared-storage). PostHog's deps all
  have managed equivalents.
- **Image-only BAIL line**: import schema has zero image/registry/OCI property —
  exhaustive grep, no matches. Only `buildFromGit` / `zeropsSetup` / `zeropsYaml`.
- **No cross-service readiness primitive**: only `priority` (creation order) +
  per-service `readinessCheck`/`healthCheck`; nothing in either schema or the knowledge
  corpus (no dependsOn/waitFor/requires). Open question 3's "current evidence: no" is
  correct.
- `zsc execOnce ${appVersionId} --retryUntilSuccessful` is the documented
  migration pattern across multiple recipes.
- The "agent-driven loop, not engine coroutine" characterization of develop/recipe is
  accurate (handler computes state + guidance, agent calls next tool, handler records
  and re-derives).
- `zcp sync recipe create-repo/publish/export` are non-interactive and scriptable.

---

## Breaks (would not compile as planned)

1. **FitCeiling builder can't live in `internal/workflow/`.** FitCeiling projects up
   the `tiers.go` ladder, but `Tiers()`/`TierAt()` live in `internal/recipe`, which
   depguard (`.golangci.yaml:131-137`) + `architecture_test.go:72` forbid workflow from
   importing. Fix: promote the ladder to `topology` (CLAUDE.md promotion rule) or move
   the builder to the tools layer.

2. **`substituteFragmentMarkers` is unexported** (`assemble.go:619`, lowercase, no
   external call sites). Reusing it from outside `internal/recipe` requires an export
   change the plan doesn't list. Related: all Stage B emit calls must live in
   `internal/tools/` (tools→recipe is permitted but has no production precedent today),
   and `EmitDeliverableYAML` takes a full `*recipe.Plan` — the PortSession→Plan
   conversion is unspecified.

---

## Design omissions

3. **"No new storage" is false as written** (§4). The fix-class table dispatches on
   signal IDs (`build:command-not-found`, `init:db-connection-refused`), but
   `DeployAttempt` (`work_session.go:76-82`) persists only the coarse `FailureClass`
   category — `Signals []string` lives on the live
   `ops.DeployResult.FailureClassification` and is never written to `Deploys[]`. Needs
   either a new persisted field or per-turn threading from the handler.

4. **The import-override gate is unmentioned and sits in the loop's path.**
   Deploy-level fixes (zerops.yaml) are ungated. But any import.yaml fix to an existing
   hostname (resources, type version, mounts, `startWithoutCode`) requires
   `override=true`, and `gateOverrideOnFailedHistory` (`internal/tools/import.go:111-216`)
   fires `ErrDiagnosisRequired` from iteration 2 onward — exactly the port loop's state.
   The R6 retryCall makes it a two-call dance, not a deadlock, but each gated import
   burns an iteration under tight caps (EASY = 4), and override **wipes the prior
   container/env state** (full redeploy + env re-set). The fix-class table and
   iteration budget must account for this.

5. **Open question 5 understates real work.** `PhasePortActive` in the fall-through
   means `action=status` returns an empty Plan — launch-production needed a dedicated
   `launch_status_recovery.go` for exactly this; port needs its own to satisfy P4.
   EngineVersion stamping is a recipe-engine mechanism (`Plan.EngineVersion`,
   `gate_engine_version_stamped.go`); `WorkSession.Version` is a schema version. Port
   builds a NEW mechanism, not a reuse. "Likely yes" → "yes, and it's net-new code."

---

## Impossible as written

6. **"build + oom-killed → raise build RAM" has no mechanism.** Neither schema exposes
   build-container resources; `internal/schema/validate_test.go:47` explicitly pins
   `build.verticalAutoscaling` as unsupported; no zsc/atom/knowledge mechanism exists.
   This fix-class row must become an escalation trigger (T1 source→prebuilt) or a bail
   reason, not a fix.

---

## Minor factual corrections

- The signal library has **34** signals (`deploy_failure_signals.go:46-331`), not 25.
- **B2 org naming inconsistent**: `zcp sync recipe create-repo` is config-pinned to
  `zerops-recipe-apps/{slug}[-suffix]` via `.sync.yaml`, while §2/§7 imply
  `zeropsio/recipe-<slug>`. It's a CLI subcommand using ambient `gh auth` — not an MCP
  tool — so the autonomous agent reaches it only via shell. Open question 1 is real and
  slightly bigger than stated.
- **D6 is half-kept**: the software-shape plan's D6 specifies the GlueRepo override at
  *two* sites — `writeRuntimeBuildFromGit` AND the `ServiceKindUtility` branch
  (`yaml_emitter.go:413-415`, today emits only `zeropsSetup`; described as the canonical
  micro-model for a software recipe's primary service). The port plan's "one real
  emitter change" covers only the runtime site; the utility branch needs a NEW
  buildFromGit emit path — arguably the more important one for this shape.
- **Tier semantics drift**: `tiers.go` is a deployment-topology ladder (AI Agent →
  Remote → Local → Stage → Small Prod → HA Prod); nothing in Tier 4 means "persists
  across redeploy." The plan's C-check→tier mapping is a new semantic layer it invents —
  workable, but should be stated as such.
- `services[].mode` is **deprecated** in the live import schema ("Deprecated, use Type
  version only"), though ZCP's export composer still emits it (`bundle/export.go:133`,
  `bundle/launch.go:193`). The plan inherits a deprecated field.

---

## Works better than the plan knows

The A2 subdomain step works **by accident**: `zerops_import` writes no ServiceMeta, and
`serviceEligibleForSubdomain` is deliberately permissive for meta-less services (pinned
by `deploy_subdomain_test.go:128`), so raw-import + standard deploy gets auto-enable.
But the plan never decides whether port writes ServiceMetas — if it does, with any
non-allowlisted Mode, `modeAllowsSubdomain` defaults false and auto-enable silently
skips. Make this decision explicit in Phase 0.

---

## Bottom line

Architecture story (peer workflow, agent-driven loop, fall-through phase slot,
work-session sidecar, topology-resident failure types): **verified sound**. The
EASY/HARD band analysis including the PostHog ceiling is well-supported by schema
evidence. Required amendments before build: §3 + §4 (signal persistence, import-override
gate budgeting), §7 (FitCeiling placement, `substituteFragmentMarkers` export, D6 second
site), §8 Phase 0/4 (ServiceMeta/subdomain decision, status recovery + version stamping
scope, sync org/surface), fix-class table (drop the build-RAM row).
