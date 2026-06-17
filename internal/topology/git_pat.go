package topology

// GitHub fine-grained personal-access-token facts shared by the deploy-failure
// classifier (ops) and the token-scope recommendation + git-push-setup TELL
// (tools). Single owner so the management link, the push-minimum scope, and the
// most common repo-access misconfig cannot drift between what the agent is TOLD
// to create (the recommendation, preventive) and what the diagnostic NAMES when
// a token is rejected (the auth-rejection classifier, reactive). topology is
// import-legal from both ops and tools (stdlib-only foundational vocabulary).
const (
	// GHPATSettingsURL is the GitHub fine-grained PAT management page — where a
	// user creates or re-scopes the token. NOT the classic `?type=beta` list the
	// transcripts surfaced.
	GHPATSettingsURL = "https://github.com/settings/personal-access-tokens"

	// GHPATPushMinScope is the minimum permission a PAT needs to push commits:
	// `Contents: Read and write`. The Actions track needs more (the tools-layer
	// ghPATScopeRecommendation owns the full CD set); this is the git-push floor.
	GHPATPushMinScope = "Contents: Read and write"

	// GHPATRepoSelectTrap names the single most common fine-grained-PAT
	// misconfig: choosing "Public repositories" (read-only) under Repository
	// access instead of selecting the specific repo. That option cannot push, so
	// a token created that way authenticates for READ but is rejected at the
	// write-auth probe — a fail→regenerate round-trip the recommendation should
	// prevent and the diagnostic should name. Phrased to read naturally in both
	// the preventive recommendation and the reactive auth-rejection suggestion.
	GHPATRepoSelectTrap = `under "Repository access" pick "Only select repositories" → your repo, NOT "Public repositories" (that option is read-only and cannot push)`
)
