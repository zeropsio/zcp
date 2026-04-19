---
id: cicd-08-github-webhook
priority: 2
phases: [cicd-active]
title: "CI/CD — GitHub webhook (GUI)"
---

## GitHub Webhook Configuration (GUI)

Alternative to Actions — Zerops pulls code directly via webhook.

Guide user through the Zerops GUI:

"Set up automatic deploy from GitHub via webhook:

 1. Open: **https://app.zerops.io/service-stack/{serviceId}/deploy**
    (or: Zerops dashboard → project → service **{targetHostname}** → **Deploy** tab)

 2. Find **'Build, Deploy, Run Pipeline Settings'**
    Click **'Connect with a GitHub repository'**

 3. A GitHub authorization popup will open — log in and grant access.

    **IMPORTANT:** You need **ADMIN rights** on the repository.
    Zerops creates a webhook which requires admin permissions.
    If the repo doesn't appear in the list, check your GitHub permissions.

 4. Select repository: **{owner}/{repoName}**

 5. Configure the build trigger:
    • **Push to branch** → select **'{branchName}'** (most common)
    • Or: **New tag** (with optional regex filter like `v*`)

 6. Make sure **'Trigger automatic builds'** is checked.

 7. Click **Save**.

 Tell me when you're done — I'll verify the webhook."
