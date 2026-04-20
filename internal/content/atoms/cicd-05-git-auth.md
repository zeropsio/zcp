---
id: cicd-05-git-auth
priority: 2
phases: [cicd-active, export-active]
title: "CI/CD — Git authentication"
---

## Git Authentication

### GIT_TOKEN — Project-Level Env Var

For pushing code from a Zerops container to GitHub/GitLab, set a project-level token:

"I need a GitHub fine-grained token to push code.

 **For push-only (Option A):**
 GitHub → Settings → Developer settings → Fine-grained tokens → select repo
 Permissions: **Contents: Read and write**

 **For full CI/CD (Option B):**
 Same path, but three permissions:
 - **Contents: Read and write** (push code)
 - **Secrets: Read and write** (set ZEROPS_TOKEN secret)
 - **Workflows: Read and write** (create .github/workflows/deploy.yml)

 **GitLab alternative:** User Settings → Access Tokens → Scope: **write_repository**

 Paste the token here — I'll store it as a project env var."

After user provides token:
```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

### .netrc for Authentication

Before any git push from a container, create .netrc (token NOT in URL, NOT in command args):

GitHub:
```bash
ssh {devHostname} 'echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

GitLab:
```bash
ssh {devHostname} 'echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

**.netrc is lost on deploy** (new container). Recreate before each push session. The GIT_TOKEN env var persists across deploys.
