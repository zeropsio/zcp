---
id: launch-classify-platform-envs
priority: 3
phases: [launch-production-active]
title: "Launch classify — platform envs auto-handled"
references-fields: []
---

### Launch classify — platform envs auto-handled

The `classifications` rows in the `classify-prompt` response carry only envs that need your judgment. Two separate mechanisms handle platform / control-plane envs without asking, so you classify ONLY your app's own envs:

1. **Type=SYSTEM → dropped (by type, not by name).** Platform-injected envs — the subdomain pair (`zeropsSubdomainHost`/`String`), isolation settings (`envIsolation`/`sshIsolation`), CDN URLs, and any other server-set value — carry `Type=SYSTEM`. The classifier drops them universally because the new prod project re-emits its own equivalents at boot. This is an OPEN set: a platform SYSTEM env you've never seen drops the same way — there is no name list to maintain.

2. **ZCP control-plane credentials → infrastructure (by exact key).** A small closed allowlist of dev-side credentials (the `ZCP_*` control-plane keys, `GIT_TOKEN`, and the staged launch token) is filtered to `infrastructure`: the destination re-emits its own at init / git-push-setup, and the composed import YAML is agent-visible, so carrying the source's live value forward would leak it. This match is by **exact key only** — a stray user-named `ZCP_CUSTOM_USER_THING` is NOT absorbed; it falls through to your classification with the default bias.

You will not see either group in the row table. Everything else — your app's config + secrets — appears as a row for you to bucket (infrastructure / auto-secret / external-secret / plain-config / exclude).
