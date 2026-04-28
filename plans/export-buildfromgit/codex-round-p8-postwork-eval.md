# Verdict

SHIP-WITH-NOTES for the handler fix and Phase 8 minimal dev-variant path; strict Phase 8 EXIT is not fully satisfied as written. The code now gathers all services before choosing the runtime, and the integration test pins managed-db inclusion. However, the plan requires dev and stage live runs, healthy re-import, committed run logs, and Codex log review before Phase 8 EXIT (`plans/export-buildfromgit-2026-04-28.md:713-717`). This audit could not independently read the requested r3 log: `ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null zcp cat ...` failed because host `zcp` did not resolve, and no local `2026-04-28-231505` log copy was found. Therefore no r3 tool-call counts, state sequence, or agent-reported issues are asserted from the log here.

# Managed-deps fix correctness

Correct. `ops.Discover` is now called with an empty hostname filter (`""`) at `internal/tools/workflow_export.go:70-78`, with comments stating the purpose is to expose all services to managed-service collection (`internal/tools/workflow_export.go:70-76`). The previous `discover.Services[0]` assumption is replaced by an in-memory loop over `discover.Services` that matches `s.Hostname == input.TargetService`, sets `svc`, and breaks on match (`internal/tools/workflow_export.go:81-89`); not found now returns `ErrServiceNotFound` (`internal/tools/workflow_export.go:90-95`). This is robust to Discover ordering because target selection no longer depends on list position.

The collector now receives the full Discover result at `internal/tools/workflow_export.go:168`, and `BundleInputs.ManagedServices` carries that output into bundle composition (`internal/tools/workflow_export.go:170-181`). `collectManagedServices` iterates all discovered services, keeps only `IsInfrastructure`, excludes the target hostname, and maps hostname/type/mode into `ops.ManagedServiceEntry` (`internal/tools/workflow_export_probe.go:251-277`). Bundle composition appends each managed service with `hostname`, `type`, and `priority: 10`, preserving non-empty mode (`internal/ops/export_bundle.go:250-261`). The only future fragility is upstream: if a future `ops.Discover` stops returning infrastructure services even with no hostname filter, this path would again have nothing to collect.

The integration assertion is present. The mock project includes runtime `appdev` plus managed `db` (`integration/export_test.go:76-104`), then asserts the publish-ready `importYaml` contains `hostname: db` and `priority: 10` (`integration/export_test.go:206-216`).

# EVAL REPORT findings adjudication

The r2 findings described in the task are plausible and should be documented as UX follow-ups, not Phase 8 handler blockers. The spec says develop flow is mandatory for code/config changes on runtime services (`docs/spec-workflows.md:719-762`), while `export-active` is stateless and returns without touching session state (`docs/spec-workflows.md:1214`). So an export validation-failed response that says to fix source without naming the develop workflow leaves an important workflow switch implicit. Recommendation: enhance validation-failed guidance to point agents to `workflow=develop` when fixing mounted runtime files is required.

The wasted `zerops_mount` call in export workflow is likewise a guidance/tool UX issue. Mounting is described as bootstrap/adoption infrastructure behavior (`docs/spec-workflows.md:461-490`), while tools generally work independently and workflows add structure rather than global gates (`docs/spec-workflows.md:141-145`). If `zerops_mount` returns `WORKFLOW_REQUIRED` during export, export atoms or error recovery should say which workflow owns mount access.

# Phase 8 scope cuts

Phase 8 executed a reduced scope compared with the plan. The plan explicitly calls for a dev variant run (`plans/export-buildfromgit-2026-04-28.md:691-694`), exported repo inspection (`plans/export-buildfromgit-2026-04-28.md:695-700`), re-import into a fresh project and health verification (`plans/export-buildfromgit-2026-04-28.md:701-706`), repeat with stage variant (`plans/export-buildfromgit-2026-04-28.md:709`), and EXIT requiring two runs plus healthy re-import (`plans/export-buildfromgit-2026-04-28.md:713-715`). Given the managed-deps bug was found and fixed in the dev path, accepting the cut is reasonable only as SHIP-WITH-NOTES with explicit tracker amendments; it is not a clean Phase 8 EXIT under §6.

# Container state hygiene

The task states Phase 8 replaced `/home/zerops/.local/bin/zcp` with a backup at `/tmp/zcp-prev-backup` on the `zcp` container, but this audit could not verify container state because the `zcp` SSH alias did not resolve. Treat this as an operational note that must be recorded in the tracker. A restore step should be either completed before Phase 8 close or intentionally waived with the exact binary path and backup path, because Phase 8 cleanup is part of the live run scope (`plans/export-buildfromgit-2026-04-28.md:710`).

# Fan-out completeness

Incomplete relative to §7. The collaboration protocol says Phase 8 POST-WORK can fan out to one dev variant log reviewer, one stage variant log reviewer, and one re-import behavior reviewer (`plans/export-buildfromgit-2026-04-28.md:812-813`). This report covers only the dev-variant review slot in intent, and even that slot lacks direct r3 log evidence because the remote log was inaccessible here. The stage and re-import slots should be documented as deferred if the reduced Phase 8 scope is accepted.

# Ship-readiness

Ready to proceed to Phase 9 docs with notes, not ready for a clean Phase 10 SHIP claim. Phase 9 entry says Phase 8 EXIT must be satisfied (`plans/export-buildfromgit-2026-04-28.md:722-724`), but the plan’s acceptance gate G5 still requires eval-zcp success for both dev and stage variants plus healthy re-import (`plans/export-buildfromgit-2026-04-28.md:815-825`). Phase 10 can still land as SHIP-WITH-NOTES later because the plan allows noted ship outcomes for documented limitations (`plans/export-buildfromgit-2026-04-28.md:780-784`), but the stage/re-import cuts must be explicit.

# Recommended amendments

1. Amend the Phase 8 tracker to record the reduced scope: dev variant only, no stage variant, no fresh-project re-import, and remote r3 log path inaccessible to this Codex review.
2. Add a follow-up item to validation-failed export guidance: when fixing live `zerops.yaml` or runtime files, switch to develop flow, because develop owns code/config changes (`docs/spec-workflows.md:719-762`).
3. Add a follow-up item for `zerops_mount` UX during export: either defer the call in guidance or return a clearer workflow handoff.
4. Record or restore `/home/zerops/.local/bin/zcp` on the eval container; if intentionally left replaced, document `/tmp/zcp-prev-backup` as the restore source.
5. Before Phase 10, run or explicitly waive the stage-variant and re-import fan-out reviews, since §6/G5 still name them as acceptance criteria (`plans/export-buildfromgit-2026-04-28.md:713-717`, `plans/export-buildfromgit-2026-04-28.md:821`).
