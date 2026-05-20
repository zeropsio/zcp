---
id: launch-classify-platform-envs
priority: 3
phases: [launch-production-active]
title: "Launch classify — platform envs auto-handled"
references-fields: []
---

### Launch classify — platform envs auto-handled

The `classifications` rows in the `classify-prompt` response carry only envs that need your judgment. Several known platform-injected envs are handled by ZCP without asking — you will not see them in the row table.

Auto-handled (by exact key):

| Key | Auto-action |
|---|---|
| `zeropsSubdomainHost` | classified as `infrastructure` — the new prod project re-emits its own subdomain pair. |
| `zeropsSubdomainString` | classified as `infrastructure` — same. |
| `envIsolation` | dropped — project-level setting; new project picks its own. |
| `sshIsolation` | dropped — project-level setting; carrying forward would reference the source project's containers. |
| `ZCP_API_KEY`, `ZCP_AGENT_TYPE`, `ZCP_BASE_HOST`, `ZCP_BUILTINS_DIR`, `ZCP_PROJECT_DIR` | dropped — ZCP control-plane envs only meaningful in the dev-side ZCP container. |

If a key is in the list above, you do not need to classify it; the bundle composer routes it (or excludes it) deterministically. Membership is closed and matches by **exact key only** — keys merely starting with `ZCP_` (e.g. `ZCP_CUSTOM_USER_THING`) fall through to your classification as normal.

The same exact-key allowlist drives the `suggestedBucket` field on classify-prompt rows: `ZCP_API_KEY`, `ZCP_AGENT_TYPE`, and `GIT_TOKEN` surface with `suggestedBucket: "infrastructure"` regardless of credential-pattern match. Any other `ZCP_*` key surfaces with the default bias (auto-secret or plain-config) — accept or override per the four-bucket table.
