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
	RegisterDeployLocal(srv, mock, okHTTP, "proj-1", authInfo, nil, "", nil, nil)

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
	RegisterDeployLocal(srv, mock, okHTTP, "proj-1", authInfo, nil, "", nil, nil)

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
	RegisterDeployLocal(srv, mock, okHTTP, "proj-1", authInfo, nil, "", nil, nil)

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

// F5 closure (audit-prerelease-internal-testing-2026-04-29):
// sessionAnnotations returns a structured WorkSessionState mirroring the
// envelope's `develop-closed-auto` lifecycle vocabulary. The three
// status values (none / open / auto-closed) carry distinct field shapes
// so the agent distinguishes "session never opened" from "session
// auto-closed mid-iteration" without an extra round-trip through
// action="status".
func TestSessionAnnotations_NoSession_StatusNone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got := sessionAnnotations(dir)
	if got == nil {
		t.Fatal("expected non-nil WorkSessionState")
	}
	if got.Status != "none" {
		t.Errorf("Status = %q, want %q", got.Status, "none")
	}
	for _, needle := range []string{"No active develop session", "scope="} {
		if !strings.Contains(got.Note, needle) {
			t.Errorf("Note missing %q: %s", needle, got.Note)
		}
	}
	if got.Progress != nil {
		t.Errorf("Progress: want nil for status=none, got %+v", got.Progress)
	}
	if got.ClosedAt != "" || got.CloseReason != "" {
		t.Errorf("ClosedAt/CloseReason: want empty for status=none, got %q / %q", got.ClosedAt, got.CloseReason)
	}
}

func TestSessionAnnotations_ActiveSession_StatusOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "test", []string{"appdev"})
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	got := sessionAnnotations(dir)
	if got == nil {
		t.Fatal("expected non-nil WorkSessionState")
	}
	if got.Status != "open" {
		t.Errorf("Status = %q, want %q", got.Status, "open")
	}
	if got.Progress == nil {
		t.Fatal("expected non-nil Progress on status=open")
	}
	if got.Progress.Total != 1 {
		t.Errorf("Progress.Total = %d, want 1", got.Progress.Total)
	}
	if got.Note != "" {
		t.Errorf("Note: want empty for status=open, got %q", got.Note)
	}
}

func TestSessionAnnotations_ClosedSession_StatusAutoClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	closedAt := time.Now().UTC().Format(time.RFC3339)
	ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "done", []string{"appdev"})
	ws.ClosedAt = closedAt
	ws.CloseReason = workflow.CloseReasonAutoComplete
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	got := sessionAnnotations(dir)
	if got == nil {
		t.Fatal("expected non-nil WorkSessionState")
	}
	if got.Status != "auto-closed" {
		t.Errorf("Status = %q, want %q", got.Status, "auto-closed")
	}
	if got.ClosedAt != closedAt {
		t.Errorf("ClosedAt = %q, want %q", got.ClosedAt, closedAt)
	}
	if got.CloseReason != workflow.CloseReasonAutoComplete {
		t.Errorf("CloseReason = %q, want %q", got.CloseReason, workflow.CloseReasonAutoComplete)
	}
	if !strings.Contains(got.Note, "auto-closed") || !strings.Contains(got.Note, closedAt) {
		t.Errorf("Note missing auto-close summary: %q", got.Note)
	}
	if got.Progress != nil {
		t.Errorf("Progress: want nil for status=auto-closed, got %+v", got.Progress)
	}
}
