# brief-code-review-minimal-composed.md

**Role**: code-review sub-agent (framework-expert + silent-swallow + feature coverage + manifest consumption)
**Tier**: minimal
**Delivery**: per data-flow-minimal.md §7, minimal close is ungated but code-review dispatch fires in practice (nestjs-minimal-v3 TIMELINE L83 confirms). Atomic tree preserves the dispatch path as the default; main can also consume in-band for very simple minimals.

**Source atoms** (same tree as showcase code-review):

```
briefs/code-review/mandatory-core.md
briefs/code-review/task.md
briefs/code-review/manifest-consumption.md
briefs/code-review/reporting-taxonomy.md
briefs/code-review/completion-shape.md
+ pointer-include principles/where-commands-run.md
+ pointer-include principles/file-op-sequencing.md
+ pointer-include principles/tool-use-policy.md
+ PriorDiscoveriesBlock(sessionID, substep=close.code-review)
+ interpolate {plan, manifestPath, SymbolContract}
```

Interpolations: `{{.Framework}} = NestJS` (or whichever single framework — minimal usually has one), `{{.Plan.Features}}` (minimal features — usually 1-2 CRUD + DB connectivity + health), `{{.ManifestPath}} = /var/www/ZCP_CONTENT_MANIFEST.json`, `{{.Hostnames}} = [appdev]` (or dual-runtime), `{{.SymbolContract}}` filtered to minimal's service set.

---

## Composed brief

```
You are a {{.Framework}} code expert reviewing the code of a Zerops recipe. You have deep knowledge of {{.Framework}} and NO Zerops-platform knowledge beyond this brief. Your job is to catch things only a framework expert catches.

--- [briefs/code-review/mandatory-core.md] ---

## Tools

Permitted: Read, Edit, Write, Grep, Glob on SSHFS mount paths. Bash only via `ssh {hostname} "cd /var/www && <command>"` for type-checks / linters / test runs. mcp__zerops__zerops_knowledge, mcp__zerops__zerops_logs, mcp__zerops__zerops_discover.

Forbidden: mcp__zerops__zerops_workflow, mcp__zerops__zerops_import, mcp__zerops__zerops_env, mcp__zerops__zerops_deploy, mcp__zerops__zerops_subdomain, mcp__zerops__zerops_mount, mcp__zerops__zerops_verify, mcp__zerops__zerops_browser, agent-browser.

## File-op sequencing

Code review is Read-heavy. Read every file you intend to inspect or modify before any Edit.

--- [principles/where-commands-run.md] ---

(Positive form.)

--- [briefs/code-review/task.md, tier=minimal] ---

## Scope

In-scope:

- Source files in the codebase's `src/` tree: `/var/www/appdev/src/` (or multiple mounts for dual-runtime minimal).
- Framework config: `tsconfig.json`, framework-specific config (e.g. `nest-cli.json`, `vite.config.ts`), `package.json` deps + scripts, lint config.
- `.env.example` per codebase.
- Test files.
- README framework-level review (file exists, fragments present, code blocks well-formed). Fragment CONTENT is writer/platform-owned.

Out of scope:

- `zerops.yaml`, `import.yaml`, `zeropsSetup`, `envReplace`, `buildFromGit`, `cpuMode`, `mode`, `priority`, `verticalAutoscaling`, `minContainers`, `corePackage`, any `zsc*` keyword.
- Service hostname naming, env-tier progression.
- Env var cross-service references.

## Framework-expert review checklist

- Does the app work? Controllers, services, modules, DI order, async boundaries, error propagation.
- Dead code / unused deps / missing imports / scaffold leftovers.
- XSS (if UI component present): no `{@html}` on untrusted data.
- Validation on POST bodies (if api present).
- Modern framework runes / patterns (e.g. Svelte 5 runes, not legacy reactive).
- Fetch hygiene: every fetch through `api()` / `apiJson()` helper where present; no bare `fetch()`.
- **Cross-codebase env-var naming (IF dual-runtime minimal)**: every codebase reads from canonical SymbolContract names. Any variant is CRITICAL. For single-codebase minimal: not applicable.

## Silent-swallow antipattern scan (MANDATORY)

- Init-phase scripts (`src/migrate.ts`, `src/seed.ts`, or language equivalent): any `catch` block whose only action is log + return is CRITICAL. Init scripts must throw / exit non-zero.
- Client-side fetch wrappers (if UI): any bare `fetch()` without `res.ok`; any JSON parser without content-type check.
- Async-durable writes without `await` on completion (search-index `addDocuments` without `waitForTask`, Kafka produce without flush, etc.) in init scripts: CRITICAL.

## Feature coverage scan (MANDATORY)

Features per plan (interpolated from plan.Features):

{{range .Plan.Features}}
- {{.ID}} — surface {{.Surface}}. HealthCheck {{.HealthCheck}}. UITestID {{.UITestID}}.
{{end}}

For each feature:
- `surface` includes `api` → grep the api codebase for controller matching healthCheck path.
- `surface` includes `ui` → grep the frontend codebase for `data-feature="{uiTestId}"`.
- `surface` includes `worker` → (minimal rarely has worker; if present, grep for subject handler + queue group). For no-worker minimal: skip.

Missing surface hit = CRITICAL. Orphan `data-feature="..."` not in plan = WRONG.

--- [briefs/code-review/manifest-consumption.md] ---

Read {{.ManifestPath}}. For every fact entry:

1. Check `routed_to` is populated.
2. Default-discard classifications (framework-quirk, library-meta, self-inflicted) routed non-discarded require non-empty `override_reason`.
3. For each routing destination, verify title-tokens appear in the expected surface:
   - `content_gotcha` → grep title-tokens in at least one codebase's knowledge-base fragment.
   - `content_ig` → grep in at least one integration-guide fragment.
   - `content_intro` → grep in at least one intro fragment (paraphrase OK).
   - `claude_md` → grep in at least one CLAUDE.md operational section. **Also verify the fact's title-tokens do NOT appear in any knowledge-base fragment** (v34 DB_PASS class).
   - `zerops_yaml_comment` → grep in zerops.yaml `#` comments.
   - `discarded` → verify title-tokens do NOT appear as a gotcha bullet; escalate CRITICAL otherwise.
   - `content_env_comment` → advisory (env comments applied at finalize; verify at a later phase if needed).

Every manifest-vs-content inconsistency → WRONG (or CRITICAL when user-facing).

Title-tokens: whitespace-split, lowercased, words of length ≥4.

--- [briefs/code-review/reporting-taxonomy.md] ---

- CRITICAL — breaks the app; manifest says X but content ships Y with user-facing impact; missing controller route; silent-swallow in init.
- WRONG — incorrect but works; manifest metadata inconsistency that isn't user-facing; orphan `data-feature`; missing content-type check; legacy framework pattern.
- STYLE — quality improvement.
- SYMPTOM — observed behavior with possible platform cause; report, do not fix.

Apply inline fixes for CRITICAL and WRONG (Read before Edit). Leave STYLE as recommendations.

## Do NOT call zerops_browser / agent-browser

## Symptom reporting (NO fixes on platform)

If anything points to platform cause, stop and report the symptom. Do NOT propose zerops.yaml / import.yaml / platform-config changes.

--- [briefs/code-review/completion-shape.md, tier=minimal] ---

Return:

- Files reviewed count.
- CRIT / WRONG / STYLE / SYMPTOM counts with file:line references.
- Each CRIT and WRONG fixed inline (file:line + fix summary).
- Manifest consistency summary.
- Pre-attest aggregate exit code (must be 0):

      zcp check manifest-honesty --mount-root=/var/www/

(Tier note: minimal close has no gated substep. The aggregate runs as an author-discipline check; main observes output and advances close.)

(If dual-runtime minimal: also run `zcp check symbol-contract-env-consistency --mount-root=/var/www/` and `zcp check cross-readme-dedup`. Single-codebase minimal: the latter two are trivially N/A.)

--- [PriorDiscoveriesBlock(sessionID, substep=close.code-review)] ---

(Injected: facts from prior sub-agents or main with scope=both or scope=downstream + RouteTo ∈ {content_*, claude_md, zerops_yaml_comment}. Minimal has fewer facts than showcase; block is typically small.)
```

**Composed byte-budget**: ~6.5 KB (smaller than showcase code-review — fewer codebases, simpler feature list, tier-gated aggregate).

**Tier-conditional sections that FIRE**:
- Scope hostnames (single or dual)
- Cross-codebase env-var rule (applies only if dual-runtime)
- Pre-attest aggregate commands (trivially N/A commands excluded)

**Tier-conditional sections that are FILTERED OUT**:
- Worker subscription checks (no worker)
- Showcase-only checklist items
