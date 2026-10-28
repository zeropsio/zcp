package ops

import (
	"net/url"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// OriginRequest asks the person where the code of the repository it came from
// is kept (docs/spec-mate.md §5.10): mounted from GitHub, or kept in Mate and
// only following it. It carries no token: the person connects the repository
// in HQ themselves, so none crosses the conversation.
type OriginRequest struct {
	// URL is the origin's https address, canonical (topology.CanonicalRepoURL).
	URL string `json:"url"`
	// Repo is the repository in HQ the pair will wire: its dev hostname.
	Repo string `json:"repo"`
	Why  string `json:"why"`
}

// NewOriginRequest checks an origin request and writes nothing.
func NewOriginRequest(rawURL, repo, why string) (OriginRequest, error) {
	origin, err := originURL(rawURL)
	if err != nil {
		return OriginRequest{}, err
	}
	if perr := platform.ValidateHostname(repo); perr != nil {
		perr.Suggestion = "repo is the repository in HQ the pair will wire: the dev service's hostname, as you will bootstrap it (appdev)."
		return OriginRequest{}, perr
	}
	if err := validateRequestLine("why", why, requestWhyMax,
		"why: why finished work would go back there, one line Mate shows whole."); err != nil {
		return OriginRequest{}, err
	}
	return OriginRequest{URL: origin, Repo: repo, Why: strings.TrimSpace(why)}, nil
}

// OriginWithdrawal is an origin ask the agent takes back: the address as it
// was asked (canonical, as the request answered it) and the repository the
// withdrawn ask named — "" for every ask naming the address.
type OriginWithdrawal struct {
	URL  string `json:"url"`
	Repo string `json:"repo,omitempty"`
}

// NewOriginWithdrawal checks a withdrawal and writes nothing: the address is
// checked as a request's is, and repo, when given, is a hostname.
func NewOriginWithdrawal(rawURL, repo string) (OriginWithdrawal, error) {
	origin, err := originURL(rawURL)
	if err != nil {
		return OriginWithdrawal{}, err
	}
	if repo = strings.TrimSpace(repo); repo != "" {
		if perr := platform.ValidateHostname(repo); perr != nil {
			perr.Suggestion = "repo is the repository in HQ the ask named — the dev service's hostname — or omit it to withdraw every ask naming the address."
			return OriginWithdrawal{}, perr
		}
	}
	return OriginWithdrawal{URL: origin, Repo: repo}, nil
}

// originURL is a repository's https address, owner/name, with neither a
// credential nor anything past its path. A refusal never echoes the address:
// a token can ride in its userinfo, its query, or an address that does not
// parse.
func originURL(raw string) (string, error) {
	const hint = "Pass the repository's https address as the person gave it, without a token: https://github.com/owner/repo."
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", platform.NewPlatformError(platform.ErrInvalidParameter, "url is not a repository address", hint)
	case u.User != nil:
		// The person connects the repository in HQ; a token in the URL would
		// sit in the conversation and the engine's records.
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			"url carries a credential; a token is never asked for or passed here — the person connects the repository in HQ", hint)
	case u.Scheme != httpsScheme || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", platform.NewPlatformError(platform.ErrInvalidParameter, "url is not a repository's https address", hint)
	}
	canonical := topology.CanonicalRepoURL(raw)
	segments := strings.Split(strings.TrimPrefix(canonical, httpsScheme+"://"+u.Host+"/"), "/")
	switch {
	case len(segments) < 2 || segments[0] == "" || segments[1] == "":
		return "", platform.NewPlatformError(platform.ErrInvalidParameter, "url names no repository", hint)
	case len(segments) > 2:
		// A page inside the repository (…/tree/develop) is no origin HQ could
		// connect, nor one SameRepoURL matches to the one it holds.
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			"url goes past the repository's owner/name",
			"Ask the person to give the repository address, e.g. https://github.com/acme/app, not a page inside it.")
	}
	return canonical, nil
}

// IsGitHubRepository reports whether origin, a canonical origin address
// (OriginRequest.URL), is a repository on github.com: the one host HQ mounts
// through its GitHub App and mints tokens for. Any other host is only
// followed.
func IsGitHubRepository(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, hq.GitHubHost)
}

// GitHubRepositoryPath is origin's `<owner>/<name>`, the path git names to a
// credential helper for it.
func GitHubRepositoryPath(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}
