---
id: cicd-03-approach
priority: 2
phases: [cicd-active, export-active]
title: "CI/CD — Choose your approach"
---

## Choose Your Approach

| Approach | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions** | Repo on GitHub | `.github/workflows/deploy.yml` runs `zcli push` on each push |
| **GitHub webhook** | Repo on GitHub (alternative) | Zerops GUI webhook triggers build on push/tag |
| **GitLab webhook** | Repo on GitLab | Zerops GUI webhook triggers build on push/tag |

**Recommendation:** GitHub Actions for GitHub repos (the setup atom auto-
detects `gh` CLI and falls back to UI steps when unavailable). GitLab
webhook for GitLab repos.
