// Tests for: hq — the project key travels in the Authorization header of
// every call the client makes, never in a URL (docs/spec-mate.md §3, TC-3).
package hq

import (
	"context"
	"strings"
	"testing"
)

// TestClient_EveryCallPresentsTheKeyInTheHeaderAndNeverInTheURL: each route
// the client speaks to HQ carries `Authorization: Project <key>`, and no path
// or query HQ sees holds the key.
func TestClient_EveryCallPresentsTheKeyInTheHeaderAndNeverInTheURL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	routes := []struct {
		name string
		call func(Client)
	}{
		{"ensure repo", func(c Client) { _, _ = c.EnsureRepo(ctx, "appdev") }},
		{"repo exists", func(c Client) { _, _ = c.RepoExists(ctx, "app-1", "web") }},
		{"open change", func(c Client) { _, _ = c.OpenChange(ctx, "appdev", "t", "b") }},
		{"edit change", func(c Client) { _, _ = c.EditChange(ctx, "appdev", 3, ChangeEdit{}) }},
		{"attach", func(c Client) { _, _ = c.Attach(ctx, "appdev", 3, []byte("png")) }},
		{"propose chief", func(c Client) { _, _ = c.ProposeChief(ctx) }},
		{"ai key", func(c Client) { _, _ = c.AIKey(ctx) }},
		{"GitHub credential", func(c Client) { _, _ = c.GitHubCredential(ctx, "acme/shop.git") }},
		{"environments", func(c Client) { _, _ = c.Environments(ctx) }},
		{"environment status", func(c Client) { _, _ = c.EnvironmentStatus(ctx, "p2") }},
		{"environment logs", func(c Client) { _, _ = c.EnvironmentLogs(ctx, "p2", "s1", 10) }},
		{"recipe tier", func(c Client) { _, _ = c.RecipeTier(ctx, "mate") }},
		{"self", func(c Client) { _, _ = c.Self(ctx) }},
		{"key recheck", func(c Client) { _, _ = c.RecheckKey(ctx) }},
		{"signers", func(c Client) { _, _ = c.Signers(ctx, "p-mate") }},
	}
	for _, tt := range routes {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, calls := answering(t, `{}`)
			tt.call(client.Once())
			if len(*calls) == 0 {
				t.Fatal("HQ was not asked")
			}
			for _, call := range *calls {
				if call.Authorization != "Project good" {
					t.Errorf("%s %s presented %q, want %q", call.Method, call.Path, call.Authorization, "Project good")
				}
				if strings.Contains(call.Path, "good") {
					t.Errorf("%s %s holds the key", call.Method, call.Path)
				}
			}
		})
	}
}
