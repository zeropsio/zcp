---
id: export-03-prepare-token
priority: 2
phases: [export-active]
title: "Export — GIT_TOKEN setup (S0/S1 only)"
---

## Prepare — GIT_TOKEN Setup

Applies to states **S0 and S1** only. If the service has no git remote, we need a token to push:

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
