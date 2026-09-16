// Tests for: ops/gitea_user.go — DeriveGiteaIdentity (the Mate's bot commits
// as itself on the account's Gitea; guide 2.3).
package ops

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestGiteaUserAPIURL pins how the /user endpoint is built from GITEA_URL —
// the value the app wrote onto the service, which may or may not carry a
// trailing slash and is never guaranteed to be a bare origin.
func TestGiteaUserAPIURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		giteaURL string
		want     string
		wantErr  bool
	}{
		{name: "bare origin", giteaURL: "https://web-2ff4-3000.prg1.zerops.app", want: "https://web-2ff4-3000.prg1.zerops.app/api/v1/user"},
		{name: "trailing slash", giteaURL: "https://web-2ff4-3000.prg1.zerops.app/", want: "https://web-2ff4-3000.prg1.zerops.app/api/v1/user"},
		{name: "in-project http origin", giteaURL: "http://web:3000", want: "http://web:3000/api/v1/user"},
		{name: "empty", giteaURL: "", wantErr: true},
		{name: "no scheme", giteaURL: "web-2ff4-3000.prg1.zerops.app", wantErr: true},
		{name: "unparseable", giteaURL: "://", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := giteaUserAPIURL(tt.giteaURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("giteaUserAPIURL(%q) = %q, want an error", tt.giteaURL, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("giteaUserAPIURL(%q): %v", tt.giteaURL, err)
			}
			if got != tt.want {
				t.Errorf("giteaUserAPIURL(%q) = %q, want %q", tt.giteaURL, got, tt.want)
			}
		})
	}
}

// TestDeriveGiteaIdentity pins the identity a Mate commits with on its own
// Gitea: the bot's login, and `{login}@mate.invalid` — an address in a domain
// reserved to resolve nowhere. There is no public-email fallback the way
// GitHub has one: a Mate bot has no mailbox and must never claim a person's.
func TestDeriveGiteaIdentity(t *testing.T) {
	t.Parallel()
	const giteaURL = "https://web-2ff4-3000.prg1.zerops.app"

	tests := []struct {
		name      string
		stub      *stubGitHubHTTP
		nilClient bool
		wantName  string
		wantEmail string
		wantErr   bool
	}{
		{
			name:      "bot login",
			stub:      &stubGitHubHTTP{status: http.StatusOK, body: `{"login":"mate-p1","id":7,"email":"mate-p1@noreply.example"}`},
			wantName:  "mate-p1",
			wantEmail: "mate-p1@mate.invalid",
		},
		{
			name:    "a token Gitea refuses",
			stub:    &stubGitHubHTTP{status: http.StatusUnauthorized, body: `{"message":"token does not exist"}`},
			wantErr: true,
		},
		{
			name:    "no login in the body",
			stub:    &stubGitHubHTTP{status: http.StatusOK, body: `{"id":7}`},
			wantErr: true,
		},
		{
			name:    "not JSON",
			stub:    &stubGitHubHTTP{status: http.StatusOK, body: `<html>a proxy error page</html>`},
			wantErr: true,
		},
		{
			name:    "transport failure",
			stub:    &stubGitHubHTTP{err: errors.New("dial tcp: connection refused")},
			wantErr: true,
		},
		{
			name:      "no HTTP client",
			nilClient: true,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var client HTTPDoer
			if !tt.nilClient {
				client = tt.stub
			}
			identity, err := DeriveGiteaIdentity(context.Background(), client, giteaURL, "gitea_bot_token")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DeriveGiteaIdentity = %+v, want an error", identity)
				}
				// The error is surfaced verbatim in agent-facing text; a
				// response body (or the token) must never ride along.
				if strings.Contains(err.Error(), "gitea_bot_token") {
					t.Errorf("error leaked the token: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeriveGiteaIdentity: %v", err)
			}
			if identity.Name != tt.wantName || identity.Email != tt.wantEmail {
				t.Errorf("identity = (%q, %q), want (%q, %q)", identity.Name, identity.Email, tt.wantName, tt.wantEmail)
			}
			// Gitea authenticates a token with the `token` scheme, not Bearer.
			if got := tt.stub.capturedReq.Header.Get("Authorization"); got != "token gitea_bot_token" {
				t.Errorf("Authorization header = %q, want %q", got, "token gitea_bot_token")
			}
			if got := tt.stub.capturedReq.URL.String(); got != giteaURL+"/api/v1/user" {
				t.Errorf("request URL = %q, want %q", got, giteaURL+"/api/v1/user")
			}
		})
	}
}
