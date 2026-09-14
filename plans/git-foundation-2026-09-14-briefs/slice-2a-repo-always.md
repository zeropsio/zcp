# Slice 2a — repo-always (additive; no deploy-behaviour change)

Base: branch `feat/git-foundation` AFTER slice 1 (package `internal/ops/git/` with a
command-runner abstraction exists — reuse it, do not create a second runner).
Read CLAUDE.md, `plans/git-foundation-2026-09-14.md` §2.1 `Repo`, §6.1 G1/G2, §6.3.

## Behaviour
1. Every dev service (mounted working tree at `/var/www`) is a git repository once
   bootstrap / adopt finishes. Git is present on every runtime (verified).
   - bootstrap: after the scaffold lands, `git init -b main` + seed
     `.git/info/exclude` (node_modules, dist/build outputs by runtime class, `.env`,
     `*.log`, `.zcp/`) + `git add -A && git commit -m "scaffold"`. zcp NEVER writes a
     `.gitignore`. Where: bootstrap is guide/agent-driven — add a StepChecker
     (`internal/workflow/bootstrap_checks.go:73`) that fails the finalize step until
     `git -C /var/www rev-parse HEAD` succeeds over SSH, and an atom
     `bootstrap-repo-init` telling the agent the exact commands. Container only;
     local mode: the checker passes when CWD is a repo, else the atom says `git init`.
   - adopt (`internal/workflow/adopt_local.go:50`, `adopt.go`): if `/var/www` is not
     a repo: `git init -b main`, exclude seeding, `git add -A`, commit `"baseline:
     adopted appVersion <id>"`, tag `zcp/baseline/<appVersionId>`; if it is a repo,
     only tag HEAD. Marker in `ServiceMeta` (`internal/workflow/service_meta.go`):
     `Repo{BaselineAppVersion string, Provenance "source"|"artifact-only"}` —
     `artifact-only` when the running appVersion has a sourceService / deployFiles
     narrower than `.` (built elsewhere), else `source`. Executed by zcp over
     `SSHDeployer.ExecSSH` (not agent-driven), errors via `withSSHStderr`.
2. `zerops_workflow action=status` envelope exposes `repo: {present, head, baseline}`
   per dev service (read live via `git rev-parse`, cached nowhere).
3. Nothing about deploy changes in this slice (slice 2b owns the self-deploy gate).

## Tests (RED first)
- `internal/ops/git/repo_test.go`: init/exclude/commit/tag command sequences via the
  fake runner; exclude content per runtime class; idempotent on an existing repo.
- adopt tests: provenance classification table (sourceService set / deployFiles `.` /
  narrower); tag name; meta fields persisted.
- bootstrap checker test: fails without HEAD, passes with.
- envelope test: `repo` block present for dev services, absent for managed.
- `go test ./... -short`, `make lint-fast`, `make lint-local`.

## Eval scenarios (write, not run)
`repo-always-bootstrap.md` (G1) and `repo-always-adopt-baseline.md` (G2) per plan §6.1,
same frontmatter shape as slice 1's `deploy-from-commit-stage.md`; add rows to
`docs/spec-scenarios.md §9.3` table G with `promote: containerCheck`; keep
`TestEvalMatrix_*` green.

## Spec
`docs/spec-workflows.md` new §4.6 "Repo is always present" (init/clone/adopt rules,
exclude-not-gitignore, baseline tag, provenance). Update `spec-mate.md §6.1` one line:
zcp guarantees the repo exists; mate never scans for it.

Atomic commits, no Co-Authored-By. Report commits, test tail, blockers.
