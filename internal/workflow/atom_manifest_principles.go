package workflow

// principleAtoms returns the 16 atoms under internal/content/workflows/recipe/
// principles/. These are pointer-included by briefs at stitch time — their
// Audience is AudienceAny because a single principle atom is consumed by
// multiple sub-agent briefs (e.g. where-commands-run.md is included by
// every brief + phases/provision/git-config-container-side.md +
// phases/generate/smoke-test/on-container-smoke-test.md).
//
// The stitcher resolves pointer-includes at brief-composition time, not by
// emitting the principle atom as a standalone transmitted unit. This
// preserves the P2 audience-separation invariant: a principle atom is
// concatenated into the consuming brief's output, not shipped alongside
// with a dual-audience header.
func principleAtoms() []Atom {
	aud := AudienceAny
	a := TierAny

	return []Atom{
		// Top-level principles (6 atoms).
		{ID: "principles.where-commands-run", Path: "principles/where-commands-run.md", Audience: aud, MaxLines: 90, TierCond: a},
		{ID: "principles.file-op-sequencing", Path: "principles/file-op-sequencing.md", Audience: aud, MaxLines: 60, TierCond: a},
		{ID: "principles.tool-use-policy", Path: "principles/tool-use-policy.md", Audience: aud, MaxLines: 70, TierCond: a},
		{ID: "principles.symbol-naming-contract", Path: "principles/symbol-naming-contract.md", Audience: aud, MaxLines: 240, TierCond: a},
		{ID: "principles.todowrite-mirror-only", Path: "principles/todowrite-mirror-only.md", Audience: aud, MaxLines: 60, TierCond: a},
		{ID: "principles.fact-recording-discipline", Path: "principles/fact-recording-discipline.md", Audience: aud, MaxLines: 130, TierCond: a},

		// principles/platform-principles/ (6 atoms — numbered-ordinal stable IDs).
		{ID: "principles.platform-principles.01-graceful-shutdown", Path: "principles/platform-principles/01-graceful-shutdown.md", Audience: aud, MaxLines: 80, TierCond: a},
		{ID: "principles.platform-principles.02-routable-bind", Path: "principles/platform-principles/02-routable-bind.md", Audience: aud, MaxLines: 60, TierCond: a},
		{ID: "principles.platform-principles.03-proxy-trust", Path: "principles/platform-principles/03-proxy-trust.md", Audience: aud, MaxLines: 60, TierCond: a},
		{ID: "principles.platform-principles.04-competing-consumer", Path: "principles/platform-principles/04-competing-consumer.md", Audience: aud, MaxLines: 90, TierCond: a},
		{ID: "principles.platform-principles.05-structured-creds", Path: "principles/platform-principles/05-structured-creds.md", Audience: aud, MaxLines: 100, TierCond: a},
		{ID: "principles.platform-principles.06-stripped-build-root", Path: "principles/platform-principles/06-stripped-build-root.md", Audience: aud, MaxLines: 70, TierCond: a},

		// Adjunct principles (4 atoms).
		{ID: "principles.dev-server-contract", Path: "principles/dev-server-contract.md", Audience: aud, MaxLines: 80, TierCond: a},
		{ID: "principles.comment-style", Path: "principles/comment-style.md", Audience: aud, MaxLines: 60, TierCond: a},
		{ID: "principles.visual-style", Path: "principles/visual-style.md", Audience: aud, MaxLines: 50, TierCond: a},
		{ID: "principles.canonical-output-paths", Path: "principles/canonical-output-paths.md", Audience: aud, MaxLines: 80, TierCond: a},
	}
}
