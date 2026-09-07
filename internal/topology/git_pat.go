package topology

import (
	"net/url"
	"strings"
)

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

// GitHostKind classifies a git remote by the forge that serves it, so token
// guidance names a page that exists. The PAT constants above are GitHub's; a
// self-hosted Gitea or GitLab keeps its credential somewhere else entirely,
// and sending a user to github.com/settings is a dead end they cannot act on.
type GitHostKind string

const (
	GitHostGitHub  GitHostKind = "github"
	GitHostGitLab  GitHostKind = "gitlab"
	GitHostUnknown GitHostKind = "unknown"
)

const (
	// GitLabPushMinScope is GitLab's equivalent of GHPATPushMinScope.
	GitLabPushMinScope = "write_repository"

	// GiteaPushMinScope is what a Gitea token needs to push and to open a
	// pull request. Gitea scopes are coarse: one scope covers both.
	GiteaPushMinScope = "write:repository"

	// SelfHostedTokenPath is where Gitea and Forgejo keep personal access
	// tokens. Named as the likely path for an unidentified host rather than
	// asserted as fact — the host is echoed back so the user can correct it.
	SelfHostedTokenPath = "/user/settings/applications"

	gitLabTokenPath = "/-/user_settings/personal_access_tokens"
)

// ClassifyGitHost reads the forge from a remote URL, in either git@host:path
// or https://host/path form. Anything not positively identified is Unknown —
// a self-hosted forge answers on an arbitrary host, and guessing GitHub for it
// is the bug this exists to stop.
func ClassifyGitHost(remoteURL string) GitHostKind {
	host := gitRemoteHost(remoteURL)
	switch {
	case host == "":
		return GitHostUnknown
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return GitHostGitHub
	case host == "gitlab.com" || strings.HasPrefix(host, "gitlab."):
		return GitHostGitLab
	default:
		return GitHostUnknown
	}
}

// GitTokenSettingsURL is where the user creates or re-scopes the token for
// this remote. An unidentified host gets its OWN origin plus the path most
// self-hosted forges use — wrong beats nothing, and github.com is wrong in a
// way the user cannot recover from.
func GitTokenSettingsURL(remoteURL string) string {
	host := gitRemoteHost(remoteURL)
	switch ClassifyGitHost(remoteURL) {
	case GitHostGitHub:
		return GHPATSettingsURL
	case GitHostGitLab:
		if host == "" {
			return "https://gitlab.com" + gitLabTokenPath
		}
		return "https://" + host + gitLabTokenPath
	default:
		if host == "" {
			return "your git host's personal-access-token settings"
		}
		return "https://" + host + SelfHostedTokenPath
	}
}

// GitTokenPushScope is the minimum permission a token needs to push to this
// remote, in that forge's own vocabulary.
func GitTokenPushScope(remoteURL string) string {
	switch ClassifyGitHost(remoteURL) {
	case GitHostGitHub:
		return GHPATPushMinScope
	case GitHostGitLab:
		return GitLabPushMinScope
	default:
		return GiteaPushMinScope
	}
}

func gitRemoteHost(remoteURL string) string {
	raw := strings.TrimSpace(remoteURL)
	if raw == "" {
		return ""
	}
	// scp-style: git@host:owner/repo.git
	if !strings.Contains(raw, "://") {
		if at := strings.Index(raw, "@"); at >= 0 {
			rest := raw[at+1:]
			if colon := strings.Index(rest, ":"); colon >= 0 {
				return strings.ToLower(rest[:colon])
			}
			return strings.ToLower(rest)
		}
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
