package workflow

import "strings"

// sectionBlock pairs a block name with a predicate. A nil predicate means
// "always include". The name must match a <block name="..."> tag in the
// corresponding section of recipe.md — TestCatalog_CoversAllMarkdownBlocks
// enforces this at build time.
type sectionBlock struct {
	Name      string
	Predicate func(*RecipePlan) bool
}

// Registered catalogs — filled in Phases 5a/6a/7a as each section is
// converted. Empty catalogs are a no-op: composeSection returns the raw
// section body verbatim, so unconverted sections still work.
var (
	recipeResearchBlocks []sectionBlock

	// recipeProvisionBlocks — Phase 6b wires real predicates on the
	// shape-specific blocks. Always-on blocks (framing, standard mode,
	// workspace restrictions, schema pointer, mount, git config,
	// attestation) keep nil.
	recipeProvisionBlocks = []sectionBlock{
		{Name: "provision-framing"},
		{Name: "import-yaml-standard-mode"},
		{Name: "import-yaml-static-frontend", Predicate: hasServeOnlyProd},
		{Name: "import-yaml-workspace-restrictions"},
		{Name: "import-yaml-framework-secrets"},
		{Name: "import-yaml-dual-runtime", Predicate: isDualRuntime},
		{Name: "provision-schema-inline"},
		{Name: "import-services-step"},
		{Name: "mount-dev-filesystem"},
		{Name: "git-config-mount"},
		{Name: "git-init-per-codebase", Predicate: hasMultipleCodebases},
		{Name: "env-var-discovery", Predicate: hasManagedServiceCatalog},
		{Name: "provision-attestation"},
	}

	// recipeGenerateBlocks — Phase 5b sets real predicates. Shape-specific
	// rules (dual-runtime URLs, dashboard skeleton, worker setup, bundler
	// host-check, serve-only dev override, multi-base dev-deps) are gated
	// on plan predicates from recipe_plan_predicates.go. Always-on blocks
	// (container-state, execution-order, code-quality, etc.) keep a nil
	// predicate — they apply regardless of recipe shape.
	recipeGenerateBlocks = []sectionBlock{
		{Name: "container-state"},
		{Name: "where-to-write-files-single", Predicate: func(p *RecipePlan) bool { return !hasMultipleCodebases(p) }},
		{Name: "where-to-write-files-multi", Predicate: hasMultipleCodebases},
		{Name: "what-to-generate-showcase", Predicate: isShowcase},
		{Name: "two-kinds-of-import-yaml"},
		{Name: "execution-order"},
		{Name: "generate-schema-pointer"},
		{Name: "zerops-yaml-header"},
		{Name: "dual-runtime-url-shapes", Predicate: isDualRuntime},
		{Name: "dual-runtime-consumption", Predicate: isDualRuntime},
		{Name: "project-env-vars-pointer", Predicate: isDualRuntime},
		{Name: "dual-runtime-what-not-to-do", Predicate: isDualRuntime},
		{Name: "setup-dev-rules"},
		{Name: "serve-only-dev-override", Predicate: hasServeOnlyProd},
		{Name: "dev-dep-preinstall", Predicate: needsMultiBaseGuidance},
		{Name: "dev-server-host-check", Predicate: hasBundlerDevServer},
		{Name: "setup-prod-rules"},
		{Name: "worker-setup-block", Predicate: hasWorker},
		{Name: "shared-across-setups"},
		{Name: "env-example-preservation"},
		{Name: "framework-env-conventions"},
		{Name: "dashboard-skeleton", Predicate: isShowcase},
		{Name: "scaffold-subagent-brief", Predicate: func(p *RecipePlan) bool { return isShowcase(p) && hasMultipleCodebases(p) }},
		{Name: "asset-pipeline-consistency"},
		// readme-with-fragments moved to recipeDeployBlocks in v14 — READMEs
		// are written during the post-verify `readmes` sub-step so the
		// gotchas section narrates real debug experience.
		{Name: "code-quality"},
		// init-script-loud-failure and client-code-observable-failure are
		// always-on structural rules the scaffold subagent must enforce
		// on every file it writes. Introduced after v18 shipped a seed
		// that silently swallowed a Meilisearch sync error and a frontend
		// that hardcoded /api/* paths — both classes of bug are now
		// blocked at generate time by these rules.
		{Name: "init-script-loud-failure"},
		{Name: "client-code-observable-failure"},
		{Name: "pre-deploy-checklist"},
		{Name: "on-container-smoke-test"},
		{Name: "comment-anti-patterns"},
		{Name: "completion"},
	}

	// recipeDeployBlocks — Phase A splits dev-deploy-flow-core into 5
	// sub-blocks gated by plan shape. API-first steps, bundler dev-server,
	// and worker process are now conditionally included, saving ~4 KB for
	// narrow recipes that don't need them.
	//
	// feature-sweep-dev and feature-sweep-stage are tier-independent
	// content-type gates added after v18 shipped a search feature that
	// returned 200 + text/html (nginx SPA fallback) while the browser
	// walk reported the dashboard as "rendered". Both sweeps iterate
	// plan.Features and enforce `application/json` on every declared
	// api-surface feature.
	recipeDeployBlocks = []sectionBlock{
		{Name: "deploy-framing"},
		{Name: "deploy-execution-order"},
		{Name: "deploy-core-universal"},
		{Name: "deploy-api-first", Predicate: isDualRuntime},
		{Name: "deploy-asset-dev-server", Predicate: hasBundlerDevServer},
		{Name: "deploy-worker-process", Predicate: hasWorker},
		{Name: "deploy-target-verification"},
		{Name: "dev-deploy-subagent-brief", Predicate: isShowcase},
		{Name: "where-commands-run"},
		// v8.80 formalized sub-agent patterns. writer/fix-subagent-brief
		// are available for multi-codebase and large-failure iterations
		// respectively; feature-subagent-mcp-schemas inlines the exact
		// MCP parameter names/types so the feature sub-agent doesn't
		// round-trip on invalid-params rejections.
		{Name: "writer-subagent-brief", Predicate: hasMultipleCodebases},
		{Name: "fix-subagent-brief"},
		{Name: "feature-subagent-mcp-schemas", Predicate: isShowcase},
		{Name: "feature-sweep-dev"},
		{Name: "dev-deploy-browser-walk", Predicate: isShowcase},
		{Name: "browser-command-reference", Predicate: isShowcase},
		{Name: "stage-deployment-flow"},
		{Name: "feature-sweep-stage"},
		{Name: "reading-deploy-failures"},
		{Name: "common-deployment-issues"},
		// v14: README writing moved from generate to the post-verify
		// readmes sub-step so gotchas narrate lived experience.
		{Name: "readme-with-fragments"},
		{Name: "deploy-completion"},
	}

	// recipeFinalizeBlocks — Phase A (progressive guidance) gates showcase
	// service-key lists and dual-runtime projectEnvVariables on plan shape,
	// saving ~3 KB for narrow recipes that don't need either.
	recipeFinalizeBlocks = []sectionBlock{
		{Name: "env-comment-rules"},
		{Name: "env-comments-example"},
		{Name: "showcase-service-keys", Predicate: isShowcase},
		{Name: "project-env-vars", Predicate: isDualRuntime},
		{Name: "review-readmes"},
		{Name: "comment-voice"},
		{Name: "finalize-completion"},
	}

	// recipeCloseBlocks — Phase A gates the browser walk on isShowcase
	// (minimal recipes skip the walk entirely, saving ~3 KB).
	recipeCloseBlocks = []sectionBlock{
		{Name: "code-review-subagent"},
		{Name: "close-browser-walk", Predicate: isShowcase},
		{Name: "export-publish"},
		{Name: "close-completion"},
	}
)

// composeSection takes the raw body of a <section> and a catalog, extracts
// its <block> children, filters by predicate, and returns the composed
// body. If the catalog is empty, the raw body is returned unchanged — so
// callers can route every section through composeSection without breaking
// unconverted sections.
//
// Composition order is strictly the catalog order, with the preamble (if
// present) always first. Blocks whose predicate returns false are silently
// dropped. Blocks in the markdown that don't appear in the catalog are
// also dropped — the catalog-coverage test prevents this happening
// accidentally.
//
// Consumed by Phase 5/6/7 of the recipe size-reduction refactor (each
// section conversion routes through this function once its catalog is
// populated).
func composeSection(sectionBody string, catalog []sectionBlock, plan *RecipePlan) string {
	if len(catalog) == 0 {
		return sectionBody
	}
	blocks := ExtractBlocks(sectionBody)
	byName := make(map[string]string, len(blocks))
	for _, b := range blocks {
		byName[b.Name] = b.Body
	}
	var out []string
	if preamble, ok := byName[""]; ok && preamble != "" {
		out = append(out, preamble)
	}
	for _, sb := range catalog {
		body, ok := byName[sb.Name]
		if !ok || body == "" {
			continue
		}
		if sb.Predicate != nil && !sb.Predicate(plan) {
			continue
		}
		out = append(out, body)
	}
	return strings.Join(out, "\n\n")
}
