package ops

import (
	"fmt"
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

// originURL is a repository's https address with neither a credential nor
// anything past its path.
func originURL(raw string) (string, error) {
	const hint = "Pass the repository's https address as the person gave it, without a token: https://github.com/owner/repo."
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("url %q is not a repository address", topology.RedactRepoURLCredentials(raw)), hint)
	case u.User != nil:
		// The person gives the token to HQ on Connect; one in the URL would
		// sit in the conversation and the engine's records.
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("url %q carries a credential; a token is never asked for or passed here — the person gives it to HQ when they connect", topology.RedactRepoURLCredentials(raw)), hint)
	case u.Scheme != httpsScheme || u.Host == "" || u.RawQuery != "" || u.Fragment != "":
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("url %q is not a repository's https address", raw), hint)
	}
	canonical := topology.CanonicalRepoURL(raw)
	if owner, name, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(canonical, httpsScheme+"://"+u.Host), "/"), "/"); owner == "" || name == "" {
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("url %q names no repository", raw), hint)
	}
	return canonical, nil
}
