# Cycle 3 deferred follow-ups

Date: 2026-04-27

Items surfaced during cycle 3 hygiene work that are out of scope and tracked here for future cycles.

## DF-1: Author `develop-push-git-deploy-local.md` + tighten existing axis

**Source**: cycle 3 Phase 4 Axis N CORPUS-SCAN; per-edit Codex round 1 (`aef2a81baf1a7eefa`).

**Issue**: `internal/content/atoms/develop-push-git-deploy.md` is universal (no `environments:` axis) but body is container-shaped throughout (`ssh ... cd /var/www && git ...`, project `GIT_TOKEN`). Currently fires on local-env push-git develop-active envelopes with WRONG content (local-env push-git uses user's git credentials per `strategy-push-git-push-local.md`, no SSH, no project `GIT_TOKEN`).

**Why deferred**: tightening axis to `environments: [container]` would break test pins (`internal/workflow/scenarios_test.go:810, 948`; `internal/workflow/corpus_coverage_test.go:624-639` has `MustContain: ["git-push", "GIT_TOKEN"]` that only `develop-push-git-deploy` currently supplies). Proper fix requires authoring a new local-env atom — content-authoring work, outside cycle 3's "5 content findings" scope.

**Proper fix**:
1. Author `internal/content/atoms/develop-push-git-deploy-local.md` covering local-env push-git deploy guidance (user's git creds; no SSH; no project `GIT_TOKEN`). Cross-reference `strategy-push-git-push-local.md` for the credential model.
2. Tighten `develop-push-git-deploy.md` axis to `environments: [container]`.
3. Update test panels: `corpus_coverage_test.go` `develop_local_push_git` fixture's `MustContain` should be drawn from the new local-env atom; `scenarios_test.go:948` bulk-pin union should include the new atom too.

**Risk if not fixed**: agents on local-env push-git develop-active envelopes continue to receive container-shaped guidance. They have to recognise the mismatch from the SSH command pattern and switch to local git credentials. Status quo from before cycle 3; not introduced by cycle 3.

## DF-2: Engine work (cross-references)

Engine improvements remain tracked in `plans/engine-atom-rendering-improvements-2026-04-27.md` — multi-service single-render atom support, `zerops_deploy` error-response enrichment, auto-handle orphan metas, Axis K/L/M/N lint enforcement in `internal/content/atoms_lint.go`.

These are NOT cycle-3 deferrals; they are pre-existing engine work that complements but doesn't gate cycle 3.
