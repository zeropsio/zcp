# Git Service-Lifecycle Fix — Implementation Plan

> **Scope**: Fix one concrete user-facing bug (mount-side `git init` poisons
> `.git/` and breaks subsequent deploys), plus the structural/content root
> causes that let this bug be reachable at all. Narrow, verified, shipped
> as one branch. Supersedes P2.4 of `plans/friction-root-causes.md` with a
> sharper, live-verified understanding.

---

## 0. Live-Verified Facts (foundation)

Every claim below was reproduced on the `eval-zcp` project using a
throwaway `probe` nodejs@22 runtime service, with direct SSH access from
the local machine via VPN. The reproduction script is in §8.

### 0.1 The zembed SFTP MKDIR bug

- Zerops runtime containers run a custom SSH daemon, `zerops-zembed`
  (`/opt/zerops/bin/zerops-zembed`), not OpenSSH.
- Its SFTP subsystem's `MKDIR` handler creates directories as `root:root`
  regardless of which user authenticated the SSH session.
- Its SFTP subsystem's `CREATE/WRITE` (file) handler correctly respects
  the authenticated user — files land as `zerops:zerops`.
- Its SSH `exec` path (running a shell command) is unaffected — an
  `ssh probe "mkdir /var/www/x"` lands `/var/www/x` as `zerops:zerops`.

This is a platform bug. It can only be fully fixed in zembed itself
(Zerops platform code, not ZCP).

### 0.2 Consequences on the container side

In a container with user `zerops` (uid 2023), a root-owned directory
(mode 0755) allows:

| Operation | As `zerops` (non-owner, non-root) | Result |
|---|---|---|
| Read files inside | mode 0755 "other" = read+list | OK |
| Modify existing file content | file is zerops-owned | OK |
| Create new file inside | needs write on parent dir | **BLOCKED** |
| `mkdir` subdirectory | needs write on parent dir | **BLOCKED** |
| `rm` existing file | needs write on parent dir | **BLOCKED** |
| `mv` / rename | needs write on parent dir | **BLOCKED** |

### 0.3 Where mount-side operations still work regardless

When ZCP writes through its SSHFS mount (zcp → SFTP → zembed), operations
happen at the SFTP protocol level inside zembed. `CREATE`, `WRITE`,
`REMOVE`, and `RENAME` all bypass the container-side unix permission
model because they execute inside the zembed daemon context.

So mount-side `rm /var/www/{host}/src/old.ts`, `mv ... new.ts`, `echo >
file`, etc. work even in root-owned dirs. **SSHFS mount-side is not the
problem.**

### 0.4 What actually triggers the user-facing bug

The one case that breaks: agent runs `git init` from the ZCP mount side.

```
cd /var/www/{host} && git init
```

This populates `.git/` with a mix of SFTP-created files (zerops-owned)
and SFTP-created subdirs (`objects/`, `branches/`, `hooks/`, `info/` —
root-owned because of the MKDIR bug).

Then `zerops_deploy` runs its SSH-side sequence:

```
cd /var/www && (test -d .git || git init) && git config user.email ... && git add -A && git commit
```

- `test -d .git` passes (poisoned `.git/` exists)
- `git init` is skipped
- `git config user.email` tries to write `.git/config` — if that write
  path hits a root-owned ancestor or file, it fails
- `git add -A` needs to write to `.git/objects/` — root-owned, **fails**
  with `fatal: insufficient permission for adding an object to repository
  database .git/objects`

Agent sees a confusing git error, tries chown (doesn't work through
SSHFS), tries workarounds, wastes cycles.

### 0.5 What doesn't trigger the bug

The typical agent workflow does not touch this:

1. Agent writes source files via Write tool (through mount). Parent
   directories for new nested paths get auto-created by the tool — these
   land root-owned but **git treats working-tree directories as
   read-only during `add`/`commit`** (mode 0755 allows read+list). No
   issue.
2. Agent calls `zerops_deploy`. The tool already runs `git init`
   **container-side via SSH** (`deploy_ssh.go:182`), so `.git/` is clean
   and zerops-owned. Commit + push work.
3. Runtime container gets rebuilt from the push — mount state doesn't
   propagate to the new runtime container.

**Nothing in the happy path needs to change.**

### 0.6 Note on code-server (VS Code in Zerops UI)

Verified live: Zerops's UI VS Code (code-server) has an **incidental
chown side-effect** — when the user expands a folder in the Explorer
panel, code-server's directory scan triggers a chown to the session
user on root-owned children. This is code-server-specific behavior,
not a platform auto-heal. Verified by:

- Passive observation (no VS Code tab open) → root-owned dir stays
  root indefinitely
- VS Code tab open but no Explorer interaction → still stays root
- User clicks/expands the parent folder in Explorer → child flips to
  zerops within milliseconds
- `strace` on `fileWatcher` and `extensionHost` did not capture the
  chown syscall, so the source is either the main code-server binary
  or a helper process spawned briefly during the scan

Consequence: users editing via the Zerops UI don't see the bug because
code-server papers over it. **Agent workflows (Claude Code, shell,
zerops_deploy) don't go through code-server and hit the raw bug.**
Our plan targets the agent path, which code-server's behavior doesn't
help with.

---

## 1. Root Causes

| ID | Root cause | Category |
|---|---|---|
| **RC-A** | Agent has no clear rule stating "don't run `git init` from the ZCP mount side — `zerops_deploy` handles it container-side". The one paragraph in `strategy-push-git-push-container.md:40-42` applies to push-git setup only; no atom covers the first-deploy push-dev context where an agent might reach for `git init` | Guidance gap → user-facing bug |
| **RC-B** | `internal/ops/deploy_ssh.go:182` runs `(test -d .git || git init -q -b main) && git config user.email ... && git config user.name ...` inside `buildSSHCommand` — i.e. on **every deploy**. Git init + config are service-lifecycle concerns (set once per service when it becomes deploy-ready), not deploy-lifecycle concerns. Living in the hot path is redundant and wastes a SSH round trip on every deploy | Architectural layer mismatch |
| **RC-C** | `develop-first-deploy-write-app.md:32-34` tells the agent *"Write code directly to `/var/www/{hostname}/` on the local SSHFS mount — NOT via SSH into the container"*. It does not distinguish **file I/O** (mount is correct) from **command execution** (some commands must run container-side for tool availability, not for ownership reasons). The atom primes the agent to treat the mount as a universal work surface | Atom content primes wrong mental model |
| **RC-D** | zembed SFTP MKDIR creates root-owned directories. Mount-side mkdir is therefore hazardous any time a container-side process (build tool, dev server, refactor-via-ssh) later needs to write into that directory. This is the platform's bug, not ZCP's; ZCP can only mitigate or document | Platform bug — out of scope for ZCP code, report only |
| **RC-E** | Local-mode `strategy=git-push` requires a user-owned git repo with ≥1 commit (verified against `zcli@v1.0.61` source, `handler_archiveGitFiles.go:67-75` — `rev-parse --is-inside-work-tree` + `rev-list --all --count != 0`). Today the agent learns this only **after** calling `zerops_deploy strategy=git-push` via a pre-flight error in `deploy_local_git.go:96-116`. No atom proactively tells the agent "git-push needs a user-owned git + commit; don't initialize it for the user" | Guidance gap → retroactive error friction |
| **RC-F** | `develop-first-deploy-write-app.md` has no `environments:` frontmatter key. Its content talks exclusively about the SSHFS mount (`/var/www/{hostname}/`), which doesn't exist in local env — yet the atom synthesizes for both environments. In local env the atom is actively misleading; the agent gets told to write to a mount that isn't there | Atom env-scoping gap → wrong-env content leakage |

### Out of scope for this plan

- `ops.ReconcileMountOwnership` / auto-chown reconciler before every
  deploy. The scenarios where a reconciler would help (container-side
  writes to mount-created dirs) aren't the agent's typical flow — agent
  uses mount tools for filesystem ops. Adding a reconciler would be
  defensive programming against a non-problem.
- Full guidance rewrite across every workflow (recipe, export, etc.).
  Recipe has its own canonical rule already; other atoms get minimal
  updates or pointer-references.

---

## 2. Fix Design

### Fix 1 (→ RC-B) — Move git init to service lifecycle

**New primitive**: `internal/ops/service_git_init.go`

```go
package ops

import (
    "context"
    "fmt"
)

// InitServiceGit ensures /var/www/.git/ exists on the container, owned
// by the zerops SSH user, with the deploy identity configured. Runs
// entirely container-side via SSH exec — SSH exec mkdir respects the
// authenticated user (zembed's SFTP MKDIR does not; see §0.1).
// Idempotent: existing .git/ is preserved; config is (re-)applied.
func InitServiceGit(ctx context.Context, ssh SSHDeployer, hostname string) error {
    email := shellQuote(DeployGitIdentity.Email)
    name := shellQuote(DeployGitIdentity.Name)
    cmd := fmt.Sprintf(
        `cd /var/www && (test -d .git || git init -q -b main) && git config user.email %s && git config user.name %s`,
        email, name,
    )
    if _, err := ssh.ExecSSH(ctx, hostname, cmd); err != nil {
        return fmt.Errorf("init git on %s: %w", hostname, err)
    }
    return nil
}
```

**Hook**: `internal/tools/workflow_bootstrap.go::autoMountTargets` loops
over `plan.Targets` after provision completes. Inside that loop, after
the existing `ops.MountService` call, add `ops.InitServiceGit`.

Both fresh bootstrap and adopt routes flow through this. Managed services
are not in `plan.Targets` (they have no runtime filesystem to init).

**Signature threading**: `RegisterWorkflow` (`internal/tools/workflow.go:118`)
and `handleWorkflowAction` (`workflow.go:157`) gain an `sshDeployer
ops.SSHDeployer` parameter. `internal/server/server.go:121` passes
`s.sshDeployer` at registration. Nil in local environment — no-op.

**Deploy hot path**: `internal/ops/deploy_ssh.go::buildSSHCommand`:

Remove the separate `git config user.email ... && git config user.name ...` statements from the inline sequence. Config was set at bootstrap and persists in `.git/config` across deploys.

Keep `(test -d .git || (git init -q -b main && git config user.email ... && git config user.name ...))` as an **atomic fallback safety net** — handles:
- **Migration** (services that existed before this plan shipped and have no `.git/` yet): the OR branch inits AND configures identity, so the subsequent `git add -A && git commit` succeeds on first post-upgrade deploy.
- **Manual recovery** (user ran `rm -rf /var/www/.git`): same path — re-inits + re-configs.
- **Robustness**: a single `test -d` stat on the happy path, negligible cost (both init and config inside the `||` branch skipped when `.git/` exists).

Before:
```go
gitInit := "(test -d .git || git init -q -b main)"
gitIdentity := fmt.Sprintf("git config user.email %s && git config user.name %s", email, name)
gitCommit := "git add -A && (git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'deploy')"
pushCmd := fmt.Sprintf("cd %s && %s && %s && %s && %s", workingDir, gitInit, gitIdentity, gitCommit, pushArgs)
```

After:
```go
email := shellQuote(DeployGitIdentity.Email)
name := shellQuote(DeployGitIdentity.Name)
gitSafety := fmt.Sprintf(
    "(test -d .git || (git init -q -b main && git config user.email %s && git config user.name %s))",
    email, name,
)
gitCommit := "git add -A && (git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'deploy')"
pushCmd := fmt.Sprintf("cd %s && %s && %s && %s", workingDir, gitSafety, gitCommit, pushArgs)
```

Remove `id GitIdentity` parameter from `buildSSHCommand` — it no longer takes identity from the caller; it reads the package constant `DeployGitIdentity` (same source of truth as `InitServiceGit`). Keep `DeployGitIdentity` var.

### Fix 2 (→ RC-A) — Atom content: explicit git-init warning

`internal/content/atoms/develop-first-deploy-write-app.md` — add after
the existing "Write code directly to mount" paragraph:

```markdown
**Don't run `git init` from the ZCP side.** Bootstrap initialized
`.git/` container-side when it provisioned the service. If you run
`git init` through the mount (`cd /var/www/{hostname}/ && git init`),
the platform's SFTP MKDIR creates `.git/objects/` owned by root,
which breaks the container-side `git add` that `zerops_deploy` runs.
Recovery: `ssh {hostname} "sudo rm -rf /var/www/.git"` — the next
deploy's safety net re-inits it.
```

### Fix 3 (→ RC-C, RC-F) — Atom content: disambiguate write vs exec + env scope

Two edits to `develop-first-deploy-write-app.md`:

**(a) Frontmatter scoping** — add `environments: [container]` so the atom stops leaking into local-env synthesis where its SSHFS mount content is misleading:

```diff
 ---
 id: develop-first-deploy-write-app
 priority: 3
 phases: [develop-active]
 deployStates: [never-deployed]
+environments: [container]
 title: "Write the application code"
 ---
```

Follows the precedent set by `develop-first-deploy-asset-pipeline-{container,local}.md` which are explicitly env-scoped. Local env is covered by `develop-platform-rules-local.md` already (no SSHFS, code lives in working dir).

**(b) Body rewrite** — replace lines 32-34 disambiguating write (mount) vs exec (SSH):

Before:
```
Write code directly to `/var/www/{hostname}/` on the local SSHFS mount —
NOT via SSH into the container. SSH deploys blow away uncommitted
working-tree changes on container restart.
```

After:
```
**Write files** directly to `/var/www/{hostname}/` through the SSHFS
mount — Read/Edit/Write tools (and plain `rm`, `mv`, `cp` against mount
paths) all work because the mount bypasses container-side permissions
at the SFTP protocol level.

**Run commands** (`go build`, `php artisan`, `pytest`, framework CLIs,
dev server) via SSH into the container: `ssh {hostname} "cd /var/www
&& <command>"`. The reason is tool availability, not ownership — most
runtime-specific CLIs aren't installed on the ZCP host.

SSH deploys replace the container; only content covered by `deployFiles`
survives across deploys.
```

No changes to `develop-platform-rules-container.md`, `export.md`, or
other atoms. Those stay as-is — they cover different concerns
(persistent platform rules, export-specific flow) and aren't primed by
or duplicated against the rule above.

### Fix 4 (→ RC-D) — Platform bug report (non-code)

Prepare a reproduction + description for the Zerops platform team.
Content in §5. ZCP ships with or without this; it's a separate
communication task.

### Fix 5 (→ RC-E) — Atom content: local git-push user-responsibility

`internal/content/atoms/develop-platform-rules-local.md` — add a new
bullet after the existing "Stage deploys ride the user's filesystem"
paragraph:

```markdown
- **`strategy=git-push` needs a user-owned git repo.** Before calling
  `zerops_deploy strategy=git-push`, verify the working dir is a git
  repo with at least one commit (`git status`, `git log`). Verified
  against `zcli@v1.0.61` source: `zcli push` (without `--no-git`)
  rejects the archive when `rev-parse --is-inside-work-tree` fails or
  `rev-list --all --count == 0`. If the user has neither, ask them to
  run `git init && git add -A && git commit -m 'initial'` themselves —
  ZCP does NOT initialize git in the user's working directory, because
  identity (user.name, user.email), default branch, and `.gitignore`
  conventions are personal choices. The default `zerops_deploy`
  strategy (without `strategy=git-push`) uses `zcli --no-git` and
  doesn't require any git state at all.
```

**Explicitly NOT doing**: no auto-init in local env. Rationale in §11
Known Limitations.

**No changes to** `deploy_local.go` or `deploy_local_git.go` — the
pre-flight error in `handleLocalGitPush` (lines 96-116) already returns
the same guidance as a hard fallback when agent skips the atom.

### Fix 6 (→ RC-B) — Drop redundant identity config in git-push deploy path

`internal/ops/deploy_git_push.go::BuildGitPushCommand` (line 44) also sets
`git config user.email ... && git config user.name ...` inline on every
git-push deploy. After Fix 1 (InitServiceGit at bootstrap), identity is
persisted in `.git/config` on the target service — this inline config is
redundant.

Before:
```go
email := shellQuote(id.Email)
name := shellQuote(id.Name)
parts = append(parts, fmt.Sprintf("git config user.email %s && git config user.name %s", email, name))
```

After: **delete these 3 lines entirely.** The `id GitIdentity` parameter
becomes unused → remove from `BuildGitPushCommand` signature; callers
drop the argument. Parallel to Fix 1's `buildSSHCommand` cleanup —
identity has exactly one source of truth: `ops.DeployGitIdentity`, read
at bootstrap by `InitServiceGit`, persisted on the service.

Unlike `buildSSHCommand`, no atomic safety-net is needed here — the
`BuildGitPushCommand` contract (comment lines 13-16) already requires a
pre-flight guarantee that `.git/` exists and HEAD has a commit. Callers
that bypass `InitServiceGit` (e.g. services imported outside bootstrap)
will hit that pre-flight, which is the correct place to enforce state.

### Fix 7 (→ RC-B) — Delete `configureGit` entirely; ZCP-host has no legitimate git-setup consumer

`internal/init/init_container.go::configureGit` currently runs three commands:

```go
cmds := [][]string{
    {"git", "config", "--global", "user.email", id.Email},
    {"git", "config", "--global", "user.name", id.Name},
    {"git", "init", gitInitDir},
}
```

**All three have zero consumers when ZCP runs as a Zerops service.** Grep across production code:

| Surface | Uses `/var/www/.git/` on host? | Uses global `git config` on host? |
|---|---|---|
| `autoMountTargets`, `InitServiceGit`, `buildSSHCommand`, `BuildGitPushCommand` | No — all `/var/www` ops target dev services over SSH | No |
| `deploy_local.go`, `deploy_local_git.go` | No — local env only | No |
| `export.go` | `git ls-files` (read-only, no identity needed) | No |
| **`publish_recipe.go::PushAppSource`** | Yes (`git commit`) | **Yes — this is the only consumer** |

`PushAppSource` is invoked exclusively by the CLI subcommand `zcp sync recipe push-app <slug> <app-dir>` (`cmd/zcp/sync.go:319`). That's a **recipe authoring tool for developers working locally on their laptops**, not a workflow that runs inside a Zerops-deployed ZCP service. No bootstrap/develop/deploy path on the platform ever hits it.

Developers running `zcp sync recipe push-app` locally already have their own `~/.gitconfig` with personal identity. Setting a global `Zerops Agent` identity inside the ZCP service container is dead configuration.

**Change**:
- Delete `configureGit` function.
- Delete `defaultGitInitDir`, `gitInitDir` var, `SetGitInitDir`/`ResetGitInitDir` test helpers.
- Remove `{"Git config", configureGit}` from `containerSteps()`.
- Remove the now-unused `ops` import from `init_container.go` (only `configureGit` consumed `ops.DeployGitIdentity`).
- Delete `TestContainerSteps_GitConfig` entirely (no function to test).
- Update `TestContainerSteps_SkippedOutsideContainer` — drop the `if gitCalled` assertion (git was never called from non-configureGit paths).

**What about push-app commits?**

`PushAppSource` continues to call `git commit` through `runGit`. In the developer's local env: commit uses the developer's `~/.gitconfig` — authored under their name, which is correct (it's their recipe publish action). In the hypothetical/unexpected case someone runs `zcp sync recipe push-app` inside a ZCP service container: commit uses whatever git default the container has (uid/gid-derived or empty — git errors out cleanly asking for `user.email`/`user.name`). The operator sees a clear error and knows to either set identity themselves or not run this CLI there.

**Why not add `-c user.email/user.name` to `runGit`?**
Considered and rejected. Per-invocation `-c` flags would pin "Zerops Agent" on every developer's recipe publish, overriding their actual authorship. Developer commits should be attributed to the developer. If we later decide recipe repo commits should be anonymized, that's a separate content-policy decision for `PushAppSource`, not a host-setup concern.

**This completes GLC-3 and adds GLC-4**: after Fix 7, the only remaining consumers of `ops.DeployGitIdentity` are `InitServiceGit` and `buildSSHCommand` safety-net — both target per-service local `.git/config` on managed runtime services. No global git state is written on the ZCP host at all.

---

## 3. Invariants

Newly established after this plan:

- **GLC-1** (Git Lifecycle): Every runtime service added to the project
  via bootstrap or adopt has `/var/www/.git/` initialized container-side,
  owned by `zerops:zerops`, with deploy identity configured. Enforced by
  `autoMountTargets` post-mount hook.

- **GLC-2** (Deploy safety net): `deploy_ssh.go::buildSSHCommand` must
  tolerate a missing `.git/` (fallback-inits it) but must not configure
  identity inline top-level. Config is set at bootstrap and persists;
  fallback path inside the `||` branch re-runs config atomically when
  init had to re-run.

- **GLC-3** (Single source of identity): `ops.DeployGitIdentity`
  (`agent@zerops.io` / `Zerops Agent`) is the one and only identity
  source. Consumers after this plan:
  - (a) `service_git_init.go::InitServiceGit` — per-service local
    `git config user.*` in each managed service's `/var/www/.git/config`.
  - (b) `buildSSHCommand` atomic safety-net — same local config,
    fallback only when `.git/` missing (migration/recovery).
  - (c) nowhere else.

  Removed redundant/vestigial consumers:
  - `BuildGitPushCommand` inline identity (Fix 6).
  - `buildSSHCommand` top-level `gitIdentity` (Fix 3).
  - `init_container.go::configureGit` entirely (Fix 7) — including
    both the legacy `git init /var/www` AND the `git config --global`
    lines.
  - `id GitIdentity` parameter vanishes from `buildSSHCommand`,
    `BuildGitPushCommand`, `deploySingleSSH`, `DeployViaSSH`.

  The constant `ops.DeployGitIdentity` survives as the shared label
  used by the two remaining consumers, both of which target
  `/var/www/.git/config` on managed runtime services, not on the
  ZCP-host container.

- **GLC-4** (ZCP-host has no git state): `/var/www` on the ZCP-host
  container is the SSHFS mount base, not a code directory — it never
  gets a `.git/`. And no global `git config --global` is written on
  the ZCP-host by `zcp init` either. The ZCP-host container is a
  **pure infrastructure host** from git's perspective; every managed
  service has its own self-contained `.git/` inside that service's
  container (owned by `zerops:zerops`, configured by InitServiceGit).
  No outer-repo nesting, no global state that leaks into
  developer-run CLI commands.

Preserved from earlier work:

- **KD-11**: `ServiceMeta` remains the single persistent per-service
  state. `InitServiceGit` does not touch metas.
- **O2**: Bootstrap steps complete synchronously. `InitServiceGit` runs
  within the `autoMountTargets` call that already blocks on mount
  readiness, so no new async concerns.

---

## 4. TDD Plan

### 4.1 Unit — new primitive

`internal/ops/service_git_init_test.go`:

- Table-driven against a `MockSSHDeployer`. Fields: name, hostname,
  expected command substring, simulated exec error, expected wrapped
  error.
- Cases:
  - Happy: hostname="probe" → command contains `"cd /var/www"`, `"git init -q -b main"`, `"git config user.email 'agent@zerops.io'"`.
  - Idempotent: second call same hostname → same command string (no
    state drift).
  - Empty hostname: rejects via platform error (or propagates upward
    from ExecSSH).
  - SSH failure: returns wrapped error containing "init git on probe".
- No network. Mock deployer just records command strings.

### 4.2 Unit — deploy_ssh command regression

`internal/ops/deploy_ssh_test.go`: update existing tests.

- Assert `buildSSHCommand` output **does not contain** a bare `"git config user.email"` outside the safety-net OR branch — i.e. no top-level identity statement. (The identity lives atomically inside `(test -d .git || (... git config ...))`).
- Assert output **contains the atomic safety net**: `"(test -d .git || (git init -q -b main && git config user.email 'agent@zerops.io' && git config user.name 'Zerops Agent'))"` — covers migration + recovery in one shell expression.
- Assert output still contains `"git add -A"` and `"zcli push"`.
- **New migration test** `TestBuildSSHCommand_FreshInitPath`: execute the emitted command against a `/tmp/` scratch dir with no `.git/` and a real `git` binary (skip on `testing.Short()` and when git isn't on PATH). Expect no error, `.git/` created, `git config --get user.email` → `agent@zerops.io`. Locks down the OR-branch atomicity against a future refactor that accidentally splits init from config.

### 4.3 Tool — bootstrap hook

`internal/tools/workflow_bootstrap_test.go`:

- New test: `TestAutoMountTargets_CallsInitServiceGit`. Uses stub
  `mounter` and stub `sshDeployer`. Seeds a plan with two runtime
  targets. Verifies `InitServiceGit` exec was invoked once per runtime
  hostname with the canonical command. Fresh and adopt (IsExisting=true)
  both covered.
- Edge: `sshDeployer == nil` (local environment) — hook is a no-op, test
  verifies no panic and no exec attempted.

### 4.4 Atom contract

`internal/workflow/atom_contract_test.go` (P2.5 framework):

- New entry for `develop-first-deploy-write-app` (container-scoped):
  - `phraseRequired`: `["Don't run \`git init\` from the ZCP side", "Write files", "Run commands"]`
  - `environments`: `[container]` — pinned so a future edit that drops the frontmatter env key gets caught
  - `testName`: link to deploy_ssh test above
  - `codeAnchor`: `"internal/ops/service_git_init.go#InitServiceGit"`
- New entry for `develop-platform-rules-local`:
  - `phraseRequired`: `["strategy=git-push needs a user-owned git repo", "ZCP does NOT initialize git in the user's working directory"]`
  - `environments`: `[local]`
  - `codeAnchor`: `"internal/tools/deploy_local_git.go#handleLocalGitPush"` — paired with the pre-flight that enforces the rule if the agent skips the guidance.

### 4.5 E2E — live platform

`e2e/bootstrap_git_init_test.go` (build tag `e2e`):

- Provision a `bs` nodejs@22 test service via `zerops_import`.
- Invoke `zerops_workflow action=complete step=provision` on a fresh
  bootstrap session.
- SSH into the service: assert `stat -c '%U' /var/www/.git` prints
  `zerops`, `git config --get user.email` prints `agent@zerops.io`,
  `git config --get user.name` prints `Zerops Agent`.
- Cleanup: delete service via `zerops_delete`.

`e2e/deploy_without_inline_config_test.go`:

- Provision service, go through bootstrap with InitServiceGit running.
- Manually corrupt `/var/www/.git/config` via SSH (simulate recovery
  need). Run `zerops_deploy`. Assert it re-inits and succeeds.

### 4.7 Unit — git-push command cleanup

`internal/ops/deploy_git_push_test.go`:

- Assert `BuildGitPushCommand` output **does not contain** any
  `"git config user.email"` or `"git config user.name"` substring.
  Locks the Fix 6 cleanup against a future re-add.
- Assert output still contains `"trap 'rm -f ~/.netrc' EXIT"`, netrc
  setup, `"git push -u origin"` — ensures only identity was removed.
- Update any existing tests that were asserting presence of the
  identity config (they were pinning the redundancy).

### 4.6 Eval — scenario

`internal/eval/scenarios/bootstrap-git-init.md` (container-env):

- Seed: `empty`.
- Prompt: *"Udělej nodejs dev službu a deployni jednoduchý Express hello
  world endpoint na /."*
- Expect: `mustCallTools: [zerops_workflow, zerops_deploy]`,
  `requiredPatterns: ["\"workflow\":\"bootstrap\"", "\"workflow\":\"develop\""]`,
  `finalUrlStatus: 200`,
  `forbiddenPatterns: ["insufficient permission", "dubious ownership", "cd /var/www/.* && git init"]`.
- Verifies that the agent never runs mount-side `git init` (forbidden
  pattern in scenario logs), and that the deploy succeeds end-to-end.

---

## 5. Platform Bug Report

Content to send to the Zerops platform team (separate from code
changes):

> **zerops-zembed SFTP MKDIR ignores connecting user identity**
> 
> **Summary**: The SFTP MKDIR handler in `zerops-zembed` creates
> directories owned by `root:root` regardless of the SSH-authenticated
> user. SFTP CREATE (file writes) and SSH-exec `mkdir` both correctly
> respect the user.
> 
> **Reproduction** (from any host with SSH access to a Zerops runtime
> container, as non-root user `zerops`):
> 
> ```
> sftp -b /dev/stdin {hostname} <<EOF
> mkdir /var/www/testdir
> put /etc/hostname /var/www/testfile
> EOF
> 
> ssh {hostname} "stat -c '%U %n' /var/www/testdir /var/www/testfile"
> ```
> 
> Expected: both owned by `zerops`.
> Actual: `testdir` owned by `root`, `testfile` owned by `zerops`.
> 
> For contrast, the ssh-exec path works correctly:
> ```
> ssh {hostname} "mkdir /var/www/testdir2"
> ssh {hostname} "stat -c '%U' /var/www/testdir2"   # zerops
> ```
> 
> **Impact**: SSHFS mounts, raw SFTP sessions, and any `mkdir` through
> the SFTP subsystem produce root-owned directories the container's
> `zerops` user can't modify. Blocks typical development flows:
> 
> - IDE Write/Create-file operations that implicitly create parent
>   directories through a mounted `/var/www/{host}/`
> - `rm`/`mv`/`mkdir` inside mount-created directories as the zerops
>   user
> - Build tools writing into mount-created output directories
> - `git init` through mount (subdirs root-owned, blocks
>   subsequent zerops-user commits)
> 
> **Suspected cause**: The MKDIR handler in the SFTP subsystem likely
> runs in the daemon's privilege context without `seteuid()` to the
> authenticated user before calling `mkdir()`. OpenSSH's internal-sftp
> handles this by forking + setuid'ing to the target user before any
> FS operations.
> 
> **Workaround in ZCP** (`InitServiceGit` in this plan): ZCP initializes
> `.git/` via SSH exec (not SFTP) so the one repo we care about is
> correctly owned. We document mount-side mkdir hazards in agent
> guidance. These workarounds stay no matter what — a platform fix
> lets them become no-ops.

---

## 6. File Delta Budget

| File | Delta |
|---|---|
| `internal/ops/service_git_init.go` | new, ~35 LOC |
| `internal/ops/service_git_init_test.go` | new, ~90 LOC |
| `internal/ops/deploy_ssh.go` | -8 lines (remove gitIdentity), -1 param |
| `internal/ops/deploy_ssh_test.go` | ~15 line updates |
| `internal/ops/deploy_git_push.go` | -4 lines (remove inline identity), -1 param |
| `internal/ops/deploy_git_push_test.go` | ~8 line updates |
| `internal/tools/deploy_ssh.go` or callers of BuildGitPushCommand | -1 arg at each call site |
| `internal/init/init_container.go` | Delete `configureGit` (~15 lines) + `defaultGitInitDir`/`gitInitDir` + `ops` import + step entry from `containerSteps()`. Net ~-20 lines. |
| `internal/init/init_container_test.go` | Delete `TestContainerSteps_GitConfig` entirely (~50 lines); trim `TestContainerSteps_SkippedOutsideContainer` git-called assertion. |
| `internal/init/export_test.go` | Delete `SetGitInitDir`/`ResetGitInitDir` helpers. |
| `internal/tools/workflow.go` | +1 param threading (3 functions) |
| `internal/tools/workflow_bootstrap.go` | +10 lines in autoMountTargets |
| `internal/tools/workflow_bootstrap_test.go` | new test, ~80 LOC |
| `internal/server/server.go` | +1 arg passed to RegisterWorkflow |
| `internal/workflow/atom_contract_test.go` | +1 entry |
| `internal/content/atoms/develop-first-deploy-write-app.md` | +1 frontmatter line (`environments: [container]`) + rewrite §32-34, +10 lines |
| `internal/content/atoms/develop-platform-rules-local.md` | +10 lines (git-push user-responsibility bullet) |
| `e2e/bootstrap_git_init_test.go` | new, ~100 LOC |
| `internal/eval/scenarios/bootstrap-git-init.md` | new, ~30 lines |

Total: roughly +300 LOC production + tests (Fix 6 + Fix 7 are strongly net negative — together delete ~90 LOC). No new packages. No public interface changes outside the already-exported `ops.SSHDeployer` parameter threading. `BuildGitPushCommand` signature narrows (removes `id GitIdentity`) — unexported-callers fix-up only.

---

## 7. Execution Order (Commits)

Each commit is independently green (`go test ./... -count=1 -short` and
`make lint-local` pass).

1. `feat(ops): InitServiceGit primitive + unit tests`
   — Just the new file + test file. No wiring.

2. `feat(workflow): thread sshDeployer to autoMountTargets; call InitServiceGit post-mount`
   — Server wiring, signature updates, hook addition, tool test.

3. `refactor(deploy-ssh): remove inline git config; keep init fallback`
   — Simplify `buildSSHCommand`, update regression tests.

4. `refactor(deploy-git-push): drop inline identity — InitServiceGit owns it`
   — `BuildGitPushCommand` drops `id GitIdentity` param + inline `git config` lines; callers updated; regression test added.

5. `refactor(init): drop legacy git init /var/www on ZCP-host self-setup`
   — `configureGit` keeps `--global` identity config, drops the no-consumer `git init`; test + helpers trimmed.

6. `content(atoms): scope write-app to container env + disambiguate write-vs-exec + warn against mount-side git init`
   — `develop-first-deploy-write-app.md` frontmatter env scoping + body rewrite + atom-contract test entry.

7. `content(atoms): local git-push user-responsibility guidance`
   — `develop-platform-rules-local.md` new bullet + atom-contract test entry.

8. `test(e2e): bootstrap-time git init is zerops-owned with configured identity`
   — Live-platform verification.

9. `test(eval): bootstrap-git-init scenario — forbid mount-side git init in logs`
   — Agent-level verification via Claude CLI.

Release: `make release` (minor bump) after all nine commits land on main
and CI is green.

---

## 8. Reproduction Script (for manual verification)

```bash
#!/usr/bin/env bash
# verify-git-service-lifecycle.sh — manually reproduce the bug and the fix.
# Requires: VPN to eval-zcp active, .mcp.json has ZCP_API_KEY, ssh zcp works.

set -euo pipefail
SSH_FLAGS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10"

# 1. Provision probe service — run this via the MCP tool in your Claude session:
#    mcp__zcp__zerops_import with:
#      services:
#        - hostname: probe
#          type: nodejs@22
#          startWithoutCode: true
#          maxContainers: 1
#          verticalAutoscaling: { minRam: 0.25 }
#    Wait until: ssh probe "echo ok" works.

# 2. Mount probe from zcp (same options as ops.MountService)
ssh $SSH_FLAGS zcp "mkdir -p /var/www/probe && sudo -E zsc unit create sshfs-probe 'sshfs -f -o reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3,transform_symlinks,no_check_root probe:/var/www /var/www/probe'"
sleep 3

# 3. REPRO THE BUG: run git init from zcp mount side
ssh $SSH_FLAGS zcp "cd /var/www/probe && git init -q -b main"
echo "--- .git/ ownership on probe ---"
ssh $SSH_FLAGS probe "stat -c '%U %A %n' /var/www/.git /var/www/.git/objects /var/www/.git/config"
# Expect: .git and config zerops-owned, .git/objects root-owned (THE BUG).

# 4. REPRO THE DEPLOY FAILURE that would happen
ssh $SSH_FLAGS probe "cd /var/www && git config user.email test@test && git add -A 2>&1 || echo 'expected failure above'"
# Expect: fatal: insufficient permission for adding an object to repository database .git/objects

# 5. CLEANUP that the atom guidance will tell the agent
ssh $SSH_FLAGS probe "sudo rm -rf /var/www/.git"

# 6. THE FIX: run InitServiceGit manually (same command ops.InitServiceGit will run)
ssh $SSH_FLAGS probe "cd /var/www && (test -d .git || git init -q -b main) && git config user.email 'agent@zerops.io' && git config user.name 'Zerops Agent'"
echo "--- .git/ after fix ---"
ssh $SSH_FLAGS probe "stat -c '%U %A %n' /var/www/.git /var/www/.git/objects /var/www/.git/config"
# Expect: all zerops-owned.

# 7. VERIFY deploy-like sequence succeeds now
ssh $SSH_FLAGS probe "cd /var/www && echo 'console.log(\"hi\")' > app.js && git add -A && git commit -q -m deploy && git log --oneline"
# Expect: one commit in log.

# 8. CLEANUP
ssh $SSH_FLAGS zcp "fusermount3 -u /var/www/probe 2>/dev/null; sudo -E zsc unit remove sshfs-probe 2>&1 | head -1; rmdir /var/www/probe 2>/dev/null"
# Then delete probe via: mcp__zcp__zerops_delete serviceHostname=probe
```

Total runtime ~3 minutes including provisioning.

---

## 9. Rollback

Per-commit non-squash merges preserve clean revert ranges.

| Commit | Rollback impact |
|---|---|
| 1 (primitive) | Unused code reverted |
| 2 (bootstrap hook) | Init no longer runs at bootstrap; deploy's safety-net fallback (atomic init+config inside OR branch) handles first deploy |
| 3 (deploy-ssh refactor) | Pre-plan inline config restored; redundant with bootstrap init but correct |
| 4 (deploy-git-push refactor) | Pre-plan inline config restored on git-push path; redundant with bootstrap init but correct |
| 5 (init cleanup) | `configureGit` and its `git init /var/www` + `git config --global` return; all functionally unused, so reverting re-introduces vestigial state but causes no regression |
| 6 (write-app atom) | Container-env wrong-surface leak to local env returns; write-vs-exec disambiguation reverts |
| 7 (local-rules atom) | Agent loses proactive git-push guidance; `handleLocalGitPush` pre-flight error still catches the case as hard fallback |
| 8-9 (tests) | Cosmetic |

Each is revertable independently. Interdependencies:
- Commits 2 + 3: removing inline identity from deploy_ssh assumes bootstrap init is present OR safety-net atomic form covers migration.
- Commits 2 + 4: removing inline identity from deploy_git_push assumes bootstrap init is present; the git-push path has no safety-net fallback (contract requires pre-initialized `.git/`) so commit 4 **must follow** commit 2.
- Commit 5 is standalone — no consumer reads the init'd repo, deletion is pure cleanup.
- Commits 6 and 7 are independent of each other and of the code commits.

---

## 10. "Done" Definition

**Automated gates**:

1. `go test ./... -count=1 -race`: green.
2. `make lint-local`: green.
3. `TestInitServiceGit_*` green (unit).
4. `TestBuildSSHCommand_FreshInitPath` green (migration fresh-init path lock-in).
5. `TestBuildGitPushCommand_NoInlineIdentity` green (Fix 6 regression lock-in).
6. `TestContainerSteps_GitConfig` **deleted** (Fix 7 lock-in — the test's subject `configureGit` no longer exists); `TestContainerSteps_SkippedOutsideContainer` green with git-called assertion removed.
7. `TestAutoMountTargets_CallsInitServiceGit` green (tool).
8. `TestAtomContract` green with both new entries (write-app container-scoped, platform-rules-local git-push).
9. `TestE2E_BootstrapGitInit` green against live platform.
10. Eval `bootstrap-git-init.md` scenario passes with no forbidden
    patterns in agent logs.

**Manual verification**:

- Run the reproduction script in §8 on a freshly built binary. All
  expected outcomes match.

**Non-automated**:

- Platform bug report (§5) sent to Zerops team. Response not required
  before merging; tracking-only.

---

## 11. Known Limitations

- **Platform bug stays present**. The reconciler approach (rejected)
  would mitigate the broader class of mount-mkdir hazards. This plan
  fixes the one concrete failure (mount-side `git init`) and prevents
  it via guidance. Other mount-mkdir hazards (explicit `mkdir` by the
  agent from a shell, build tools pre-creating output dirs by accident)
  can still cause container-side permission errors. Acceptance: these
  edge cases aren't common enough to justify automatic-reconcile
  overhead; when they surface, agent guidance points at
  `ssh {host} "sudo chown -R zerops:zerops /var/www"` as the manual
  escape hatch.

- **Migration**. Services provisioned before this plan don't have
  InitServiceGit-initialized `.git/`. The deploy-time safety-net's
  **atomic OR branch** (`test -d .git || (git init -q -b main && git config
  user.email ... && git config user.name ...)`) handles them on first
  post-upgrade deploy — init AND identity both land before `git add
  -A` runs. No explicit migration step required.

- **buildFromGit services**. Services imported via
  `buildFromGit: <url>` might already have `.git/` from the upstream
  repo when the runtime container starts. `InitServiceGit` is
  idempotent — `test -d .git` skips init, and `git config user.*` just
  (re-)sets identity. Safe.

- **Local-mode `strategy=git-push`: no auto-init**. ZCP deliberately
  does **not** initialize git in the user's working directory. Three
  reasons:
  1. **Identity leakage**: auto-init would write `agent@zerops.io` into
     the user's `.git/config`, polluting their personal commit history
     until they override it.
  2. **Branch convention collision**: `git init -b main` overrides
     local git config defaults (some users use `master`, `trunk`, or
     per-repo `init.defaultBranch`).
  3. **Monorepo / parent-repo conflict**: `git init` in a subfolder of
     an existing repo creates a nested repo, which is a UX gotcha
     (submodule-ish confusion without the submodule wiring).

  Container-mode `InitServiceGit` doesn't hit any of these because
  `/var/www/.git/` is ZCP-internal plumbing for the deploy pipeline,
  never surfaced to the user as their commit history. The symmetry is
  intentional: ZCP owns container-side git; user owns local-side git.

  Default `zerops_deploy` strategy uses `zcli --no-git` and doesn't
  need any git state in the user's dir — verified against
  `zcli@v1.0.61` source (`handler_archiveGitFiles.go:26-28`, 86-98:
  `createNoGitArchive` walks filesystem directly, honors
  `.deployignore`). Only `strategy=git-push` requires init + commit,
  and the atom (Fix 5) tells the agent to ask the user rather than
  initialize on their behalf; `handleLocalGitPush` pre-flight error
  remains as the hard fallback.
