---
id: cicd-08-webhook
priority: 2
phases: [cicd-active, export-active]
title: "CI/CD — Git provider webhook (GUI)"
---

## Webhook Configuration (GUI)

Alternative to Actions — Zerops pulls code directly via webhook. Same
flow for GitHub and GitLab; provider differs only in the OAuth popup.

Guide the user through the Zerops GUI:

"Set up automatic deploy via webhook:

 1. Open: **https://app.zerops.io/service-stack/{serviceId}/deploy**
    (or: Zerops dashboard → project → service **{targetHostname}** → **Deploy** tab)

 2. Click **'Connect with a {provider} repository'** (GitHub or GitLab).

 3. A {provider} authorization popup will open — log in and grant access.

    **IMPORTANT:** You need **ADMIN rights** on the repository — Zerops
    creates a webhook which requires admin permissions. If the repo
    doesn't appear, check your permissions on {provider}.

 4. Select repository: **{owner}/{repoName}**

 5. Configure the build trigger:
    • **Push to branch** → select **'{branchName}'** (most common)
    • Or: **New tag** (with optional regex filter like `v*`)

 6. Make sure **'Trigger automatic builds'** is checked.

 7. Click **Save**.

 Tell me when you're done — I'll verify the webhook."
