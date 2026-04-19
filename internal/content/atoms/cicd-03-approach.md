---
id: cicd-03-approach
priority: 2
phases: [cicd-active]
title: "CI/CD — Choose your approach"
---

## Choose Your Approach

| Approach | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions (auto)** | Repo on GitHub + `gh` CLI available | Auto-set secret + generate workflow via `gh` |
| **GitHub Actions (manual)** | Repo on GitHub | User manually adds secret + workflow file |
| **GitHub webhook** | Repo on GitHub (alternative) | Zerops GUI webhook triggers build on push/tag |
| **GitLab webhook** | Repo on GitLab | Zerops GUI webhook triggers build on push/tag |

**Recommendation:** GitHub Actions (auto) when `gh` CLI is installed — fastest path, zero browser visits. Otherwise GitHub Actions (manual). GitLab webhook for GitLab repos.
