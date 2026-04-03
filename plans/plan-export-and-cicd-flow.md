# Implementation Plan: Export Flow + CI/CD Enhancement

**Date**: 2026-04-03
**Status**: READY FOR IMPLEMENTATION
**Based on**: `analysis-infra-to-repo-buildfromgit.analysis-1.md` + conversation analysis
**Estimated total effort**: 8-12 days

---

## 1. Problem Statement

User says: "Create a repo from service X for buildFromGit" or "Set up CI/CD for my service". Today ZCP has no workflow for this. All existing workflows (bootstrap, recipe, deploy, cicd) are forward-only — they create infrastructure from intent, not extract configuration from running infrastructure.

The CI/CD workflow (`cicd.md`, 151 lines) exists but is incomplete — no error handling, untested, missing auth guidance, and missing detailed GUI walkthrough for GitHub/GitLab webhook setup.

---

## 2. Decision Map

### 2.1 Auto-Detection (agent does this, no user input)

```
ssh {hostname} "cd /var/www && git remote -v 2>/dev/null"
zerops_discover service={hostname} includeEnvs=true
```

Result: one of three starting states:

| State | .git | Remote | How it got there |
|-------|------|--------|-----------------|
| **S0** | No | No | SSH/SSHFS development, never git |
| **S1** | Yes | No | zerops_deploy did internal git init for zcli push |
| **S2** | Yes | Yes | CI/CD webhook, buildFromGit, or manual git setup |

### 2.2 User Intent (one question)

```
"What do you need?"
A) CI/CD — push to git → automatic deploy
B) Reproducible import — import.yaml with buildFromGit
C) Both — buildFromGit for initial + CI/CD for ongoing (DEFAULT, most common)
```

### 2.3 Flow

```
            ┌─────────────────────────────────────────┐
            │           COMMON BASE                    │
            │                                         │
S0/S1 ──────► 1. Ensure GIT_TOKEN on project          │
            │  2. Create .netrc on container           │
            │  3. Create/verify zerops.yml             │
            │  4. Cleanup code (.env, node_modules)    │
S2 ─────────► 5. git init + remote + push             │
(skip 1-5)  │     (or verify existing remote)         │
            └──────────────┬──────────────────────────┘
                           │
                ┌──────────┼──────────┐
                ▼          ▼          ▼
           ┌────────┐ ┌────────┐ ┌──────────┐
           │Intent A│ │Intent B│ │ Intent C │
           │ CI/CD  │ │buildFG │ │  BOTH    │
           ├────────┤ ├────────┤ ├──────────┤
           │ Guide  │ │export  │ │ export   │
           │ user   │ │API +   │ │ API +    │
           │ thru   │ │discover│ │ discover │
           │ GUI    │ │→ gen   │ │ → gen    │
           │ OAuth  │ │import  │ │ import   │
           │ setup  │ │.yaml   │ │ .yaml    │
           │        │ │        │ │ + guide  │
           │        │ │        │ │ GUI OAuth│
           └────────┘ └────────┘ └──────────┘
```

---

## 3. Git Authentication Design

### 3.1 Token Model

- **Project-level env var** `GIT_TOKEN` — set once, all services see it
- **Fine-grained GitHub PAT** with `Contents: Read and write` on target repo(s)
- User provides token in conversation → agent sets via `zerops_env`

### 3.2 Token Flow

```
Agent: "I need a GitHub token for push. Create a fine-grained PAT:
        GitHub → Settings → Developer settings → Fine-grained tokens
        → Select repo → Permissions: Contents: Read and write → Generate.
        Paste the token here."

User: "ghp_xxxxx"

Agent: zerops_env action="set" project=true variables=["GIT_TOKEN=ghp_xxxxx"]
```

### 3.3 .netrc for Auth (not token in URL)

Before each push, agent ensures .netrc exists on container:

```bash
ssh {hostname} 'test -f ~/.netrc || echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

Why .netrc:
- Token NOT in git config (not visible via `git remote -v`)
- Token NOT in command line arguments (not visible in `ps aux` — addresses audit C1/C2)
- Git reads .netrc automatically for HTTPS auth
- .netrc lost on deploy (new container) — recreated on demand

### 3.4 GitLab Variant

```bash
ssh {hostname} 'echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

### 3.5 Token Lifecycle

- Set at beginning of export/CI/CD work session
- Persists in project env vars across deploys
- NOT cleaned up automatically — user may need it for future pushes
- If GitHub Actions CI/CD is set up, token stays but is unused (Actions uses ZEROPS_TOKEN on GitHub side)

### 3.6 Security Considerations

- Token in conversation logs (`~/.claude/.../*.jsonl`) — acceptable, same machine has `~/.ssh/`, `.mcp.json` with ZCP_API_KEY
- Agent NEVER echoes token back in responses
- Agent NEVER puts token in git remote URL
- Agent NEVER passes token as command-line argument

---

## 4. Implementation Phases

### Phase 1: Platform Layer (2-4 hours)

#### 4.1 Add Mode to ServiceInfo

**File**: `internal/ops/discover.go`

Add `Mode` field to `ServiceInfo` struct (after line 35):
```go
Mode             string           `json:"mode,omitempty"`
```

Populate in `buildSummaryServiceInfo()` and `buildDetailedServiceInfo()`:
```go
info.Mode = svc.Mode
```

**Tests**: Add to existing discover tests — verify Mode appears in output.

#### 4.2 Add Export API to platform.Client

**File**: `internal/platform/client.go`

Add 2 methods to Client interface:
```go
// Export
GetProjectExport(ctx context.Context, projectID string) (string, error)
GetServiceStackExport(ctx context.Context, serviceID string) (string, error)
```

**File**: `internal/platform/zerops_api.go` (new methods on ZeropsClient)

Implementation wraps zerops-go SDK:
- `sdk.GetProjectExport` → `GET /api/rest/public/project/{id}/export`
- `sdk.GetServiceStackExport` → `GET /api/rest/public/service-stack/{id}/export`
- Both return YAML string

**File**: `internal/platform/mock_methods.go`

Add mock implementations for tests.

**Tests**: Unit test with mock, E2E test against live API.

### Phase 2: Export Operations (2-3 days)

#### 4.3 Create ops/export.go — Import YAML Reconstruction

**File**: `internal/ops/export.go` (NEW, max 200 lines)

```go
type ExportResult struct {
    ImportYAML   string            `json:"importYaml"`
    Services     []ExportedService `json:"services"`
    ProjectName  string            `json:"projectName"`
    Warnings     []string          `json:"warnings,omitempty"`
}

type ExportedService struct {
    Hostname         string `json:"hostname"`
    Type             string `json:"type"`
    Mode             string `json:"mode"`
    IsInfrastructure bool   `json:"isInfrastructure"`
    HasZeropsYml     bool   `json:"hasZeropsYml"`     // checked via SSH
    HasGitRemote     bool   `json:"hasGitRemote"`     // checked via SSH
    GitRemoteURL     string `json:"gitRemoteUrl,omitempty"`
}

func ExportProject(ctx context.Context, client platform.Client, projectID string) (*ExportResult, error)
```

Logic:
1. Call `client.GetProjectExport(projectID)` → base YAML skeleton
2. Call `Discover(ctx, client, projectID, "", true)` → full service details
3. Merge: export YAML + discover data (mode, scaling ranges, ports, containers)
4. Classify services: managed vs runtime
5. Return structured result with merged import.yaml

**File**: `internal/ops/export_test.go`

Table-driven tests: mock export API response + mock discover response → verify merged output.

#### 4.4 Create ops/git_status.go — Container Git State Check

**File**: `internal/ops/git_status.go` (NEW, max 80 lines)

```go
type GitStatus struct {
    HasGit       bool   `json:"hasGit"`
    HasRemote    bool   `json:"hasRemote"`
    RemoteURL    string `json:"remoteUrl,omitempty"`
    Branch       string `json:"branch,omitempty"`
    HasZeropsYml bool   `json:"hasZeropsYml"`
    IsDirty      bool   `json:"isDirty"`
}

func CheckGitStatus(ctx context.Context, sshExec SSHExecutor, hostname string) (*GitStatus, error)
```

Runs via SSH:
```bash
cd /var/www && git remote -v 2>/dev/null && git branch --show-current 2>/dev/null && git status --porcelain 2>/dev/null && ls zerops.yml 2>/dev/null
```

Parses output into structured result.

### Phase 3: Export Workflow (3-4 days)

#### 4.5 Create workflow content: workflows/export.md

**File**: `internal/content/workflows/export.md` (NEW, ~400 lines)

Structure follows existing workflow patterns with `<section>` tags:

```markdown
# Export: Create Git Repository from Running Infrastructure

## Overview

Create a deployable git repository from an existing Zerops service.
Outputs: git repo with source code + zerops.yml, and import.yaml with buildFromGit.

<section name="discover">
## Discover — Assess Current State

### Steps
1. Auto-detect service state:
   zerops_discover service="{hostname}" includeEnvs=true
   
2. Check git state on container:
   ssh {hostname} "cd /var/www && git remote -v 2>/dev/null; git status 2>/dev/null; ls zerops.yml 2>/dev/null"

3. Export project configuration:
   zerops_export projectId="{projectId}"

### State Classification

| Detected State | Git? | Remote? | Path |
|----------------|------|---------|------|
| S0: No git | No | No | Full setup needed |
| S1: Internal git | Yes | No | Add remote + push |
| S2: Has remote | Yes | Yes | Verify, generate import.yaml |

### User Intent Question
"What do you need?"
A) CI/CD — automatic deploy on git push
B) Reproducible import.yaml with buildFromGit  
C) Both (recommended) — buildFromGit for initial + CI/CD for ongoing

### Completion
zerops_workflow action="complete" step="discover" 
  attestation="State: {S0|S1|S2}, Intent: {A|B|C}, Services: {list}"
</section>

<section name="prepare">
## Prepare — Git Repository Setup

### GIT_TOKEN Setup (if S0 or S1)

Agent asks user for GitHub/GitLab fine-grained PAT:

"I need a GitHub token to push code. Create a fine-grained PAT:
 GitHub → Settings → Developer settings → Fine-grained tokens
 → Select repository → Permissions: Contents: Read and write → Generate.
 Paste the token here — I'll set it as a project env var."

After user provides token:
  zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]

### .netrc Setup (before any git push)

  ssh {hostname} 'echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'

For GitLab:
  ssh {hostname} 'echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'

### Repository Creation (if S0 or S1)

Ask user: "Where should I push the code?"
- New GitHub repo → guide user: gh repo create {name} --public/--private
- Existing repo → user provides URL

### Git Init + Push (if S0 or S1)

  ssh {hostname} "cd /var/www && git init -q -b main"
  ssh {hostname} "cd /var/www && git config user.email 'deploy@zerops.io' && git config user.name 'ZCP Export'"
  
  # .gitignore (if missing)
  ssh {hostname} "cd /var/www && test -f .gitignore || echo 'node_modules/\nvendor/\n.env\n.env.*\n*.log\ndist/\nbuild/\n.cache/' > .gitignore"
  
  ssh {hostname} "cd /var/www && git remote add origin https://github.com/{owner}/{repo}.git"
  ssh {hostname} "cd /var/www && git add -A && git commit -m 'initial: export from Zerops' && git push -u origin main"

### zerops.yml Verification

Check if zerops.yml exists and has required setups:
  ssh {hostname} "cat /var/www/zerops.yml 2>/dev/null"

If missing or incomplete:
- Detect framework from service type + env patterns
- Load matching recipe template: zerops_knowledge recipe="{matched-recipe}"
- Generate zerops.yml from template + discovered ports/env vars
- Write to container: ssh {hostname} "cat > /var/www/zerops.yml << 'EOF' ... EOF"
- Mark generated sections with "# VERIFY: default from {recipe} template"

### Completion
zerops_workflow action="complete" step="prepare"
  attestation="Repo: {url}, Branch: {branch}, zerops.yml: {present|generated}"
</section>

<section name="generate">
## Generate — import.yaml with buildFromGit

### Steps

1. Get export YAML:
   zerops_export projectId="{projectId}"

2. Get discover data:
   zerops_discover includeEnvs=true

3. Merge into comprehensive import.yaml:
   - Project: name, corePackage, envVariables (from export)
   - Managed services: hostname, type, mode, priority: 10 (from discover)
   - Runtime services: hostname, type, scaling, ports (from discover)
   - Runtime services: buildFromGit: {repo-url} (from prepare step)
   - Runtime services: zeropsSetup (only if setup name differs from hostname)
   - envSecrets: vars with isSecret=true (from discover)
   - enableSubdomainAccess: from discover SubdomainEnabled

4. Present import.yaml to user for review.

### import.yaml Template

```yaml
project:
  name: {projectName}
  corePackage: {LIGHT|SERIOUS}
  envVariables:
    {key}: {value}    # from export API (project-level vars)

services:
  # Managed services (from discover, no buildFromGit)
  - hostname: {db-hostname}
    type: {db-type}
    mode: {HA|NON_HA}
    priority: 10

  # Runtime services (from discover + repo URL)
  - hostname: {app-hostname}
    type: {app-type}
    buildFromGit: {repo-url}
    enableSubdomainAccess: true
    envSecrets:
      {secret-key}: {secret-value}
    verticalAutoscaling:
      cpuMode: {SHARED|DEDICATED}
      minRam: {value}
      maxRam: {value}
      minFreeRamGB: {value}
    minContainers: {value}
    maxContainers: {value}
```

### Completion
zerops_workflow action="complete" step="generate"
  attestation="import.yaml generated with {N} services, buildFromGit: {url}"
</section>

<section name="cicd">
## CI/CD Setup (Intent A or C only)

### Choose Approach

| Approach | When to use |
|----------|------------|
| GitHub Actions | Repo on GitHub |
| GitLab Integration | Repo on GitLab |

### GitHub Actions

1. Create Zerops access token:
   "Go to: https://app.zerops.io/settings/token-management → Generate"
   
2. Add GitHub secret:
   "Go to: GitHub repo → Settings → Secrets and variables → Actions
    → New repository secret → Name: ZEROPS_TOKEN, Value: the token"

3. Create workflow file on container:
   ssh {hostname} "mkdir -p /var/www/.github/workflows"
   Write deploy.yml:
   ```yaml
   name: Deploy to Zerops
   on:
     push:
       branches: [main]
   jobs:
     deploy:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
         - uses: zeropsio/actions@main
           with:
             access-token: ${{ secrets.ZEROPS_TOKEN }}
             service-id: {serviceId}
   ```

4. Commit + push:
   ssh {hostname} "cd /var/www && git add -A && git commit -m 'add CI/CD workflow' && git push"

5. Verify:
   zerops_events serviceHostname="{hostname}" limit=5
   → Confirm build triggered

### GitLab Integration (webhook via GUI)

Guide user step by step:

"Set up automatic deploy from GitLab:

 1. Open: https://app.zerops.io/service-stack/{serviceId}/deploy
    (or: Zerops dashboard → project → service {hostname} → Deploy tab)

 2. Find 'Build, Deploy, Run Pipeline Settings'
    Click 'Connect with a GitLab repository'

 3. GitLab authorization will open — log in and grant access.
    NOTE: You need ADMIN rights on the repository.
    If the repo doesn't appear in the list, check your GitLab permissions.

 4. Select repository: {owner}/{repoName}

 5. Configure trigger:
    • For deploy on every push: 'Push to branch' → select '{branchName}'
    • For deploy on new tag: 'New tag' (optional regex filter)

 6. Ensure 'Trigger automatic builds' is checked.

 7. Save.

 Tell me when done — I'll verify the webhook is working."

After user confirms:
   zerops_events serviceHostname="{hostname}" limit=5
   → Check for webhook-triggered build

### GitHub Integration (webhook via GUI — alternative to Actions)

Same as GitLab but:

"Set up automatic deploy from GitHub:

 1. Open: https://app.zerops.io/service-stack/{serviceId}/deploy

 2. Click 'Connect with a GitHub repository'

 3. GitHub authorization will open — log in and grant access.
    NOTE: You need ADMIN rights on the repository.

 4. Select repository: {owner}/{repoName}

 5. Configure trigger:
    • Push to branch → select '{branchName}'
    • Or: New tag (with optional regex)

 6. Check 'Trigger automatic builds'.

 7. Save."

### Verification

After any CI/CD setup:
  zerops_events serviceHostname="{hostname}" limit=5
  
  If no build event visible:
  - Check commit message doesn't contain [ci skip] or [skip ci]
  - GitHub Actions: verify workflow file is on correct branch
  - GitLab/GitHub webhook: verify connection in Zerops dashboard
  - Verify access token is valid

### Completion
zerops_workflow action="complete" step="cicd"
  attestation="CI/CD configured: {github-actions|gitlab-webhook|github-webhook}"
</section>

<section name="close">
## Close — Summary

Present final results:

"Export complete:
 
 Repository: {repo-url}
 Branch: {branch}
 
 Generated files:
 - import.yaml (with buildFromGit pointing to repo)
 - zerops.yml (in repo, {present|generated from template})
 
 {IF CI/CD}: CI/CD: {type} configured — push to {branch} triggers deploy
 
 To replicate this infrastructure:
   zcli project project-import import.yaml
 
 To deploy manually:
   zcli push --service-id {serviceId}"

### Completion
zerops_workflow action="complete" step="close"
  attestation="Export complete: repo={url}, import.yaml generated, CI/CD={yes|no}"
</section>
```

#### 4.6 Register Export Workflow

**File**: `internal/content/workflows/export.md` — embedded automatically via `//go:embed workflows/*.md`

**File**: `internal/tools/workflow.go`

Add export to `handleStart()` (around line 149):
- Export is a stateful workflow (like bootstrap, not like cicd)
- Needs session state for multi-step flow
- Add `ExportState` to `WorkflowState` struct

**File**: `internal/workflow/state.go`

Add:
```go
type ExportState struct {
    Step         string   `json:"step"`
    Intent       string   `json:"intent"`       // A, B, C
    ServiceState string   `json:"serviceState"` // S0, S1, S2
    Hostname     string   `json:"hostname"`
    RepoURL      string   `json:"repoUrl,omitempty"`
    Branch       string   `json:"branch,omitempty"`
    Services     []string `json:"services,omitempty"`
}
```

Add to `WorkflowState`:
```go
Export *ExportState `json:"export,omitempty"`
```

#### 4.7 Create MCP tool: zerops_export

**File**: `internal/tools/export.go` (NEW, max 100 lines)

```go
type ExportInput struct {
    ProjectID string `json:"projectId,omitempty"`
    ServiceID string `json:"serviceId,omitempty"`
}
```

Calls `ops.ExportProject()` or individual service export. Returns merged import.yaml.

Register in `internal/server/server.go`.

### Phase 4: CI/CD Workflow Enhancement (2-3 days)

#### 4.8 Rewrite cicd.md

**File**: `internal/content/workflows/cicd.md`

Current: 151 lines, incomplete.
Target: ~300 lines, comprehensive.

Changes:
- Add GIT_TOKEN setup via project env var (new section, replaces inline token-in-URL)
- Add .netrc auth pattern (replaces token-in-remote-URL)
- Add detailed GitHub OAuth walkthrough (step-by-step GUI instructions with URLs)
- Add detailed GitLab OAuth walkthrough
- Add multi-service CI/CD (one Actions step per service)
- Add error handling section (auth failures, webhook issues, build failures)
- Add GitHub webhook vs Actions comparison table
- Add .gitignore generation guidance per runtime type
- Remove "NEVER paste tokens in conversation" (replaced by zerops_env flow)

#### 4.9 Update knowledge guide: ci-cd.md

**File**: `internal/knowledge/guides/ci-cd.md`

Current: 84 lines.
Add:
- GIT_TOKEN project env var pattern
- .netrc auth explanation
- GitHub webhook vs Actions decision guide
- Zerops GUI OAuth walkthrough summary
- `trigger-pipeline` API for manual/programmatic builds

### Phase 5: Testing (1-2 days)

#### 4.10 Unit Tests

| File | Tests |
|------|-------|
| `ops/export_test.go` | ExportProject with mock export API + mock discover |
| `ops/git_status_test.go` | ParseGitStatus for all 3 states (S0, S1, S2) |
| `tools/export_test.go` | MCP tool handler + annotations |
| `platform/zerops_api_test.go` | Export endpoint calls (mock HTTP) |

#### 4.11 Integration Tests

| Test | Scope |
|------|-------|
| Export flow: discover → export → merge | Multi-tool, mock API |
| Git status detection: all 3 states | SSH mock |
| import.yaml generation correctness | Schema validation against live JSON schema |

#### 4.12 E2E Tests

| Test | Scope |
|------|-------|
| Export real project via API | Live Zerops API |
| Verify exported YAML is re-importable | Export → dry-run import |

---

## 5. Files Changed / Created

### New Files

| File | Lines (est.) | Purpose |
|------|-------------|---------|
| `internal/content/workflows/export.md` | ~400 | Export workflow conductor |
| `internal/ops/export.go` | ~200 | Export + discover merge logic |
| `internal/ops/export_test.go` | ~300 | Tests for export |
| `internal/ops/git_status.go` | ~80 | Container git state check |
| `internal/ops/git_status_test.go` | ~150 | Tests for git status |
| `internal/tools/export.go` | ~100 | zerops_export MCP tool |
| `internal/tools/export_test.go` | ~150 | Tests for export tool |

### Modified Files

| File | Change | Lines (est.) |
|------|--------|-------------|
| `internal/platform/client.go` | Add 2 export methods to interface | +5 |
| `internal/platform/zerops_api.go` | Implement export methods | +30 |
| `internal/platform/mock_methods.go` | Mock export methods | +15 |
| `internal/ops/discover.go` | Add Mode field to ServiceInfo + populate | +5 |
| `internal/workflow/state.go` | Add ExportState struct | +15 |
| `internal/tools/workflow.go` | Register export in handleStart | +15 |
| `internal/server/server.go` | Register zerops_export tool | +3 |
| `internal/content/workflows/cicd.md` | Full rewrite with auth + GUI guidance | ~300 (rewrite) |
| `internal/knowledge/guides/ci-cd.md` | Add auth patterns + GUI walkthrough | +40 |

### Total Estimate

- New code: ~1380 lines (680 source + 600 test)
- Modified code: ~430 lines
- Workflow content: ~700 lines (export.md + cicd.md rewrite)

---

## 6. Architecture Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | New "export" workflow, not extension of bootstrap/recipe | Export is conceptually distinct — extracting config, not creating infra |
| D2 | Project-level GIT_TOKEN, not per-service | Simpler UX — set once, all services see it. Per-repo fine-grained PAT is user's choice |
| D3 | .netrc for git auth, not token in URL | Addresses audit C1/C2 security findings. Token not in `ps aux` or `git config` |
| D4 | Token pasted in conversation, set via zerops_env | Zero-friction UX. Conversation logs are same threat model as existing credentials on disk |
| D5 | Push from container via SSH, not local copy | Code is already on container. Avoids download overhead. .netrc handles auth |
| D6 | CI/CD via GitHub Actions preferred over webhook | Actions file is in repo (versioned), doesn't need Zerops GUI OAuth. Webhook for GitLab. |
| D7 | Separate import.yaml from app repo | import.yaml is infra definition. buildFromGit POINTS to app repo but import.yaml is not part of built code |
| D8 | export.md uses `<section>` tags like other workflows | Consistent with bootstrap.md, deploy.md, recipe.md — workflow engine parses sections |
| D9 | Mode field added to ServiceInfo (trivial) | Data already in API (`ServiceStack.Mode`), just not exposed. Needed for import.yaml generation |
| D10 | Export API wraps zerops-go SDK endpoints | SDK has `GetProjectExport`, `GetServiceStackExport`. No custom API needed |

---

## 7. Verification Plan

### 7.1 Manual Verification Scenarios

| Scenario | Starting State | Intent | Expected Result |
|----------|---------------|--------|----------------|
| Node.js app, no git | S0 | C (both) | Repo created, import.yaml generated, GitHub Actions configured |
| Go app, has git + remote | S2 | B (buildFromGit) | import.yaml generated pointing to existing repo |
| PHP app, internal git | S1 | A (CI/CD) | Remote added, pushed, GitLab webhook guidance |
| Multi-service project | S0+S2 mixed | C | Per-service handling, unified import.yaml |

### 7.2 CI/CD Verification

After any CI/CD setup:
```
zerops_events serviceHostname="{hostname}" limit=5
```
Must show build triggered within 2 minutes of push.

### 7.3 import.yaml Verification

Generated import.yaml must pass:
```
zerops_import content="{yaml}" dryRun=true
```
Dry-run validates schema without creating services.

---

## 8. Open Questions

| # | Question | Impact | Resolution Path |
|---|---------|--------|----------------|
| OQ1 | Does export API include service-level env vars? (Live test showed only project-level) | If not, discover fills the gap (already handles this) | Verify with multi-service project export |
| OQ2 | Can `trigger-pipeline` API be used instead of/alongside buildFromGit in import.yaml? | Could simplify flow — skip import, just trigger | Test: `PUT /api/service-stack/{id}/trigger-pipeline` with buildFromGit param |
| OQ3 | For compiled languages (Go, Rust), source not in /var/www — how to handle? | User must provide repo URL. Agent detects and asks. | Detect compiled runtime types, skip code extraction |
| OQ4 | Multi-service monorepo — one repo, multiple zerops.yml setups | Need `zeropsSetup` in import.yaml to select correct setup | Document in export.md guidance |

---

## 9. Implementation Order

```
Phase 1 (day 1):       Platform layer — Mode field + Export API in Client
Phase 2 (days 2-4):    Export operations — merge logic + git status check
Phase 3 (days 4-7):    Export workflow — export.md + workflow registration
Phase 4 (days 7-9):    CI/CD enhancement — cicd.md rewrite + knowledge guide
Phase 5 (days 9-10):   Testing — unit + integration + E2E
Phase 6 (day 11):      Manual verification — all 4 scenarios from 7.1
```

Each phase is independently deployable. Phase 1-2 provide value immediately (export tool works without workflow). Phase 3-4 add guided flows. Phase 5-6 validate everything.

---

## 10. Dependencies

| Dependency | Status | Blocker? |
|-----------|--------|----------|
| zerops-go SDK v1.0.17 has export endpoints | VERIFIED (live API test) | No |
| ServiceStack.Mode available in API | VERIFIED (types.go:30) | No |
| SSH access to containers | Available (CLAUDE.local.md) | No |
| zerops_env can set project vars | Available (tools/env.go, project=true) | No |
| Workflow engine supports new workflows | Supported (content.go auto-embeds) | No |
| Audit C1/C2 token security fixes | NOT DONE — .netrc pattern sidesteps the issue | No (workaround) |

All dependencies resolved. No blockers.
