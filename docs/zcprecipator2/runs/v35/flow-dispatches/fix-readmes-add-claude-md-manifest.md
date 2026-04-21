# Dispatch 6 — fix-readmes-add-claude-md-manifest

**Dispatched at**: `2026-04-21T08:23:45`
**Description**: Fix READMEs + add CLAUDE.md + manifest
**Subagent type**: `general-purpose`
**Prompt length**: 6612 chars
**Tool-use id**: `toolu_014RRvF9ANyMEMwnDSgkwLfx`

---

## Transmitted prompt (verbatim)

```
You are fixing deploy-check failures in the nestjs-showcase recipe. The deploy-step README check rejected the prior README writer's output. Apply these FIVE concrete fixes, all on SSHFS mounts. Each is derived directly from the check failure list below.

## Fix 1 — README fragment markers (format mismatch)

The writer used `<!--#ZEROPS_EXTRACT_START:X-->` but the checker wants the EXACT literal `<!-- #ZEROPS_EXTRACT_START:X# -->` shape (note: spaces around the comment body AND a trailing `#` before the closing `-->`). Apply to all three READMEs:

- `/var/www/apidev/README.md`
- `/var/www/appdev/README.md`
- `/var/www/workerdev/README.md`

For each README, replace the start/end markers for fragments `intro`, `integration-guide`, `knowledge-base`. The correct literal form, one per line (no tabs):

```
<!-- #ZEROPS_EXTRACT_START:intro# -->
...content...
<!-- #ZEROPS_EXTRACT_END:intro# -->
```

Same shape for `integration-guide` and `knowledge-base`. Read each README, Edit the markers with `replace_all: true` if they appear multiple times per file. Do NOT rewrite the body content — only the marker strings change.

## Fix 2 — Add CLAUDE.md to every codebase

Create three new files:

- `/var/www/apidev/CLAUDE.md`
- `/var/www/appdev/CLAUDE.md`
- `/var/www/workerdev/CLAUDE.md`

Each is a repo-local dev-loop operations guide (audience: a developer who clones THIS repo to iterate). Content must include, per the check failure detail:

1. How to SSH into the dev container (`ssh {hostname}` where hostname is apidev/appdev/workerdev)
2. Exact command to start the dev server (`npm run start:dev` for NestJS codebases, `npm run dev` for the Svelte SPA)
3. How to run migrations/seed manually (`ssh apidev "cd /var/www && npx ts-node src/migrate.ts && npx ts-node src/seed.ts"` — applies to apidev only)
4. Container traps you hit during this recipe's deploy (use the facts log at `/tmp/zcp-facts-8324884b199361d9.jsonl` for concrete traps: TypeORM ts-node glob bug, Meilisearch ESM dynamic import, execOnce gating seed re-run, multer types, cache-manager undefined-on-miss, Svelte 5 bind:value type-vs-fill. Filter per-codebase: apidev gets TypeORM+Meilisearch+multer+execOnce+cache-manager; appdev gets Svelte/Vite notes; workerdev gets NATS queue group)
5. How to run tests (`npm test` if present in package.json — check each codebase)

Keep each CLAUDE.md tight: 100-200 lines max. ASCII only. Use `--` for em-dashes.

## Fix 3 — Worker drain fenced code block

In `/var/www/workerdev/README.md`, inside the integration-guide fragment, add a numbered step (e.g. "### N. Drain on SIGTERM") with a copy-pasteable fenced ```typescript code block showing the exact SIGTERM → drain → exit sequence for a NestJS standalone microservice. Use the NestJS idiom that `src/main.ts` already uses: `app.enableShutdownHooks()` wires the module lifecycle to SIGTERM. Also show the explicit drain fallback for non-Nest users:

```typescript
process.on('SIGTERM', async () => {
  await app.close();       // drains NATS subscription + closes DataSource
  process.exit(0);
});
```

Append the step into the integration-guide fragment (before the closing marker).

## Fix 4 — Write ZCP_CONTENT_MANIFEST.json

Path: `/var/www/ZCP_CONTENT_MANIFEST.json` (recipe root).

Read the facts log `/tmp/zcp-facts-8324884b199361d9.jsonl`. Produce a JSON file with this shape (one entry per fact; derive classification + routing from the fact's own `type`, `scope`, and `routeTo` fields):

```json
{
  "recipeSlug": "nestjs-showcase",
  "facts": [
    {
      "id": "<stable-kebab-from-title>",
      "title": "<fact.title>",
      "type": "<fact.type>",
      "scope": "<fact.scope or 'content'>",
      "routeTo": "<fact.routeTo or derived>",
      "published": <true if routeTo is content_gotcha/content_ig/content_intro/content_env_comment/zerops_yaml_comment else false>,
      "codebase": "<fact.codebase or 'cross'>",
      "surface": "<readme|claude_md|discarded>"
    }
  ]
}
```

Derivation rules:
- If fact.scope == "downstream": `surface = "discarded"` from content lane (not in any README), but still listed in manifest.
- If fact.type == "fix_applied": `surface = "claude_md"` (dev-loop traps) unless the title describes a platform invariant.
- If fact.type == "gotcha_candidate" AND scope != "downstream": `surface = "readme"`, `published = true`.
- If fact.type == "cross_codebase_contract" AND scope != "downstream": `surface = "readme"`, published in the codebase named in `fact.codebase`.
- If fact.type == "verified_behavior" AND scope == "downstream": `surface = "discarded"`.

Write valid JSON. ASCII only. Close with a newline.

## Fix 5 — Commit everything

```
ssh apidev "cd /var/www && git add -A && git commit -q -m 'docs: correct README markers + CLAUDE.md + drain step'"
ssh appdev "cd /var/www && git add -A && git commit -q -m 'docs: correct README markers + CLAUDE.md'"
ssh workerdev "cd /var/www && git add -A && git commit -q -m 'docs: correct README markers + CLAUDE.md + drain code block'"
```

`/var/www/ZCP_CONTENT_MANIFEST.json` lives in the zcp orchestrator's /var/www (no ssh needed — Write directly).

## Check failure reference (for your context)

```
fragment_intro/integration-guide/knowledge-base: missing correct marker format
{apidev,appdev,workerdev}_claude_md_exists: file missing
worker_drain_code_block: README has drain gotcha but no fenced code block
writer_content_manifest_exists: /var/www/ZCP_CONTENT_MANIFEST.json missing
```

## Return report

- Confirmation that each fix was applied (marker form used, CLAUDE.md byte counts, drain block added, manifest created with N fact entries).

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

```
