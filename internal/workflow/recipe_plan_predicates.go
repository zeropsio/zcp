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

// hasSharedCodebaseWorker returns true when a worker shares its codebase
// with another target. Drives the `setup: worker` block injection in the
// generate section.
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

// hasSeparateCodebaseWorker returns true for workers without a sharing host.
// Drives the separate-codebase provisioning block at provision, and the
// per-codebase README rule at generate.
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
	return false
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

// hasMultiBaseBuildCommand returns true when the plan runs a compound
// build that requires dev-dep pre-installation across multiple runtime
// bases (e.g., a PHP runtime with a JS asset pipeline). Drives the
// multi-base dev buildCommands rule at generate.
func hasMultiBaseBuildCommand(p *RecipePlan) bool {
	if p == nil || len(p.BuildBases) == 0 {
		return false
	}
	return len(p.BuildBases) > 1
}
