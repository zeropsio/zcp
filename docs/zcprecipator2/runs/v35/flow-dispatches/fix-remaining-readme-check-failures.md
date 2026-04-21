# Dispatch 7 — fix-remaining-readme-check-failures

**Dispatched at**: `2026-04-21T08:28:42`
**Description**: Fix remaining README check failures
**Subagent type**: `general-purpose`
**Prompt length**: 8011 chars
**Tool-use id**: `toolu_01FBqGf9mgsK7qLxVSADveWN`

---

## Transmitted prompt (verbatim)

```
You are fixing the remaining deploy-check README failures on the nestjs-showcase recipe. Seven concrete issues to fix; no more speculation, just mechanical rewrites.

## Facts log path

`/tmp/zcp-facts-8324884b199361d9.jsonl` — use this for the manifest fix (#7). Each line is a JSON object with fields `title`, `type`, `scope`, `routeTo`, `codebase`, `substep`, etc.

## Fix 1 — Intro must be 1-3 lines (all three READMEs)

Files:
- `/var/www/apidev/README.md`
- `/var/www/appdev/README.md`
- `/var/www/workerdev/README.md`

Between `<!-- #ZEROPS_EXTRACT_START:intro# -->` and `<!-- #ZEROPS_EXTRACT_END:intro# -->`, trim the content to EXACTLY 1 to 3 lines (not more). Preserve meaning: one sentence describing what the codebase is + what it demonstrates on Zerops. Example target for apidev: "NestJS 11 API backend for the nestjs-showcase recipe. Exposes 5 feature endpoints (items CRUD, cache demo, file storage, search, job dispatch) backed by PostgreSQL, Valkey, NATS, Meilisearch, and S3-compatible object storage." One sentence = 1 line. Two is fine. Three is the max.

## Fix 2 — Headings inside fragments must be H3 (###), not H2 (##)

For each README, every heading between `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->` / END markers, and between `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` / END markers, MUST be H3 (`### `) or lower. If you find H2 (`## `) inside these fragments, demote to H3.

If the README currently has an H2 title like "## Integration guide" that's OUTSIDE the start marker, leave it. If you have "## Something" INSIDE the markers, change to "### Something".

## Fix 3 — App gotcha-vs-IG deduplication

In `/var/www/appdev/README.md` knowledge-base fragment, the gotcha `"Vite rejects the .zerops.app Host header with a 403 unless server.allowedHosts includes it"` restates the IG item about Vite allowedHosts. DELETE that gotcha entirely and replace with a different concrete platform gotcha pulled from the facts log that isn't already an IG item — or, if no fact fits, add a gotcha about the SPA fallback class (HTTP 200 `text/html` returned on `/api/*` paths when `VITE_API_URL` is empty/wrong, so `res.json()` throws `Unexpected token '<'`). That's a porter-facing trap not covered elsewhere.

## Fix 4 — Worker gotcha-vs-IG deduplication

In `/var/www/workerdev/README.md` knowledge-base, the gotcha `"AUTHORIZATION_VIOLATION on first subscribe when NATS credentials are URL-embedded"` restates the IG item about structured NATS creds. DELETE it and replace with one of:
- A gotcha about `minContainers >= 2` duplicating messages WITHOUT queue group (concrete failure: each replica processes the same message = N-times side effects)
- A gotcha about workers having no HTTP surface and how this affects `zerops_verify` (logs-only)

Pick one that isn't already covered in integration-guide.

## Fix 5 — Cross-README gotcha uniqueness

api and app README knowledge-base each have a `"zsc noop --silent"` gotcha. Keep it in ONE codebase's README (pick `appdev` since the Vite dev server flow is where a porter most commonly trips on idle containers) and DELETE from `apidev`'s knowledge-base. Replace the deleted slot in apidev with a different gotcha — the cache-manager `undefined` on miss trap from the facts log is a good fit if not already present; otherwise pick another from the log not on api's current list.

## Fix 6 — API comment specificity

`/var/www/apidev/README.md` zerops.yaml in integration-guide has 38 comments but only 7 specific. Need ≥25% specific. "Specific" means explains WHY (because/so that/prevents/required/fails/breaks) OR names a Zerops term (execOnce, L7 balancer, ${env_var}, httpSupport, 0.0.0.0, subdomain, advisory lock, trust proxy, cold start, build time, horizontal container, readinessCheck, healthCheck).

Read the embedded YAML. Rewrite ~5-8 existing generic comments to be specific — e.g. upgrade "# Cache node_modules between builds" to "# Zerops-side build cache; skips re-download on unchanged lockfile" — but don't add new comments; rewrite existing ones. Target 10+ specific out of 38 (≥25%).

**Important**: the embedded zerops.yaml inside README's integration-guide should MATCH the on-disk `/var/www/apidev/zerops.yaml` — so if you improve comments in the README's embedded YAML, ALSO update `/var/www/apidev/zerops.yaml` on disk with the same text. Keep them in sync byte-for-byte (same comments, same indentation).

## Fix 7 — Regenerate ZCP_CONTENT_MANIFEST.json

The current manifest at `/var/www/ZCP_CONTENT_MANIFEST.json` has 8 entries but 4 are missing (the writer invented IDs instead of copying `title` verbatim) and all 8 have empty `routeTo`. Regenerate.

Steps:
1. `cat /tmp/zcp-facts-8324884b199361d9.jsonl` to see all facts.
2. Write manifest where:
   - Every JSONL fact gets ONE manifest entry with `title` equal to the fact's `title` field verbatim.
   - `routeTo` (also accepted spelling: `routed_to`) is filled from the fact's `routeTo` field IF present. If the fact has no `routeTo`, derive from `type` + `scope`:
     - `type: gotcha_candidate`, `scope: content` or unset → `content_gotcha`
     - `type: ig_item_candidate`, `scope: content` → `content_ig`
     - `type: cross_codebase_contract`, `scope: content` → `content_gotcha`
     - `type: verified_behavior`, `scope: downstream` → `discarded`
     - `type: fix_applied`, `scope: content` → `claude_md`
     - `type: fix_applied`, `scope: downstream` → `discarded`
     - Any `scope: downstream` → `discarded`
   - Also populate `routed_to` with the SAME value (checker may read either field name).
   - Valid enum values: content_gotcha, content_intro, content_ig, content_env_comment, claude_md, zerops_yaml_comment, scaffold_preamble, feature_preamble, discarded.
3. JSON shape:
   ```json
   {
     "recipeSlug": "nestjs-showcase",
     "facts": [
       { "title": "<verbatim>", "type": "<fact.type>", "scope": "<fact.scope or 'content'>", "routeTo": "<enum>", "routed_to": "<same enum>", "codebase": "<fact.codebase or 'cross'>", "published": <bool> }
     ]
   }
   ```
   `published` = true if routeTo in {content_gotcha, content_intro, content_ig, content_env_comment, zerops_yaml_comment}; false otherwise.
4. Write to `/var/www/ZCP_CONTENT_MANIFEST.json` (zcp-side, no ssh needed).

## Final step — commit changes on each mount

```
ssh apidev "cd /var/www && git add -A && git commit -q -m 'docs: fix README intro length, H3 headings, comment specificity'"
ssh appdev "cd /var/www && git add -A && git commit -q -m 'docs: fix README intro length, H3 headings, distinct gotchas'"
ssh workerdev "cd /var/www && git add -A && git commit -q -m 'docs: fix README intro length, H3 headings, distinct gotchas'"
```

## Return report

- Per-fix confirmation: what changed in which file.
- New intro text per README (for verification).
- New gotchas added (appdev, workerdev, apidev — each fix's replacement text).
- Comment specificity count in apidev zerops.yaml integration-guide (before/after).
- Manifest entry count + sample routeTo values.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

```
