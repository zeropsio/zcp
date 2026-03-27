// Tests for: tools/deploy_local.go — zerops_deploy local mode MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeployLocalTool_Schema_NoSourceService(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, "proj-1", authInfo, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var foundTool *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "zerops_deploy" {
			foundTool = tool
			break
		}
	}
	if foundTool == nil {
		t.Fatal("zerops_deploy not found in tool list")
	}

	// Marshal the schema to JSON and inspect properties.
	if foundTool.InputSchema == nil {
		t.Fatal("expected non-nil input schema")
	}
	schemaJSON, err := json.Marshal(foundTool.InputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	schemaStr := string(schemaJSON)

	// Schema must NOT contain sourceService (local mode has no source concept).
	if strings.Contains(schemaStr, "sourceService") {
		t.Error("local mode schema should NOT have sourceService property")
	}
	// Must have targetService.
	if !strings.Contains(schemaStr, "targetService") {
		t.Error("local mode schema should have targetService property")
	}
	// Must have workingDir.
	if !strings.Contains(schemaStr, "workingDir") {
		t.Error("local mode schema should have workingDir property")
	}
}

func TestDeployLocalTool_Description_MentionsZcli(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, "proj-1", authInfo, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var desc string
	for _, tool := range result.Tools {
		if tool.Name == "zerops_deploy" {
			desc = tool.Description
			break
		}
	}
	if desc == "" {
		t.Fatal("zerops_deploy not found")
	}
	if !strings.Contains(desc, "zcli") {
		t.Errorf("local deploy description should mention zcli, got: %q", desc)
	}
	if strings.Contains(desc, "SSH") {
		t.Errorf("local deploy description should NOT mention SSH, got: %q", desc)
	}
}

func TestDeployLocalTool_SameToolName(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, "proj-1", authInfo, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	found := false
	for _, tool := range result.Tools {
		if tool.Name == "zerops_deploy" {
			found = true
		}
	}
	if !found {
		t.Error("expected zerops_deploy tool to be registered in local mode")
	}
}
