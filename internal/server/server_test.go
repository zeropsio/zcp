// Tests for: server package — MCP server setup and tool registration.
package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestServer_AllToolsRegistered(t *testing.T) {
	// Non-parallel: t.Chdir rebases cwd so server.New's stateDir derivation
	// (filepath.Join(cwd, .zcp/state)) lands under TempDir instead of polluting
	// internal/server/.zcp/.
	t.Chdir(t.TempDir())

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
		"zerops_record_fact", "zerops_workspace_manifest",
		"zerops_logs", "zerops_events", "zerops_process", "zerops_verify",
		"zerops_deploy", "zerops_export",
		"zerops_manage", "zerops_scale", "zerops_env", "zerops_import", "zerops_delete", "zerops_subdomain",
		"zerops_mount", "zerops_preprocess",
		"zerops_recipe",
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

func TestServer_Connect(t *testing.T) {
	// Non-parallel: t.Chdir rebases cwd so server.New's stateDir derivation
	// (filepath.Join(cwd, .zcp/state)) lands under TempDir instead of polluting
	// internal/server/.zcp/.
	t.Chdir(t.TempDir())

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

// TestServer_New_LocalAutoAdopt pins the eager adoption hook: when
// server.New runs in local env against an empty state dir, it writes a
// ServiceMeta keyed by the Zerops project name. Container env skips
// adoption entirely.
//
// Non-parallel: t.Chdir rebases cwd so the stateDir derivation in
// server.New (filepath.Join(cwd, .zcp/state)) lands under a TempDir.
// Note-text shape is covered in workflow/adopt_local_test.go
// TestFormatAdoptionNote_Shapes; here we assert the wired side-effect.
func TestServer_New_LocalAutoAdopt(t *testing.T) {
	tests := []struct {
		name        string
		rt          runtime.Info
		services    []platform.ServiceStack
		wantMeta    bool
		wantMode    topology.Mode
		wantStage   string
		wantManaged []string
	}{
		{
			name: "local + one runtime → local-stage",
			rt:   runtime.Info{},
			services: []platform.ServiceStack{{
				ID: "s1", Name: "apistage", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			}},
			wantMeta:  true,
			wantMode:  topology.PlanModeLocalStage,
			wantStage: "apistage",
		},
		{
			name:     "local + zero runtimes → local-only",
			rt:       runtime.Info{},
			services: nil,
			wantMeta: true,
			wantMode: topology.PlanModeLocalOnly,
		},
		{
			name: "local + multiple runtimes → local-only (no auto-link)",
			rt:   runtime.Info{},
			services: []platform.ServiceStack{
				{
					ID: "s1", Name: "api", Status: "ACTIVE",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "nodejs@22",
						ServiceStackTypeCategoryName: "USER",
					},
				},
				{
					ID: "s2", Name: "web", Status: "ACTIVE",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "nodejs@22",
						ServiceStackTypeCategoryName: "USER",
					},
				},
			},
			wantMeta: true,
			wantMode: topology.PlanModeLocalOnly,
		},
		{
			name: "container env → no adoption",
			rt:   runtime.Info{InContainer: true, ServiceName: "zcpx"},
			services: []platform.ServiceStack{{
				ID: "s1", Name: "apistage", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			}},
			wantMeta: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "p1", Name: "demo"}).
				WithServices(tt.services)
			authInfo := &auth.Info{ProjectID: "p1", Token: "t", APIHost: "localhost"}
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("knowledge store: %v", err)
			}

			_ = New(context.Background(), mock, authInfo, store, platform.NewMockLogFetcher(), nil, nil, tt.rt)

			// Verify side-effect: meta file existence + shape.
			cwd, _ := os.Getwd()
			stateDir := filepath.Join(cwd, ".zcp", "state")

			meta, _ := workflow.ReadServiceMeta(stateDir, "demo")
			if tt.wantMeta {
				if meta == nil {
					t.Fatalf("expected meta at %q after adoption", stateDir)
				}
				if meta.Mode != tt.wantMode {
					t.Errorf("Mode = %q, want %q", meta.Mode, tt.wantMode)
				}
				if meta.StageHostname != tt.wantStage {
					t.Errorf("StageHostname = %q, want %q", meta.StageHostname, tt.wantStage)
				}
				if meta.BootstrapSession != "" {
					t.Errorf("BootstrapSession = %q, want empty (adopted, not bootstrapped)", meta.BootstrapSession)
				}
			} else if meta != nil {
				t.Errorf("container env must not auto-adopt; got meta: %+v", meta)
			}
		})
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
