package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// giteaUserAPIPath is Gitea's REST endpoint for the authenticated user,
// relative to the instance origin. A Mate bot's token carries `read:user`
// (docs/vocabulary.md: scopes `write:repository,read:user`), so this is the
// one call it can make about itself.
const giteaUserAPIPath = "/api/v1/user"

// giteaUserAPIURL builds the /user endpoint from GITEA_URL as the app wrote
// it — an origin, possibly with a trailing slash, possibly the in-project
// `http://web:3000` form. A value that is not an absolute http(s) URL is an
// error rather than a guess: the alternative is sending the bot's token at
// whatever a bare hostname happens to resolve to.
func giteaUserAPIURL(giteaURL string) (string, error) {
	raw := strings.TrimSpace(giteaURL)
	if raw == "" {
		return "", fmt.Errorf("GITEA_URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("GITEA_URL is not a URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("GITEA_URL must be an http(s) origin")
	}
	if u.Host == "" {
		return "", fmt.Errorf("GITEA_URL names no host")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/") + giteaUserAPIPath, nil
}

// giteaUserAPIResponse is the subset of Gitea's /user response
// DeriveGiteaIdentity needs.
type giteaUserAPIResponse struct {
	Login string `json:"login"`
}

// DeriveGiteaIdentity reads the Mate's own bot login from its Gitea
// (`GET {GITEA_URL}/api/v1/user`, the token in Gitea's own `token` auth
// scheme) and returns the identity it commits with: the login, and
// `{login}@mate.invalid` (topology.GiteaCommitIdentity).
//
// Why not the account's e-mail the way DeriveGitHubIdentity does: a Mate bot
// is not a person and has no mailbox. Its commits are the Mate's, and
// attributing them to any real address — the creator's included — would make
// the history lie about who wrote the code and mail a human about it.
// `.invalid` is reserved for exactly this (RFC 2606).
//
// httpClient is the caller's HTTPDoer, the same seam DeriveGitHubIdentity
// uses; a nil client is a supported "derivation unavailable" input and
// returns an error rather than panicking. Every error is DELIBERATELY coarse
// (status code / class only, never a response body and never the token):
// callers put these straight into agent-facing warning text, and a Gitea
// error page is not safe to reflect verbatim. Every error path is meant to be
// NON-BLOCKING — the caller falls back to the robot identity.
func DeriveGiteaIdentity(ctx context.Context, httpClient HTTPDoer, giteaURL, token string) (GitIdentity, error) {
	if httpClient == nil {
		return GitIdentity{}, fmt.Errorf("no HTTP client configured")
	}
	endpoint, err := giteaUserAPIURL(giteaURL)
	if err != nil {
		return GitIdentity{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return GitIdentity{}, fmt.Errorf("build Gitea /user request failed")
	}
	// Gitea's own scheme for an access token. `Bearer` is accepted for OAuth2
	// access tokens only — a personal/bot token sent as Bearer reads as
	// anonymous, which would surface here as a 401 nobody can explain.
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return GitIdentity{}, fmt.Errorf("request to the Gitea /user endpoint failed (transport error)")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GitIdentity{}, fmt.Errorf("read Gitea /user response failed")
	}
	if resp.StatusCode != http.StatusOK {
		return GitIdentity{}, fmt.Errorf("the Gitea /user endpoint returned status %d", resp.StatusCode)
	}

	var parsed giteaUserAPIResponse
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil {
		return GitIdentity{}, fmt.Errorf("the Gitea /user response was not valid JSON")
	}
	if parsed.Login == "" {
		return GitIdentity{}, fmt.Errorf("the Gitea /user response names no login")
	}

	name, email := topology.GiteaCommitIdentity(parsed.Login)
	return GitIdentity{Name: name, Email: email}, nil
}
