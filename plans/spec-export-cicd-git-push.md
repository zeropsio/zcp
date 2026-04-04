# Spec: Export Flow, CI/CD Enhancement, and `git-push` Deploy Strategy

**Date**: 2026-04-04
**Status**: SPECIFICATION — ready for implementation
**Audience**: Implementation team (standalone document, all context included)

---

## 1. Executive Summary

ZCP needs three interconnected capabilities:

1. **`strategy="git-push"` on `zerops_deploy`** — push code from a Zerops container to an external git remote (GitHub/GitLab), reusing the existing deploy infrastructure
2. **Export workflow** — create a git repository + import.yaml from running infrastructure for `buildFromGit`
3. **CI/CD workflow rewrite** — complete auth model, GUI walkthrough, error handling

These share a common foundation: **git operations on Zerops containers**. Today, `zerops_deploy` handles git internally for `zcli push` (to Zerops). The same mechanism needs to support `git push` (to GitHub/GitLab).

---

## 2. Architecture: Two Push Targets, Shared Core

### 2.1 Current State

`zerops_deploy` internally builds one SSH command (`deploy_ssh.go:160-191`):

```
zcli login {TOKEN}                    ← AUTH: Zerops token
cd /var/www
(test -d .git || git init -q -b main) ← SHARED: git init
git config user.email ... name ...    ← SHARED: git config
git add -A                            ← SHARED: stage
(diff-index --quiet HEAD || commit)   ← SHARED: commit
zcli push --service-id {ID} [-g]     ← PUSH: to Zerops
```

### 2.2 Target State

Two strategies with different responsibility splits:

```
  DEFAULT (zcli push)                    git-push
  ────────────────────                   ──────────────────────
  ZCP does EVERYTHING:                   SPLIT RESPONSIBILITY:
  
  zcli login {TOKEN}                     LLM does commit:
  cd /var/www                              ssh {h} "cd /var/www &&
  git init (conditional)                     git add -A &&
  git config user.*                          git commit -m '{meaningful msg}'"
  git add -A
  git commit -m 'deploy'                 ZCP does push:
  zcli push --service-id                   .netrc from $GIT_TOKEN
                                           git init (conditional, fallback)
                                           git remote add/set-url
                                           git push -u origin {branch}
                                           rm .netrc
```

**Why different splits**: For zcli push, the commit is a technical necessity with throwaway message ("deploy"). For git-push, the commit is the DELIVERABLE — it shows up in public repo history. The LLM has the context to write meaningful messages; the tool doesn't.

### 2.3 Files Affected

| File | Change |
|------|--------|
| `internal/ops/deploy_ssh.go` | Extract shared core, add `buildGitPushCommand()` |
| `internal/ops/deploy_ssh_test.go` | Tests for git-push command building |
| `internal/tools/deploy_ssh.go` | Add `Strategy`, `RemoteUrl`, `Branch` to `DeploySSHInput` |
| `internal/ops/deploy_common.go` | Add `GitPushResult` return type |
| `internal/content/workflows/export.md` | Update to use `zerops_deploy strategy="git-push"` |
| `internal/content/workflows/cicd.md` | Update to use `zerops_deploy strategy="git-push"` |

---

## 3. `zerops_deploy strategy="git-push"` — Detailed Spec

### 3.1 New Input Parameters

Add to `DeploySSHInput` struct in `tools/deploy_ssh.go`:

```go
type DeploySSHInput struct {
    // Existing fields (unchanged):
    SourceService string `json:"sourceService,omitempty"`
    TargetService string `json:"targetService"`
    Setup         string `json:"setup,omitempty"`
    WorkingDir    string `json:"workingDir,omitempty"`
    IncludeGit    bool   `json:"includeGit,omitempty"`
    
    // New fields for git-push strategy:
    Strategy  string `json:"strategy,omitempty"  jsonschema:"Deploy strategy. Omit for default (zcli push to Zerops). Set to 'git-push' to push committed code to an external git remote (GitHub/GitLab). LLM should commit changes BEFORE calling git-push."`
    RemoteUrl string `json:"remoteUrl,omitempty" jsonschema:"Git remote URL (HTTPS). Required for strategy=git-push on first push. Omit on subsequent pushes if remote already configured."`
    Branch    string `json:"branch,omitempty"    jsonschema:"Git branch name. Default: main."`
}
```

**Note: No `CommitMessage` parameter.** The LLM commits separately via SSH before calling git-push. The LLM has context about what changed and writes meaningful commit messages. The tool only handles auth + push. Exception: on init (no commits exist), the tool does a fallback `git add -A && git commit -m 'initial commit'`.

### 3.2 Validation Rules

| Condition | Action |
|-----------|--------|
| `strategy=""` or omitted | Default path: zcli push (existing behavior, zero changes) |
| `strategy="git-push"` + no `remoteUrl` | Check if remote already configured on container. If not → error `MISSING_REMOTE_URL` |
| `strategy="git-push"` + `remoteUrl` provided | Set/update remote before push |
| `strategy="git-push"` on stage/prod (no `.git/`, remote has history) | Error `GIT_HISTORY_CONFLICT` (see §3.5) |
| `strategy="git-push"` + `$GIT_TOKEN` not in env | Error `GIT_TOKEN_MISSING` |

### 3.3 SSH Command for `git-push` Strategy

New function `buildGitPushCommand()` in `deploy_ssh.go`:

```bash
# Auth: .netrc from $GIT_TOKEN env var (never in command args)
echo "machine {host} login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc

cd {workingDir}

# Init fallback: only if .git doesn't exist (first-time setup)
(test -d .git || git init -q -b {branch})
git config user.email {email} && git config user.name {name}

# Remote setup (idempotent: add or update)
# Only if remoteUrl provided; skip if omitted (use existing remote)
git remote add origin {remoteUrl} 2>/dev/null || git remote set-url origin {remoteUrl}

# Init fallback commit: only if NO commits exist yet
# (LLM normally commits before calling git-push, this handles the first-ever push)
git rev-parse HEAD >/dev/null 2>&1 || (git add -A && git commit -q -m 'initial commit')

# Push
git push -u origin {branch}

# Cleanup auth
rm -f ~/.netrc
```

**Key design**: The tool does NOT commit changes. The LLM commits before calling git-push:
```bash
ssh {hostname} "cd /var/www && git add -A && git commit -m 'feat: add user authentication'"
```
Then calls:
```
zerops_deploy targetService="{hostname}" strategy="git-push"
```

The fallback commit (`git rev-parse HEAD || git add -A && git commit -m 'initial commit'`) exists only for the init case when there are no commits at all (fresh git init).

**Host detection** from `remoteUrl`:
- `https://github.com/...` → `machine github.com`
- `https://gitlab.com/...` → `machine gitlab.com`
- Custom: parse hostname from URL

### 3.4 Return Value

For git-push, return a different result than zcli push:

```go
type GitPushResult struct {
    Status    string `json:"status"`    // "PUSHED" or "NOTHING_TO_PUSH"
    RemoteUrl string `json:"remoteUrl"` // The remote URL used
    Branch    string `json:"branch"`    // Branch pushed to
    Message   string `json:"message"`   // Human-readable summary
}
```

No `MonitorHint` (no async build to poll) unless CI/CD is configured — then hint: "If CI/CD is configured, a build may start automatically."

### 3.5 Error Handling

| Error Code | Condition | Message | Suggestion |
|-----------|-----------|---------|------------|
| `GIT_TOKEN_MISSING` | `$GIT_TOKEN` not set on container | "GIT_TOKEN env var not found on service" | "Set project token: zerops_env action=set project=true variables=[\"GIT_TOKEN=...\"]" |
| `MISSING_REMOTE_URL` | No `remoteUrl` param + no remote configured | "No git remote configured and remoteUrl not provided" | "Provide remoteUrl parameter or configure remote on container" |
| `GIT_HISTORY_CONFLICT` | `.git/` doesn't exist but remote has commits (detected via `git ls-remote`) | "Cannot push: container has no git history but remote has existing commits" | "Push from a dev service (which preserves .git/) or clone the repo locally" |
| `PUSH_REJECTED` | `git push` returns non-fast-forward | "Push rejected: remote has commits not present locally" | "Pull changes first or push from a service with complete git history" |
| `AUTH_FAILED` | `.netrc` auth rejected by remote | "Git authentication failed" | "Verify GIT_TOKEN: GitHub fine-grained PAT with Contents: Read and write permission" |

**Detection of `GIT_HISTORY_CONFLICT`**: Before push, if `.git/` doesn't exist, run `git ls-remote {remoteUrl}` to check if remote has any refs. If yes → error. If empty → safe to init + push.

### 3.6 Interaction with Existing Deploy

**No breaking changes.** When `strategy` is omitted or empty, behavior is identical to current code. The `buildSSHCommand` function routes based on strategy:

```go
func buildSSHCommand(authInfo auth.Info, targetServiceID, workingDir, setup string, includeGit bool, id GitIdentity, strategy, remoteUrl, branch string) string {
    if strategy == "git-push" {
        return buildGitPushCommand(workingDir, remoteUrl, branch, id)
    }
    // ... existing zcli push logic (unchanged)
}
```

---

## 4. Git Authentication Model

### 4.1 Token Setup

**Project-level env var** `GIT_TOKEN` — set once, visible to all services:

```
zerops_env action="set" project=true variables=["GIT_TOKEN=ghp_xxxxx"]
```

User provides token in conversation. Agent calls `zerops_env` to store it. Token persists across deploys (env vars are persistent, independent of container lifecycle).

### 4.2 .netrc on Container

Before each git-push, ZCP creates `.netrc` and removes it after push. This happens **inside the SSH command**, not as separate steps:

```bash
echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc
# ... git operations ...
rm -f ~/.netrc
```

**Why .netrc**:
- Token NOT in git remote URL (not visible via `git remote -v`)
- Token NOT in command-line arguments (not visible via `ps aux` — addresses audit findings C1/C2)
- Standard mechanism, git reads it natively
- Ephemeral: created before push, deleted after, lost on deploy

### 4.3 Token Lifecycle

| Event | Token state |
|-------|-------------|
| User provides token | Agent stores via `zerops_env` as project env var |
| git-push needed | ZCP reads `$GIT_TOKEN` from container env, creates `.netrc` |
| Push completes | `.netrc` deleted |
| Service deploys (new container) | `.netrc` gone. `$GIT_TOKEN` env var still set. |
| Next git-push | `.netrc` recreated from `$GIT_TOKEN` |
| User removes token | `zerops_env action="delete" project=true variables=["GIT_TOKEN"]` |

### 4.4 GitLab Variant

GitLab uses the same `.netrc` pattern with `oauth2` as username:

```bash
echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc
```

No code difference — just different host in `.netrc`. ZCP parses host from `remoteUrl`.

### 4.5 Security Constraints

| Rule | Enforcement |
|------|-------------|
| Token never in command-line args | `.netrc` pattern (not `git push https://TOKEN@...`) |
| Token never in git config | `.netrc` pattern (not `git remote add https://TOKEN@...`) |
| Token removed after push | `rm -f ~/.netrc` at end of SSH command |
| Token never echoed by agent | Agent calls `zerops_env` to set, never displays value |
| Auth failure doesn't leak token | Error classification strips token from SSH output (existing `classifySSHError` pattern) |

---

## 5. Container Git State and Deploy Interactions

### 5.1 `.git/` Persistence Rules

| Deploy method | `.git/` after deploy | Why |
|--------------|---------------------|-----|
| Self-deploy dev (`deployFiles: [.]`, `-g` flag) | **Preserved** | `-g` includes `.git/`, `[.]` includes everything |
| Cross-deploy dev→stage | **Lost** on stage | Stage gets only `deployFiles` from build |
| CI/CD Actions (GitHub → zcli push) | **Lost** | Build runs on GitHub runner, not container |
| CI/CD webhook (Zerops clones) | **Lost** | Zerops clones internally, container gets artifacts |
| `buildFromGit` import | **Lost** | One-time clone + build |

### 5.2 Strategy Compatibility Matrix

| Scenario | Works? | Why |
|----------|--------|-----|
| Dev service → git-push | ✅ | `.git/` preserved across deploys (self-deploy + `-g`) |
| Dev → git-push → dev-push → git-push | ✅ | zcli push ignores remotes; git push syncs all commits |
| Dev → git-push → CI/CD Actions deploy → git-push | ✅ | Dev `deployFiles: [.]` preserves `.git/` |
| Stage after CI/CD deploy → git-push | ❌ | `.git/` lost, `GIT_HISTORY_CONFLICT` |
| Prod after buildFromGit → git-push | ❌ | `.git/` lost, `GIT_HISTORY_CONFLICT` |
| Multiple services, mixed strategies | ✅ | Each service independent |

### 5.3 Verified Switching Scenarios

**Scenario A: dev-push → git-push**
1. Container has `.git/` (from previous zcli push deploys), no remote
2. `git-push` adds remote, commits, pushes → all existing history goes to GitHub
3. Both strategies coexist: zcli push ignores remote, git push uses it
4. **Result**: ✅ Works

**Scenario B: git-push → dev-push → git-push**
1. After git-push: `.git/` has remote "origin"
2. Dev-push (zcli push): creates new "deploy" commit, ignores remote
3. Next git-push: sends all commits (including "deploy") to GitHub
4. **Result**: ✅ Works (history has mix of "deploy" and meaningful messages)

**Scenario C: CI/CD deploy → git-push on dev**
1. CI/CD (Actions/webhook) triggers build → new container on dev
2. Dev has `deployFiles: [.]` → `.git/` preserved with remote
3. git-push: `.netrc` → commit → push
4. **Result**: ✅ Works

**Scenario D: CI/CD deploy → git-push on stage**
1. Stage has specific `deployFiles` (e.g., `[dist, node_modules]`) → `.git/` LOST
2. git-push: no `.git/`, init creates fresh repo
3. Push fails: `GIT_HISTORY_CONFLICT` (remote has history, local doesn't)
4. **Result**: ❌ Expected failure. Tool returns clear error with suggestion.

**Scenario E: After buildFromGit import → git-push**
1. Same as D: `.git/` lost after build
2. **Result**: ❌ Expected failure. User should push from dev or locally.

### 5.4 Design Principle

**`git-push` is designed for dev services.** Dev services use `deployFiles: [.]` which preserves `.git/`. Stage and prod services lose `.git/` on deploy and should not be push sources. The tool enforces this via `GIT_HISTORY_CONFLICT` detection.

---

## 6. Export Workflow

### 6.1 Overview

User says: "Create a repo from service X for buildFromGit"

Agent auto-detects state, asks intent, executes flow.

### 6.2 Decision Map

**Auto-detection** (no user input needed):
```
zerops_discover service="{hostname}" includeEnvs=true
ssh {hostname} "cd /var/www && git remote -v 2>/dev/null; test -d .git && echo GIT || echo NOGIT"
```

| State | .git | Remote | Next step |
|-------|------|--------|-----------|
| **S0** | No | No | Full setup: token + init + push |
| **S1** | Yes | No | Add remote + push |
| **S2** | Yes | Yes | Verify, skip to import.yaml generation |

**User intent** (one question):
- **A) CI/CD only** — push to git + set up automatic deploy
- **B) buildFromGit only** — push to git + generate import.yaml
- **C) Both** (default) — push + import.yaml + CI/CD

### 6.3 Flow Steps

```
Step 1: DISCOVER
  zerops_discover + SSH git check + zerops_export
  → Classify state (S0/S1/S2), ask intent (A/B/C)

Step 2: PREPARE (S0/S1 only)
  a) GIT_TOKEN: User provides token → zerops_env project=true
  b) Repo: User provides URL or creates new (gh repo create)
  c) File prep: LLM writes .gitignore, verifies/generates zerops.yml via SSHFS/SSH

Step 3: INITIAL PUSH
  zerops_deploy targetService="{hostname}" strategy="git-push"
                remoteUrl="{url}"
  → ZCP handles: .netrc, git init (fallback), remote, fallback commit, push, cleanup
  → Fallback commit ("initial commit") used because no prior commits exist

Step 4: GENERATE IMPORT.YAML (Intent B or C)
  zerops_export → merge export API + discover data
  → Add buildFromGit: {repoUrl} to runtime services
  → Present to user for review

Step 5: CI/CD SETUP (Intent A or C)
  GitHub Actions:
    a) LLM writes .github/workflows/deploy.yml via SSHFS/SSH
    b) LLM commits: ssh {h} "cd /var/www && git add -A && git commit -m 'ci: add deploy workflow'"
    c) zerops_deploy strategy="git-push"  ← push only (LLM already committed)
    d) User sets ZEROPS_TOKEN on GitHub (guidance)
  
  GitHub/GitLab webhook:
    a) Agent guides user through GUI OAuth (step-by-step instructions)
    b) User connects repo + sets trigger in Zerops dashboard

Step 6: VERIFY + CLOSE
  zerops_events → confirm build triggered (if CI/CD)
  Present summary: repo URL, import.yaml, CI/CD status
```

### 6.4 Workflow Conductor Updates

`internal/content/workflows/export.md` — update sections to use `zerops_deploy strategy="git-push"` instead of raw SSH commands. Sections: discover, prepare, generate, cicd, close.

`internal/content/workflows/cicd.md` — update "Push Initial Code" and subsequent push sections to use `zerops_deploy strategy="git-push"`.

---

## 7. CI/CD Workflow Updates

### 7.1 Replace Raw SSH with Tool Calls

**Current** (cicd.md — 6+ SSH commands):
```
ssh {dev} 'echo "machine github.com ..." > ~/.netrc'
ssh {dev} "cd /var/www && git init -q -b main"
ssh {dev} "cd /var/www && git config user.email ..."
ssh {dev} "cd /var/www && git remote add origin {url}"
ssh {dev} "cd /var/www && git add -A && git commit -m 'initial' && git push"
```

**After** (1 tool call — for init push; subsequent pushes: LLM commits first, then tool pushes):
```
zerops_deploy targetService="{dev}" strategy="git-push"
              remoteUrl="{url}"
```

### 7.2 Commit-then-Push Pattern in Workflow Guidance

Workflow conductors (export.md, cicd.md, bootstrap.md) must clearly communicate the two-step pattern to the LLM:

**Template for workflow guidance (every push section):**
```
### Push changes to GitHub

1. Commit your changes with a descriptive message:
   ssh {hostname} "cd /var/www && git add -A && git commit -m '{describe what changed and why}'"

2. Push to remote:
   zerops_deploy targetService="{hostname}" strategy="git-push"
```

**For init push (first time, no prior commits):**
```
### Initial push to GitHub

Push code to the repository. The tool handles git init, remote setup, and initial commit:
   zerops_deploy targetService="{hostname}" strategy="git-push"
                 remoteUrl="{url}"
```

The distinction must be clear in guidance: init push = one tool call (tool does fallback commit). Subsequent pushes = LLM commits first, then tool pushes.

### 7.3 Remaining LLM Responsibilities

Even with `git-push` in ZCP, the LLM still handles:

| Task | Why LLM, not tool |
|------|-------------------|
| Write `.gitignore` | Framework-specific content (Node vs PHP vs Go) |
| Write/verify `zerops.yml` | App-specific build/deploy configuration |
| Write `.github/workflows/deploy.yml` | CI/CD pipeline configuration |
| Guide user through GUI OAuth | Browser-based flow, cannot be automated |
| Guide user to set GitHub secrets | GitHub UI, cannot be automated |

### 7.3 Typical CI/CD Flow with git-push

```
1. [User] Provides GIT_TOKEN
   [ZCP]  zerops_env project=true variables=["GIT_TOKEN=..."]

2. [LLM]  Write .gitignore via SSHFS/SSH                       ← framework-specific

3. [ZCP]  zerops_deploy strategy="git-push"                     ← initial push
          remoteUrl="..."                                         (fallback commit: 'initial commit')

4. [LLM]  Write .github/workflows/deploy.yml via SSHFS/SSH     ← CI/CD config

5. [LLM]  ssh {dev} "cd /var/www && git add -A &&              ← LLM commits with context
           git commit -m 'ci: add GitHub Actions deploy workflow'"
   [ZCP]  zerops_deploy strategy="git-push"                     ← push only

6. [LLM]  Guide user: set ZEROPS_TOKEN on GitHub               ← GitHub UI

7. [ZCP]  zerops_events → verify build triggered                ← verification
```

**Responsibility split visible**: Step 3 is init (tool handles commit fallback). Step 5 is ongoing (LLM commits with meaningful message, tool just pushes).

---

## 8. What Already Exists (Implemented)

The following were implemented in commit `3cb7c8e` and are available:

| Component | File | Status |
|-----------|------|--------|
| `Mode` field in `ServiceInfo` | `ops/discover.go` | ✅ Done |
| Export API in `platform.Client` | `platform/client.go`, `zerops_ops.go` | ✅ Done |
| `ops/export.go` (ExportProject, ExportService) | `ops/export.go` | ✅ Done |
| `ops/git_status.go` (CheckGitStatus, parseGitStatus) | `ops/git_status.go` | ✅ Done |
| `zerops_export` MCP tool | `tools/export.go` | ✅ Done |
| `export.md` workflow conductor | `content/workflows/export.md` | ✅ Done (needs update for git-push) |
| `cicd.md` rewrite | `content/workflows/cicd.md` | ✅ Done (needs update for git-push) |
| E2E tests for export | `e2e/export_test.go`, `e2e/export_multi_test.go` | ✅ Done (7 tests passing) |
| `ExportState` in workflow state | `workflow/state.go` | ✅ Done (struct exists, not yet used) |

---

## 9. DeployStrategy Integration — Rename `ci-cd` to `push-git`

### 9.1 Conceptual Distinction

There are TWO separate concepts that use the word "strategy":

| Concept | Name | Where | What it means |
|---------|------|-------|---------------|
| DeployStrategy | `push-git` | `ServiceMeta.DeployStrategy` | **Long-term decision** — "I push code to git" |
| Tool action param | `git-push` | `zerops_deploy strategy="git-push"` | **Action** — "do a git push to remote" |

These intentionally differ in naming. The strategy follows the `push-*` pattern (`push-dev`, `push-git`). The tool parameter describes the git operation.

**Connection**: If `DeployStrategy = "push-git"`, the workflow guidance (extracted from `deploy-push-git` section in deploy.md) tells the LLM to commit via SSH, then call `zerops_deploy strategy="git-push"` for pushing.

### 9.2 Strategy Taxonomy — 3 Strategies (not 4)

The old `ci-cd` strategy is **replaced by `push-git`**. CI/CD is a configuration on top of push-git, not a separate deployment mechanism. Both `ci-cd` and the proposed `git-push` used the same underlying action (push to git remote) — they only differed in what happens after the push.

**After implementation there will be 3 strategies:**

| Strategy | Description | How deploy works | When to use |
|----------|------------|-----------------|-------------|
| `push-dev` | SSH self-deploy from dev container | `zerops_deploy` (zcli push) | Prototyping, fast iteration |
| `push-git` | Push to git remote (optional CI/CD) | LLM commits → `zerops_deploy strategy="git-push"` | Team dev, CI/CD, export |
| `manual` | User manages everything | Direct `zerops_deploy` | Experienced users, custom CI |

**`push-git` absorbs `ci-cd`**: Whether CI/CD is configured (Actions/webhook) or not is an observable property of the service, not a separate strategy. `push-git` means: "I push to git." That's it. CI/CD and export are **optional follow-ups**, not required steps — the user may just want code on GitHub and nothing else.

**CI/CD state tracking**: Not tracked explicitly in ServiceMeta. The LLM observes actual state (zerops_events shows build triggered or not). If precision becomes important later, add `CICDConfigured bool` to ServiceMeta.

**Migration**: `ReadServiceMeta` silently maps old `"ci-cd"` → `"push-git"`:
```go
if meta.DeployStrategy == "ci-cd" {
    meta.DeployStrategy = StrategyPushGit
}
```

### 9.3 Files That Must Be Updated for Strategy Integration

| # | File | What to change | Why |
|---|------|---------------|-----|
| S1 | `workflow/service_meta.go:13-17` | Replace `StrategyCICD = "ci-cd"` with `StrategyPushGit = "push-git"` | Rename strategy constant |
| S2 | `workflow/service_meta.go` (ReadServiceMeta) | Add `ci-cd` → `push-git` migration in reader | Backward compat for existing ServiceMeta files |
| S3 | `tools/workflow_strategy.go:15-19` | Replace `workflow.StrategyCICD: true` with `workflow.StrategyPushGit: true` in `validStrategies` | Accept new name, reject old |
| S4 | `tools/workflow_strategy.go:27` | Update error message: `"Valid strategies: push-dev, push-git, manual"` | User-facing validation message |
| S5 | `tools/workflow_strategy.go:72-73` | Replace `StrategyCICD` with `StrategyPushGit` in next-step hint | Correct routing after strategy selection |
| S6 | `tools/workflow_strategy.go:149-153` | Replace ci-cd section in `buildStrategySelectionResponse()` with push-git description | Strategy selection prompt |
| S7 | `workflow/deploy_guidance.go:11-15` | Replace `StrategyCICD: "deploy-ci-cd"` with `StrategyPushGit: "deploy-push-git"` in `StrategyToSection` | Maps strategy to correct deploy.md section |
| S8 | `workflow/deploy_guidance.go:18-22` | Replace `StrategyCICD: "auto-deploy..."` with `StrategyPushGit: "push to git remote (optional CI/CD)"` in `strategyDescriptions` | One-line description for guidance |
| S9 | `workflow/router.go:153-167` | Replace `StrategyCICD` case with `StrategyPushGit` — offer deploy + cicd + export workflows | Router offers all git-related workflows |
| S10 | `workflow/bootstrap_guide_assembly.go:107-115` | Replace ci-cd with push-git in strategy list and next-step guidance | Bootstrap close guidance |
| S11 | `server/instructions_orientation.go:220-223` | Replace `StrategyCICD` case with `StrategyPushGit` | MCP instructions |
| S12 | `tools/workflow_cicd_context.go:23` | Replace `StrategyCICD` with `StrategyPushGit` in filter | CI/CD context targets push-git services |
| S13 | `content/workflows/deploy.md:329-346` | Replace `<section name="deploy-ci-cd">` with `<section name="deploy-push-git">` | Deploy guidance section |
| S14 | `content/workflows/bootstrap.md` | Update "push-dev, ci-cd, or manual" → "push-dev, push-git, or manual" | Bootstrap close text |

---

## 10. What Needs to Be Implemented — Complete Task List

### Phase 1: `git-push` Action in Deploy Tool

Core implementation — makes `zerops_deploy strategy="git-push"` work.

| Task | File | Description |
|------|------|-------------|
| 1.1 | `ops/deploy_git_push.go` (new) | Add `buildGitPushCommand()`: trap-based .netrc cleanup, `shellQuote(remoteUrl)`, init fallback, remote setup, push |
| 1.2 | `ops/deploy_git_push.go` | Add `GIT_HISTORY_CONFLICT` detection: `git ls-remote` before init if `.git/` missing |
| 1.3 | `ops/deploy_git_push.go` | Add `parseGitHost()`: extract hostname from remoteUrl (GitHub, GitLab, custom, SSH-style) |
| 1.4 | `ops/deploy_common.go` | Add `GitPushResult` type + `DeploySSHOptions` struct (replaces 10+ param function) |
| 1.5 | `ops/deploy_git_push_test.go` | Tests for `buildGitPushCommand()` — all param combinations, error cases, host parsing |
| 1.6 | `tools/deploy_ssh.go` | Add `Strategy`, `RemoteUrl`, `Branch` to `DeploySSHInput`. Route to git-push in handler. Skip `pollDeployBuild` for git-push. |
| 1.7 | `tools/deploy_ssh.go` | Validation: git-push requires `remoteUrl` or existing remote; validate strategy enum |
| 1.8 | `platform/errors.go` | Add error codes: `GIT_TOKEN_MISSING`, `GIT_HISTORY_CONFLICT`, `PUSH_REJECTED`, `GIT_AUTH_FAILED` |

### Phase 2: Strategy Rename `ci-cd` → `push-git`

Replaces `StrategyCICD` with `StrategyPushGit` across the codebase.

| Task | File | Description |
|------|------|-------------|
| 2.1 | `workflow/service_meta.go` | Replace `StrategyCICD = "ci-cd"` with `StrategyPushGit = "push-git"` (S1). Add migration in `ReadServiceMeta` (S2). |
| 2.2 | `tools/workflow_strategy.go` | Replace `StrategyCICD` with `StrategyPushGit` in `validStrategies` (S3), error message (S4), next-step hint (S5), `buildStrategySelectionResponse()` (S6) |
| 2.3 | `workflow/deploy_guidance.go` | Replace `StrategyCICD` with `StrategyPushGit` in `StrategyToSection` (S7) and `strategyDescriptions` (S8) |
| 2.4 | `workflow/router.go` | Replace `StrategyCICD` case with `StrategyPushGit` — offer deploy + cicd + export workflows (S9) |
| 2.5 | `workflow/bootstrap_guide_assembly.go` | Replace ci-cd with push-git in strategy list and next-step guidance (S10) |
| 2.6 | `server/instructions_orientation.go` | Replace `StrategyCICD` case with `StrategyPushGit` (S11) |
| 2.7 | `tools/workflow_cicd_context.go` | Replace `StrategyCICD` filter with `StrategyPushGit` (S12) |
| 2.8 | Tests | Update all `StrategyCICD` → `StrategyPushGit` references in: service_meta_test, router_test, deploy_guidance_test, deploy_test, bootstrap_outputs_test, workflow_test, cicd_context_test, instructions_test |

### Phase 3: Update Workflow Conductors

Updates guidance in workflow .md files to use the new tool and strategy.

| Task | File | Description |
|------|------|-------------|
| 3.1 | `content/workflows/deploy.md` | Replace `<section name="deploy-ci-cd">` with `<section name="deploy-push-git">` with commit-then-push steps (S13) |
| 3.2 | `content/workflows/export.md` | Replace raw SSH git commands with commit-then-push pattern using `zerops_deploy strategy="git-push"`. Add to close section: suggest `push-git` strategy if not set. |
| 3.3 | `content/workflows/cicd.md` | Replace raw SSH git commands with commit-then-push pattern |
| 3.4 | `content/workflows/bootstrap.md` | Update close section: replace "ci-cd" with "push-git" in strategy list (S14) |

### Phase 4: Testing

| Task | Scope | Description |
|------|-------|-------------|
| 4.1 | Unit tests | `buildGitPushCommand` output for all parameter combinations |
| 4.2 | Unit tests | Error detection: `GIT_HISTORY_CONFLICT`, `GIT_TOKEN_MISSING` |
| 4.3 | Unit tests | `parseGitHost` for GitHub, GitLab, custom domains, SSH-style URLs |
| 4.4 | Unit tests | Strategy rename: router offers deploy+cicd+export for push-git |
| 4.5 | Unit tests | Migration: `ReadServiceMeta` maps `"ci-cd"` → `"push-git"` |
| 4.6 | Integration test | MCP tool call with `strategy="git-push"` + mock SSH |
| 4.7 | E2E test | `git-push` initial push (fallback commit) to test GitHub repo |
| 4.8 | E2E test | LLM commits first, tool pushes, verify message on GitHub |

---

## 11. Cross-Workflow Impact Summary

### How each workflow uses git-push

| Workflow | Uses git-push? | When | Example |
|----------|:-------------:|------|---------|
| **Bootstrap** | Optional | After deploy, user wants code in git. Close step offers push-git as strategy. | `zerops_workflow action="strategy" strategies={"appdev":"push-git"}` |
| **Deploy** | Yes (if strategy=push-git) | Deploy guidance uses `zerops_deploy strategy="git-push"` | `deploy-push-git` section in deploy.md |
| **Recipe** | No | Uses GitHub API (PR creation) | Stays `sync push` |
| **CI/CD** | Yes | Initial push + subsequent pushes (follow-up workflow for push-git) | `zerops_deploy strategy="git-push" ...` |
| **Export** | Yes | Push existing code to GitHub (follow-up workflow for push-git) | `zerops_deploy strategy="git-push" ...` |

### Strategy → Workflow → Tool mapping

| DeployStrategy | Workflows offered | What LLM does | What tool does |
|---------------|-------------------|---------------|----------------|
| `push-dev` | deploy | Edit via SSHFS | `zerops_deploy` (zcli push) |
| `push-git` | deploy, cicd, export | Edit → SSH commit → push | `zerops_deploy strategy="git-push"` |
| `manual` | none (direct) | Edit → direct deploy | `zerops_deploy` (zcli push, no guided flow) |

### When strategy gets selected

| Trigger | What happens |
|---------|-------------|
| Bootstrap close | Transition message lists 3 strategies. User/LLM picks via `action="strategy"`. |
| Adoption complete | Same as bootstrap close — strategy selection prompt. |
| First deploy without strategy | `buildStrategySelectionResponse()` prompts for strategy selection. |
| User changes mind | `zerops_workflow action="strategy" strategies={"appdev":"push-git"}` anytime. |
| Router query | `action="route"` returns flow offerings based on current strategy. |

### What the LLM does vs what ZCP does (after implementation)

| Operation | Before | After | Who decides WHAT |
|-----------|--------|-------|-----------------|
| git init | LLM/SSH | **ZCP** (inside git-push, fallback) | ZCP (conditional) |
| git config | LLM/SSH | **ZCP** (inside git-push) | ZCP (from auth.Info) |
| git remote add | LLM/SSH | **ZCP** (inside git-push) | ZCP (from remoteUrl param) |
| .netrc creation | LLM/SSH | **ZCP** (inside git-push) | ZCP (from $GIT_TOKEN) |
| git add + commit | LLM/SSH | **LLM/SSH** (unchanged!) | **LLM** — knows context, writes meaningful messages |
| git push | LLM/SSH | **ZCP** (inside git-push) | ZCP (safe auth + error handling) |
| .netrc cleanup | LLM/SSH | **ZCP** (inside git-push) | ZCP (automatic) |
| .gitignore write | LLM/SSHFS | LLM/SSHFS (unchanged) | LLM (framework-specific) |
| zerops.yml write | LLM/SSHFS | LLM/SSHFS (unchanged) | LLM (app-specific) |
| .github/workflows write | LLM/SSH | LLM/SSH (unchanged) | LLM (CI/CD config) |
| GitHub OAuth setup | User/GUI | User/GUI (unchanged) | User (browser) |
| GitHub secrets setup | User/GUI | User/GUI (unchanged) | User (browser) |

**Key principle**: `git add + commit` stays with LLM because the LLM has semantic context (what changed, why). The tool handles the mechanical/security parts (auth, push, remote, cleanup).

---

## 12. New `deploy-push-git` Section for deploy.md

This section **replaces** `deploy-ci-cd` in `content/workflows/deploy.md`:

```markdown
<section name="deploy-push-git">
### Push-Git Deploy Strategy

Push committed code from the dev container to an external git repository (GitHub/GitLab).

**First time setup** (once per service):

1. Get a GitHub/GitLab personal access token from the user:
   "I need a token to push code. GitHub: Settings → Developer settings → Fine-grained tokens → Contents: Read and write. GitLab: Access Tokens → write_repository."

2. Store it as project env var:
   `zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]`

3. Commit code:
   `ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"`

4. Push with remote URL:
   `zerops_deploy targetService="{devHostname}" strategy="git-push" remoteUrl="{repoUrl}"`

**Subsequent deploys:**

1. Make your code changes (via SSHFS mount or SSH)

2. Commit with a descriptive message:
   `ssh {devHostname} "cd /var/www && git add -A && git commit -m '{what changed and why}'"`

3. Push to remote:
   `zerops_deploy targetService="{devHostname}" strategy="git-push"`

4. If CI/CD is configured:
   Build triggers automatically. Monitor: `zerops_events serviceHostname="{stageHostname}"`

5. If no CI/CD:
   Deploy to stage manually: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`

**No `zcli push` needed.** Code goes to GitHub via git push, not to Zerops directly.

**To switch strategy:** `zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}`
**Set up CI/CD:** `zerops_workflow action="start" workflow="cicd"`
**Export with import.yaml:** `zerops_workflow action="start" workflow="export"`
</section>
```

---

## 13. Open Questions

| # | Question | Impact | Recommended Resolution |
|---|---------|--------|----------------------|
| 1 | Should `git-push` also work in local mode (`DeployLocalInput`)? | Local users might want to push from their machine | Yes — add to `DeployLocalInput` too. Simpler: just `git push` locally, no SSH needed. |
| 2 | Should tool support `--force` push? | Recovery from `GIT_HISTORY_CONFLICT` | No by default. Add `force` boolean param but document it as destructive. |
| 3 | Should tool auto-detect GitHub vs GitLab from URL? | `.netrc` host line | Yes — parse hostname from URL. Fall back to `github.com` if can't parse. |
| 4 | Pre-push hook from cicd.md — should it be part of git-push? | Safety | No — keep as separate LLM guidance. It's optional and opinionated. |
| 5 | Should `ExportState` be used (stateful workflow) or stay immediate? | Workflow complexity | Stay immediate for now. Stateful only if iterative export needs session tracking. |
| 6 | Should the tool warn if there are uncommitted changes? | UX — LLM forgot to commit before push | Yes — if `git status --porcelain` shows changes, return warning (not error). Push proceeds with whatever is committed. |

---

## 14. Test Plan

### Unit Tests (deploy_ssh_test.go)

```go
func TestBuildGitPushCommand_Basic(t *testing.T)              // push with remoteUrl + branch
func TestBuildGitPushCommand_ExistingRemote(t *testing.T)     // remote already set, remoteUrl updates it
func TestBuildGitPushCommand_NoRemoteUrl(t *testing.T)        // no remoteUrl → uses existing remote (no remote set-url)
func TestBuildGitPushCommand_DefaultBranch(t *testing.T)      // branch defaults to "main"
func TestBuildGitPushCommand_CustomBranch(t *testing.T)       // branch="develop"
func TestBuildGitPushCommand_GitLabHost(t *testing.T)         // gitlab.com in .netrc
func TestBuildGitPushCommand_CustomHost(t *testing.T)         // self-hosted git
func TestBuildGitPushCommand_NetrcCleanup(t *testing.T)       // .netrc removed at end of command
func TestBuildGitPushCommand_FallbackCommit(t *testing.T)     // git rev-parse fails → fallback initial commit
func TestBuildGitPushCommand_NoCommitIfExists(t *testing.T)   // git rev-parse succeeds → no commit (LLM committed)
func TestBuildGitPushCommand_UncommittedChangesWarning(t *testing.T) // git status shows changes → warning in output
```

### Integration Tests (tools)

```go
func TestDeploySSH_GitPush_ValidatesGitToken(t *testing.T)  // no GIT_TOKEN → error
func TestDeploySSH_GitPush_ValidatesRemoteUrl(t *testing.T) // no remote + no URL → error
func TestDeploySSH_GitPush_ReturnsGitPushResult(t *testing.T) // correct return type
func TestDeploySSH_DefaultStrategy_Unchanged(t *testing.T)  // no strategy → existing behavior
```

### E2E Tests

```go
func TestE2E_GitPush_InitialPush(t *testing.T)    // first push (fallback commit), verify on GitHub
func TestE2E_GitPush_AfterLLMCommit(t *testing.T) // LLM commits first, tool pushes, verify message on GitHub
```

---

## 15. Migration Notes

### No Breaking Changes

- `zerops_deploy` without `strategy` parameter behaves identically to current version
- Existing workflows (bootstrap, deploy) are unaffected
- Export and CI/CD workflows get updated guidance but maintain backward compatibility

### Deployment Sequence

1. Deploy `git-push` strategy code (Phase 1) — tool is immediately available
2. Update workflow conductors (Phase 2) — LLM starts using the tool
3. Old raw-SSH guidance in cicd.md/export.md is replaced, not broken

### Rollback

If `git-push` has issues, workflows can revert to raw SSH commands by reverting cicd.md/export.md to pre-update versions. The tool addition is additive, not destructive.
