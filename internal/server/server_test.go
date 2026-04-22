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
		"zerops_record_fact", "zerops_workspace_manifest",
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
			name: "local dev points at zerops_deploy tool",
			rt:   runtime.Info{},
			want: "zerops_deploy",
			miss: "zcli push",
		},
		{
			name: "container mentions SSHFS mount path",
			rt:   runtime.Info{InContainer: true},
			want: "/var/www/",
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
			inst := BuildInstructions(tt.rt)
			if tt.want != "" && !strings.Contains(inst, tt.want) {
				t.Errorf("Instructions should contain %q, got: %s", tt.want, inst)
			}
			if tt.miss != "" && strings.Contains(inst, tt.miss) {
				t.Errorf("Instructions should NOT contain %q, got: %s", tt.miss, inst)
			}
		})
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
