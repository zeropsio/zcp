---
id: cicd-02-prereqs
priority: 2
phases: [cicd-active]
title: "CI/CD — Option B prerequisites"
---

### Option B: Full CI/CD (push → automatic deploy)

**GitHub fine-grained token permissions: Contents: Read and write + Secrets: Read and write + Workflows: Read and write**

**Requirements — gather ALL of these before starting:**
1. **Git push token with CI/CD permissions** — needs Contents + Secrets + Workflows (three permissions above) for pushing code AND creating the workflow file + setting secrets
2. **Zerops deploy token** — for CI/CD to deploy back to Zerops
   - Use the existing ZCP API key (ask user: "Can I use the existing API key as the deploy token? It has full project access. For a scoped token, create one at https://app.zerops.io/settings/token-management")
   - Or user creates a dedicated token at https://app.zerops.io/settings/token-management
3. **GitHub repo secret `ZEROPS_TOKEN`** — store the deploy token as a secret
   - Via `gh` CLI: `gh secret set ZEROPS_TOKEN --repo {owner}/{repo} --body "{zeropsToken}"`
   - Or manually: repo **Settings** → **Secrets and variables** → **Actions** → **New repository secret** → Name: `ZEROPS_TOKEN`, Value: the deploy token
4. **GitHub Actions permissions** — the repo must allow workflows to run
   - **Settings** → **Actions** → **General** → **Actions permissions**: "Allow all actions" (or at minimum allow actions from the repository)
   - **Settings** → **Actions** → **General** → **Workflow permissions**: "Read and write permissions"
5. **Service ID** of the deploy target — get via `zerops_discover service="{targetHostname}"`

Verify all prerequisites before generating the workflow file. This prevents the "push workflow → CI fails → fix permissions → push again" loop.
