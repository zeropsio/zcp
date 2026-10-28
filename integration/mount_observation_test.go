package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

type unreadableMountObservation struct{ nopMounter }

func (*unreadableMountObservation) CheckMount(context.Context, string) (platform.MountState, error) {
	return platform.MountStateNotMounted, errors.New("mount observation did not answer")
}

func TestMountObservation_Integration_UnreadableStatus_IsErrorWithoutAbsentMountFacts(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}})
	authInfo := &auth.Info{ProjectID: "proj-1", Token: "test-token", APIHost: "localhost", Region: "prg1", ClientID: "client-1", ProjectName: "test-project"}
	ctx := context.Background()
	srv := server.New(ctx, mock, authInfo, store, defaultLogFetcher(), nil, &unreadableMountObservation{}, runtime.Info{InContainer: true}, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()
	for _, hostname := range []string{"app", "apppool-3", ""} {
		res := callAndGetResult(t, session, "zerops_mount", map[string]any{"action": "status", "serviceHostname": hostname})
		text := getTextContent(t, res)
		if !res.IsError || !strings.Contains(text, "mount observation did not answer") {
			t.Fatalf("%q unreadable status must preserve the error diagnostic: isError %v, text %s", hostname, res.IsError, text)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &fields); err != nil {
			t.Fatalf("error response is not JSON: %v", err)
		}
		if mounts, ok := fields["mounts"]; ok {
			t.Fatalf("%q unreadable status supplied absent mount facts: %s", hostname, mounts)
		}
		if res.StructuredContent != nil {
			t.Fatalf("error response must remain a text envelope: %v", res.StructuredContent)
		}
	}
}
