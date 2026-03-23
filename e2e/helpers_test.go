//go:build e2e

// Tests for: e2e — helpers for E2E lifecycle tests against real Zerops API.

package e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// testServicePrefixes lists all hostname prefixes used by e2e tests.
var testServicePrefixes = []string{
	"bs", "in", "inc",                           // bootstrap_workflow_test.go
	"b2", "b3", "b4", "b5", "b9",                // bootstrap_modes_test.go
	"b6", "b8", "ba", "bb", "bad",               // bootstrap_advanced_test.go
	"zcprt", "zcpdb",                             // lifecycle_test.go, verify_test.go
	"zcppf", "zcpdpl",                            // deploy tests
	"zcpvrt", "zcpvdb",                           // verify_test.go
	"zcpsub", "zcpbl",                            // subdomain, build_logs
	"zcpmnt", "zcpapp",                           // mount_test.go
	"zcpsd",                                      // discover_subdomain_test.go
	"zcpsl",                                      // subdomain_lifecycle_test.go
	"bn",                                         // bootstrap_negative_test.go
}

func TestMain(m *testing.M) {
	code := m.Run()
	cleanupOrphanedTestServices()
	os.Exit(code)
}

// cleanupOrphanedTestServices deletes any services whose hostname matches a test prefix.
func cleanupOrphanedTestServices() {
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		return
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}

	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orphan cleanup: create client: %v\n", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orphan cleanup: auth resolve: %v\n", err)
		return
	}

	services, err := client.ListServices(ctx, authInfo.ProjectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orphan cleanup: list services: %v\n", err)
		return
	}

	for _, svc := range services {
		if !hasTestPrefix(svc.Name) {
			continue
		}
		fmt.Fprintf(os.Stderr, "orphan cleanup: deleting %s (id=%s)\n", svc.Name, svc.ID)
		proc, err := client.DeleteService(ctx, svc.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "orphan cleanup: delete %s: %v\n", svc.Name, err)
			continue
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}
	}
}

// hasTestPrefix checks if a hostname starts with any known test prefix.
func hasTestPrefix(hostname string) bool {
	for _, prefix := range testServicePrefixes {
		if strings.HasPrefix(hostname, prefix) {
			return true
		}
	}
	return false
}

// e2eHarness provides a real Zerops API client and MCP server for E2E tests.
type e2eHarness struct {
	t         *testing.T
	client    platform.Client
	projectID string
	authInfo  *auth.Info
	srv       *server.Server
}

// newHarness creates an E2E harness. Skips if ZCP_API_KEY is not set.
func newHarness(t *testing.T) *e2eHarness {
	t.Helper()

	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}

	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}

	region := os.Getenv("ZCP_REGION")
	if region == "" {
		region = "prg1"
	}

	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	// Ensure the region is set.
	authInfo.Region = region

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	logFetcher := platform.NewLogFetcher()
	sshDeployer := platform.NewSystemSSHDeployer()
	srv := server.New(context.Background(), client, authInfo, store, logFetcher, sshDeployer, nil, nil, runtime.Info{})

	return &e2eHarness{
		t:         t,
		client:    client,
		projectID: authInfo.ProjectID,
		authInfo:  authInfo,
		srv:       srv,
	}
}

// e2eSession wraps a connected MCP client session for E2E tool calls.
type e2eSession struct {
	t       *testing.T
	session *mcp.ClientSession
}

// newSession creates an MCP client session connected to the E2E server.
func newSession(t *testing.T, srv *server.Server) *e2eSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	_, err := srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return &e2eSession{t: t, session: session}
}

// callTool calls an MCP tool and returns the full result.
func (s *e2eSession) callTool(name string, args map[string]any) *mcp.CallToolResult {
	s.t.Helper()
	result, err := s.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		s.t.Fatalf("call %s: %v", name, err)
	}
	return result
}

// mustCallSuccess calls a tool and fatals if it returns IsError.
func (s *e2eSession) mustCallSuccess(name string, args map[string]any) string {
	s.t.Helper()
	result := s.callTool(name, args)
	if result.IsError {
		text := getE2ETextContent(s.t, result)
		s.t.Fatalf("%s returned error: %s", name, text)
	}
	return getE2ETextContent(s.t, result)
}

// getE2ETextContent extracts text from the first content item.
func getE2ETextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// randomSuffix returns a random 8-char hex string for unique test hostnames.
func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// cleanupServices deletes services by hostname (best-effort, logs errors).
func cleanupServices(ctx context.Context, client platform.Client, projectID string, hostnames ...string) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cleanup: list services: %v\n", err)
		return
	}
	for _, hostname := range hostnames {
		for _, svc := range services {
			if svc.Name == hostname {
				proc, err := client.DeleteService(ctx, svc.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "cleanup: delete %s: %v\n", hostname, err)
					continue
				}
				if proc == nil {
					continue
				}
				waitForProcessDirect(ctx, client, proc.ID)
			}
		}
	}
}
