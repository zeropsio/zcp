package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// githubUserAPIURL is GitHub's REST endpoint for the authenticated user.
// Works for both classic and fine-grained PATs with NO extra scope or
// repository permission required (Codex-verified 2026-07-12) — the same
// token git-push-setup already collected for the write-auth probe is
// sufficient.
const githubUserAPIURL = "https://api.github.com/user"

// githubAPIHost is the exact host githubUserAPIURL resolves to. Used to
// validate the response actually came from there (see the redirect-host
// check in DeriveGitHubIdentity) rather than trusting whatever the final
// hop of a followed redirect returned.
const githubAPIHost = "api.github.com"

// IsGitHubRemote reports whether remoteURL's host is EXACTLY github.com —
// the strict, fail-CLOSED gate for GitHub PAT derivation (F3, Codex
// diff-review finding 1a). Deliberately NOT ops.ParseGitHost: that helper
// fail-OPENs to "github.com" on any parse failure or malformed host, which
// is safe for its actual owner (url-scoping an already-validated
// credential-helper config key) but would be a PAT-confinement bug here —
// a malformed or non-GitHub remote must never cause a token collected for
// a different host to be sent to api.github.com. Requires a successfully
// parsed URL whose host case-insensitively equals "github.com" exactly —
// no subdomains (api.github.com, gist.github.com), no IP literals, no
// uppercase-spelling bypass (GITHUB.COM still normalizes to a match, but
// evilgithub.com or github.com.evil.tld do not).
func IsGitHubRemote(remoteURL string) bool {
	u, err := url.Parse(remoteURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "github.com")
}

// githubUserAPIResponse is the subset of GitHub's /user response
// DeriveGitHubIdentity needs.
type githubUserAPIResponse struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

// DeriveGitHubIdentity derives a human git identity from a GitHub PAT via
// `GET /user`: name is the account's login; email is the account's PUBLIC
// email if set, else GitHub's documented noreply pattern
// `<id>+<login>@users.noreply.github.com` — GitHub accepts commits/pushes
// attributed to that address for the account even when no public email is
// set or email-privacy is enabled.
//
// httpClient is the caller's HTTPDoer — the SAME seam already threaded
// through RegisterWorkflow for HTTP-readiness checks (no new package-
// global client, no parallel test hook). Production wires a plain
// `*http.Client` with no custom CheckRedirect, so this function cannot
// forbid redirects outright; instead it validates the response actually
// came from api.github.com (`resp.Request.URL.Host` — the host of the
// LAST request Go's client sent, after following any redirects) before
// trusting the body. Go's stdlib already strips the Authorization header
// on a cross-host redirect, so the token itself cannot leak to a
// redirect target; this check additionally stops a malicious/misrouted
// same-enough-looking response from being trusted as if it were real
// GitHub data.
//
// A nil httpClient is a supported "derivation unavailable" input (mirrors
// WaitHTTPReady's nil-client contract) and returns an error rather than
// panicking.
//
// Every returned error is DELIBERATELY coarse (status code / class only —
// never a raw response body or transport error string): callers surface
// these directly in agent-facing warning text, and neither a GitHub error
// page nor an arbitrary transport error message is safe to reflect
// verbatim into that surface. Every error path (nil client, transport
// failure, non-200, malformed body, missing login, wrong response host)
// is meant to be treated as NON-BLOCKING by the caller: git-push-setup
// proceeds with the robot identity fallback on any derivation failure —
// this function never itself decides that policy, it only reports what
// happened.
func DeriveGitHubIdentity(ctx context.Context, httpClient HTTPDoer, token string) (GitIdentity, error) {
	if httpClient == nil {
		return GitIdentity{}, fmt.Errorf("no HTTP client configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserAPIURL, nil)
	if err != nil {
		return GitIdentity{}, fmt.Errorf("build GitHub /user request failed")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return GitIdentity{}, fmt.Errorf("GitHub /user request failed (transport error)")
	}
	defer resp.Body.Close()

	// Redirect-host hardening: trust the body only if it actually came
	// from api.github.com. resp.Request is the LAST request the client
	// sent (post-redirect); a nil resp.Request would be unexpected for a
	// real http.Client response but is treated as untrusted too.
	if resp.Request == nil || !strings.EqualFold(resp.Request.URL.Hostname(), githubAPIHost) {
		return GitIdentity{}, fmt.Errorf("GitHub /user response did not come from %s", githubAPIHost)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GitIdentity{}, fmt.Errorf("read GitHub /user response failed")
	}
	if resp.StatusCode != http.StatusOK {
		return GitIdentity{}, fmt.Errorf("GitHub /user returned status %d", resp.StatusCode)
	}

	var parsed githubUserAPIResponse
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil {
		return GitIdentity{}, fmt.Errorf("GitHub /user response was not valid JSON")
	}
	if parsed.Login == "" {
		return GitIdentity{}, fmt.Errorf("GitHub /user response missing login")
	}

	email := parsed.Email
	if email == "" {
		email = fmt.Sprintf("%d+%s@users.noreply.github.com", parsed.ID, parsed.Login)
	}
	return GitIdentity{Name: parsed.Login, Email: email}, nil
}
