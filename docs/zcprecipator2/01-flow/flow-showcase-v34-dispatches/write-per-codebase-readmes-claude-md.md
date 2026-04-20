# Dispatch 5 — write-per-codebase-readmes-claude-md

**Dispatched at**: `2026-04-20T11:04:40`
**Description**: Write per-codebase READMEs + CLAUDE.md
**Subagent type**: `general-purpose`
**Prompt length**: 11346 chars
**Tool-use id**: `toolu_016EEJUiG6SmZL3GaG31nt1s`

---

## Transmitted prompt (verbatim)

```
You are a content-authoring sub-agent for the `nestjs-showcase` Zerops recipe. You have NO memory of the run that dispatched you. This is deliberate — reader-facing content must be written from the reader's perspective, not the author's.

Session ID: `4856bb30df43b2b1`. Facts log: `/tmp/zcp-facts-4856bb30df43b2b1.jsonl`.

<<<MANDATORY — TRANSMIT VERBATIM>>>

**Canonical output tree** — the ONLY files you write are:

- Per-codebase `/var/www/{hostname}dev/README.md` and `/var/www/{hostname}dev/CLAUDE.md` — one pair per dev mount the plan declares (apidev, appdev, workerdev).
- The content manifest at `/var/www/ZCP_CONTENT_MANIFEST.json`.

You do NOT write the root recipe README, the env READMEs, or env `import.yaml` files. Those are emitted from Go templates by `BuildFinalizeOutput` at the finalize step. Writing them yourself creates a parallel orphan tree that the publish CLI ignores.

Forbidden output locations include (non-exhaustive): `/var/www/recipe-{slug}/`, `/var/www/{slug}-output/`, any `environments/` tree you create on the SSHFS mount, any `0 — Development with agent` / `4 — Small production` / `5 — HA production` folder you invent by paraphrasing.

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. Most of your work is Write-from-scratch (READMEs, CLAUDE.md, the manifest); Read is needed only when extending an existing file the main agent or scaffold already authored.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

## Recipe state (read from the mounts directly)

Three codebases, three zerops.yaml files at:
- `/var/www/apidev/` — NestJS 11 API on nodejs@22. Connects to db (postgresql@18), redis (valkey@7.2), queue (nats@2.12), storage (object-storage), search (meilisearch@1.20). Routes: `/api/health`, `/api/status`, `/api/items`, `/api/cache`, `/api/files`, `/api/search`, `/api/jobs`, `/api/mail`.
- `/var/www/appdev/` — Svelte 5 + Vite 7 SPA. Calls apidev via VITE_API_URL baked at build time. 7 dashboard panels.
- `/var/www/workerdev/` — NestJS standalone app subscribing `jobs.scaffold` and `jobs.process` from NATS with queue group `workers`, writing job results back to Postgres.

Read the zerops.yaml on each mount before writing the integration guide — paste its commented content verbatim into README § "Adding zerops.yaml". Never re-author zerops.yaml from memory.

## Plan shape

- Framework: NestJS (slug `nestjs-showcase`, showcase tier).
- Dual-runtime API-first (Svelte SPA frontend calls NestJS JSON API).
- Separate-codebase worker (own repo, own zerops.yaml, NATS consumer).
- Services: db postgresql@18, redis valkey@7.2 (no auth), queue nats@2.12 (JetStream off — core NATS), storage object-storage (S3/MinIO), search meilisearch@1.20.
- 6 features: items-crud, cache-demo, storage-upload, search-items, jobs-dispatch, mail-send.

## Role

Three pathologies shipped across v20–v28 when the debugging agent also wrote content:

1. **Fabricated mental models** — inventing mechanisms to explain observations.
2. **Wrong-surface placement** — framework docs / npm metadata / own-scaffold details shipped as Zerops gotchas.
3. **Self-referential decoration** — documenting the recipe's own helpers as universal integration steps.

Avoid all three by writing against reader-facing tests, not author-facing impressions.

## Inputs you HAVE

- **Facts log at `/tmp/zcp-facts-4856bb30df43b2b1.jsonl`** — `cat` it. Each line is a FactRecord. The scope field routes facts: `content` (or unset) → published content, `downstream` → skip (not for publication), `both` → consider for publication.
- **The final recipe state at SSHFS-mounted paths.** Read-only except for files you write.
- **Platform guides via `mcp__zerops__zerops_knowledge`.** Call on demand — see Citation Map below.
- **Workspace manifest** via `mcp__zerops__zerops_workspace_manifest action=read` if available.

## Inputs you do NOT have

The run transcript, the main agent's context, any memory of what went wrong. If you want to know what happened during deploy, read the facts log.

## Return contract: content manifest (MANDATORY)

Before returning, Write `/var/www/ZCP_CONTENT_MANIFEST.json` with this exact shape:

```json
{
  "version": 1,
  "facts": [
    {
      "fact_title": "<exact title from FactRecord.Title>",
      "classification": "invariant|intersection|framework-quirk|library-meta|scaffold-decision|operational|self-inflicted",
      "routed_to": "apidev-gotcha|apidev-ig|apidev-claude-md|apidev-zerops-yaml-comment|appdev-gotcha|appdev-ig|appdev-claude-md|appdev-zerops-yaml-comment|workerdev-gotcha|workerdev-ig|workerdev-claude-md|workerdev-zerops-yaml-comment|env-yaml-comment|discarded",
      "override_reason": ""
    }
  ]
}
```

Rules:
- Every distinct `FactRecord.Title` with `scope != "downstream"` gets exactly one manifest entry.
- Default-discard classifications (`framework-quirk`, `library-meta`, `self-inflicted`) routing to anywhere except `"discarded"` requires a non-empty `override_reason`.
- A discarded fact must not appear (Jaccard-similar) as a gotcha bullet.

## The six content surfaces (you author #4-7 only)

| # | Surface | Reader | Test |
|---|---------|--------|------|
| 4 | Per-codebase README IG fragment | Porter with their own app | "Does a porter copy THIS?" |
| 5 | Per-codebase README knowledge-base fragment | Dev hitting confusing failure | "Would someone who read both sets of docs STILL be surprised?" |
| 6 | Per-codebase CLAUDE.md | Someone with this repo checked out | "Useful for operating THIS repo?" |
| 7 | Per-codebase zerops.yaml comments | Someone reading deploy config | "Explains a trade-off?" |

## Classification taxonomy (apply BEFORE routing)

| Class | Test | Route |
|-------|------|-------|
| **Invariant** | True of Zerops regardless of this scaffold | KB gotcha with guide citation |
| **Intersection** | Framework-specific + platform-caused | KB gotcha, name both sides |
| **Framework quirk** | Framework only, Zerops not involved | DISCARD |
| **Library metadata** | npm/package manager concern | DISCARD |
| **Scaffold decision** | "We chose X over Y" | zerops.yaml comment / IG prose / CLAUDE.md |
| **Operational** | How to iterate/test/reset this repo | CLAUDE.md |
| **Self-inflicted** | Our code had a bug, we fixed it | DISCARD |

## Citation map (MANDATORY consultation)

When a fact matches, CALL `zerops_knowledge topic=<id>` before writing about it.

| Topic | Guide ID |
|-------|----------|
| Cross-service env vars, self-shadow | `env-var-model` |
| `zsc execOnce`, `appVersionId` | `init-commands` |
| Rolling deploys, SIGTERM, HA | `rolling-deploys` |
| Object storage (MinIO, forcePathStyle) | `object-storage` |
| L7 balancer, httpSupport, trust proxy | `http-support` |
| Deploy files, tilde suffix, static base | `deploy-files` |
| Readiness / health check | `readiness-health-checks` |

## Per-codebase README skeleton (markers BYTE-LITERAL)

```markdown
# NestJS Showcase Recipe — {apidev|appdev|workerdev}

<!-- #ZEROPS_EXTRACT_START:intro# -->
{1-2 sentence intro — service list, no headings, 1-3 lines}
<!-- #ZEROPS_EXTRACT_END:intro# -->

⬇️ **Full recipe page and deploy with one-click**

[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/nestjs-showcase?environment=small-production)

![nestjs cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-nestjs.svg)

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding `zerops.yaml`
The main configuration file — place at repository root.

\`\`\`yaml
{paste full commented yaml from disk}
\`\`\`

### 2. {Platform-forced change — one H3 per real change}
{Narrate the symptom + fix + mechanism. Cite matching knowledge guide. Include fenced code block with the diff — EVERY H3 in IG must carry a code block.}

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **{Concrete symptom}** — {mechanism + failure mode with evidence; cite guide when applicable}

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
```

## Per-codebase CLAUDE.md

Plain markdown, no fragments. Sections: Dev Loop / Migrations & Seed / Container Traps / Testing + ≥2 custom (Resetting dev state / Log tailing / Driving a test request / Adding a managed service). ≥1200 bytes total.

## Worker-specific rules (workerdev)

- **Queue-group semantics under minContainers > 1** (MANDATORY gotcha) — cite NATS + "queue group" + "minContainers" + exact client-library option (`{ queue: 'workers' }`).
- **Graceful shutdown on SIGTERM** (MANDATORY gotcha) — cite SIGTERM or "graceful shutdown" or "drain"; include fenced code block showing the call sequence (`onModuleDestroy` → `await sub.drain()` → `await nc.drain()`).

## Workflow

1. `cat /tmp/zcp-facts-4856bb30df43b2b1.jsonl` — group facts by codebase + substep.
2. Read `/var/www/apidev/zerops.yaml`, `/var/www/appdev/zerops.yaml`, `/var/www/workerdev/zerops.yaml`. These are the authoritative source for the IG code blocks.
3. Classify every fact using the taxonomy. Record classification decisions.
4. For citation-map matches, call `mcp__zerops__zerops_knowledge` BEFORE writing.
5. Write the three READMEs + three CLAUDE.md files + manifest.
6. Self-review: per item, answer the surface test. Any "no" → remove the item, do not rewrite.

## Self-review checklist (answer for each item before returning)

- Each IG item: "A porter bringing their own code — do they need to copy THIS exact content?" Yes/No
- Each gotcha: "A dev who read both doc sets — would they STILL be surprised?" Yes/No. If citation-map match, did I read the guide? Yes/No.
- Each CLAUDE.md section: "Useful for operating THIS repo, not for deploying or porting?" Yes/No.
- Each zerops.yaml comment: "Explains a trade-off the reader couldn't infer from the field name?" Yes/No.

Any "no" → remove the item.

## Deliverables

- `/var/www/apidev/README.md` + `/var/www/apidev/CLAUDE.md`
- `/var/www/appdev/README.md` + `/var/www/appdev/CLAUDE.md`
- `/var/www/workerdev/README.md` + `/var/www/workerdev/CLAUDE.md`
- `/var/www/ZCP_CONTENT_MANIFEST.json`
- A structured `env-comment-set` JSON payload in your completion message for the main agent to apply at `generate-finalize` — covering the 6 env stages (0 AI Agent, 1 Remote CDE, 2 Local, 3 Stage, 4 Small Production, 5 HA Production). Provide per-env `service` comments for `appdev`/`appstage`/`apidev`/`apistage`/`workerdev`/`workerstage`/`db`/`redis`/`queue`/`storage`/`search` as applicable for that env, plus a `project` comment. Comments should explain decisions (scale choices, HA mode, minContainers) not narrate field meaning.

## Return message

Include:
1. List of files written + byte counts.
2. Classification summary: fact counts per class.
3. Self-review answers.
4. `env-comment-set` JSON payload.
```
