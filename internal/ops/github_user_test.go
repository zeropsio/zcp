// Tests for: ops/github_user.go — DeriveGitHubIdentity (F3 human attribution).
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestIsGitHubRemote pins the strict, fail-CLOSED gate (Codex diff-review
// finding 1a): only an exact, successfully-parsed "github.com" host
// qualifies for PAT derivation. Every other case — including inputs that
// ops.ParseGitHost's fail-OPEN credential-scoping default would happily
// resolve to "github.com" — must be rejected, since a false positive here
// would send a token collected for a different host to GitHub's API.
func TestIsGitHubRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"exact https github.com", "https://github.com/owner/repo.git", true},
		{"uppercase host normalizes to a match", "https://GITHUB.COM/owner/repo.git", true},
		{"mixed-case host normalizes to a match", "https://GitHub.Com/owner/repo.git", true},
		{"gitlab.com rejected", "https://gitlab.com/owner/repo.git", false},
		{"api subdomain rejected", "https://api.github.com/owner/repo.git", false},
		{"gist subdomain rejected", "https://gist.github.com/owner/repo.git", false},
		{"lookalike domain rejected", "https://github.com.evil.tld/owner/repo.git", false},
		{"lookalike prefix rejected", "https://evilgithub.com/owner/repo.git", false},
		{"IPv6 literal rejected", "https://[::1]/owner/repo.git", false},
		{"IPv4 literal rejected", "https://192.0.2.1/owner/repo.git", false},
		{"empty string rejected (would fail-open under ParseGitHost)", "", false},
		{"malformed URL rejected (would fail-open under ParseGitHost)", "not a url at all", false},
		{"scp-form SSH remote rejected", "git@github.com:owner/repo.git", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsGitHubRemote(tt.url); got != tt.want {
				t.Errorf("IsGitHubRemote(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// stubGitHubHTTP is a minimal HTTPDoer that returns a fixed status/body for
// every request, or a transport error when err is set — mirrors the
// stubHTTPAlwaysOK pattern already used elsewhere (tools/subdomain_test.go)
// so DeriveGitHubIdentity is tested through the EXISTING HTTPDoer seam, not
// a parallel test hook. capturedReq lets tests assert the auth header/URL
// without a second mock layer. respondingHost, when non-empty, simulates a
// response that arrived from a DIFFERENT host than the one requested (a
// redirect elsewhere) — the redirect-host hardening tests use this; every
// other test leaves it empty, which mirrors a real *http.Client populating
// resp.Request with the actual (same-host) final request.
type stubGitHubHTTP struct {
	status         int
	body           string
	err            error
	respondingHost string
	capturedReq    *http.Request
}

func (s *stubGitHubHTTP) Do(req *http.Request) (*http.Response, error) {
	s.capturedReq = req
	if s.err != nil {
		return nil, s.err
	}
	finalReq := req
	if s.respondingHost != "" {
		redirectedURL := *req.URL
		redirectedURL.Host = s.respondingHost
		redirected := *req
		redirected.URL = &redirectedURL
		finalReq = &redirected
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    finalReq,
	}, nil
}

// TestDeriveGitHubIdentity_PublicEmail pins the happy path: name is the
// login, email is the account's public email when set. Also pins the
// request shape (Bearer auth header, exact /user URL) since that's the
// contract the whole derivation depends on.
func TestDeriveGitHubIdentity_PublicEmail(t *testing.T) {
	t.Parallel()
	body, marshalErr := json.Marshal(githubUserAPIResponse{Login: "octocat", ID: 583231, Email: "octocat@github.com"})
	if marshalErr != nil {
		t.Fatalf("json.Marshal fixture: %v", marshalErr)
	}
	stub := &stubGitHubHTTP{status: http.StatusOK, body: string(body)}

	identity, err := DeriveGitHubIdentity(context.Background(), stub, "ghp_token")
	if err != nil {
		t.Fatalf("DeriveGitHubIdentity: %v", err)
	}
	if identity.Name != "octocat" {
		t.Errorf("Name = %q, want octocat", identity.Name)
	}
	if identity.Email != "octocat@github.com" {
		t.Errorf("Email = %q, want octocat@github.com (public email)", identity.Email)
	}
	if got := stub.capturedReq.Header.Get("Authorization"); got != "Bearer ghp_token" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer ghp_token")
	}
	if got := stub.capturedReq.URL.String(); got != githubUserAPIURL {
		t.Errorf("request URL = %q, want %q", got, githubUserAPIURL)
	}
}

// TestDeriveGitHubIdentity_PrivateEmail_NoreplyFallback pins the
// documented noreply pattern when the account has no public email (private
// or unset) — GitHub accepts commits/pushes attributed to this address.
func TestDeriveGitHubIdentity_PrivateEmail_NoreplyFallback(t *testing.T) {
	t.Parallel()
	body, marshalErr := json.Marshal(githubUserAPIResponse{Login: "octocat", ID: 583231, Email: ""})
	if marshalErr != nil {
		t.Fatalf("json.Marshal fixture: %v", marshalErr)
	}
	stub := &stubGitHubHTTP{status: http.StatusOK, body: string(body)}

	identity, err := DeriveGitHubIdentity(context.Background(), stub, "ghp_token")
	if err != nil {
		t.Fatalf("DeriveGitHubIdentity: %v", err)
	}
	if identity.Name != "octocat" {
		t.Errorf("Name = %q, want octocat", identity.Name)
	}
	want := "583231+octocat@users.noreply.github.com"
	if identity.Email != want {
		t.Errorf("Email = %q, want %q (documented noreply pattern)", identity.Email, want)
	}
}

// TestDeriveGitHubIdentity_APIFailure pins that a non-200 GitHub response
// (e.g. bad/expired token) is a plain error — the CALLER decides that's
// non-blocking, this function only reports what happened.
func TestDeriveGitHubIdentity_APIFailure(t *testing.T) {
	t.Parallel()
	stub := &stubGitHubHTTP{status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}

	if _, err := DeriveGitHubIdentity(context.Background(), stub, "bad_token"); err == nil {
		t.Fatal("expected error for non-200 GitHub response")
	}
}

// TestDeriveGitHubIdentity_TransportFailure pins that a network-level
// failure (no response at all) surfaces as an error too.
func TestDeriveGitHubIdentity_TransportFailure(t *testing.T) {
	t.Parallel()
	stub := &stubGitHubHTTP{err: fmt.Errorf("connection refused")}

	if _, err := DeriveGitHubIdentity(context.Background(), stub, "ghp_token"); err == nil {
		t.Fatal("expected error for transport failure")
	}
}

// TestDeriveGitHubIdentity_NilHTTPClient mirrors WaitHTTPReady's nil-client
// contract: a nil httpClient is a supported "derivation unavailable" input
// that returns an error rather than panicking.
func TestDeriveGitHubIdentity_NilHTTPClient(t *testing.T) {
	t.Parallel()
	if _, err := DeriveGitHubIdentity(context.Background(), nil, "ghp_token"); err == nil {
		t.Fatal("expected error for nil httpClient")
	}
}

// TestDeriveGitHubIdentity_MalformedBody pins that unparseable JSON is an
// error, not a zero-value identity silently returned.
func TestDeriveGitHubIdentity_MalformedBody(t *testing.T) {
	t.Parallel()
	stub := &stubGitHubHTTP{status: http.StatusOK, body: `not json`}
	if _, err := DeriveGitHubIdentity(context.Background(), stub, "ghp_token"); err == nil {
		t.Fatal("expected error for malformed JSON body")
	}
}

// TestDeriveGitHubIdentity_MissingLogin pins that a response missing the
// login field (should never happen against the real API, but defensive)
// is an error rather than a blank-name identity.
func TestDeriveGitHubIdentity_MissingLogin(t *testing.T) {
	t.Parallel()
	stub := &stubGitHubHTTP{status: http.StatusOK, body: `{"id":1,"email":"a@b.com"}`}
	if _, err := DeriveGitHubIdentity(context.Background(), stub, "ghp_token"); err == nil {
		t.Fatal("expected error when login is missing/empty")
	}
}

// TestDeriveGitHubIdentity_RedirectedResponse_Rejected is the Codex
// diff-review finding 1c pin: a response whose FINAL request (post any
// redirect a real *http.Client followed) landed on a host other than
// api.github.com must never be trusted, even if it carries a 200 + a
// well-formed body — a redirected response could be attacker-controlled
// content that never actually authenticated against GitHub.
func TestDeriveGitHubIdentity_RedirectedResponse_Rejected(t *testing.T) {
	t.Parallel()
	body, marshalErr := json.Marshal(githubUserAPIResponse{Login: "octocat", ID: 1, Email: "octocat@github.com"})
	if marshalErr != nil {
		t.Fatalf("json.Marshal fixture: %v", marshalErr)
	}
	stub := &stubGitHubHTTP{status: http.StatusOK, body: string(body), respondingHost: "evil.example.com"}

	if _, err := DeriveGitHubIdentity(context.Background(), stub, "ghp_token"); err == nil {
		t.Fatal("expected error for a response whose final request landed on a non-api.github.com host")
	}
}

// TestDeriveGitHubIdentity_ErrorsAreSanitized is the Codex diff-review
// finding 1b pin: neither a non-200 response body nor a transport error's
// raw text may appear in DeriveGitHubIdentity's returned error — callers
// reflect that error directly into agent-facing warning text, and neither
// a GitHub error page nor an arbitrary transport error string is safe to
// echo verbatim there.
func TestDeriveGitHubIdentity_ErrorsAreSanitized(t *testing.T) {
	t.Parallel()
	const sentinel = "SENSITIVE_SENTINEL_MUST_NOT_LEAK"

	t.Run("non-200 body dropped", func(t *testing.T) {
		t.Parallel()
		stub := &stubGitHubHTTP{status: http.StatusUnauthorized, body: `{"message":"` + sentinel + `"}`}
		_, err := DeriveGitHubIdentity(context.Background(), stub, "bad_token")
		if err == nil {
			t.Fatal("expected error")
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Errorf("error must not reflect the raw response body; got: %v", err)
		}
	})

	t.Run("transport error text dropped", func(t *testing.T) {
		t.Parallel()
		stub := &stubGitHubHTTP{err: fmt.Errorf("dial tcp %s: connection refused", sentinel)}
		_, err := DeriveGitHubIdentity(context.Background(), stub, "ghp_token")
		if err == nil {
			t.Fatal("expected error")
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Errorf("error must not reflect the raw transport error text; got: %v", err)
		}
	})
}
