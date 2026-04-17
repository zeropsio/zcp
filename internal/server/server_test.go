// Tests for: server package — MCP server setup and tool registration.
package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestServer_AllToolsRegistered(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()

	// Mount tool is now always registered (nil mounter returns error at call time).
	srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	_, err = srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	// zerops_deploy is always registered: SSH mode when sshDeployer is non-nil,
	// local mode (zcli push) when sshDeployer is nil.
	expectedTools := []string{
		"zerops_workflow", "zerops_discover", "zerops_knowledge", "zerops_guidance",
		"zerops_record_fact",
		"zerops_logs", "zerops_events", "zerops_process", "zerops_verify",
		"zerops_deploy", "zerops_export",
		"zerops_manage", "zerops_scale", "zerops_env", "zerops_import", "zerops_delete", "zerops_subdomain",
		"zerops_mount", "zerops_preprocess",
	}

	if len(result.Tools) != len(expectedTools) {
		names := make([]string, 0, len(result.Tools))
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("expected %d tools, got %d: %v", len(expectedTools), len(result.Tools), names)
	}

	toolMap := make(map[string]bool)
	for _, tool := range result.Tools {
		toolMap[tool.Name] = true
	}
	for _, name := range expectedTools {
		if !toolMap[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
	if !toolMap["zerops_deploy"] {
		t.Error("zerops_deploy should be registered in local mode when sshDeployer is nil")
	}
}

// stubBrowserRunner satisfies the ops browser runner interface for the
// browser-gating test. Returns a scripted LookPath error.
type stubBrowserRunner struct {
	lookPathErr error
}

func (s *stubBrowserRunner) LookPath() (string, error) {
	if s.lookPathErr != nil {
		return "", s.lookPathErr
	}
	return "/usr/local/bin/agent-browser", nil
}

func (*stubBrowserRunner) Run(_ context.Context, _ string, _ time.Duration) (string, string, bool, error) {
	return "", "", false, nil
}

func (*stubBrowserRunner) RecoverFork(_ context.Context) {}

// TestServer_BrowserToolGating locks the registration condition: zerops_browser
// is exposed IFF running in a Zerops container AND agent-browser is on PATH.
// Uses the ops runner override to simulate binary presence without requiring
// agent-browser actually installed on the test machine.
//
// Non-parallel: overrides a package-level global in internal/ops. Go runs
// non-parallel tests sequentially before parallel tests begin, so this will
// not race the other parallel tests in this file.
func TestServer_BrowserToolGating(t *testing.T) {
	tests := []struct {
		name     string
		rt       runtime.Info
		binErr   error
		wantTool bool
	}{
		{
			name:     "in container with agent-browser present",
			rt:       runtime.Info{InContainer: true, ServiceID: "s1"},
			binErr:   nil,
			wantTool: true,
		},
		{
			name:     "in container without agent-browser",
			rt:       runtime.Info{InContainer: true, ServiceID: "s1"},
			binErr:   errors.New("not found in PATH"),
			wantTool: false,
		},
		{
			name:     "local dev with agent-browser present",
			rt:       runtime.Info{},
			binErr:   nil,
			wantTool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := ops.OverrideBrowserRunnerForTest(&stubBrowserRunner{lookPathErr: tt.binErr})
			defer restore()

			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "p1", Name: "test"}).
				WithServices(nil)
			authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("knowledge store: %v", err)
			}
			logFetcher := platform.NewMockLogFetcher()

			srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, tt.rt)

			ctx := context.Background()
			st, ct := mcp.NewInMemoryTransports()
			if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
				t.Fatalf("server connect: %v", err)
			}
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
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
				if tool.Name == "zerops_browser" {
					found = true
					break
				}
			}
			if found != tt.wantTool {
				t.Errorf("zerops_browser registered = %v, want %v", found, tt.wantTool)
			}
		})
	}
}

func TestServer_Instructions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rt   runtime.Info
		want string
		miss string
	}{
		{
			name: "in container with service name",
			rt:   runtime.Info{InContainer: true, ServiceName: "zcpx", ServiceID: "abc", ProjectID: "def"},
			want: "zcpx",
		},
		{
			name: "in container without service name",
			rt:   runtime.Info{InContainer: true, ServiceID: "abc"},
			miss: "running inside",
		},
		{
			name: "local dev (no context)",
			rt:   runtime.Info{},
			miss: "running inside",
		},
		{
			name: "base instructions always included",
			rt:   runtime.Info{InContainer: true, ServiceName: "myservice"},
			want: "ZCP manages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inst := BuildInstructions(context.Background(), nil, "", tt.rt, "")
			if tt.want != "" && !strings.Contains(inst, tt.want) {
				t.Errorf("Instructions should contain %q, got: %s", tt.want, inst)
			}
			if tt.miss != "" && strings.Contains(inst, tt.miss) {
				t.Errorf("Instructions should NOT contain %q, got: %s", tt.miss, inst)
			}
		})
	}
}

func TestServer_Instructions_FitIn2KB(t *testing.T) {
	t.Parallel()
	// MCP instructions are capped at 2KB by Claude Code v2.1.84+.
	// Static instructions must leave room for dynamic content (service listing, router).
	containerStatic := baseInstructions + routingInstructions + containerEnvironment
	if len(containerStatic) > 800 {
		t.Errorf("container static instructions = %d bytes, want < 800 to leave room for dynamic content", len(containerStatic))
	}
}

func TestServer_BaseInstructions_WorkflowDirective(t *testing.T) {
	t.Parallel()
	if !strings.Contains(baseInstructions, "Every code task") {
		t.Error("baseInstructions should contain workflow cycle directive")
	}
	if !strings.Contains(baseInstructions, `workflow="develop"`) {
		t.Error("baseInstructions should reference develop workflow")
	}
	if !strings.Contains(baseInstructions, "bootstrap") {
		t.Error("baseInstructions should mention bootstrap workflow")
	}
}

func TestBuildInstructions_WithServices(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "appdev", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "appstage", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "db", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	})

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, "")

	for _, want := range []string{"appdev", "appstage", "db", "nodejs@22", "postgresql@16", "RUNNING"} {
		if !strings.Contains(inst, want) {
			t.Errorf("instructions should contain %q", want)
		}
	}
	if !strings.Contains(inst, "ZCP manages") {
		t.Error("instructions should contain base instructions")
	}
	// Should use tracked mode syntax.
	if !strings.Contains(inst, `action="start"`) {
		t.Error("should use tracked mode syntax")
	}
	// Should contain anti-deletion language.
	if !strings.Contains(inst, "Do NOT delete") {
		t.Error("should contain anti-deletion warning")
	}
	// Unmanaged runtimes should show auto-adopt hint.
	if !strings.Contains(inst, "auto-adopted") {
		t.Error("unmanaged runtime services should show auto-adopt label")
	}
}

func TestBuildInstructions_UnmanagedProject(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "api", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, "")

	// Should contain anti-deletion language.
	if !strings.Contains(inst, "Do NOT delete") {
		t.Error("should contain anti-deletion warning")
	}
	// Should show auto-adopt hint for unmanaged runtime.
	if !strings.Contains(inst, "auto-adopted") {
		t.Error("unmanaged runtime should have auto-adopt label")
	}
}

func TestBuildInstructions_FiltersSystemServices(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "core", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "core", ServiceStackTypeCategoryName: "CORE"}},
		{Name: "buildappdevv123", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "ubuntu-build@1", ServiceStackTypeCategoryName: "BUILD"}},
		{Name: "api", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{Name: "db", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}},
	})

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, "")

	// User services should appear.
	if !strings.Contains(inst, "api") {
		t.Error("instructions should contain user service 'api'")
	}
	if !strings.Contains(inst, "db") {
		t.Error("instructions should contain user service 'db'")
	}
	// System services should NOT appear.
	if strings.Contains(inst, "core") {
		t.Error("instructions should not contain system service 'core'")
	}
	if strings.Contains(inst, "buildappdevv123") {
		t.Error("instructions should not contain system service 'buildappdevv123'")
	}
}

func TestBuildInstructions_FreshProject(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices(nil)

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, "")

	if !strings.Contains(inst, "empty") {
		t.Error("instructions should mention empty project")
	}
	if !strings.Contains(inst, "bootstrap") {
		t.Error("instructions should recommend bootstrap")
	}
	if !strings.Contains(inst, `action="start"`) {
		t.Error("empty project directive should use tracked mode syntax")
	}
	if !strings.Contains(inst, `workflow="bootstrap"`) {
		t.Error("empty project directive should specify bootstrap workflow")
	}
}

func TestBuildInstructions_APIFailure(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithError("ListServices", platform.NewPlatformError(
		platform.ErrAPIError, "connection refused", "",
	))

	inst := BuildInstructions(context.Background(), mock, "proj-1", runtime.Info{}, "")

	if !strings.Contains(inst, "ZCP manages") {
		t.Error("instructions should still contain base instructions on API failure")
	}
}

func TestBuildInstructions_NilClient(t *testing.T) {
	t.Parallel()

	inst := BuildInstructions(context.Background(), nil, "", runtime.Info{}, "")

	if !strings.Contains(inst, "ZCP manages") {
		t.Error("instructions should contain base instructions with nil client")
	}
}

func TestBuildInstructions_WorkflowHint_ActiveBootstrap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Start bootstrap and complete 2 steps.
	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete discover with plan.
	if _, err := eng.BootstrapCompletePlan([]workflow.BootstrapTarget{{
		Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
	}}, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete provision.
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Attestation for provision ok", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	inst := BuildInstructions(context.Background(), nil, "", runtime.Info{}, dir)
	if !strings.Contains(inst, "Active workflow") {
		t.Error("instructions should contain workflow hint")
	}
	if !strings.Contains(inst, "bootstrap") {
		t.Error("hint should mention bootstrap workflow")
	}
	if !strings.Contains(inst, "step 3/5") {
		t.Errorf("hint should mention step 3/5, got: %s", inst)
	}
}

func TestBuildInstructions_WorkflowHint_NoState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // empty dir, no state file

	inst := BuildInstructions(context.Background(), nil, "", runtime.Info{}, dir)
	if strings.Contains(inst, "Active workflow") {
		t.Error("instructions should not contain workflow hint without state")
	}
}

func TestBuildInstructions_WorkflowHint_PhaseDone_NoHint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Complete full bootstrap — DONE sessions are immediately unregistered.
	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete discover with plan.
	if _, err := eng.BootstrapCompletePlan([]workflow.BootstrapTarget{{
		Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
	}}, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	steps := []string{
		"provision", "generate",
		"deploy", "close",
	}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	inst := BuildInstructions(context.Background(), nil, "", runtime.Info{}, dir)
	if strings.Contains(inst, "Active workflow") {
		t.Error("DONE sessions should not appear as active workflow hints")
	}
}

func TestBuildInstructions_WorkflowHint_EmptyDir(t *testing.T) {
	t.Parallel()
	inst := BuildInstructions(context.Background(), nil, "", runtime.Info{}, "")
	if strings.Contains(inst, "Active workflow") {
		t.Error("empty stateDir should produce no hint")
	}
}

func TestServer_Connect(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()

	srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	// Verify connection is alive by pinging.
	if err := session.Ping(ctx, nil); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestLogLevel_FromEnv(t *testing.T) {
	tests := []struct {
		env  string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"INFO", slog.LevelInfo},
		{"", slog.LevelDebug},
		{"invalid", slog.LevelDebug},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv("ZCP_LOG_LEVEL", tt.env)
			if got := logLevel(); got != tt.want {
				t.Errorf("logLevel(%q) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}

func TestObserve_CountsToolCalls(t *testing.T) {
	t.Parallel()

	s := &Server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mw := s.observe()

	nop := func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	}
	handler := mw(nop)

	// Tool calls are counted.
	if _, err := handler(context.Background(), methodCallTool, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := handler(context.Background(), methodCallTool, nil); err != nil {
		t.Fatal(err)
	}
	// Non-tool methods are not counted.
	if _, err := handler(context.Background(), "tools/list", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := handler(context.Background(), "initialize", nil); err != nil {
		t.Fatal(err)
	}

	if got := s.CallCount(); got != 2 {
		t.Errorf("CallCount() = %d, want 2", got)
	}
}

func TestObserve_PassesThrough(t *testing.T) {
	t.Parallel()

	s := &Server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mw := s.observe()

	sentinel := errors.New("handler error")
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return nil, sentinel
	})

	_, err := handler(context.Background(), methodCallTool, nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("middleware should pass through handler error, got %v", err)
	}
}
