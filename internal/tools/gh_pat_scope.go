package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/topology"
)

// ghPATSettingsURL is the GitHub fine-grained personal-access-token management
// page — the place a user creates or re-scopes the PAT. Aliased to the single
// topology owner so this TELL and the deploy-failure diagnostic (ops) emit the
// SAME link (NOT the classic `?type=beta` list the transcripts surfaced).
const ghPATSettingsURL = topology.GHPATSettingsURL

// ownerRepoPlaceholder is the "<owner>/<repo>" sentinel used when the real
// owner/repo couldn't be parsed from the remote URL yet.
const ownerRepoPlaceholder = "<owner>/<repo>"

// ghPATScopeRecommendation is the SINGLE owner of the GitHub fine-grained PAT
// scope recommendation. Every git-push-setup / build-integration / launch site
// derives its token guidance from here so the scope set + settings link cannot
// drift.
//
// Why this owner exists (p.txt #6 / p2 #4): the recommendation was hand-authored
// in six places that had already diverged — several said only `Secrets: Read and
// write`, omitting the `Workflows` scope the GitHub Actions track REQUIRES (the
// first thing it does is push a `.github/workflows/` file, which GitHub rejects
// without it) and the `Actions: Read` / `Checks: Read` scopes needed to WATCH
// the run (`gh run list` / `gh run watch`). Under-scoped tokens cost a user
// round-trip mid-launch.
//
//   - actions=false → the git-push-only minimum (`Contents: Read and write`).
//   - actions=true  → the full CD-track set required to push the workflow file,
//     set the repo secret, and watch the resulting run.
func ghPATScopeRecommendation(ownerRepo string, actions bool) string {
	target := "the single target repo (owner/repo)"
	if ownerRepo != "" && ownerRepo != ownerRepoPlaceholder {
		target = ownerRepo
	}
	if !actions {
		return fmt.Sprintf(
			"a fine-grained GitHub PAT scoped ONLY to %s with `%s` (single-repo blast radius) — %s. Create or edit it at %s — GitHub PATs require an expiration, pick the longest you're comfortable with (max 1 year).",
			target, topology.GHPATPushMinScope, topology.GHPATRepoSelectTrap, ghPATSettingsURL,
		)
	}
	return fmt.Sprintf(
		"a fine-grained GitHub PAT scoped ONLY to %s with `%s` + `Workflows: Read and write` (REQUIRED — pushing any file under .github/workflows/ is rejected without it) + `Secrets: Read and write` (for `gh secret set`) + `Actions: Read` and `Checks: Read` (so `gh run list` / `gh run watch` can read the CI run you just created) — %s. Create or edit it at %s — single-repo blast radius; GitHub PATs require an expiration, pick the longest you're comfortable with (max 1 year).",
		target, topology.GHPATPushMinScope, topology.GHPATRepoSelectTrap, ghPATSettingsURL,
	)
}
