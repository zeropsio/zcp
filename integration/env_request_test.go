// Tests for: integration — zerops_env action="request" through the full MCP
// server: a Mate's zcp records each ask it opens, so a second request for a
// key whose ask is open shows the person no second prompt; a container
// without ZCP_MATE_ENABLED answers every request as before
// (docs/spec-mate.md §5.8, §2.0).

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/hq/hqtest"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// Non-parallel: t.Chdir rebases the state dir server.New derives, and
// hqtest.Link sets the process environment.
func TestEnvRequest_OverMCP_AMateAsksAKeyOnce(t *testing.T) {
	tests := []struct {
		name       string
		mate       bool
		wantSecond bool // the second request asks again
	}{
		{name: "a Mate's zcp shows no second prompt", mate: true},
		{name: "outside a Mate every request answers as before", mate: false, wantSecond: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			hq := httptest.NewServer(http.NotFoundHandler())
			t.Cleanup(hq.Close)
			hqtest.Link(t, hq.URL, "proj-1", "fixture")
			dir := t.TempDir()
			t.Chdir(dir)

			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "flip"}).
				WithServices(nil)
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("knowledge store: %v", err)
			}
			srv := server.New(context.Background(), mock,
				&auth.Info{ProjectID: "proj-1", ProjectName: "flip", Token: "test-token", APIHost: "localhost"},
				store, platform.NewMockLogFetcher(), &mockSSHDeployer{output: []byte("ok")}, nil,
				runtime.Info{InContainer: true, ServiceID: "s-zcp", ServiceName: "zcp", ProjectID: "proj-1", MateEnabled: tt.mate}, nil)
			t.Cleanup(srv.Stop)

			ctx := context.Background()
			st, ct := mcp.NewInMemoryTransports()
			if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
				t.Fatalf("server connect: %v", err)
			}
			session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
			if err != nil {
				t.Fatalf("client connect: %v", err)
			}
			defer session.Close()

			ask := map[string]any{"action": "request", "project": true, "key": "FLIP_GOOGLE_CLIENT_ID",
				"label": "Google sign-in client ID", "why": "for Google sign-in on the preview", "sensitive": true}
			first := callAndGetResult(t, session, "zerops_env", ask)
			if first.IsError || !strings.Contains(getTextContent(t, first), `"requested"`) {
				t.Fatalf("the first request asked nothing: %s", getTextContent(t, first))
			}
			second := callAndGetResult(t, session, "zerops_env", ask)
			text := getTextContent(t, second)
			asked := !second.IsError && strings.Contains(text, `"requested"`)
			if asked != tt.wantSecond {
				t.Errorf("second request asked = %v, want %v: %s", asked, tt.wantSecond, text)
			}
			if !tt.wantSecond && !strings.Contains(text, platform.ErrAlreadyAsked) {
				t.Errorf("the second request is not refused as already asked: %s", text)
			}

			_, statErr := os.Stat(filepath.Join(dir, ".zcp", "state", "vault-asks"))
			if recorded := statErr == nil; recorded != tt.mate {
				t.Errorf("an ask record exists = %v, want %v (only a Mate records asks)", recorded, tt.mate)
			}
		})
	}
}
