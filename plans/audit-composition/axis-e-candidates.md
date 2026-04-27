# Phase 3 axis-E candidates — Codex CORPUS-SCAN (2026-04-27)

Round type: CORPUS-SCAN per §10.1 Phase 3 row 1
Reviewer: Codex
Inputs read: All three templates (`claude_shared.md`, `claude_container.md`, `claude_local.md`), all 21 `internal/knowledge/guides/*.md`, and all `internal/content/atoms/*.md` (79 atoms)

> **Artifact write protocol note (carries over).** Codex sandbox blocks artifact writes; reconstructed verbatim from text response.

## Static-surface inventory

### claude_shared.md facts (per-session-boot)

- F-CS-1 [`claude_shared.md:7-22`]: workflow=develop command shape — `zerops_workflow action="start" workflow="develop" intent="..." scope=[...]`
- F-CS-2 [`claude_shared.md:24-33`]: workflow=bootstrap command shape — when no services exist, `zerops_workflow action="start" workflow="bootstrap"`

### claude_container.md / claude_local.md facts

- F-CC-1 [`claude_container.md:5-7`]: "Service code is SSHFS-mounted at `/var/www/{hostname}/` — edit there with Read/Edit/Write, not over SSH. Edits on the mount survive restart but not deploy."
- F-CL-1 [`claude_local.md:1-6`]: "Code in your working directory is the source of truth — deploy via `zerops_deploy targetService="<hostname>"` … Requires `zerops.yaml` at repo root. Reach managed services over `zcli vpn up <projectId>`."

### Knowledge-guide facts (fetch-on-demand)

- F-G-local-dev-1 [`local-development.md:31-37`]: "`zcli vpn up <project-id>` … All services accessible by hostname … One project at a time … Env vars NOT available via VPN."
- F-G-local-dev-2 [`local-development.md:39-65`]: "ZCP generates `.env` from `zerops_discover` … Start your dev server as usual … `zerops_deploy targetService="appstage"` … Uses `zcli push` under the hood."
- F-G-local-dev-3 [`local-development.md:123-126`]: "VPN = network only … `.env` contains secrets … Add to `.gitignore` … One VPN project at a time."
- F-G-cicd-1 [`ci-cd.md:3-4`]: "Zerops supports GitHub/GitLab webhook triggers … and GitHub Actions / GitLab CI via `zcli push` with an access token."
- F-G-cicd-2 [`ci-cd.md:7-12`]: "Service detail → Build, Deploy, Run Pipeline Settings … Connect with GitHub repository … Choose trigger: New tag … or Push to branch."
- F-G-cicd-3 [`ci-cd.md:13-29`]: "GitHub Actions … `runs-on: ubuntu-latest` … `uses: actions/checkout@v4` … `access-token: ${{ secrets.ZEROPS_TOKEN }}` … `service-id`."
- F-G-cicd-4 [`ci-cd.md:65-70`]: "Any CI system with shell access can deploy via `zcli push` … Install zcli … Authenticate … Deploy."
- F-G-verify-1 [`verify-web-agent-protocol.md:3-6`]: "The main agent reads `develop-verify-matrix` … the protocol body itself lives here so it ships only when fetched, not on every per-turn payload."
- F-G-verify-2 [`verify-web-agent-protocol.md:13-56`]: Full sub-agent dispatch prompt template with `zerops_verify serviceHostname="{targetHostname}"` and VERDICT.
- F-G-deploy-1 [`deployment-lifecycle.md:152-156`]: "Build and run are SEPARATE containers … You must specify `deployFiles` … `deployFiles land in /var/www`."
- F-G-deploy-2 [`deployment-lifecycle.md:160-164`]: "deploy replaces the container … run container only has `deployFiles` content … `zerops.yml` must be in deployFiles."

## Atom-side restatement matches

Ranked by recoverable bytes descending.

### Match #1 — webhook GUI walkthrough duplicates CI/CD guide basics

- **Atom**: `internal/content/atoms/strategy-push-git-trigger-webhook.md:19-31`
- **Static surface**: `internal/knowledge/guides/ci-cd.md:7-12`
- **Match strength**: MEDIUM
- **Recoverable bytes**: ~657 B
- **Disposition**: KEEP-WITH-LINK
- **Reasoning**: Atom adds ZCP-specific URL template + serviceId placeholders. Tighten to URL + guide reference.

### Match #2 — bootstrap-provision-local repeats VPN/.env/gitignore facts from local-dev guide

- **Atom**: `internal/content/atoms/bootstrap-provision-local.md:30-41`
- **Static surface**: `internal/knowledge/guides/local-development.md:31-40` + `:123-126`
- **Match strength**: STRONG
- **Recoverable bytes**: ~607 B
- **Disposition**: DROP (replace with 1-liner reference)

### Match #3 — develop-platform-rules-common restates build/run separation from deployment-lifecycle guide

- **Atom**: `internal/content/atoms/develop-platform-rules-common.md:13-21`
- **Static surface**: `internal/knowledge/guides/deployment-lifecycle.md:152-155`
- **Match strength**: MEDIUM
- **Recoverable bytes**: ~545 B
- **Disposition**: KEEP-WITH-LINK (keep pitfall rule; drop package-command prose)

### Match #4 — strategy-push-git-trigger-actions repeats GitHub Actions plumbing from CI/CD guide

- **Atom**: `internal/content/atoms/strategy-push-git-trigger-actions.md:60-78`
- **Static surface**: `internal/knowledge/guides/ci-cd.md:13-29` + `:65-70`
- **Match strength**: MEDIUM
- **Recoverable bytes**: ~484 B
- **Disposition**: KEEP-WITH-LINK (preserve zcli-path; compress install/auth)

### Match #5 — develop-platform-rules-container restates SSHFS mount rule from claude_container.md

- **Atom**: `internal/content/atoms/develop-platform-rules-container.md:13-19`
- **Static surface**: `internal/content/templates/claude_container.md:5-6`
- **Match strength**: STRONG
- **Recoverable bytes**: ~473 B
- **Disposition**: DROP prose; retain only operational cautions not in the boot shim

### Match #6 — develop-platform-rules-local restates VPN/.env facts from claude_local.md + local-dev guide

- **Atom**: `internal/content/atoms/develop-platform-rules-local.md:29-44`
- **Static surface**: `internal/content/templates/claude_local.md:1-6` + `internal/knowledge/guides/local-development.md:31-40`
- **Match strength**: STRONG
- **Recoverable bytes**: ~436 B
- **Disposition**: DROP VPN/.env prose; keep local-pitfall framing only

### Match #7 — develop-deploy-modes restates build-container/deployFiles semantics

- **Atom**: `internal/content/atoms/develop-deploy-modes.md:31-35`
- **Static surface**: `internal/knowledge/guides/deployment-lifecycle.md:15-22`
- **Match strength**: MEDIUM
- **Recoverable bytes**: ~319 B
- **Disposition**: KEEP-WITH-LINK

### Match #8 — develop-verify-matrix already acts as pointer (WEAK, keep)

- **Disposition**: KEEP-AS-IS

### Match #9 — idle-develop-entry command echo from claude_shared.md (WEAK, keep)

- **Disposition**: KEEP-AS-IS

## Total recoverable bytes

| Bucket | Count | Bytes | Notes |
|---|---:|---:|---|
| STRONG matches → DROP | 3 | ~1,516 B | Matches #2, #5, #6 — verbatim/near-verbatim of static surface |
| MEDIUM matches → KEEP-WITH-LINK | 4 | ~2,005 B | Matches #1, #3, #4, #7 — paraphrased; tighten to one-liner + link |
| WEAK matches → KEEP-AS-IS | 2 | ~408 B | Matches #8, #9 — working as designed or negligible |
| **Total Phase 3 recoverable** | **9** | **~3,521 B** | All bytes from DROP + most of KEEP-WITH-LINK tightening |

## Phase 3 work plan (priority by bytes)

1. **Match #1** (657 B) — webhook GUI walkthrough → URL + guide reference
2. **Match #2** (607 B) — bootstrap-provision-local VPN/.env → checklist stub + guide ref
3. **Match #3** (545 B) — develop-platform-rules-common: keep pitfall, drop prepareCommands prose
4. **Match #4** (484 B) — Actions plumbing → preserve zcli + compress
5. **Match #5** (473 B) — develop-platform-rules-container SSHFS → operational cautions only
6. **Match #6** (436 B) — develop-platform-rules-local VPN → local-pitfall framing only
7. **Match #7** (319 B) — develop-deploy-modes → keep "not your working tree" + guide link

## Risks + watch items

- Don't add to `claude_shared.md` — per-turn paid.
- `ci-cd.md` uses `zeropsio/actions@main`; `strategy-push-git-trigger-actions.md` uses raw `zcli`. Different mechanisms — preserve distinction.
- `claude_container.md:5-7` says mount survives restart NOT deploy. `develop-platform-rules-container` may add warning nuance — audit before drop.
- **Grep before trusting** — verify each cited static-surface text is present at the cited line before any DROP.
- KEEP-AS-IS atoms (#8, #9) are intentional fetch-on-demand pointers; do not touch.
