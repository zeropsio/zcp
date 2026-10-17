package ops

import (
	"net/url"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// OriginRequest asks the person whether finished work goes back to the
// repository it came from (docs/spec-mate.md §5.10). It carries no token: on
// Connect the person gives HQ the origin and its token themselves, so neither
// crosses the conversation.
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
		"why: why finished work would go back there, one short line."); err != nil {
		return OriginRequest{}, err
	}
	return OriginRequest{URL: origin, Repo: repo, Why: strings.TrimSpace(why)}, nil
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
		// The person gives the token to HQ on Connect; one in the URL would
		// sit in the conversation and the engine's records.
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			"url carries a credential; a token is never asked for or passed here — the person gives it to HQ when they connect", hint)
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
