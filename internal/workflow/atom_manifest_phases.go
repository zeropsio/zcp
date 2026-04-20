package workflow

// phaseAtoms returns the 65 atoms under internal/content/workflows/recipe/
// phases/. Ordering follows tree-walk of atomic-layout.md §1.
//
// Tier-conditional atoms:
//
//	execution-order-minimal.md          — TierMinimal
//	dashboard-skeleton-showcase.md      — TierShowcase
//	deploy/subagent.md                  — TierShowcase (feature-sub dispatch)
//	deploy/snapshot-dev.md              — TierShowcase
//	deploy/browser-walk-dev.md          — TierShowcase
//	finalize/service-keys-showcase.md   — TierShowcase
//	close/close-browser-walk.md         — TierShowcase
//
// Everything else is TierAny. Multi-codebase gating (single vs multi
// scaffold) is applied by the stitcher, not by tier, since dual-runtime
// minimal is also multi-codebase.
func phaseAtoms() []Atom {
	m := AudienceMain
	a := TierAny
	s := TierShowcase
	mi := TierMinimal

	return []Atom{
		// phases/research/
		{ID: "research.entry", Path: "phases/research/entry.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "research.symbol-contract-derivation", Path: "phases/research/symbol-contract-derivation.md", Audience: m, MaxLines: 120, TierCond: a},
		{ID: "research.completion", Path: "phases/research/completion.md", Audience: m, MaxLines: 60, TierCond: a},

		// phases/provision/
		{ID: "provision.entry", Path: "phases/provision/entry.md", Audience: m, MaxLines: 100, TierCond: a},
		{ID: "provision.import-yaml.standard-mode", Path: "phases/provision/import-yaml/standard-mode.md", Audience: m, MaxLines: 110, TierCond: a},
		{ID: "provision.import-yaml.static-frontend", Path: "phases/provision/import-yaml/static-frontend.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "provision.import-yaml.dual-runtime", Path: "phases/provision/import-yaml/dual-runtime.md", Audience: m, MaxLines: 120, TierCond: a},
		{ID: "provision.import-yaml.workspace-restrictions", Path: "phases/provision/import-yaml/workspace-restrictions.md", Audience: m, MaxLines: 50, TierCond: a},
		{ID: "provision.import-yaml.framework-secrets", Path: "phases/provision/import-yaml/framework-secrets.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "provision.import-services-step", Path: "phases/provision/import-services-step.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "provision.mount-dev-filesystem", Path: "phases/provision/mount-dev-filesystem.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "provision.git-config-container-side", Path: "phases/provision/git-config-container-side.md", Audience: m, MaxLines: 110, TierCond: a},
		{ID: "provision.git-init-per-codebase", Path: "phases/provision/git-init-per-codebase.md", Audience: m, MaxLines: 50, TierCond: a},
		{ID: "provision.env-var-discovery", Path: "phases/provision/env-var-discovery.md", Audience: m, MaxLines: 120, TierCond: a},
		{ID: "provision.provision-attestation", Path: "phases/provision/provision-attestation.md", Audience: m, MaxLines: 70, TierCond: a},
		{ID: "provision.completion", Path: "phases/provision/completion.md", Audience: m, MaxLines: 50, TierCond: a},

		// phases/generate/
		{ID: "generate.entry", Path: "phases/generate/entry.md", Audience: m, MaxLines: 90, TierCond: a},

		// phases/generate/scaffold/
		{ID: "generate.scaffold.entry", Path: "phases/generate/scaffold/entry.md", Audience: m, MaxLines: 80, TierCond: a},
		{ID: "generate.scaffold.where-to-write-single", Path: "phases/generate/scaffold/where-to-write-single.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "generate.scaffold.where-to-write-multi", Path: "phases/generate/scaffold/where-to-write-multi.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "generate.scaffold.completion", Path: "phases/generate/scaffold/completion.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "generate.scaffold.dev-server-host-check", Path: "phases/generate/scaffold/dev-server-host-check.md", Audience: m, MaxLines: 40, TierCond: a},

		// phases/generate/app-code/
		{ID: "generate.app-code.entry", Path: "phases/generate/app-code/entry.md", Audience: m, MaxLines: 70, TierCond: a},
		{ID: "generate.app-code.execution-order-minimal", Path: "phases/generate/app-code/execution-order-minimal.md", Audience: m, MaxLines: 40, TierCond: mi},
		{ID: "generate.app-code.dashboard-skeleton-showcase", Path: "phases/generate/app-code/dashboard-skeleton-showcase.md", Audience: m, MaxLines: 60, TierCond: s},
		{ID: "generate.app-code.completion", Path: "phases/generate/app-code/completion.md", Audience: m, MaxLines: 40, TierCond: a},

		// phases/generate/smoke-test/
		{ID: "generate.smoke-test.entry", Path: "phases/generate/smoke-test/entry.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "generate.smoke-test.on-container-smoke-test", Path: "phases/generate/smoke-test/on-container-smoke-test.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "generate.smoke-test.completion", Path: "phases/generate/smoke-test/completion.md", Audience: m, MaxLines: 40, TierCond: a},

		// phases/generate/zerops-yaml/
		{ID: "generate.zerops-yaml.entry", Path: "phases/generate/zerops-yaml/entry.md", Audience: m, MaxLines: 70, TierCond: a},
		{ID: "generate.zerops-yaml.env-var-model", Path: "phases/generate/zerops-yaml/env-var-model.md", Audience: m, MaxLines: 140, TierCond: a},
		{ID: "generate.zerops-yaml.dual-runtime-consumption", Path: "phases/generate/zerops-yaml/dual-runtime-consumption.md", Audience: m, MaxLines: 200, TierCond: a},
		{ID: "generate.zerops-yaml.setup-rules-dev", Path: "phases/generate/zerops-yaml/setup-rules-dev.md", Audience: m, MaxLines: 110, TierCond: a},
		{ID: "generate.zerops-yaml.setup-rules-prod", Path: "phases/generate/zerops-yaml/setup-rules-prod.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "generate.zerops-yaml.setup-rules-worker", Path: "phases/generate/zerops-yaml/setup-rules-worker.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "generate.zerops-yaml.setup-rules-static-frontend", Path: "phases/generate/zerops-yaml/setup-rules-static-frontend.md", Audience: m, MaxLines: 70, TierCond: a},
		{ID: "generate.zerops-yaml.seed-execonce-keys", Path: "phases/generate/zerops-yaml/seed-execonce-keys.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "generate.zerops-yaml.comment-style-positive", Path: "phases/generate/zerops-yaml/comment-style-positive.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "generate.zerops-yaml.completion", Path: "phases/generate/zerops-yaml/completion.md", Audience: m, MaxLines: 40, TierCond: a},

		// phases/generate/ — phase-level completion
		{ID: "generate.completion", Path: "phases/generate/completion.md", Audience: m, MaxLines: 70, TierCond: a},

		// phases/deploy/
		{ID: "deploy.entry", Path: "phases/deploy/entry.md", Audience: m, MaxLines: 80, TierCond: a},
		{ID: "deploy.deploy-dev", Path: "phases/deploy/deploy-dev.md", Audience: m, MaxLines: 110, TierCond: a},
		{ID: "deploy.start-processes", Path: "phases/deploy/start-processes.md", Audience: m, MaxLines: 70, TierCond: a},
		{ID: "deploy.verify-dev", Path: "phases/deploy/verify-dev.md", Audience: m, MaxLines: 80, TierCond: a},
		{ID: "deploy.init-commands", Path: "phases/deploy/init-commands.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "deploy.subagent", Path: "phases/deploy/subagent.md", Audience: m, MaxLines: 40, TierCond: s},
		{ID: "deploy.snapshot-dev", Path: "phases/deploy/snapshot-dev.md", Audience: m, MaxLines: 50, TierCond: s},
		{ID: "deploy.feature-sweep-dev", Path: "phases/deploy/feature-sweep-dev.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "deploy.browser-walk-dev", Path: "phases/deploy/browser-walk-dev.md", Audience: m, MaxLines: 140, TierCond: s},
		{ID: "deploy.cross-deploy-stage", Path: "phases/deploy/cross-deploy-stage.md", Audience: m, MaxLines: 90, TierCond: a},
		{ID: "deploy.verify-stage", Path: "phases/deploy/verify-stage.md", Audience: m, MaxLines: 70, TierCond: a},
		{ID: "deploy.feature-sweep-stage", Path: "phases/deploy/feature-sweep-stage.md", Audience: m, MaxLines: 80, TierCond: a},
		{ID: "deploy.readmes", Path: "phases/deploy/readmes.md", Audience: m, MaxLines: 80, TierCond: a},
		{ID: "deploy.completion", Path: "phases/deploy/completion.md", Audience: m, MaxLines: 80, TierCond: a},

		// phases/finalize/
		{ID: "finalize.entry", Path: "phases/finalize/entry.md", Audience: m, MaxLines: 100, TierCond: a},
		{ID: "finalize.env-comment-rules", Path: "phases/finalize/env-comment-rules.md", Audience: m, MaxLines: 140, TierCond: a},
		{ID: "finalize.project-env-vars", Path: "phases/finalize/project-env-vars.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "finalize.service-keys-showcase", Path: "phases/finalize/service-keys-showcase.md", Audience: m, MaxLines: 80, TierCond: s},
		{ID: "finalize.review-readmes", Path: "phases/finalize/review-readmes.md", Audience: m, MaxLines: 60, TierCond: a},
		{ID: "finalize.completion", Path: "phases/finalize/completion.md", Audience: m, MaxLines: 50, TierCond: a},

		// phases/close/
		{ID: "close.entry", Path: "phases/close/entry.md", Audience: m, MaxLines: 80, TierCond: a},
		{ID: "close.code-review", Path: "phases/close/code-review.md", Audience: m, MaxLines: 70, TierCond: a},
		{ID: "close.close-browser-walk", Path: "phases/close/close-browser-walk.md", Audience: m, MaxLines: 100, TierCond: s},
		{ID: "close.export-on-request", Path: "phases/close/export-on-request.md", Audience: m, MaxLines: 80, TierCond: a},
		{ID: "close.completion", Path: "phases/close/completion.md", Audience: m, MaxLines: 50, TierCond: a},
	}
}
