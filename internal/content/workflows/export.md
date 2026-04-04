# Export: Create Git Repository from Running Infrastructure

Create a deployable git repository from an existing Zerops service, with import.yaml for buildFromGit and optional CI/CD setup.

---

<section name="discover">
## Discover — Assess Current State

### Step 1: Discover services

```
zerops_discover service="{hostname}" includeEnvs=true
```

Note: service type, status, ports, env var keys (keys only), scaling config, mode.

### Step 2: Check git state on container

```bash
ssh {hostname} "cd /var/www && git remote -v 2>/dev/null; ls zerops.yml zerops.yaml 2>/dev/null; test -d .git && echo GIT_EXISTS || echo NO_GIT"
```

Classify into one of three states:

| State | .git | Remote | Meaning |
|-------|------|--------|---------|
| **S0** | No | No | Never initialized — full setup needed |
| **S1** | Yes | No | Internal git from zerops_deploy — add remote + push |
| **S2** | Yes | Yes | Has remote — verify, generate import.yaml |

### Step 3: Export project configuration

```
zerops_export
```

Returns the platform export YAML (re-importable) plus service metadata.

### Step 4: Ask user intent

"What do you need?"
- **A) CI/CD** — automatic deploy on git push
- **B) Reproducible import.yaml** with buildFromGit for replication
- **C) Both** (recommended) — buildFromGit for initial deploy + CI/CD for ongoing

Default to C if user is unsure.
</section>

<section name="prepare">
## Prepare — Git Repository Setup

### GIT_TOKEN Setup (states S0 and S1 only)

If the service has no git remote, we need a token to push:

"I need a GitHub/GitLab token to push code to a repository.

 **For GitHub:**
 1. Go to: GitHub → Settings → Developer settings → Fine-grained tokens
 2. Click 'Generate new token'
 3. Select the target repository (or 'All repositories' if creating new)
 4. Permissions: **Contents → Read and write**
 5. Generate and paste the token here

 **For GitLab:**
 1. Go to: GitLab → User Settings → Access Tokens
 2. Select scope: **write_repository**
 3. Generate and paste the token here

 I'll store it as a project env var — it won't be exposed to your services."

After user provides token:
```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

### .netrc Setup (before any push)

Create .netrc on the container for git authentication:

For GitHub:
```bash
ssh {hostname} 'echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

For GitLab:
```bash
ssh {hostname} 'echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

### Repository Creation (states S0 and S1)

Ask user: "Where should I push the code?"
- **New GitHub repo** — guide user to create via GitHub UI or `gh repo create`
- **Existing repo** — user provides URL (e.g. `https://github.com/user/repo`)

### Git Init and Push (states S0 and S1)

Create .gitignore if missing:
```bash
ssh {hostname} "cd /var/www && test -f .gitignore || echo 'node_modules/\nvendor/\n.env\n.env.*\n*.log\ndist/\nbuild/\n.cache/' > .gitignore"
```
Customize for framework: `zerops_knowledge query="{runtime} gitignore"`

Commit and push (tool handles git init, .netrc auth, remote setup):
```bash
ssh {hostname} "cd /var/www && git add -A && git commit -m 'initial: export from Zerops'"
```
```
zerops_deploy targetService="{hostname}" strategy="git-push" remoteUrl="{repoUrl}"
```

If push fails with auth error: verify GIT_TOKEN is set via `zerops_discover includeEnvs=true`.
If push fails with history conflict: push from a dev service (which preserves .git/).

### zerops.yml Verification

Check if zerops.yml exists and has the correct setup:

```bash
ssh {hostname} "cat /var/www/zerops.yml 2>/dev/null || cat /var/www/zerops.yaml 2>/dev/null || echo 'NOT_FOUND'"
```

If zerops.yml is missing or incomplete:
1. Detect framework from service type (e.g., `nodejs@22` → Node.js)
2. Load matching recipe: `zerops_knowledge recipe="{runtime}-hello-world"`
3. Generate zerops.yml from recipe template + discovered ports and env vars
4. Write to container and commit:
   ```bash
   ssh {hostname} "cat > /var/www/zerops.yml << 'ZEROPS_EOF'
   {generated zerops.yml content}
   ZEROPS_EOF"
   ssh {hostname} "cd /var/www && git add zerops.yml && git commit -m 'add zerops.yml' && git push"
   ```
5. Mark generated sections with comments: `# VERIFY: default from {recipe} template`

### State S2: Verify Existing Remote

If service already has a git remote:
1. Note the remote URL — this is the repo for buildFromGit
2. Verify zerops.yml is in the repo
3. Skip to Generate step
</section>

<section name="generate">
## Generate — import.yaml with buildFromGit

### Build import.yaml

Use the export YAML from the discover step as a base, enriched with discover data:

```yaml
project:
  name: {projectName}
  # corePackage, envVariables from export API

services:
  # Managed services — no buildFromGit, just infrastructure
  - hostname: {db-hostname}
    type: {db-type}
    mode: {HA|NON_HA}        # from zerops_discover (Mode field)
    priority: 10

  # Runtime services — with buildFromGit
  - hostname: {app-hostname}
    type: {app-type}
    buildFromGit: {repo-url}  # from prepare step
    enableSubdomainAccess: true
    envSecrets:
      {key}: {value}          # envSecrets are rare (APP_KEY etc.) — auto-generated at import time or provided by user. envVariables use ${hostname_varName} references (keys-only discover is sufficient)
    verticalAutoscaling:
      cpuMode: {SHARED|DEDICATED}
      minRam: {value}
      minFreeRamGB: {value}
    minContainers: {value}
    maxContainers: {value}
```

### Field Mapping

| import.yaml field | Source |
|-------------------|--------|
| project.name | Export API |
| project.corePackage | Export API |
| project.envVariables | Export API |
| hostname, type | Discover |
| mode | Discover (Mode field) |
| verticalAutoscaling | Discover (Resources) |
| minContainers, maxContainers | Discover (Containers) |
| enableSubdomainAccess | Discover (SubdomainEnabled) |
| envSecrets | Discover (env vars with isSecret) |
| buildFromGit | Repo URL from prepare step |
| priority | 10 for managed, omit for runtime |

### Present to User

Show the generated import.yaml for review:

"Here's the import.yaml for your project. It will recreate all services with buildFromGit pointing to your repo:

```yaml
{generated import.yaml}
```

Review it — especially:
- Are the service types and versions correct?
- Are the env vars complete? (secrets are included)
- Is the scaling config appropriate?

Want me to adjust anything?"
</section>

<section name="cicd">
## CI/CD Setup (Intent A or C)

Skip this section if intent is B (buildFromGit only).

### Choose Approach

| Approach | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions** | Repo on GitHub | Workflow file in repo, runs zcli push |
| **GitHub webhook** | Repo on GitHub | Zerops pulls code via webhook (GUI setup) |
| **GitLab webhook** | Repo on GitLab | Zerops pulls code via webhook (GUI setup) |

### GitHub Actions (recommended for GitHub repos)

1. **Create Zerops access token:**
   "Go to: https://app.zerops.io/settings/token-management → Generate a new token"

2. **Add GitHub secret:**
   "Go to: GitHub repo → Settings → Secrets and variables → Actions
    → New repository secret
    → Name: `ZEROPS_TOKEN`, Value: the token you just created"

3. **Create workflow file:**
   ```bash
   ssh {hostname} "mkdir -p /var/www/.github/workflows"
   ```
   Write `.github/workflows/deploy.yml`:
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

4. **Commit and push:**
   ```bash
   ssh {hostname} "cd /var/www && git add -A && git commit -m 'add CI/CD workflow' && git push"
   ```

5. **Verify:** First push to main triggers deploy.
   ```
   zerops_events serviceHostname="{hostname}" limit=5
   ```

### GitHub/GitLab Webhook (GUI setup required)

Guide user through the Zerops GUI OAuth flow:

"Set up automatic deploy via webhook:

 1. Open: https://app.zerops.io/service-stack/{serviceId}/deploy
    (or: Zerops dashboard → project → service {hostname} → Deploy tab)

 2. Click **'Connect with a GitHub repository'** (or GitLab)

 3. A GitHub/GitLab authorization popup will open.
    Log in and grant Zerops access to your repositories.

    **IMPORTANT:** You need **ADMIN rights** on the repository.
    If the repo doesn't appear in the list, check your permissions
    on GitHub/GitLab.

 4. Select repository: **{owner}/{repoName}**

 5. Configure the build trigger:
    • **Push to branch** → select **'{branchName}'** (recommended)
    • Or: **New tag** (with optional regex filter like `v*`)

 6. Make sure **'Trigger automatic builds'** is checked.

 7. Click **Save**.

 Tell me when you're done — I'll verify the webhook is working."

After user confirms:
```
zerops_events serviceHostname="{hostname}" limit=5
```

If no build event within 2 minutes:
- Check commit message doesn't contain `[ci skip]` or `[skip ci]`
- GitHub Actions: verify workflow file is on the correct branch, check Actions tab
- Webhook: verify connection in Zerops dashboard Deploy tab
- Verify the access token / webhook authorization is valid
</section>

<section name="close">
## Close — Present Results

Summarize what was created:

"Export complete.

 **Repository:** {repoUrl}
 **Branch:** {branch}

 **Generated files:**
 - `import.yaml` — infrastructure definition with `buildFromGit: {repoUrl}`
 - `zerops.yml` — build/deploy pipeline config (in repo)

 {IF CI/CD configured:}
 **CI/CD:** {GitHub Actions | GitHub webhook | GitLab webhook} configured
 Push to `{branch}` will trigger automatic deploy.

 **To replicate this infrastructure on a new project:**
 ```
 zcli project project-import import.yaml
 ```

 **To deploy manually:**
 ```
 zcli push --service-id {serviceId}
 ```

 **Set deploy strategy** (if not already set):
 `zerops_workflow action=\"strategy\" strategies={\"{hostname}\":\"push-git\"}`"
</section>
