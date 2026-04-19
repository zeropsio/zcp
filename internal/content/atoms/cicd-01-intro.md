---
id: cicd-01-intro
priority: 2
phases: [cicd-active]
title: "CI/CD — What do you need?"
---

# CI/CD Setup: Connect Git Repository to Zerops

## What Do You Need?

Ask the user first:
"Do you want to **just push code** to a remote repository, or set up **full CI/CD** (push triggers automatic deploy to Zerops)?"

### Option A: Just push code to remote

**GitHub fine-grained token permissions: Contents: Read and write** (that's all)
- GitHub → Settings → Developer settings → Fine-grained tokens → select repo → Permissions: **Contents: Read and write**
- GitLab alternative: User Settings → Access Tokens → Scope: **write_repository**

That's all. Skip to **Git Authentication** section below.
