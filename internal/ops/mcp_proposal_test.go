// Tests for: internal/ops/mcp_proposal.go — a Mate proposes connecting an
// MCP server for its agents and runs nothing; the person types any key on
// the card (docs/spec-mate.md §2.11).
package ops

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// Fake credentials are built from parts so no scanner mistakes them for real ones.
var (
	fakeGitHubToken = "gh" + "p_" + strings.Repeat("a1B2", 9)
	fakeLongToken   = strings.Repeat("Zx9", 12)
)

// TestProposeMcp_Shape pins the mcp.add proposal the client builds its card
// from, field by field: the server's definition apart from the fields the
// person fills in.
func TestProposeMcp_Shape(t *testing.T) {
	t.Parallel()
	const head = `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"mcp.add",`
	const project = `"projectId":"pJuno","projectName":"Northwind Shop - Juno",`
	tests := []struct {
		name string
		in   McpProposalInput
		want string
	}{
		{
			name: "a remote server that signs in with OAuth, scope defaulting to the Mate",
			in: McpProposalInput{
				Name: "linear", Title: "Linear", Why: "to read the issues you link", Transport: "http",
				URL: "https://mcp.linear.app/mcp", Auth: "oauth", DocsURL: "https://linear.app/docs/mcp",
			},
			want: head + `"summary":"Set up Linear","reason":"to read the issues you link",` + project +
				`"params":{"scope":"mate","server":{"name":"linear","title":"Linear","transport":{"type":"http","url":"https://mcp.linear.app/mcp"},"auth":{"kind":"oauth"},"docsUrl":"https://linear.app/docs/mcp"},"credentials":[]}},` +
				`"note":"` + McpProposeNote + `"}`,
		},
		{
			name: "a remote server with a bearer header, scopes and a project scope",
			in: McpProposalInput{
				Name: "sentry", Why: "to read the errors", Transport: "http", URL: "https://mcp.sentry.dev/mcp",
				Auth: "header", Scope: "project",
				Fields: []McpFieldInput{{Key: "SENTRY_TOKEN", Label: "Sentry auth token", Placement: "header", HeaderName: "Authorization", Prefix: "Bearer "}},
			},
			want: head + `"summary":"Set up sentry","reason":"to read the errors",` + project +
				`"params":{"scope":"project","server":{"name":"sentry","title":"sentry","transport":{"type":"http","url":"https://mcp.sentry.dev/mcp"},"auth":{"kind":"header"}},"credentials":[{"key":"SENTRY_TOKEN","label":"Sentry auth token","placement":"header","headerName":"Authorization","prefix":"Bearer "}]}},` +
				`"note":"` + McpProposeNote + McpScopeNote + `"}`,
		},
		{
			name: "a command with a key in its environment",
			in: McpProposalInput{
				Name: "github", Title: "GitHub", Why: "to open pull requests", Transport: "stdio",
				Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"}, Auth: "none",
				Fields: []McpFieldInput{{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Label: "GitHub token", Placement: "env"}},
			},
			want: head + `"summary":"Set up GitHub","reason":"to open pull requests",` + project +
				`"params":{"scope":"mate","server":{"name":"github","title":"GitHub","transport":{"type":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-github"]},"auth":{"kind":"none"}},"credentials":[{"key":"GITHUB_PERSONAL_ACCESS_TOKEN","label":"GitHub token","placement":"env"}]}},` +
				`"note":"` + McpProposeNote + `"}`,
		},
		{
			name: "an OAuth app with its scopes",
			in: McpProposalInput{
				Name: "acme", Title: "Acme", Why: "w", Transport: "sse", URL: "https://acme.example/sse",
				Auth: "oauth-app", Scopes: []string{"read", " write "},
			},
			want: head + `"summary":"Set up Acme","reason":"w",` + project +
				`"params":{"scope":"mate","server":{"name":"acme","title":"Acme","transport":{"type":"sse","url":"https://acme.example/sse"},"auth":{"kind":"oauth-app","scopes":["read","write"]}},"credentials":[]}},` +
				`"note":"` + McpProposeNote + `"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeMcp("pJuno", "Northwind Shop - Juno", fixedID, tt.in)
			if err != nil {
				t.Fatalf("ProposeMcp: %v", err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Errorf("proposal =\n%s\nwant\n%s", raw, tt.want)
			}
		})
	}
}

// TestProposeMcp_RefusesSecretsAndWhatCannotConnect: a proposal never
// carries a key — the person types it on the card — and names a server
// every agent can be given.
func TestProposeMcp_RefusesSecretsAndWhatCannotConnect(t *testing.T) {
	t.Parallel()
	remote := func(edit func(*McpProposalInput)) McpProposalInput {
		in := McpProposalInput{Name: "linear", Why: "w", Transport: "http", URL: "https://mcp.linear.app/mcp", Auth: "oauth"}
		edit(&in)
		return in
	}
	command := func(edit func(*McpProposalInput)) McpProposalInput {
		in := McpProposalInput{Name: "github", Why: "w", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"}, Auth: "none"}
		edit(&in)
		return in
	}
	field := McpFieldInput{Key: "TOKEN", Label: "Token", Placement: "header", HeaderName: "Authorization", Prefix: "Bearer "}
	tests := []struct {
		name    string
		in      McpProposalInput
		wantMsg string
	}{
		{"no reason", remote(func(in *McpProposalInput) { in.Why = " " }), "needs a reason"},
		{"a name no agent's config takes", remote(func(in *McpProposalInput) { in.Name = "my server" }), "letters, digits"},
		{"Zerops' own name", remote(func(in *McpProposalInput) { in.Name = "Zerops" }), "Zerops' own"},
		{"an unknown transport", remote(func(in *McpProposalInput) { in.Transport = "ws" }), "transport"},
		{"a remote server without its address", remote(func(in *McpProposalInput) { in.URL = "" }), "needs its url"},
		{"an address that is not http", remote(func(in *McpProposalInput) { in.URL = "ftp://x.example/mcp" }), "http"},
		{"a remote server with a command", remote(func(in *McpProposalInput) { in.Command = "npx" }), "no command"},
		{"a command without one", command(func(in *McpProposalInput) { in.Command = "" }), "needs its command"},
		{"a command with an address", command(func(in *McpProposalInput) { in.URL = "https://x.example" }), "no url"},
		{"an unknown sign-in", remote(func(in *McpProposalInput) { in.Auth = "password" }), "auth"},
		{"a command that signs in with OAuth", command(func(in *McpProposalInput) { in.Auth = "oauth" }), "remote"},
		{"a header sign-in without a header field", remote(func(in *McpProposalInput) { in.Auth = "header" }), "header field"},
		{"an unknown scope", remote(func(in *McpProposalInput) { in.Scope = "team" }), "scope"},
		{"scopes without OAuth", command(func(in *McpProposalInput) { in.Scopes = []string{"repo"} }), "scopes"},
		{"a field without a label", remote(func(in *McpProposalInput) {
			in.Auth = "header"
			in.Fields = []McpFieldInput{{Key: "TOKEN", Placement: "header", HeaderName: "Authorization"}}
		}), "label"},
		{"a field placed nowhere known", remote(func(in *McpProposalInput) {
			in.Fields = []McpFieldInput{{Key: "TOKEN", Label: "Token", Placement: "body"}}
		}), "placement"},
		{"an environment field on a remote server", remote(func(in *McpProposalInput) {
			in.Fields = []McpFieldInput{{Key: "TOKEN", Label: "Token", Placement: "env"}}
		}), "a command's"},
		{"a header field on a command", command(func(in *McpProposalInput) { in.Fields = []McpFieldInput{field} }), "a remote server's"},
		{"a field twice", remote(func(in *McpProposalInput) {
			in.Auth = "header"
			in.Fields = []McpFieldInput{field, field}
		}), "twice"},
		{"a header name that is no header", remote(func(in *McpProposalInput) {
			in.Auth = "header"
			in.Fields = []McpFieldInput{{Key: "TOKEN", Label: "Token", Placement: "header", HeaderName: "Auth header"}}
		}), "header name"},
		{"a known token in the address", remote(func(in *McpProposalInput) { in.URL = "https://x.example/mcp/" + fakeGitHubToken }), "secret"},
		{"a key in the address's query", remote(func(in *McpProposalInput) { in.URL = "https://x.example/mcp?api_key=abc" }), "secret"},
		{"a password in the address", remote(func(in *McpProposalInput) { in.URL = "https://me:pw@x.example/mcp" }), "secret"},
		{"a token in the arguments", command(func(in *McpProposalInput) { in.Args = []string{"-y", "pkg", fakeLongToken} }), "secret"},
		{"a key flag with its value", command(func(in *McpProposalInput) { in.Args = []string{"pkg", "--api-key=abc"} }), "secret"},
		{"a key flag before its value", command(func(in *McpProposalInput) { in.Args = []string{"pkg", "--token", "abc"} }), "secret"},
		{"a token for a prefix", remote(func(in *McpProposalInput) {
			in.Auth = "header"
			in.Fields = []McpFieldInput{{Key: "TOKEN", Label: "Token", Placement: "header", HeaderName: "Authorization", Prefix: "Bearer " + fakeGitHubToken}}
		}), "secret"},
		{"a token for a label", remote(func(in *McpProposalInput) {
			in.Fields = []McpFieldInput{{Key: "TOKEN", Label: fakeGitHubToken, Placement: "header", HeaderName: "X-Key"}}
		}), "secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ProposeMcp("pJuno", "Juno", fixedID, tt.in)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter || !strings.Contains(pe.Message, tt.wantMsg) {
				t.Fatalf("err = %v, want %s containing %q", err, platform.ErrInvalidParameter, tt.wantMsg)
			}
		})
	}
	// What only looks long is no secret: a package, a project's id, a key read from the environment.
	for _, in := range []McpProposalInput{
		command(func(in *McpProposalInput) {
			in.Args = []string{"-y", "@modelcontextprotocol/server-everything-everywhere", "--token", "${GITHUB_TOKEN}"}
		}),
		remote(func(in *McpProposalInput) {
			in.URL = "https://x.example/p/123e4567-e89b-12d3-a456-426614174000/mcp?region=eu"
		}),
	} {
		if _, err := ProposeMcp("pJuno", "Juno", fixedID, in); err != nil {
			t.Errorf("%+v is refused: %v", in, err)
		}
	}
}
