// Tests for: integration — zerops_lease in a Mate puts the Mate credential on
// the pool's GIT_TOKEN for a repository on its HQ, through the full MCP server
// (docs/spec-workflows.md §8 DP-16).

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
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// TestLease_Integration_HQRepoInAMate_PutsTheMateCredentialOnThePool: in a
// Mate enrolled with its HQ, the first acquire of a repository on HQ writes
// the Mate credential to the pool's GIT_TOKEN as a sensitive secret before it
// checks out; the next one finds zcp's fingerprint matching and writes
// nothing. No result and no file under the working directory carries the
// credential.
func TestLease_Integration_HQRepoInAMate_PutsTheMateCredentialOnThePool(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir), t.Setenv (HOME holds the
	// enrollment) and a swap of http.DefaultTransport (the orchestrator).
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: leaseIntHQ, ProjectID: "proj-1", Credential: leaseIntHQCredential}); err != nil {
		t.Fatal(err)
	}
	routeLeaseOrchestrator(t)
	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	ssh := &stageLeaseSSH{}
	srv := server.New(context.Background(), mock,
		&auth.Info{ProjectID: "proj-1", Token: "test-token", APIHost: "localhost", Region: "prg1"},
		store, platform.NewMockLogFetcher(), ssh, nil,
		runtime.Info{InContainer: true, ServiceID: "s-zcp", ServiceName: "zcp", ProjectID: "proj-1", MateEnabled: true}, nil)
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
		t.Errorf("GIT_TOKEN writes = %d, want one: the second acquire finds zcp's fingerprint matching", n)
	}
	envs, err := mock.GetServiceEnv(ctx, "svc-pool")
	if err != nil {
		t.Fatal(err)
	}
	var token *platform.ServiceEnvVar
	for i := range envs {
		if envs[i].Key == "GIT_TOKEN" {
			token = &envs[i]
		}
	}
	if token == nil || token.Content != leaseIntHQCredential || !token.Sensitive {
		t.Errorf("the pool's GIT_TOKEN = %+v, want the Mate credential as a sensitive secret", token)
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
