# Audit addendum — Two live agent runs from the last release

**Date:** 2026-05-16
**Inputs:** `run1.txt` (1.2 MB, 444 lines JSONL) — Next.js+Postgres PM app (session id `27034730…`). `run2.txt` (1.8 MB, 708 lines JSONL) — Cadence multi-service PM app with apidev/apistage/appdev/appstage + db/cache/search/storage (session id `7b3a6d7c…`).
**Context:** Two separate user-driven sessions, different projects, both running the LAST RELEASED zcp binary (no local changes). Sessions are still alive — Karel can ask the agents for clarification.
**Method:** Filtered the JSONL transcripts for env-related tool calls, edits, and texts. Counted deploy retries, cataloged the failure patterns, mapped to the audit's contradictions/gaps.

---

## A. Headline numbers

| Metric | Run 1 | Run 2 |
|---|---|---|
| Services | 2 (appdev + db) | 8 (apidev/apistage/appdev/appstage + db/cache/search/storage) |
| `zerops_deploy` calls | **23** | **32** |
| `BUILD_FAILED` events (raw text matches) | 30 | 70 |
| `deploy refused` events | 0 | 10 |
| `zerops.yaml` rewrites (Write calls) | **9** | **17** |
| "bisect" self-narration | 20 | 52 |
| Memory files agent wrote about env quirks | **3** | 0 (different project, no memory carryover) |

The agent itself acknowledged it was bisecting in both runs — run2 has an explicit TodoWrite entry: *"Bisect appdev envVariables — likely HOSTNAME reserved"*.

---

## B. Concrete env-var failure modes — repeatable across both runs

### B1. `HOSTNAME: 0.0.0.0` in `run.envVariables` → silent BUILD_FAILED in 4-5s

**Run 1:** First zerops.yaml at run1.L66 has `HOSTNAME: 0.0.0.0`. 4 consecutive deploys fail (L72-78). Agent removes lines one-by-one (L192-213) until only `DATABASE_URL` remains. Each retry costs ~30-60s wall time on the build attempt.

**Run 2:** First zerops.yaml at run2.L86 has `HOSTNAME: 0.0.0.0`. Build cycles 10+ times (L196-378) before the agent's todo entry surfaces: *"Bisect appdev envVariables — likely HOSTNAME reserved"*. Removes HOSTNAME at L342. Deploy succeeds. Same exact pattern as run1.

**Damning detail:** Run 1's session wrote `feedback_zerops_yaml_envvars.md` to memory (`/home/zerops/.claude/projects/-var-www/memory/`) with the exact discovery. Run 2 is on a DIFFERENT project — its memory directory is `/home/zerops/.claude/projects/-var-www/memory/` too (same path because both runs are inside Zerops VS Code containers with `cwd=/var/www`) but the project / session is different so the agent rediscovers from scratch. The damage is per-project, not amortized.

**Mechanism (verified live in main audit):** `HOSTNAME` is a Linux OS env that Zerops also auto-injects per container. The platform's build pipeline rejects the line as a reserved-name conflict but the rejection surfaces as `BUILD_FAILED` with empty build logs. The corpus has zero coverage of reserved env-var names. The agent has no way to learn this except by bisecting.

### B2. `${db_connectionString}` resolves WITHOUT `/dbName` → Prisma connects to wrong database

**Run 1:** Agent writes `DATABASE_URL: ${db_connectionString}` at run1.L66. After deploys succeed, runtime check at L237-241 reveals: `DATABASE_URL=postgresql://db:DYg...@db:5432` — **no database name**. Prisma defaults to admin `postgres` DB, hits permission denied on `public` schema. Agent fixes by manually composing: `DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}` at run1.L231. Wrote `feedback_zerops_postgres.md` to memory.

**Run 2:** Agent writes `DATABASE_URL: ${db_connectionString}` at run2.L68. Same flow: signup hits `permission denied for schema public`. Agent at run2.L356-417 SSHs in, manually grants schema permissions using superUser credentials (`db_superUser` + `db_superUserPassword`), then at run2.L461 swaps to manual composition. **Identical workaround, 5 deploys late.**

**Mechanism (verified live in main audit):** Postgres `connectionString` env var is intentionally generic (`postgresql://${user}:${password}@${hostname}:${port}`) so it works for clients that auto-discover databases. Prisma needs the fully-qualified URL with the database name appended. The audit's stream-A catalog includes this gap under `develop-first-deploy-env-vars.md`'s coverage gap list — "no `connectionString` shape documentation."

### B3. `${search_masterKey}` used despite atom guidance to pick narrow keys

**Run 2 only** (no search service in run1): zerops.yaml at run2.L68 has:
```
MEILI_API_KEY: ${search_masterKey}
```

The atom `develop-first-deploy-env-vars.md:23-30` explicitly says *"Pick the narrow scoped API key for search services, never the master key."* The agent SAW this atom — the string "narrow" or "narrower" appears 4 times in tool_results within run2.txt — and ignored it.

**Why the guidance failed:** The guidance is teaching that the master key has elevated privileges (admin scope, can create/delete indexes). Apps doing search reads need a `search` scoped key. But:
1. The atom only names the prohibition; it doesn't tell the LLM how to FIND the narrow key in the discover output (Meilisearch lists keys as numbered entries, e.g. `keys.0.key`, `keys.1.key`).
2. The agent's mental model: "I see `search_masterKey` in discover → I use it." The atom doesn't override the path of least resistance.

This is a CORPUS-CONFIDENCE problem — the rule exists but isn't reinforced at the moment of decision (the agent looks at `zerops_discover` output, sees `search_masterKey`, and the atom guidance is no longer in working memory).

### B4. `INTERNAL_API_URL: http://apidev:3001` literal — no atom on inter-service HTTP

**Run 2:** Agent writes `INTERNAL_API_URL: http://apidev:3001` (hardcoded hostname:port for frontend → backend). This is correct (services talk to each other via private hostnames + internal ports), but the agent had to derive it from training data; no atom shows this pattern.

The atom `develop-env-var-channels.md` and `develop-first-deploy-env-vars.md` cover managed-service env vars (db/cache/search/storage) but NOT runtime-to-runtime HTTP wiring. Agents on multi-runtime projects must invent the pattern.

### B5. `NEXT_PUBLIC_API_URL: https://apidev-${zeropsSubdomainHost}-3001.prg1.zerops.app` — agent constructed the URL template

**Run 2:** For Next.js's NEXT_PUBLIC_* (baked into client bundle), the agent needs a PUBLIC URL. It correctly used `${zeropsSubdomainHost}` to construct `https://apidev-${hash}-3001.prg1.zerops.app`. This works because `zeropsSubdomainHost` is project-scope (`227a` or similar) and propagates.

But the agent suspected (correctly per run2.L329) that this template might not resolve: *"Možná `${zeropsSubdomainHost}` v `run.envVariables` se nedaří resolve. Zkusím to dočasně vyhodit."* — the agent took ~3 deploys to verify the template actually does resolve.

**Confirms audit finding A5 (zeropsSubdomain triplet):** The agent had no guidance on which of `zeropsSubdomainHost` / `zeropsSubdomainString` / `zeropsSubdomain` to use, nor on whether they resolve in `run.envVariables`. Bisect cost: ~3 deploys.

---

## C. What the agent did NOT do (negative evidence for Model α habit)

Both agents declared every cross-service var explicitly in `run.envVariables`. Neither agent:

- Tested `process.env.db_connectionString` directly (without declaring DATABASE_URL) to see if auto-inject would deliver the value.
- Used `process.env.db_password` / `process.env.db_hostname` in app code directly (would have needed zero `run.envVariables` lines for the DB).
- Read the audit's Model β behavior (auto-inject) anywhere in their tool results.

The corpus that DID reach the agent (in tool_results) said:
- *"Zerops injects env vars as OS env vars. Do NOT create `.env` files — empty values shadow OS vars."* (4× per run)
- *"Cross-service wiring: `${hostname_varname}` in zerops.yaml `run.envVariables`."* (4× per run)

These two bullets *together* hint at the auto-inject mechanism but the agent reads them as: "Use `${hostname_varname}` template in run.envVariables" — i.e., Model α. The OS-injection sentence is interpreted as the BACKEND for the template, not as "values are already in process.env without declaration."

**This is the strongest empirical evidence in the audit for the Model α/β confusion.** Two production sessions, two agents, identical mistake pattern, no realization that declaration is for renames only.

---

## D. New gaps surfaced by the runs (not in the main audit)

### D1. Reserved env-var names (HOSTNAME, …)
Zero coverage. The corpus must list reserved names that platform pre-occupies and would silently fail if written to `run.envVariables`. Candidates: `HOSTNAME`, `PATH`, possibly `USER`, `HOME`, others. Need to enumerate from platform behavior.

**Atom F11 (new, priority 1):** `develop-reserved-env-names.md` — list reserved names + symptom (BUILD_FAILED ~4s, empty logs).

### D2. Postgres `connectionString` shape
The catalog atoms list `connectionString` as the canonical key but don't reveal it lacks `/dbName`. For Prisma + every ORM that needs a fully-qualified URL, this is a deploy-breaking gap.

**Atom edit F12 (priority 1):** In `develop-first-deploy-env-vars.md`, add a worked example: *"Postgres `connectionString` resolves to `postgresql://user:password@host:port` WITHOUT a database name. For Prisma, Drizzle, sqlx, or any client that needs the database in the URL, compose explicitly: `DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}`."*

### D3. Inter-runtime HTTP wiring
Zero coverage. When app→api or worker→api communication is needed inside the project network, agents reinvent `http://hostname:port`. The pattern is correct but should be in an atom.

**Atom F13 (new, priority 2):** `develop-inter-service-http.md` — covers internal HTTP (plain HTTP, hostname-only, no need for `${...}` since hostnames are stable per project) AND when to use public subdomain URLs (browser/client-side → public; server-side → internal).

### D4. `zeropsSubdomainHost` does resolve in `run.envVariables`
Run 2 the agent suspected it wouldn't. It does. The corpus should confirm this with a one-liner example.

**Atom edit F14 (priority 2):** In the new zeropsSubdomain-triplet atom (F5 in main audit), add: *"All three keys (`zeropsSubdomainHost`, `zeropsSubdomainString`, `zeropsSubdomain`) resolve when used in another service's `run.envVariables`. Example: `NEXT_PUBLIC_API_URL: https://api-${zeropsSubdomainHost}-3001.prg1.zerops.app` is valid."*

### D5. Prisma + Alpine + OpenSSL
Tangential to env vars but observed in run 2 (L529): `prepareCommands: - sudo apk add --no-cache openssl` to fix Prisma's libssl detection on Alpine. Not an env issue but worth a recipe-level note. Out of scope for this audit; flag for the recipe team.

### D6. Schema permissions on Postgres `db` user
Run 2 (L356-417) the agent had to manually `GRANT ALL ON SCHEMA public TO "${db_user}"` because the regular user lacks default schema privileges. Prisma's first `db push` fails on `permission denied for schema public`. Recipe-level note; out of scope but the env catalog atom could mention the `db_superUser` / `db_superUserPassword` exists for DDL operations.

---

## E. Updated priority ranking (with run-evidence weight)

Re-ranking F1-F14 by how much agent-time the gap costs per session:

| Rank | Fix | Cost evidence | Audit ID |
|---|---|---|---|
| 1 | **Reserved env-var names** (HOSTNAME at minimum) | run1: 4 deploys / ~8 min; run2: 10+ deploys / ~15 min | D1 / new F11 |
| 2 | **Postgres `connectionString` lacks dbName** | both runs: 1-2 extra deploys + manual schema grants | D2 / F12 |
| 3 | **Auto-inject mechanism in main atom** | both runs: agent declared every var; cost is integrated into every deploy | A1 / F1 |
| 4 | **Self-shadow symptom = literal `${var}`, not empty** | indirect — would help debugging the unresolved cases | A2 / F2 |
| 5 | **`zeropsSubdomainHost` resolves in run.envVariables** | run2: ~3 deploys bisecting | A5 + D4 / F5+F14 |
| 6 | **Inter-runtime HTTP wiring atom** | run2: 0 deploys (agent guessed right), but cognitive load was non-zero | D3 / F13 |
| 7 | **`envIsolation` mode semantics** | not surfaced in these runs (both projects defaulted to whatever the project had) | B3 / F4 |
| 8 | **Narrow-key reinforcement (search_masterKey)** | run2: agent ignored existing guidance → security regression, no immediate failure | indirect / atom edit |
| 9-14 | rest (envSecrets, envReplace, ZEROPS_*, RUNTIME_*/BUILD_*, build-time timing, env coherence) | not surfaced in these runs | unchanged |

The top two (reserved names + connectionString shape) cost approximately **20-25 deploy attempts split across the two sessions** — at ~30-60s wall time per failed build, that's 10-25 minutes of friction per agent run that DOES not surface anywhere except as a confusing build-failure-with-empty-logs.

---

## F. Three direct questions for the agents that are still alive

Karel offered to ask them. Suggested probes:

1. **For run1 agent**: *"When you bisected zerops.yaml at the HOSTNAME line, what was the empty build log actually showing? Was there any signal that pointed at HOSTNAME specifically, or did you arrive at it by elimination?"* — confirms whether build_logs surfaces any hint we could amplify.

2. **For run2 agent**: *"You suspected `${zeropsSubdomainHost}` wouldn't resolve in `run.envVariables` and removed it to test. What was the evidence that made you suspect it specifically? Was it a tool description, a guide line, or a guess?"* — tells us whether existing corpus hints in the wrong direction.

3. **For both**: *"Did you ever consider NOT declaring `DATABASE_URL` and reading `process.env.db_connectionString` (auto-injected by the platform) instead? If not, why wasn't that an option in your mental model?"* — direct test of Model α/β confusion.

If Karel asks, the answers will sharpen the F1 rewrite considerably — instead of writing a general "auto-inject is the model" atom, the rewrite can target the EXACT mental shortcut the agents took.

---

## G. Conclusion of the addendum

The two live runs **empirically confirm** the audit's hypothesis. The agent operates in Model α (declarative wiring) despite the platform running Model β (auto-inject). The cost is measured: ~10-25 minutes per session of bisecting BUILD_FAILED events to identify env-var problems the corpus could prevent.

The reserved-name failure (HOSTNAME) is a NEW finding not in the main audit's authoritative-docs review — Zerops' own docs don't list reserved names either, so this is a platform behavior that needs documentation-via-atom (or a deploy-time error message upgrade).

The Postgres connectionString shape is a second new finding — auditing the public docs (`../zerops-docs/apps/docs/content/features/env-variables.mdx`) shows `connectionString` mentioned but its exact format never spelled out. This needs both a docs update and an atom-level worked example.

Recommended next step (Karel's call):
- (a) Implement F11 + F12 immediately (highest-cost, lowest-effort fixes) before the next release.
- (b) Ask the three questions above to the live agents to sharpen F1 (auto-inject atom rewrite).
- (c) Run the audit's `(b)` open question — provision a fresh `envIsolation=service` project to empirically verify Model β behavior under default isolation, so F1 can be written with empirical confidence.
