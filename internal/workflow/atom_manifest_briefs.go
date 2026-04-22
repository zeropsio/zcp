package workflow

// briefAtoms returns the 39 atoms under internal/content/workflows/recipe/
// briefs/. Each atom is audience-locked to exactly one sub-agent role
// (principle P2). All briefs atoms are TierAny — tier branching inside a
// brief is handled by the stitcher (e.g. surface-walk-task.md contains
// tier-conditional sections rendered based on plan.Tier).
func briefAtoms() []Atom {
	a := TierAny
	sc := AudienceScaffoldSub
	fe := AudienceFeatureSub
	wr := AudienceWriterSub
	cr := AudienceCodeReviewSub
	er := AudienceEditorialReviewSub

	return []Atom{
		// briefs/scaffold/ (8 atoms)
		{ID: "briefs.scaffold.mandatory-core", Path: "briefs/scaffold/mandatory-core.md", Audience: sc, MaxLines: 80, TierCond: a},
		{ID: "briefs.scaffold.symbol-contract-consumption", Path: "briefs/scaffold/symbol-contract-consumption.md", Audience: sc, MaxLines: 90, TierCond: a},
		{ID: "briefs.scaffold.framework-task", Path: "briefs/scaffold/framework-task.md", Audience: sc, MaxLines: 140, TierCond: a},
		{ID: "briefs.scaffold.pre-ship-assertions", Path: "briefs/scaffold/pre-ship-assertions.md", Audience: sc, MaxLines: 110, TierCond: a},
		{ID: "briefs.scaffold.completion-shape", Path: "briefs/scaffold/completion-shape.md", Audience: sc, MaxLines: 60, TierCond: a},
		{ID: "briefs.scaffold.api-codebase-addendum", Path: "briefs/scaffold/api-codebase-addendum.md", Audience: sc, MaxLines: 80, TierCond: a},
		{ID: "briefs.scaffold.frontend-codebase-addendum", Path: "briefs/scaffold/frontend-codebase-addendum.md", Audience: sc, MaxLines: 80, TierCond: a},
		{ID: "briefs.scaffold.worker-codebase-addendum", Path: "briefs/scaffold/worker-codebase-addendum.md", Audience: sc, MaxLines: 80, TierCond: a},

		// briefs/feature/ (6 atoms)
		{ID: "briefs.feature.mandatory-core", Path: "briefs/feature/mandatory-core.md", Audience: fe, MaxLines: 80, TierCond: a},
		{ID: "briefs.feature.symbol-contract-consumption", Path: "briefs/feature/symbol-contract-consumption.md", Audience: fe, MaxLines: 90, TierCond: a},
		{ID: "briefs.feature.task", Path: "briefs/feature/task.md", Audience: fe, MaxLines: 180, TierCond: a},
		{ID: "briefs.feature.diagnostic-cadence", Path: "briefs/feature/diagnostic-cadence.md", Audience: fe, MaxLines: 50, TierCond: a},
		{ID: "briefs.feature.ux-quality", Path: "briefs/feature/ux-quality.md", Audience: fe, MaxLines: 80, TierCond: a},
		{ID: "briefs.feature.completion-shape", Path: "briefs/feature/completion-shape.md", Audience: fe, MaxLines: 60, TierCond: a},

		// briefs/writer/ (11 atoms — classification-pointer added v39 Commit 5a)
		{ID: "briefs.writer.mandatory-core", Path: "briefs/writer/mandatory-core.md", Audience: wr, MaxLines: 70, TierCond: a},
		{ID: "briefs.writer.fresh-context-premise", Path: "briefs/writer/fresh-context-premise.md", Audience: wr, MaxLines: 80, TierCond: a},
		{ID: "briefs.writer.canonical-output-tree", Path: "briefs/writer/canonical-output-tree.md", Audience: wr, MaxLines: 100, TierCond: a},
		{ID: "briefs.writer.content-surface-contracts", Path: "briefs/writer/content-surface-contracts.md", Audience: wr, MaxLines: 260, TierCond: a},
		{ID: "briefs.writer.classification-taxonomy", Path: "briefs/writer/classification-taxonomy.md", Audience: wr, MaxLines: 160, TierCond: a},
		{ID: "briefs.writer.routing-matrix", Path: "briefs/writer/routing-matrix.md", Audience: wr, MaxLines: 120, TierCond: a},
		{ID: "briefs.writer.classification-pointer", Path: "briefs/writer/classification-pointer.md", Audience: wr, MaxLines: 40, TierCond: a},
		{ID: "briefs.writer.citation-map", Path: "briefs/writer/citation-map.md", Audience: wr, MaxLines: 160, TierCond: a},
		{ID: "briefs.writer.manifest-contract", Path: "briefs/writer/manifest-contract.md", Audience: wr, MaxLines: 120, TierCond: a},
		{ID: "briefs.writer.self-review-per-surface", Path: "briefs/writer/self-review-per-surface.md", Audience: wr, MaxLines: 130, TierCond: a},
		{ID: "briefs.writer.completion-shape", Path: "briefs/writer/completion-shape.md", Audience: wr, MaxLines: 70, TierCond: a},

		// briefs/code-review/ (5 atoms)
		{ID: "briefs.code-review.mandatory-core", Path: "briefs/code-review/mandatory-core.md", Audience: cr, MaxLines: 70, TierCond: a},
		{ID: "briefs.code-review.task", Path: "briefs/code-review/task.md", Audience: cr, MaxLines: 180, TierCond: a},
		{ID: "briefs.code-review.manifest-consumption", Path: "briefs/code-review/manifest-consumption.md", Audience: cr, MaxLines: 80, TierCond: a},
		{ID: "briefs.code-review.reporting-taxonomy", Path: "briefs/code-review/reporting-taxonomy.md", Audience: cr, MaxLines: 60, TierCond: a},
		{ID: "briefs.code-review.completion-shape", Path: "briefs/code-review/completion-shape.md", Audience: cr, MaxLines: 50, TierCond: a},

		// briefs/editorial-review/ (10 atoms — added per research-refinement 2026-04-20)
		// No tier-conditional atoms; tier branching inside surface-walk-task.md
		// is handled by the stitcher (minimal walks 4 env tiers, no worker
		// codebase; showcase walks 6 env tiers + worker).
		{ID: "briefs.editorial-review.mandatory-core", Path: "briefs/editorial-review/mandatory-core.md", Audience: er, MaxLines: 70, TierCond: a},
		{ID: "briefs.editorial-review.porter-premise", Path: "briefs/editorial-review/porter-premise.md", Audience: er, MaxLines: 80, TierCond: a},
		{ID: "briefs.editorial-review.surface-walk-task", Path: "briefs/editorial-review/surface-walk-task.md", Audience: er, MaxLines: 120, TierCond: a},
		{ID: "briefs.editorial-review.single-question-tests", Path: "briefs/editorial-review/single-question-tests.md", Audience: er, MaxLines: 100, TierCond: a},
		{ID: "briefs.editorial-review.classification-reclassify", Path: "briefs/editorial-review/classification-reclassify.md", Audience: er, MaxLines: 140, TierCond: a},
		{ID: "briefs.editorial-review.citation-audit", Path: "briefs/editorial-review/citation-audit.md", Audience: er, MaxLines: 100, TierCond: a},
		{ID: "briefs.editorial-review.counter-example-reference", Path: "briefs/editorial-review/counter-example-reference.md", Audience: er, MaxLines: 160, TierCond: a},
		{ID: "briefs.editorial-review.cross-surface-ledger", Path: "briefs/editorial-review/cross-surface-ledger.md", Audience: er, MaxLines: 90, TierCond: a},
		{ID: "briefs.editorial-review.reporting-taxonomy", Path: "briefs/editorial-review/reporting-taxonomy.md", Audience: er, MaxLines: 80, TierCond: a},
		{ID: "briefs.editorial-review.completion-shape", Path: "briefs/editorial-review/completion-shape.md", Audience: er, MaxLines: 60, TierCond: a},
	}
}
