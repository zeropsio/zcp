package workflow

import "strings"

// Plan predicates classify a RecipePlan along the shape dimensions that
// drive conditional composition of the recipe workflow guide. Each predicate
// is pure (no I/O, no allocation beyond local strings) and returns a stable
// answer for a given plan — safe to call multiple times during a single
// guide assembly.
//
// Naming convention: `isX` for tier/shape classifiers, `hasX` for
// target-composition classifiers. Every predicate accepts nil and returns
// false, so callers don't need to defend against nil plans.

// isDualRuntime returns true for API-first showcases that have both an
// app role (frontend/static) and an api role (backend runtime). The
// dual-runtime URL env-var pattern block in the generate section is
// gated on this predicate.
func isDualRuntime(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	var hasApp, hasAPI bool
	for _, t := range p.Targets {
		switch t.Role {
		case RecipeRoleApp:
			hasApp = true
		case RecipeRoleAPI:
			hasAPI = true
		}
	}
	return hasApp && hasAPI
}

// hasWorker returns true when the plan declares at least one worker target.
func hasWorker(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	for _, t := range p.Targets {
		if t.IsWorker {
			return true
		}
	}
	return false
}

// hasSharedCodebaseWorker returns true when at least one worker target
// has SharesCodebaseWith set (i.e. the worker rides inside its host
// target's codebase, resulting in a third `setup: worker` block in the
// host's zerops.yaml rather than a separate worker repo).
//
// Not used to gate any catalog block — the worker-setup-block explains
// both shapes inline (the block text is symmetric and fires on
// hasWorker regardless of sharing). Consumed by buildGenerateRetryDelta
// to decide whether the retry reminder should mention `setup: worker`.
// The block-level shape distinction lives in prose (the authoritative
// 4-row table in zerops-yaml-header), not in predicate gating — one
// block per section keeps the duplication down.
func hasSharedCodebaseWorker(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	for _, t := range p.Targets {
		if t.IsWorker && t.SharesCodebaseWith != "" {
			return true
		}
	}
	return false
}

// hasSeparateCodebaseWorker returns true when at least one worker target
// has SharesCodebaseWith empty (i.e. the worker owns its own repo with
// its own zerops.yaml, typical for the 3-repo dual-runtime case and for
// any worker consuming from a standalone broker).
//
// Consumed by buildDeployRetryDelta to decide which cross-deploy
// sequence to remind the agent about. Not used to gate any catalog
// block — see the note on hasSharedCodebaseWorker for why. The
// authoritative shape enumeration lives in the 4-row table inside the
// zerops-yaml-header block at generate.
func hasSeparateCodebaseWorker(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	for _, t := range p.Targets {
		if t.IsWorker && t.SharesCodebaseWith == "" {
			return true
		}
	}
	return false
}

// serveOnlyBases are Zerops runtime types that can only serve precompiled
// assets — no shell, no package manager, no dev process. Recipes whose
// prod target uses one of these must switch to a compile-capable base in
// the setup:dev block or dev mode has nothing to run.
var serveOnlyBases = map[string]struct{}{
	"static": {},
	"nginx":  {},
}

// hasServeOnlyProd returns true when any prod-facing (non-worker) target
// runs on a serve-only base and therefore needs a dev-base override at
// generate.
func hasServeOnlyProd(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	for _, t := range p.Targets {
		if t.IsWorker {
			continue
		}
		base, _, _ := strings.Cut(t.Type, "@")
		if _, ok := serveOnlyBases[base]; ok {
			return true
		}
	}
	return false
}

// hasBundlerDevServer returns true when the recipe runs a framework whose
// dev server enforces an HTTP Host-header allow-list (Vite family, webpack,
// Angular CLI, Next.js dev). Drives the dev-server allow-list block at
// generate and the Vite collision trap block at deploy.
//
// Two triggering cases:
//  1. The primary framework matches a bundler prefix (single-runtime SPA,
//     e.g. a svelte-showcase plan whose Framework is "svelte").
//  2. A dual-runtime recipe whose API framework is a plain backend (NestJS,
//     Express) but whose frontend target is static/serve-only — the
//     frontend dev container still runs a bundler dev server (Vite,
//     webpack) and needs the host-check allow-list even though
//     p.Framework names the API.
//
// The framework list matches the set of recipes where LOG2 bug 15
// (dev-server host-check) is reachable. Adding a new framework that uses a
// bundler dev server requires extending `bundlerFrameworks` here.
func hasBundlerDevServer(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	fw := strings.ToLower(p.Framework)
	for _, prefix := range bundlerFrameworks {
		if strings.HasPrefix(fw, prefix) {
			return true
		}
	}
	// Dual-runtime + static frontend: the frontend runs a bundler dev
	// server regardless of what p.Framework names.
	return isDualRuntime(p) && hasServeOnlyProd(p)
}

// bundlerFrameworks lists framework name prefixes whose dev servers enforce
// an HTTP Host-header allow-list. Match is prefix-based so framework
// variants (e.g., "next.js", "nextjs", "next-intl") all match "next".
//
// Source: cross-referenced against internal/knowledge/recipes/*.md —
// every recipe whose framework runs a host-checking dev server must have
// its prefix in this list, or LOG2 bug 15 will not be surfaced.
var bundlerFrameworks = []string{
	"react", "vue", "svelte", "sveltekit", "nuxt", "next", "nextjs",
	"astro", "qwik", "angular", "remix", "solid", "solidstart",
	"analog", "react-router",
}

// managedServiceBases is the set of Zerops service types whose env vars the
// agent needs to catalog at provision time. Drives hasManagedServiceCatalog.
// Keep in sync with internal/platform or schema if new managed types ship —
// unknown bases are silently treated as "not managed".
var managedServiceBases = map[string]struct{}{
	"postgresql":     {},
	"mariadb":        {},
	"mysql":          {},
	"mongodb":        {},
	"keydb":          {},
	"valkey":         {},
	"redis":          {},
	"nats":           {},
	"kafka":          {},
	"rabbitmq":       {},
	"meilisearch":    {},
	"elasticsearch":  {},
	"typesense":      {},
	"object-storage": {},
}

// hasManagedServiceCatalog returns true when the plan has at least one
// managed service (db, cache, queue, storage, search) whose env vars will
// need cataloging at provision. Drives the env-var catalog block.
func hasManagedServiceCatalog(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	for _, t := range p.Targets {
		base, _, _ := strings.Cut(t.Type, "@")
		if _, ok := managedServiceBases[base]; ok {
			return true
		}
	}
	return false
}

// hasMultipleCodebases returns true when the recipe has more than one
// codebase to scaffold — either dual-runtime (frontend + API) or a
// separate-codebase worker. Drives the multi-codebase variant of the
// "WHERE to write files" block at generate.
func hasMultipleCodebases(p *RecipePlan) bool {
	if p == nil {
		return false
	}
	return isDualRuntime(p) || hasSeparateCodebaseWorker(p)
}

// isShowcase returns true for showcase-tier recipes. Drives the dashboard
// skeleton, sub-agent brief, browser walk, and UX contract blocks.
func isShowcase(p *RecipePlan) bool {
	return p != nil && p.Tier == RecipeTierShowcase
}

// Note on multi-base detection: the single source of truth is
// needsMultiBaseGuidance in recipe_multibase.go, which keys on the plan's
// actual BuildCommands (JS package-manager invocation in a non-JS primary
// runtime). An earlier `hasMultiBaseBuildCommand` predicate that keyed on
// len(BuildBases) > 1 was deleted — it could disagree with the
// BuildCommands detector on the same plan (retry delta vs full-guide
// divergence), and BuildBases is often unset at the time the guide needs
// to decide whether to emit the block. All callers (catalog, retry delta)
// go through needsMultiBaseGuidance.
