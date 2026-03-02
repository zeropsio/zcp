# Recipe Validation Report - Agent 4

Recipes: svelte-nodejs, svelte-static, qwik-nodejs, qwik-static, react-nodejs-ssr

---

# Validation: svelte-nodejs

## Repo: recipe-svelte-nodejs

## Findings

### F1: Recipe adds `cache: node_modules` not present in repo zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: node_modules` in build section
- Repo says: no `cache` field at all
- Recommendation: Keep in recipe -- `cache` is a best practice optimization. Not a mismatch per se, but a recipe enhancement over the repo. No action needed.

### F2: Recipe omits `run.base: nodejs@20` present in repo zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: no `run.base` field (only `build.base: nodejs@20`)
- Repo says: `run.base: nodejs@20` explicitly declared
- Recommendation: Add `run.base: nodejs@20` to recipe zerops.yml. While nodejs services may default this, the repo explicitly sets it and the recipe should match.

### F3: Recipe Configuration section shows `preprocess` before `kit`, repo has `kit` before `preprocess`
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `preprocess: vitePreprocess(), kit: { adapter: adapter() }`
- Repo says: `kit: { adapter: adapter() }, preprocess: vitePreprocess()`
- Recommendation: Cosmetic difference, no functional impact. No action needed.

### F4: Recipe mentions `@sveltejs/vite-plugin-svelte` must be installed but it is already a dependency
- Severity: P2
- Category: knowledge-gap
- Recipe says: (Gotchas) "`@sveltejs/vite-plugin-svelte` must be installed -- required for `vitePreprocess()`"
- Repo says: Already in `dependencies` in package.json
- Recommendation: This is correct documentation -- it warns users who may be starting from scratch. Keep as-is.

## Verdict: NEEDS_FIX (1 issue)

P1: F2 (missing `run.base`)

---

# Validation: svelte-static

## Repo: recipe-svelte-static

## Findings

### F1: Recipe adds `cache: node_modules` not present in repo zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: node_modules` in build section
- Repo says: no `cache` field
- Recommendation: Keep in recipe -- best practice enhancement. No action needed.

### F2: Repo layout.ts has `export const ssr = false` which recipe does not mention
- Severity: P1
- Category: knowledge-gap
- Recipe says: only mentions `export const prerender = true;` in `src/routes/+layout.ts`
- Repo says: `export const ssr = false;` AND `export const prerender = true;` in `src/routes/+layout.ts`
- Recommendation: Add `export const ssr = false;` to the recipe's Configuration section and Gotchas. Both lines are present in the repo and both serve a purpose (ssr=false prevents SSR attempts, prerender=true enables static generation).

### F3: Recipe Gotchas references `svelte-ssr` recipe but the actual recipe name is `svelte-nodejs`
- Severity: P1
- Category: missing-section
- Recipe says: "For SSR SvelteKit with Node.js runtime, use the `svelte-ssr` recipe instead"
- Repo says: N/A (recipe cross-reference issue)
- Recommendation: Change to "use the `svelte-nodejs` recipe instead" to match the actual recipe filename.

## Verdict: NEEDS_FIX (2 issues)

P1: F2 (missing `ssr = false`), F3 (wrong recipe cross-reference name)

---

# Validation: qwik-nodejs

## Repo: recipe-qwik-nodejs

## Findings

### F1: Recipe adds `cache: node_modules` not present in repo zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: node_modules` in build section
- Repo says: no `cache` field
- Recommendation: Keep in recipe -- best practice enhancement. No action needed.

### F2: Recipe omits `run.base: nodejs@20` present in repo zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: no `run.base` field (only ports and start)
- Repo says: `run.base: nodejs@20` explicitly declared
- Recommendation: Add `run.base: nodejs@20` to recipe zerops.yml run section.

### F3: Recipe claims trust proxy should be enabled but repo does NOT have it
- Severity: P2
- Category: knowledge-gap
- Recipe says: (Configuration) "Enable it in the generated `src/entry.express.tsx`: `app.set('trust proxy', true);`"
- Repo says: No `trust proxy` setting anywhere in `src/entry.express.tsx` or any other file
- Recommendation: This is advisory guidance in the recipe that the repo hasn't implemented. The recipe is correct to recommend it for production behind Zerops L7 balancer. Keep as-is, but consider noting it as "recommended" rather than implying it's already configured.

### F4: Recipe says run `npm run qwik add express` but repo already has adapters/express
- Severity: P2
- Category: knowledge-gap
- Recipe says: (Configuration) "The Express adapter must be added to the Qwik project before deploying"
- Repo says: `adapters/express/` directory already exists with `vite.config.ts`, `src/entry.express.tsx` already exists
- Recommendation: This is correct documentation for users starting fresh. The repo has it pre-configured. No action needed.

### F5: Repo uses dotenv but recipe does not mention it
- Severity: P2
- Category: knowledge-gap
- Recipe says: no mention of dotenv
- Repo says: `import "dotenv/config";` in `entry.express.tsx`, `dotenv` in devDependencies
- Recommendation: Minor -- dotenv is for local dev, not needed on Zerops where env vars are injected. No action needed.

### F6: Repo getOrigin proxy code is commented out
- Severity: P2
- Category: knowledge-gap
- Recipe says: recommends `app.set('trust proxy', true)` for proxy handling
- Repo says: has commented-out `getOrigin` function in entry.express.tsx that handles x-forwarded-proto/host headers (Qwik City's own proxy mechanism)
- Recommendation: The recipe could mention the Qwik City `getOrigin` approach as an alternative to Express trust proxy. Low priority.

## Verdict: NEEDS_FIX (1 issue)

P1: F2 (missing `run.base`)

---

# Validation: qwik-static

## Repo: recipe-qwik-static

## Findings

### F1: Recipe adds `cache: node_modules` not present in repo zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: node_modules` in build section
- Repo says: no `cache` field
- Recommendation: Keep in recipe -- best practice enhancement. No action needed.

### F2: Recipe Gotchas references `qwik-ssr` recipe but the actual recipe name is `qwik-nodejs`
- Severity: P1
- Category: missing-section
- Recipe says: "For SSR Qwik with Node.js runtime, use the `qwik-ssr` recipe instead"
- Repo says: N/A (recipe cross-reference issue)
- Recommendation: Change to "use the `qwik-nodejs` recipe instead" to match the actual recipe filename.

### F3: Repo static adapter has hardcoded origin URL
- Severity: P2
- Category: knowledge-gap
- Recipe says: no mention of origin configuration
- Repo says: `origin: "https://yoursite.qwik.dev"` in `adapters/static/vite.config.ts`
- Recommendation: Consider mentioning in Gotchas that users should update the origin URL in `adapters/static/vite.config.ts` for proper sitemap generation. Low priority since it doesn't affect deployment.

## Verdict: NEEDS_FIX (1 issue)

P1: F2 (wrong recipe cross-reference name)

---

# Validation: react-nodejs-ssr

## Repo: recipe-react-nodejs

## Findings

### F1: Recipe adds `cache: node_modules` not present in repo zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: node_modules` in build section
- Repo says: no `cache` field
- Recommendation: Keep in recipe -- best practice enhancement. No action needed.

### F2: Recipe omits `run.base: nodejs@20` present in repo zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: no `run.base` field (only ports and start)
- Repo says: `run.base: nodejs@20` explicitly declared
- Recommendation: Add `run.base: nodejs@20` to recipe zerops.yml run section.

### F3: Recipe claims trust proxy should be enabled but repo does NOT have it
- Severity: P2
- Category: knowledge-gap
- Recipe says: (Configuration + Gotchas) "Enable trust proxy in `server.js`... `app.set('trust proxy', true);`"
- Repo says: No `trust proxy` setting anywhere in `server.js`
- Recommendation: Same as qwik-nodejs -- this is advisory guidance. The repo hasn't implemented it. Keep as-is but note it as "recommended" rather than implying it's pre-configured.

### F4: Recipe shows simplified server.js but repo has full implementation
- Severity: P2
- Category: knowledge-gap
- Recipe says: simplified 5-line server.js example with `app.listen(3000)`
- Repo says: 71-line server.js with Vite dev mode, SSR manifest, compression, sirv static serving, template rendering, error handling, and `process.env.PORT || 3000`
- Recommendation: This is expected for a recipe -- it shows the essential pattern. Consider mentioning that the full server.js handles both dev and production modes. Low priority.

### F5: Repo server.js uses `process.env.PORT || 3000` but recipe hardcodes 3000
- Severity: P2
- Category: knowledge-gap
- Recipe says: `app.listen(3000);`
- Repo says: `const port = process.env.PORT || 3000;` and `app.listen(port, ...)`
- Recommendation: Consider showing the PORT env var pattern in the recipe example. Low priority since Zerops sets PORT=3000 by default matching the declared port.

### F6: Recipe uses `pnpm build` but repo package.json script is `npm run build:client && npm run build:server`
- Severity: P2
- Category: knowledge-gap
- Recipe says: `pnpm build`
- Repo says: `"build": "npm run build:client && npm run build:server"` -- but the repo zerops.yml also says `pnpm build`
- Recommendation: No mismatch -- `pnpm build` resolves to the package.json build script. Recipe matches repo zerops.yml exactly.

### F7: Repo uses `cross-env` as a devDependency but it's needed at runtime via start script
- Severity: P2
- Category: knowledge-gap
- Recipe says: `start: pnpm start`
- Repo says: `"start": "cross-env NODE_ENV=production node server"` with `cross-env` as devDependency
- Recommendation: This works because `pnpm i` installs devDependencies and node_modules is deployed. But cross-env as a devDependency used in production start is fragile. Could mention this in Gotchas. Low priority.

### F8: Recipe does not mention `compression` and `sirv` packages used in repo
- Severity: P2
- Category: knowledge-gap
- Recipe says: no mention of compression or sirv
- Repo says: Uses `compression` and `sirv` packages in production mode in server.js
- Recommendation: These are implementation details of the full server.js. Recipe's simplified example is sufficient. No action needed.

## Verdict: NEEDS_FIX (1 issue)

P1: F2 (missing `run.base`)

---

# Summary

| Recipe | Verdict | P0 | P1 | P2 |
|--------|---------|----|----|-----|
| svelte-nodejs | NEEDS_FIX | 0 | 1 | 3 |
| svelte-static | NEEDS_FIX | 0 | 2 | 1 |
| qwik-nodejs | NEEDS_FIX | 0 | 1 | 4 |
| qwik-static | NEEDS_FIX | 0 | 1 | 2 |
| react-nodejs-ssr | NEEDS_FIX | 0 | 1 | 7 |

## P1 Issues Requiring Fix (Total: 6)

1. **svelte-nodejs**: Add `run.base: nodejs@20` to recipe zerops.yml
2. **svelte-static**: Add `export const ssr = false;` to Configuration section and layout.ts example
3. **svelte-static**: Fix cross-reference from `svelte-ssr` to `svelte-nodejs`
4. **qwik-nodejs**: Add `run.base: nodejs@20` to recipe zerops.yml
5. **qwik-static**: Fix cross-reference from `qwik-ssr` to `qwik-nodejs`
6. **react-nodejs-ssr**: Add `run.base: nodejs@20` to recipe zerops.yml

## Common Pattern: `cache: node_modules`

All 5 recipes include `cache: node_modules` which none of the repos have. This is a recipe enhancement (best practice) over the repos. Consistent across all recipes, no action needed.

## Common Pattern: `run.base` for nodejs services

3 of 3 nodejs recipes (svelte-nodejs, qwik-nodejs, react-nodejs-ssr) are missing `run.base: nodejs@20` which all 3 repos explicitly declare. This appears to be a systematic omission in recipe generation.
