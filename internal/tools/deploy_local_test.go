// Tests for: tools/deploy_local.go — zerops_deploy local mode MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestDeployLocalTool_Schema_NoSourceService(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, "proj-1", authInfo, nil, "", nil)

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
	RegisterDeployLocal(srv, mock, "proj-1", authInfo, nil, "", nil)

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
	RegisterDeployLocal(srv, mock, "proj-1", authInfo, nil, "", nil)

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

// P9: workSessionNote emits a warning when a deploy lands without an
// active work session, and stays empty when one is in flight. Soft
// nudge, not a hard block — agent keeps discretion per
// spec-work-session.md §0.4.
func TestWorkSessionNote_NoSession_Warns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	note := workSessionNote(dir)
	if note == "" {
		t.Fatal("expected warning when no session is open")
	}
	for _, needle := range []string{"No active develop session", "scope="} {
		if !strings.Contains(note, needle) {
			t.Errorf("warning missing %q: %s", needle, note)
		}
	}
}

func TestWorkSessionNote_ActiveSession_Silent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "test", []string{"appdev"})
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	if note := workSessionNote(dir); note != "" {
		t.Errorf("no warning expected with open session, got: %s", note)
	}
}

func TestWorkSessionNote_ClosedSession_Warns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "done", []string{"appdev"})
	ws.ClosedAt = time.Now().UTC().Format(time.RFC3339)
	ws.CloseReason = workflow.CloseReasonAutoComplete
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	if note := workSessionNote(dir); note == "" {
		t.Error("expected warning when session is closed (not being tracked for next task)")
	}
}
