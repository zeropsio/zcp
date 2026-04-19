---
id: export-04-prepare-netrc
priority: 2
phases: [export-active]
title: "Export — .netrc setup for container git auth"
---

## Prepare — .netrc Setup

Before any push, create .netrc on the container for git authentication:

For GitHub:
```bash
ssh {devHostname} 'echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

For GitLab:
```bash
ssh {devHostname} 'echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```
