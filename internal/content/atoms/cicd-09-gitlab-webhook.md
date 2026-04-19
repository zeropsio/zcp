---
id: cicd-09-gitlab-webhook
priority: 2
phases: [cicd-active]
title: "CI/CD — GitLab webhook (GUI)"
---

## GitLab Webhook Configuration (GUI)

GitLab uses the same GUI flow with GitLab OAuth instead of GitHub.

"Set up automatic deploy from GitLab:

 1. Open: **https://app.zerops.io/service-stack/{serviceId}/deploy**

 2. Click **'Connect with a GitLab repository'**

 3. GitLab authorization popup — log in and grant access.
    **Requires ADMIN rights** on the repository.

 4. Select repository: **{owner}/{repoName}**

 5. Configure trigger:
    • **Push to branch** → select **'{branchName}'**
    • Or: **New tag** (optional regex)

 6. Check **'Trigger automatic builds'** → **Save**.

 Tell me when done — I'll verify."
