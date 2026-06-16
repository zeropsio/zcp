---
id: develop-local-env-troubleshoot
priority: 4
phases: [develop-active]
environments: [local]
title: "Local env troubleshooting — refused / VPN-down / multi-setup"
coverageExempt: "local-mode env troubleshooting — covered by env-handling spec + Theme 2 design pass"
---

### Common errors and recovery

**"Refused: existing .env has unowned keys [...]"** — you edited `.env` directly. Move those keys to `.env.local` (they survive across regens) and re-run `generate-dotenv`. To discard them instead, pass `force=true` (the values are dropped on write).

**"Setup parameter required; multiple setup blocks: [...]"** — your `zerops.yaml` has more than one `setup` block. Pick one with `setup=<name>` (e.g. `setup="prod"` for the deployed-shape block, `setup="dev"` for a local-friendly block when present).

**"Transient resolve failure for service ... (likely VPN/API issue)"** — Zerops API or managed-service env couldn't be reached. Run `zcli vpn up` and retry. Your existing `.env` was NOT overwritten on this error — prior values stay safe until the next successful generation.

**Keep `.env.local` out of git.** It's per-developer state (debug flags, machine-specific overrides) — add it to `.gitignore` if it isn't already. Move shared values to `project.envVariables` (for cross-developer secrets) or `zerops.yaml run.envVariables` (for deployed config). For team-shared documentation of expected keys, commit a `.env.local.example` instead.

**"`.env` got deleted"** — no problem. `zerops_env action="generate-dotenv"` re-creates it deterministically from sources + `.env.local` overlay.
