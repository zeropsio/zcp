// Tests for: integration — zerops_lease in a Mate puts the Mate credential on
// the pool's ZCP_HQ_GIT_TOKEN for a repository on its HQ — never its
// GIT_TOKEN, the user's — through the full MCP server (docs/spec-workflows.md
// §8 DP-16).

package integration_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// TestLease_Integration_HQRepoInAMate_PutsTheMateCredentialOnThePool: in a
// Mate linked to its HQ, the first acquire of a repository on HQ writes
// the Mate's Git credential to the pool's ZCP_HQ_GIT_TOKEN as a sensitive secret
// before it checks out, leaving the user's GIT_TOKEN as it was; the next one
// finds zcp's fingerprint matching and writes nothing. No result and no file
// under the working directory carries the credential.
func TestLease_Integration_HQRepoInAMate_PutsTheMateCredentialOnThePool(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir) and a swap of
	// http.DefaultTransport (the orchestrator).
	dir := t.TempDir()
	t.Chdir(dir)
	routeLeaseOrchestrator(t)
	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{
			{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true},
			{ID: "env-g", Key: ops.GitTokenEnvKey, Content: "the-users-own-token", Sensitive: true},
		})
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	ssh := &stageLeaseSSH{}
	srv := server.New(context.Background(), mock,
		&auth.Info{ProjectID: "proj-1", Token: "test-token", APIHost: "localhost", Region: "prg1"},
		store, platform.NewMockLogFetcher(), ssh, nil,
		runtime.Info{InContainer: true, ServiceID: "s-zcp", ServiceName: "zcp", ProjectID: "proj-1", MateEnabled: true, HQ: leaseIntHQLink(t)}, nil)
	defer srv.WaitBackground()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	texts := make([]string, 0, 2)
	for range 2 {
		res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseIntHQRepo, "branch": "feat"})
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("acquire: %s", text)
		}
		texts = append(texts, text)
	}

	if n := mock.CallCount("CreateServiceEnvVar"); n != 1 {
		t.Errorf("HQ credential writes = %d, want one: the second acquire finds zcp's fingerprint matching", n)
	}
	envs, err := mock.GetServiceEnv(ctx, "svc-pool")
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]platform.ServiceEnvVar{}
	for _, e := range envs {
		held[e.Key] = e
	}
	if token, ok := held[ops.PoolHQCredentialEnvKey]; !ok || token.Content != leaseIntHQCredential || !token.Sensitive {
		t.Errorf("the pool's %s = %+v, want the Mate credential as a sensitive secret", ops.PoolHQCredentialEnvKey, token)
	}
	if token := held[ops.GitTokenEnvKey]; token.Content != "the-users-own-token" {
		t.Errorf("the pool's GIT_TOKEN = %+v, want the user's own token, never written over", token)
	}
	if at, _ := ssh.first("fetch -q --prune origin"); at < 0 {
		t.Error("the checkout did not run")
	}
	for _, text := range texts {
		if strings.Contains(text, leaseIntHQCredential) || strings.Contains(text, leaseIntLease) {
			t.Errorf("a secret reached a result: %s", text)
		}
	}
	if at, call := ssh.first(leaseIntHQCredential); at >= 0 {
		t.Errorf("the credential reached a session command: %s", call.command)
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry holds nothing to check
		}
		if raw, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(raw), leaseIntHQCredential) {
			t.Errorf("%s holds the Mate credential", path)
		}
		return nil
	})
}
