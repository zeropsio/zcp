# Plan: Export workflow — single-atom task list

**Status**: Implemented (atom written, old atoms deleted, tests green).

**Goal**: Let an LLM turn any deployed service into a re-importable git repo
(`import.yaml` pushed alongside existing code + `zerops.yaml`).

---

## Design decisions

1. **Stateless immediate workflow** — same wrapper as today (`workflow="export"`
   synthesizes atoms, returns guidance, no session). No cursor file, no
   envelope state axes, no service parameter plumbing.

2. **One atom** — `internal/content/atoms/export.md`. The flow is linear
   (fetch → transform → write → push → report) with no conditional branches
   that would justify multiple atom files. Splitting was pattern-matching to
   develop/bootstrap, which have real branches; export does not.

3. **Task-checklist format** — the atom opens with a numbered task list the
   LLM copies into its task tracker, then executes task by task. Each task
   section is the specific procedure: commands as code blocks, decision
   rules as tables, no narrative.

4. **Delegate all non-export-specific concerns**:
   - Deploy strategy switch → `zerops_workflow action="strategy"` (existing)
   - `GIT_TOKEN` missing → `zerops_deploy strategy=git-push` returns
     structured `gitPushPrerequisites` with full setup instructions
   - `FirstDeployedAt` missing → same tool returns
     `PREREQUISITE_MISSING` with "run develop first" text
   - Repo URL → `ssh {host} "cd /var/www && git remote get-url origin"`
     or user prompt
   
   Export atom references these failure modes as error-recovery branches in
   task 10 (push). Export doesn't pre-check any of them.

5. **Trust `buildFromGit` semantics** — Zerops pulls code + `zerops.yaml`
   from the repo at deploy. `zeropsSetup` picks the setup block. So import.yaml
   services need only: `type`, `zeropsSetup`, `buildFromGit`, platform
   settings (scaling, subdomain, envs). **Do NOT copy build/deploy/run
   fields** from zerops.yaml into import.yaml.

6. **Trust platform export at service level** — it emits `envSecrets:` only
   for user-set envs (platform-injected keys are omitted). Keep what's
   there. Scrub only at project level (drop `ZCP_*`, test fixtures, etc.).

---

## Platform export gaps the atom compensates for

From live verification against `opus/appdev` (Laravel, imported via
`laravel-minimal.import.yml`):

| Gap | Compensation |
|---|---|
| `enableSubdomainAccess` stripped | Reinstate from `discover.subdomainEnabled` |
| `priority` stripped on managed services | Reinstate as `priority: 10` |
| `zeropsSetup` never emitted | Add via hostname-suffix mapping (dev/prod/worker) or SSH probe of zerops.yaml |
| `buildFromGit` never emitted | Add from SSH `git remote get-url origin` |
| `verticalAutoscaling` dumps corePackage defaults | Scrub fields matching LIGHT defaults (`minCpu: 1`, `maxCpu: 8`, `maxRam: 48`, etc.) |
| Project `envIsolation`, `sshIsolation`, `sharedIpv4` emitted | Drop (platform defaults) |
| `corePackage: LIGHT` emitted | Drop (default). Keep `SERIOUS`. |
| `ZCP_API_KEY`, test vars in `project.envVariables` | Drop via `ZCP_*`/`TEST_*`/`PROBE_*`/`VERIFY_*` rules |
| Literal random secrets instead of preprocessor directives | Replace via known-secret-key table (`APP_KEY`, `SECRET_KEY_BASE`, etc.) |

`ZCP_API_KEY` leaking into the platform export is **a separate security
concern** — the ZCP-internal API key shouldn't surface in exports. Atom
mitigates by instructing LLM to drop. File separately.

---

## zerops_deploy for export — cheat sheet

Export uses only `strategy="git-push"` (handler: `tools/deploy_git_push.go:42-153`).

**Inputs relevant to export**:
- `targetService` (required) — hostname to push from
- `strategy="git-push"` (required)
- `remoteUrl` (required first push; optional after — persists in
  container's `.git/config`)
- `branch` (default `main`)

**Pre-flight gates** (tool enforces, atom references as recovery paths):
- `meta.IsDeployed()` false → `PREREQUISITE_MISSING`
- `GIT_TOKEN` missing on container → structured `GIT_TOKEN_MISSING` response

**What the tool does on the container**:
```
trap 'rm -f ~/.netrc' EXIT
  && umask 077 && echo "machine <host> login oauth2 password $GIT_TOKEN" > ~/.netrc
  && chmod 600 ~/.netrc
  && cd /var/www
  && (test -d .git || git init -q -b main)
  && git config user.email ... && git config user.name ...
  && (git remote add origin <url> 2>/dev/null || git remote set-url origin <url>)
  && (git rev-parse HEAD >/dev/null 2>&1 || (git add -A && git commit -q -m 'initial commit'))
  && git push -u origin main
```

LLM commits **before** calling. No manual `git init` / `git config` /
`git remote add` needed.

---

## Changes applied

- **Added**: `internal/content/atoms/export.md` — single atom, 11 tasks,
  ~190 lines of markdown
- **Deleted**: `export-01-intro.md`, `export-02-discover.md`,
  `export-05-prepare-init.md`, `export-06-zerops-yaml.md`,
  `export-07-generate-import.md`, `export-09-close.md`
- **No Go code changed** — `workflow="export"` stays stateless immediate
- **No envelope axes added** — `PhaseExportActive` with no new axes
- **Placeholders used**: `{targetHostname}`, `{repoUrl}` (already in
  `allowedSurvivingPlaceholders` whitelist in `synthesize.go:197-220`)
- **Tests green**: `go test ./... -count=1 -short` all passing, including
  `TestCorpusCoverage_RoundTrip/export_active` and
  `TestCorpusCoverage_CompactionSafe/export_active` which verify the atom
  body contains `buildFromGit`, `zerops_export`, `import.yaml`

---

## E2E validation plan (when ready to test live)

Against `opus/appdev` (Laravel, existing):

1. Call `zerops_workflow action="start" workflow="export"` from an MCP-connected
   session. Confirm guidance matches the expected task list.
2. Walk the 11 tasks manually:
   - Task 1: both tool calls succeed, return expected data
   - Task 2: SSH returns `https://github.com/zerops-recipe-apps/laravel-minimal-app`
   - Task 3: filter keeps `appdev`, `appstage`, `db`; drops unrelated
     services in opus project
   - Task 4: appdev gets `zeropsSetup: dev`, appstage gets `zeropsSetup: prod`,
     both get `enableSubdomainAccess: true`
   - Task 5: db gets `priority: 10`
   - Task 6: scaling scrubbed to just `minRam` per service
   - Task 7: `APP_KEY` → `<@generateRandomString(<32>)>`; `ZCP_API_KEY` dropped;
     `PROBE_TEST` / `VERIFY_FIX` flagged for user confirmation
   - Task 8: `#zeropsPreprocessor=on` prepended (because APP_KEY directive
     emitted)
   - Task 9: SSH heredoc writes `/var/www/import.yaml`, commit succeeds
   - Task 10: `zerops_deploy strategy=git-push` — if GIT_TOKEN missing (current
     state in opus), recovery loop: set token via `zerops_env`, retry
   - Task 11: success message includes repo URL + re-import command
3. Compare generated `import.yaml` with
   `internal/knowledge/recipes/laravel-minimal.import.yml` —
   should be functionally identical (differences only in `project.name`).
4. Round-trip: run `zcli project project-import <generated>` on a fresh
   project; verify services come up in equivalent configuration.

---

## Out of scope

- Multi-service "bundle export" — atom naturally handles dev+stage pair of
  same repo (task 3 keeps both, task 4 enriches both with the same
  `buildFromGit`).
- Automated repo creation — user creates the repo; atom assumes URL provided.
- `ServiceMeta.GitRemoteURL` field — SSH probe on each call is sufficient.
- Go-side env classification / YAML enrichment — LLM does it from atom
  rules.
- `FirstDeployedAt` on adopted services — existing gap (adopt workflow
  doesn't stamp it), file separately. Atom's task 10 recovery handles the
  symptom.
