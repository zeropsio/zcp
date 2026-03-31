# Response: refactor/recipe-knowledge-system

## Point 1 — Knowledge condensation

> "ta knowledge pro llm v zasade muze byt kondenzovanejsi"

The recipe knowledge-base content is already as slim as it gets. Here's the complete bun-hello-world knowledge-base after cleanup:

```markdown
### Base Image
Includes: Bun, npm, yarn, git, bunx. NOT included: pnpm.

### Gotchas
- BUN_INSTALL: ./.bun for build caching — default ~/.bun is outside the project tree
- Use bunx instead of npx — npx may not resolve correctly in Bun runtime
```

5 lines. Everything else the agent receives is structural YAML:
- **Integration guide** (~80 lines): the full zerops.yml with both prod and dev setups, inline comments explaining every decision. This is the actual configuration, not prose — you can't condense YAML fields without losing information.
- **Service definitions** (~35 lines): complete import YAML from env0 (dev/stage) and env4 (small-prod) with proven scaling values. Also actual YAML.

Nothing is duplicated. Platform universals (bind 0.0.0.0, deployFiles, tilde, build/run separation, autoscaling timing) live in `themes/universals.md` and are prepended once. The `NoPlatformDuplication` lint catches any recipe that restates them.

The concept of not having 1000 versions of knowledge for each thing — that's exactly what this implements. Universals = one place. Runtime gotchas = one place (knowledge-base fragment). Configuration patterns = one place (integration guide). Scaling values = one place (service definitions from imports).

## Point 2 — Layered runtime knowledge

> "sou to obecny knowleage spojeny s tou runtime services - ten system tech vrstev je podle me spravne"

The layered system is preserved. What changed is where the runtime layer lives.

**Before**: separate hand-written `runtimes/bun.md` (39 lines of prose) prepended to every Bun recipe via `GetRecipe`. This created:
- Binding stated 3 times (universals + runtime guide + recipe)
- Build procedure described in English paragraphs, not shown in actual YAML
- Deploy patterns as prose ("deployFiles: [.], start: zsc noop"), not actual config
- `npx` listed in base image (wrong — should be `bunx`)
- BUN_INSTALL not mentioned at all (the #1 Bun gotcha on Zerops)

**After**: `bun-hello-world` IS the runtime guide. `getRuntimeGuide("bun")` resolves to it via the fallback chain. It's served as layer 3 in `GetBriefing` when the agent asks about a stack. The content is:
- Integration guide with both prod and dev zerops.yml setups (inline comments teach the build procedure, deploy patterns, caching — by showing actual YAML, not describing it)
- Knowledge-base with 2 bun-specific gotchas (BUN_INSTALL, bunx)
- Service definitions with proven scaling values

Every knowledge item from the old `bun.md` exists in the new recipe — either in the knowledge-base sections or in the YAML itself. Plus BUN_INSTALL which was missing entirely.

What was removed: double-injection into `GetRecipe`. Old: `GetRecipe("laravel")` prepended the PHP runtime guide + universals + Laravel content. New: universals + Laravel content. Laravel's own integration guide teaches PHP configuration (Composer, Artisan, queue workers, nginx config) better than a generic PHP runtime guide could.

## Point 3 — Generative approach vs pre-made templates

> "mas llm ktery dneska dokaze z fleku generovat tisice a tisice radku, staci mu spravny info a validace"

The agent still generates from first principles. The service definitions are reference data, not templates.

At the provision step, the agent receives:
1. bootstrap.md — hostname patterns, dev/stage property table, validation checklist (unchanged)
2. core.md import.yml Schema — all field definitions, preprocessor functions, rules (unchanged)
3. The agent writes every line of import.yaml from scratch using these inputs (unchanged)

The service definitions in each recipe contain `buildFromGit` and `zeropsSetup` — recipe-system fields that have no meaning in bootstrap. If copied verbatim, the import would fail. They're reference implementations the agent reads to understand structural patterns and proven scaling values, then generates its own import.yaml from the schema.

**The concrete improvement**: before, the only scaling data was `minRam: 1.0 for compiled runtimes` — one value for 6 runtimes, Bun not even listed. Now for any runtime, the agent can reference a proven `verticalAutoscaling` block: Bun 0.5/0.25, Go 1.0/0.25, Java 1.5/1.0. These are data points for the generative approach, not replacements for it.

**Day 365 composite stack** (`bun + nextjs + python + postgres + valkey`): no single recipe matches. Agent generates from the schema. For each runtime, it looks up proven scaling values from the hello-world recipe's service definitions. Generation path identical to main — just with better numbers.

**Day 365 modification** (add service, change HA, custom scaling): service definitions irrelevant. Agent reads current state via `zerops_discover`, reads schema, generates modification. Unchanged from main.

## How the agent uses this content

The agent does not copy-paste recipe YAML. It generates zerops.yml and import.yaml from scratch during bootstrap, because the user's app has different hostnames, dependencies, env vars, and code than the recipe.

What the recipe content provides is **knowledge through commented examples**. The integration guide's zerops.yml teaches patterns: how to structure build vs run, where `BUN_INSTALL` goes, why `--frozen-lockfile`, how `initCommands` work with `zsc execOnce`, what `readinessCheck` looks like, how dev and prod setups differ structurally. The inline comments are critical — they explain *why* each value was chosen (`# Redirect bun's install cache into the project tree so Zerops can cache it`, `# Only bundled artifacts — no node_modules, no source. 156 KB total.`), not just what it is.

The agent reads these patterns, then applies them to the user's specific situation using the schema rules from `core.md` and the actual env vars discovered during provision. Same for service definitions — the agent reads proven scaling values and structural patterns (priority ordering, dev/stage pairing, verticalAutoscaling shape), then generates its own import from the schema.

This is exactly "the right info" — not English prose the agent interprets, but actual working YAML with explanatory comments the agent learns from.

## What the branch delivers

1. **Recipes sync from the API** — no drift. 33 recipes pulled dynamically. Integration guides include full zerops.yml with inline comments PLUS framework-specific integration steps (S3 setup, env var mapping, mailer config).

2. **Zero-duplication architecture** — platform universals in one place, prepended once. Knowledge-base contains only runtime-specific gotchas (5 lines for Bun). Integration guide is structural YAML with comments. `NoPlatformDuplication` lint enforces the boundary.

3. **Richer knowledge for the LLM** — old: 1 zerops.yml setup (prod only, no comments) + 8-line bare import skeleton + 39 lines of hand-written prose. New: 2 zerops.yml setups (prod + dev, inline comments explaining every decision) + complete import YAML from 2 environment tiers + framework-specific integration steps (when available). The agent learns patterns from commented YAML, not from English paragraphs.

4. **Better data for the generative approach** — per-runtime scaling values from production recipes. The agent generates from schema + rules + better reference data. Not templates — knowledge through example.
