---
id: export-09-close
priority: 2
phases: [export-active]
title: "Export — Close and present results"
---

## Close — Present Results

Summarize what was created:

"Export complete.

 **Repository:** {repoUrl}
 **Branch:** {branch}

 **Generated files:**
 - `import.yaml` — infrastructure definition with `buildFromGit: {repoUrl}`
 - `zerops.yaml` — build/deploy pipeline config (in repo)

 **If CI/CD configured:**
 **CI/CD:** GitHub Actions | GitHub webhook | GitLab webhook — configured
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
 `zerops_workflow action=\"strategy\" strategies={\"{targetHostname}\":\"push-git\"}`"
