# Minimal-tier dispatch template — readme-with-fragments (deploy.readmes)

**Dispatched at**: schematic (no live session log; reconstruction path per RESUME decision #1)
**Source**: [`internal/content/workflows/recipe.md`](../../../internal/content/workflows/recipe.md#L2205-L2388) — the block text authored as the transmitted brief when main delegates to a sub-agent at this substep
**Companion showcase-tier dispatch**: [../flow-showcase-v34-dispatches/write-per-codebase-readmes-claude-md.md](../flow-showcase-v34-dispatches/write-per-codebase-readmes-claude-md.md) — showcase uses the DIFFERENT block `content-authoring-brief` (recipe.md L2390-2737, v8.94 fresh-context shape). The two blocks produce structurally different dispatches; see [subStepToTopic](../../../internal/workflow/recipe_guidance.go#L588).
**Block length**: 20764 chars

---

## Notes

- **Dispatch is main-agent discretion.** The block text is written to function BOTH as a sub-agent dispatch prompt (the TOOL-USE POLICY + MANDATORY sentinels are the dispatch-ready framing) AND as a main-inline authoring reference. In observed minimal runs (e.g. nestjs-minimal-v3 TIMELINE L33), main wrote the README inline at generate rather than dispatching a writer subagent at deploy.readmes. Whether this is policy or emergent is a reconstruction gap.
- **This is the OLD v8 shape** per README.md §2's tier-coverage table. The showcase writer migrated to `content-authoring-brief` (v8.94 fresh-context + facts-log + classification taxonomy). Minimal still uses this pre-v8.94 shape.
- The block includes both sub-agent framing (TOOL-USE POLICY, MANDATORY block) and substantive authoring guidance (fragment shape, content-per-section rules, gotcha authenticity rules). The dispatcher-vs-transmitted-brief separation that the rewrite plan (§1) calls out is NOT yet applied here — the block is one of the artifacts the rewrite targets.

---

## Block template (verbatim from recipe.md)

```
<block name="readme-with-fragments">

### Per-codebase README with extract fragments (post-deploy `readmes` sub-step)

**⚠ TOOL-USE POLICY — if this brief is used as a sub-agent dispatch prompt, read before your first tool call.**

When the main agent delegates README writing to a sub-agent, that sub-agent is bound by the same rules as every other recipe sub-agent. The main agent holds workflow state; the writer's job is narrow, scoped to this brief.

**Permitted tools:**
- File ops: `Read`, `Edit`, `Write`, `Grep`, `Glob` against the SSHFS-mounted README paths named in this brief
- `Bash` — but ONLY via `ssh <hostname> "<command>"` patterns. Writer rarely needs SSH; most work is file-local.
- `mcp__zerops__zerops_knowledge` — on-demand platform knowledge queries to confirm service types / gotcha framing
- `mcp__zerops__zerops_logs` — read container logs if you need to verify a gotcha against real output
- `mcp__zerops__zerops_discover` — introspect service shape for service-keys table

**Forbidden tools — calling any of these is a sub-agent-misuse bug (workflow state is main-agent-only):**
- `mcp__zerops__zerops_workflow` — never call `action=start`, `action=complete`, `action=status`, `action=reset`, `action=iterate`, `action=generate-finalize`
- `mcp__zerops__zerops_import` — service provisioning is main-agent-only
- `mcp__zerops__zerops_env` — env-var management is main-agent-only
- `mcp__zerops__zerops_deploy` — deploy orchestration is main-agent-only
- `mcp__zerops__zerops_subdomain` — subdomain management is main-agent-only
- `mcp__zerops__zerops_mount` — mount lifecycle is main-agent-only
- `mcp__zerops__zerops_verify` — step verification is main-agent-only

**File-op sequencing — Read before Edit (Claude Code constraint, NOT a Zerops rule):** every `Edit` call must be preceded by a `Read` of the same file in this session. The Edit tool enforces this; hitting "File has not been read yet" and reactively Read+retry is trace pollution. For README/CLAUDE.md files you create from scratch, use `Write` (no Read needed). When extending an existing README the scaffold or main agent already wrote, `Read` it once before your first `Edit`.

If the server rejects a call with `SUBAGENT_MISUSE`, you are the cause. Return to writing the READMEs.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. For README/CLAUDE.md files you create from scratch, use Write (no Read required). When extending an existing file the scaffold or main agent already wrote, Read it once before your first Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

**This is the `readmes` sub-step of deploy.** You land here after `verify-stage`, after every service is verified healthy on both dev and stage. READMEs are written now — not during generate — so the gotchas section narrates the debug rounds you just lived through. A speculative gotchas section written during generate is the root cause of the authenticity failures in v11/v12.

Write **two files per codebase mount**: `README.md` and `CLAUDE.md`. They have different audiences and neither substitutes for the other:

- `README.md` — **published recipe-page content**. Fragments are extracted to zerops.io/recipes at finalize time. Audience: integrators porting their own codebase. Content: platform-forced code changes + symptom-framed gotchas. Fragment format enforced byte-literally.
- `CLAUDE.md` — **repo-local dev-loop operations guide**. Not extracted, not published. Audience: anyone (human or Claude Code) who clones this codebase and needs to work in it. Content: SSH commands, dev server startup, migration/seed commands, container traps (SSHFS uid, npx tsc wrong-package, fuser -k for stuck ports), test commands. Plain markdown, no fragments, no rules other than "be useful."

For a dual-runtime showcase, that is 6 files: `/var/www/appdev/{README.md,CLAUDE.md}`, `/var/www/apidev/{README.md,CLAUDE.md}`, `/var/www/workerdev/{README.md,CLAUDE.md}`. Use `prettyName` from the workflow response for titles (e.g., "Minimal", "Hello World", "Showcase").

**Critical formatting for README.md** — match the structure below exactly. The literal `<!-- #ZEROPS_EXTRACT_START:name# -->` / `<!-- #ZEROPS_EXTRACT_END:name# -->` marker shape is enforced by the checker byte-for-byte. Invented variants like `<!-- FRAGMENT:intro:start -->` or `<!-- BEGIN:intro -->` are rejected.

```markdown
# {Framework} {PrettyName} Recipe App

<!-- #ZEROPS_EXTRACT_START:intro# -->
A minimal {Framework} application with a {DB} connection,
demonstrating database connectivity, migrations, and a health endpoint.
Used within [{Framework} {PrettyName} recipe](https://app.zerops.io/recipes/{slug}) for [Zerops](https://zerops.io) platform.
<!-- #ZEROPS_EXTRACT_END:intro# -->

⬇️ **Full recipe page and deploy with one-click**

[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/{slug}?environment=small-production)

![{framework} cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-{framework}.svg)

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding `zerops.yaml`
The main configuration file — place at repository root. It tells Zerops how to build, deploy and run your app.

\`\`\`yaml
zerops:
  ... (paste full zerops.yaml with comments — read it back from disk, do not rewrite from memory)
\`\`\`

### 2. Step Title (for each code adjustment you actually made)
Describe the debug round that forced the change. Example: "Bind NestJS to 0.0.0.0" / "Add `allowedHosts: ['.zerops.app']` to vite.config.ts" / "Use `forcePathStyle: true` for MinIO S3 client". Each section is one real thing that broke and how you fixed it, with the code diff.

\`\`\`typescript
// the actual patch you applied
\`\`\`

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **Concrete symptom 1** — exact error message, HTTP status, or observable misbehavior (e.g. "`AUTHORIZATION_VIOLATION` on first subscribe", "HTTP 200 with plain-text 'Blocked request' body", "`MODULE_NOT_FOUND` for package that IS in node_modules"). Written from memory of the debug round. Clones of the predecessor's stems fail the `knowledge_base_exceeds_predecessor` check; restatements of integration-guide items in THIS README fail the `gotcha_distinct_from_guide` check; facts that also appear in a sibling codebase's README fail `cross_readme_gotcha_uniqueness`.
- **Concrete symptom 2** — same. Showcase tier needs at least 3 net-new gotchas beyond the predecessor AND 3 authentic (platform-anchored or failure-mode described), AND each stem must be cross-README unique.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
```

**Then write `CLAUDE.md` next to it** — plain markdown, no fragments, no extraction rules. The template below is the MINIMUM. A v17-compliant CLAUDE.md clears **1200 bytes** of substantive content AND carries **≥ 2 custom sections beyond the template** (Resetting dev state / Log tailing / Adding a managed service / Driving a test request — whatever operational knowledge you actually accumulated for this codebase):

```markdown
# {Framework} {PrettyName} — Dev Operations

Repo-local operations guide for anyone (human or Claude Code) working in this codebase after cloning. For the published recipe content (integration guide + platform gotchas), see README.md.

## Dev Loop

SSH into the dev container: `zcli vpn up` then `ssh zerops@{hostname}dev`.
Start the dev server via `zerops_dev_server action=start hostname={hostname}dev command="<exact cmd>" port=<port> healthPath="<path>"` — do NOT hand-roll `ssh host "cmd &"` (hits the 120s SSH channel timeout).
Source lives at `/var/www/` on the container, SSHFS-mounted from zcp at `/var/www/{hostname}dev/`.

## Migrations & Seed

Run manually: `<exact command>` — e.g. `npx ts-node src/migrate.ts` then `npx ts-node src/seed.ts`.
On deploy, these run via `initCommands` — keyed by the lifetime the command actually needs:

- `migrate` is keyed by `${appVersionId}` — re-runs once per deploy, reconverges schema on each version (idempotent by design: `CREATE TABLE IF NOT EXISTS`, additive column adds).
- `seed` is keyed by a static string (e.g. `bootstrap-seed-v1`) — runs once per service lifetime; bump the suffix when seed data changes so the next deploy re-runs once under the new key.

See "Two `execOnce` keys, two lifetimes" in the recipe guidance for the full rationale. Do NOT key `seed` on `${appVersionId}` — that runs seed on every deploy and forces an in-script row-count guard that silently skips sibling work (search-index creation, cache warmup). If the seeder crashed mid-insert and burned the per-deploy key on the migrate command, touch any source file and redeploy to force a fresh `appVersionId`.

## Container Traps

- **SSHFS ownership** — files land owned by `root`, container runs as `zerops` (uid 2023). `npm install` fails with `EACCES`. Fix: `sudo chown -R zerops:zerops /var/www/`.
- **`npx tsc` resolves to deprecated tsc@2.0.4** — use `node_modules/.bin/tsc` instead.
- **Port 3000 stuck after background command** — `zerops_dev_server action=stop hostname={hostname}dev port=3000` (tolerates "nothing to kill").
- *(add any other container-ecosystem traps you hit during this build)*

## Testing

- Unit tests: `<command>`
- Smoke check: `zerops_dev_server action=status hostname={hostname}dev port=<port> healthPath="<path>"`
- To exercise the full feature path: `<concrete curl sequence the agent actually ran>`
```

**Now add at least 2 of these custom sections** (pick the ones that apply to this codebase):

- **Resetting dev state** — how to drop/re-seed the database without a full redeploy (avoids the `appVersionId` rotation dance).
- **Log tailing** — the exact log file path + `tail -f` command for each long-running process in this codebase, plus when to reach for `zerops_logs` instead.
- **Driving a test job / endpoint** — a real curl (or psql / redis-cli / nats-cli) command sequence that exercises the feature path end-to-end on the dev container. For a worker, the exact NATS message shape + how to dispatch it from the API.
- **Adding a new managed service** — the delta against this recipe's current zerops.yaml / import.yaml when the user wants to bolt on another dependency.
- **Recovering from a burned `zsc execOnce` key** — the exact `touch` or file-mtime trick for THIS codebase's source tree, step by step.

**Rules:**
- Section headings (`## Integration Guide`) go OUTSIDE markers in README.md — they're visible but not extracted
- Content INSIDE fragment markers uses **H3** (`###`), not H2
- **All fragments**: blank line required after the start marker (intro, integration-guide, knowledge-base)
- **Intro content**: plain text, no headings, 1-3 lines
- **Step 1** of integration-guide must be `### 1. Adding \`zerops.yaml\`` with a description paragraph before the code block
- **Worker codebase README** does not need the integration-guide code-block floor (workers rarely have user-facing code adjustments), but still needs all three fragments, the predecessor-floor gotchas, its own CLAUDE.md, AND the two production-correctness gotchas below.
- **Fragment format is byte-literal.** The checker searches for `#ZEROPS_EXTRACT_START:{name}#` exactly. Do not guess.
- **CLAUDE.md is required for every codebase, every tier.** Plain markdown, no fragments. **New v17 floors**: ≥ 1200 bytes of substantive content AND ≥ 2 custom sections beyond the template boilerplate (Dev Loop / Migrations / Container Traps / Testing). A 40-line file that only fills in the template fails the depth check.
- **No fact appears in two README.md files.** If the fact applies to multiple codebases (NATS credentials, shared DB migration ownership), put it in exactly one README — by convention, the service most responsible for owning it (api for server-side wiring, app for frontend config) — and have the others cross-reference: `See apidev/README.md §Gotchas for NATS credential format.`
- **No gotcha restates an integration-guide heading in the same README.** A gotcha must teach a symptom the guide did not cover. If your gotcha stem normalizes to the same tokens as an IG heading, rewrite it to focus on the observable symptom (error message, HTTP status, browser state) instead of the topic.
- **Container-ops content (SSHFS uid fix, npx tsc trap, dev-server restart)** goes in CLAUDE.md, NOT in README.md gotchas. README.md is for platform facts an integrator porting their own code cares about.

**Worker production-correctness gotchas (MANDATORY for every `isWorker: true` target with `sharesCodebaseWith` empty).** A separate-codebase worker README MUST carry gotchas covering BOTH of these production-correctness concerns — they are enforced at deploy-step completion by `{hostname}_worker_queue_group_gotcha` and `{hostname}_worker_shutdown_gotcha`:

1. **Queue-group semantics under `minContainers > 1`.** Whenever a runtime service runs more than one container — whether the replicas exist for throughput scaling OR for HA/rolling-deploy availability — a broker consumer without a queue group (NATS `queue: 'workers'`, Kafka consumer group, etc.) processes every message ONCE PER REPLICA, so a 2-container worker runs every job twice. A reader scaling out a fresh deployment will fill the database with duplicates and never know why. The gotcha stem must name the broker + "queue group" or "consumer group" + "minContainers" / "double-process" / "exactly once" / "per replica", and the body must show the exact client-library option that sets the group.

2. **Graceful shutdown on SIGTERM.** Zerops sends SIGTERM to running containers during rolling deploys. A consumer that exits on SIGTERM without draining in-flight messages acks the batch, crashes, and loses the work. The gotcha stem must name SIGTERM or "graceful shutdown" or "in-flight" or "drain", and the body must show the concrete call sequence (catch SIGTERM → `nc.drain()` or equivalent → await → `process.exit(0)`).

Both of these interact with Zerops-specific mechanisms (`minContainers > 1` replica count — whether the replicas exist for throughput scaling or for HA / rolling-deploy availability, SIGTERM timing during rolling deploys) and belong in the PUBLISHED README, not CLAUDE.md — a porting user needs to know them before their first scaled deploy.

**Per-item IG code-block floor (enforced by `{hostname}_integration_guide_per_item_code`).** Every H3 heading inside the `integration-guide` fragment must carry at least one fenced code block in its section — any language (typescript, javascript, python, go, bash, yaml for a non-zerops.yaml snippet). The v18 appdev regression shipped IG step 3 ("Place `VITE_API_URL` in `build.envVariables` for prod, `run.envVariables` for dev") as prose only, with no code. A reader can't lift prose — they can lift a diff. If a step is prose-only, fold its content into a neighbouring step that carries a code block, or delete it.

**Worker drain code-block floor (enforced by `{hostname}_worker_drain_code_block`).** A separate-codebase worker README must contain at least one fenced code block showing the actual SIGTERM → drain → exit call sequence somewhere in either the integration-guide OR the knowledge-base fragment. The `worker_shutdown_gotcha` check verifies the topic is *mentioned*; this check verifies there's a copy-pasteable *implementation*. v7 shipped this as IG step 3 with full typescript; v18 lost it to prose inside a gotcha. Write the drain sequence as an IG item with the concrete code: `process.on('SIGTERM', ...)` → `await nc.drain()` → `await dataSource.destroy()` → `process.exit(0)`.

**Framework × platform gotcha candidates to consider.** The predecessor floor and authenticity classifier accept any platform-anchored or framework-intersection gotcha. The v7–v14 gold-standard runs included framework-integration insights that v15–v18 systematically filtered out because they didn't hit during the current debug round. When you reach the `readmes` sub-step, actively consider whether any of the following classes applied to *this* recipe — if yes and you have room under the per-codebase limits, write them up:

- **SDK module-system boundary (ESM-only vs CommonJS).** Managed-service client libraries (Meilisearch, Stripe, Prisma edge, some AWS v3 sub-packages) that ship ESM-only bindings fight with CommonJS-output frameworks (NestJS v10, Express, older Next.js). The symptom is `ERR_REQUIRE_ESM` or `Cannot use import statement outside a module` at import time, not at runtime. If your framework is CommonJS-based and you talked to an ESM-only SDK, add the gotcha with the workaround you used (fetch() over HTTP, dynamic import, tsconfig module shift).
- **Bundler major-version shift.** Vite 8 → Rolldown, Webpack 4 → 5, Turbopack → lightningcss — major-version bundler shifts silently change plugin compatibility, CSS handling, or output shape. If the recipe uses a bleeding-edge version that differs from the predecessor's, note what changed and whether ecosystem plugins for the previous bundler still work.
- **Dev-server `preview` mode separate host-check.** Vite-family dev servers have BOTH `server.allowedHosts` and `preview.allowedHosts` — configuring only one breaks the mode you didn't configure. If you set allowedHosts for dev, set the preview variant too, or note explicitly that preview mode isn't used.
- **Reconnect-forever pattern for long-running broker clients.** NATS, RabbitMQ, Kafka clients on Zerops need `reconnect: true` with `maxReconnectAttempts: -1` (or the client-library equivalent) so a brief broker restart doesn't take the worker down. The v7 worker README had this as IG #2; v15+ lost it.
- **Search-index re-push on redeploy seed.** When the seeder guard skips insert because rows already exist, ORM save-hooks never fire and the search index stays empty. The recipe must re-push the current entity set to the search engine regardless of whether the seed insert ran. Applies to every ORM + search combination — TypeORM/Meilisearch, Eloquent/Scout, Django/Whoosh.
- **Auto-indexing skips on idempotent re-seed.** Same root cause as above but the symptom is "search returns empty right after a redeploy". If you encountered it, write it. If you didn't (because your seeder does a raw re-push), still consider writing the gotcha as a "this is what WOULD break if you removed the re-push" warning.
- **Static-mount SPA tilde suffix (`./dist/~`).** The tilde strips the dist directory wrapper so files land at `/var/www/index.html` not `/var/www/dist/index.html`. Without it, Nginx serves a 404 on root. This is a Zerops-specific syntax — users from Vercel/Netlify will miss it.

These are *candidates*, not requirements. Don't pad the README with gotchas that don't apply. But do consciously walk the list instead of only writing gotchas from the specific failures that happened to surface during this particular run's debug rounds — the debug experience is a biased sample.

**Completion:**
```
zerops_workflow action="complete" step="deploy" substep="readmes" attestation="Wrote README.md + CLAUDE.md for appdev/apidev/workerdev. README gotchas narrate: NATS credential split (apidev only, worker cross-refs), Vite allowedHosts symptom (appdev — Blocked request HTTP 200), MinIO forcePathStyle (apidev only). Net-new >= 3, cross-README unique, no restatements. CLAUDE.md covers SSH, dev server startup, migration commands, and the SSHFS/tsc/fuser traps hit during this build."
```

After the sub-step completes, call the full deploy-step completion. The deploy-step checker runs every README content check (fragments, integration-guide code block floor, **integration-guide per-item code block** (v18), comment specificity, predecessor floor, knowledge-base authenticity, cross-README dedup, gotcha-distinct-from-guide, worker queue-group gotcha, worker shutdown gotcha, **worker drain code-block** (v18)) AND the per-codebase CLAUDE.md existence check — iterate on the content until they all pass, then the deploy step closes.

</block>
```
