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
	GitHostGitHub GitHostKind = "github"
	GitHostGitLab GitHostKind = "gitlab"
	// GitHostGitea is the account's OWN Gitea — the forge the Mate's group
	// keeps its code in. It is recognised by one signal only: the remote's
	// host is the host of GITEA_URL, the value the Mate app wrote onto the
	// `zcp` service. There is deliberately no path or URL shape to match on:
	// a Gitea answers on an arbitrary host and serves `/{owner}/{repo}` the
	// way every other forge does, so nothing but the environment's own answer
	// can tell it apart — and guessing would misclassify a stranger's
	// self-hosted forge as the account's, which is the failure that matters
	// (the Mate's bot token must never be offered for a host it has no
	// business authenticating to).
	GitHostGitea   GitHostKind = "gitea"
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

	// GiteaBotTokenSource is what stands in for a settings URL on the
	// account's own Gitea: there is no page to send anyone to. The Mate
	// authenticates as its own bot, with the token the app already wrote
	// into GITEA_TOKEN — a person never mints one, and an agent that goes
	// looking for a token to ask for is about to ask for the wrong thing.
	GiteaBotTokenSource = "$GITEA_TOKEN (the Mate's own bot token, already in this container's environment — nobody mints one)"

	// GiteaBotEmailDomain is the domain of the Mate bot's commit address.
	// `.invalid` is reserved by RFC 2606 and resolves nowhere by
	// construction, so a commit attributed to the bot can never mail a real
	// person.
	GiteaBotEmailDomain = "mate.invalid"

	gitLabTokenPath = "/-/user_settings/personal_access_tokens"
)

// ClassifyGitHost reads the forge from a remote URL, in either git@host:path
// or https://host/path form. Anything not positively identified is Unknown —
// a self-hosted forge answers on an arbitrary host, and guessing GitHub for it
// is the bug this exists to stop.
//
// giteaURL is the process's GITEA_URL as the caller read it (runtime.Info
// carries it from one read at startup). It is a PARAMETER, not an os.Getenv
// deep inside: topology is stdlib-only foundational vocabulary, and a
// classification that silently consults the ambient environment cannot be
// tested at a table and cannot be reasoned about at a call site. Empty means
// "this environment has no Gitea", which is the honest answer for every
// container the app never wrote the variable onto.
func ClassifyGitHost(remoteURL, giteaURL string) GitHostKind {
	host := gitRemoteHost(remoteURL)
	switch {
	case host == "":
		return GitHostUnknown
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return GitHostGitHub
	case host == "gitlab.com" || strings.HasPrefix(host, "gitlab."):
		return GitHostGitLab
	case host == gitRemoteHost(giteaURL):
		// Reached only with a non-empty giteaURL: an unparseable or absent
		// one yields "", and host is non-empty here.
		return GitHostGitea
	default:
		return GitHostUnknown
	}
}

// GiteaCommitIdentity is the commit identity a Mate writes on a Gitea remote
// (guide 2.3): the bot's own login, and an address in a domain that resolves
// nowhere. The Mate commits as itself — never as the person who opened it,
// and never as the anonymous robot default that makes two Mates on one
// repository indistinguishable in the history. An empty login returns an
// empty identity: the caller falls back rather than committing as
// `@mate.invalid` with no name.
func GiteaCommitIdentity(login string) (name, email string) {
	if login == "" {
		return "", ""
	}
	return login, login + "@" + GiteaBotEmailDomain
}

// GitTokenSettingsURL is where the user creates or re-scopes the token for
// this remote. An unidentified host gets its OWN origin plus the path most
// self-hosted forges use — wrong beats nothing, and github.com is wrong in a
// way the user cannot recover from.
func GitTokenSettingsURL(remoteURL, giteaURL string) string {
	host := gitRemoteHost(remoteURL)
	switch ClassifyGitHost(remoteURL, giteaURL) {
	case GitHostGitHub:
		return GHPATSettingsURL
	case GitHostGitLab:
		if host == "" {
			return "https://gitlab.com" + gitLabTokenPath
		}
		return "https://" + host + gitLabTokenPath
	case GitHostGitea:
		return GiteaBotTokenSource
	case GitHostUnknown:
		if host == "" {
			return "your git host's personal-access-token settings"
		}
		return "https://" + host + SelfHostedTokenPath
	}
	if host == "" {
		return "your git host's personal-access-token settings"
	}
	return "https://" + host + SelfHostedTokenPath
}

// GitTokenPushScope is the minimum permission a token needs to push to this
// remote, in that forge's own vocabulary.
func GitTokenPushScope(remoteURL, giteaURL string) string {
	switch ClassifyGitHost(remoteURL, giteaURL) {
	case GitHostGitHub:
		return GHPATPushMinScope
	case GitHostGitLab:
		return GitLabPushMinScope
	case GitHostGitea, GitHostUnknown:
		return GiteaPushMinScope
	}
	return GiteaPushMinScope
}

func gitRemoteHost(remoteURL string) string {
	raw := strings.TrimSpace(remoteURL)
	if raw == "" {
		return ""
	}
	// scp-style: git@host:owner/repo.git
	if !strings.Contains(raw, "://") {
		if _, rest, found := strings.Cut(raw, "@"); found {
			if host, _, hasPath := strings.Cut(rest, ":"); hasPath {
				return strings.ToLower(host)
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
