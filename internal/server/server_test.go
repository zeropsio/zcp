// Tests for: server package — MCP server setup and tool registration.
package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// defaultExpectedTools is the end-user tool surface — what every agent
// sees without the ZCP_AUTHORING gate. zerops_deploy is always
// registered: SSH mode when sshDeployer is non-nil, local mode (zcli
// push) when sshDeployer is nil.
var defaultExpectedTools = []string{
	"zerops_workflow", "zerops_discover", "zerops_knowledge",
	"zerops_record_fact", "zerops_workspace_manifest",
	"zerops_logs", "zerops_events", "zerops_process", "zerops_verify",
	"zerops_deploy", "zerops_export",
	"zerops_manage", "zerops_scale", "zerops_env", "zerops_import", "zerops_delete", "zerops_subdomain",
	"zerops_mount", "zerops_preprocess",
}

// authoringExpectedTools is the maintainer surface added on top when
// ZCP_AUTHORING=1 (docs/spec-authoring-boundary.md §gate).
var authoringExpectedTools = []string{
	"zerops_recipe",
	"zerops_port",
}

// listServerTools builds a fresh server for the given runtime.Info and
// returns its tool map. The authoring gate is driven by rt.Authoring
// (the single owner — see runtime.Detect), so callers select gate
// state by passing runtime.Info{Authoring: true/false}, NOT via env.
func listServerTools(t *testing.T, rt runtime.Info) map[string]bool {
	t.Helper()

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
	srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, rt, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	if _, err = srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	toolMap := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		toolMap[tool.Name] = true
	}
	return toolMap
}

func TestServer_AllToolsRegistered(t *testing.T) {
	// Non-parallel: t.Chdir rebases cwd so server.New's stateDir derivation
	// (filepath.Join(cwd, .zcp/state)) lands under TempDir instead of
	// polluting internal/server/.zcp/. Authoring gate OFF = the end-user
	// surface (rt.Authoring zero-value false).
	t.Chdir(t.TempDir())

	toolMap := listServerTools(t, runtime.Info{})

	if len(toolMap) != len(defaultExpectedTools) {
		names := make([]string, 0, len(toolMap))
		for name := range toolMap {
			names = append(names, name)
		}
		t.Fatalf("expected %d tools, got %d: %v", len(defaultExpectedTools), len(toolMap), names)
	}
	for _, name := range defaultExpectedTools {
		if !toolMap[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
	for _, name := range authoringExpectedTools {
		if toolMap[name] {
			t.Errorf("authoring tool %s registered WITHOUT ZCP_AUTHORING=1 — the gate leaked", name)
		}
	}
	if !toolMap["zerops_deploy"] {
		t.Error("zerops_deploy should be registered in local mode when sshDeployer is nil")
	}
}

// TestServer_AuthoringToolsRegistered — ZCP_AUTHORING=1 adds the
// maintainer authoring surface ON TOP of the unchanged end-user list
// (docs/spec-authoring-boundary.md §gate).
func TestServer_AuthoringToolsRegistered(t *testing.T) {
	// Non-parallel: t.Chdir (stateDir) + t.Setenv (recipe mount root).
	t.Chdir(t.TempDir())
	// Keep the recipe store's mount root inside the test sandbox — the
	// gated path constructs it eagerly.
	t.Setenv("ZCP_RECIPE_MOUNT_ROOT", t.TempDir())

	toolMap := listServerTools(t, runtime.Info{Authoring: true})

	want := len(defaultExpectedTools) + len(authoringExpectedTools)
	if len(toolMap) != want {
		names := make([]string, 0, len(toolMap))
		for name := range toolMap {
			names = append(names, name)
		}
		t.Fatalf("expected %d tools with ZCP_AUTHORING=1, got %d: %v", want, len(toolMap), names)
	}
	for _, name := range append(append([]string{}, defaultExpectedTools...), authoringExpectedTools...) {
		if !toolMap[name] {
			t.Errorf("missing tool with ZCP_AUTHORING=1: %s", name)
		}
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

			srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, tt.rt, nil)

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

	srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, runtime.Info{}, nil)

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

// TestServer_DoesNotAdvertiseResourcesCapability pins that ZCP is a
// tools-only MCP server: the initialize handshake MUST NOT advertise
// capabilities.resources. ZCP serves knowledge exclusively through the
// zerops_knowledge tool (uri="zerops://..."), never the MCP resources
// protocol — a non-universal client capability that cannot carry ZCP's
// adaptive, placeholder-substituted knowledge. The go-sdk advertises
// resources only when HasResources is set or a resource/template is
// registered (SDK server.go:587); ZCP does neither. This test catches a
// future AddResourceTemplate re-introduction that would silently re-open
// the dual-namespace retrieval trap. See
// plans/converge-knowledge-retrieval-format-2026-06-04.md.
func TestServer_DoesNotAdvertiseResourcesCapability(t *testing.T) {
	// Non-parallel: t.Chdir rebases cwd for server.New's stateDir derivation.
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

	srv := New(context.Background(), mock, authInfo, store, logFetcher, nil, nil, runtime.Info{}, nil)

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

	init := session.InitializeResult()
	if init == nil || init.Capabilities == nil {
		t.Fatal("expected a non-nil InitializeResult with capabilities")
	}
	if init.Capabilities.Resources != nil {
		t.Errorf("server must NOT advertise capabilities.resources (tools-only); got %+v", init.Capabilities.Resources)
	}
	// Sanity: tools capability IS advertised — confirms the assertion above
	// is meaningful (capabilities populated, resources specifically absent).
	if init.Capabilities.Tools == nil {
		t.Error("expected capabilities.tools to be advertised")
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
			rt:   runtime.Info{InContainer: true, ServiceName: "zcp"},
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

			_ = New(context.Background(), mock, authInfo, store, platform.NewMockLogFetcher(), nil, nil, tt.rt, nil)

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

// TestServerNew_LocalEnv_RefreshesAgentContext pins that the AGENTS.md +
// CLAUDE.md refresh-at-serve hook fires in local env. Multi-agent
// migration: AGENTS.md is canonical (refreshes when stale), CLAUDE.md
// is the thin @AGENTS.md wrapper (refreshes when stale, but ONLY if
// AGENTS.md exists — see TestRefreshAgentContext_PreUpgradeCLAUDEmd
// WithoutAgentsMD_LeftUntouched in content/ for the safeguard).
//
// Non-parallel: t.Chdir rebases cwd so server.New's stateDir derivation
// (filepath.Join(cwd, .zcp/state)) lands under TempDir.
func TestServerNew_LocalEnv_RefreshesAgentContext(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Seed AGENTS.md (canonical, stale body inside markers) + CLAUDE.md
	// (current wrapper). Refresh should rewrite AGENTS.md to current
	// template content and leave CLAUDE.md alone (already correct).
	agentsPath := filepath.Join(dir, "AGENTS.md")
	staleAgents := "<!-- ZCP:BEGIN -->\nstale body\n<!-- ZCP:END -->\nuser additions remain\n"
	if err := os.WriteFile(agentsPath, []byte(staleAgents), 0o644); err != nil {
		t.Fatalf("write AGENTS.md seed: %v", err)
	}
	claudePath := filepath.Join(dir, "CLAUDE.md")
	claudeWrapper := "<!-- ZCP:BEGIN -->\n@AGENTS.md\n<!-- ZCP:END -->\n"
	if err := os.WriteFile(claudePath, []byte(claudeWrapper), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md seed: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "demo"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "t", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	_ = New(context.Background(), mock, authInfo, store, platform.NewMockLogFetcher(), nil, nil, runtime.Info{}, nil)

	agentsBody, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(agentsBody) == staleAgents {
		t.Errorf("AGENTS.md not refreshed in local env; still:\n%s", string(agentsBody))
	}
	// User-additions outside the managed block must be preserved.
	if !strings.Contains(string(agentsBody), "user additions remain") {
		t.Errorf("user-additions section dropped during refresh; got:\n%s", string(agentsBody))
	}
}

// TestServerNew_ContainerEnv_StillRefreshesAgentContext pins the
// container-path regression: same refresh fires in container env.
func TestServerNew_ContainerEnv_StillRefreshesAgentContext(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	agentsPath := filepath.Join(dir, "AGENTS.md")
	staleAgents := "<!-- ZCP:BEGIN -->\nstale body\n<!-- ZCP:END -->\n"
	if err := os.WriteFile(agentsPath, []byte(staleAgents), 0o644); err != nil {
		t.Fatalf("write AGENTS.md seed: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "demo"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "t", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	_ = New(context.Background(), mock, authInfo, store, platform.NewMockLogFetcher(), nil, nil, runtime.Info{InContainer: true, ServiceName: "zcp"}, nil)

	agentsBody, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(agentsBody) == staleAgents {
		t.Errorf("AGENTS.md not refreshed in container env; still:\n%s", string(agentsBody))
	}
}

// TestServerNew_PreUpgradeClaudeMDWithoutAgentsMD_LeftUntouched pins
// the Codex-review-flagged safeguard: a pre-upgrade Claude user with
// stale full-body CLAUDE.md (no AGENTS.md yet) MUST NOT have CLAUDE.md
// rewritten by serve startup — the wrapper refresh would orphan the
// @AGENTS.md include. `zcp init` owns the migration; serve must wait.
func TestServerNew_PreUpgradeClaudeMDWithoutAgentsMD_LeftUntouched(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	claudePath := filepath.Join(dir, "CLAUDE.md")
	preUpgradeClaude := "<!-- ZCP:BEGIN -->\n# Zerops\n\nPRE-UPGRADE FULL BODY (stale, but doctrine still here)\n<!-- ZCP:END -->\n"
	if err := os.WriteFile(claudePath, []byte(preUpgradeClaude), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md seed: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "demo"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "t", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	_ = New(context.Background(), mock, authInfo, store, platform.NewMockLogFetcher(), nil, nil, runtime.Info{InContainer: true, ServiceName: "zcp"}, nil)

	got, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if string(got) != preUpgradeClaude {
		t.Errorf("CLAUDE.md was rewritten despite missing AGENTS.md — would orphan @AGENTS.md include:\n%s", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Errorf("AGENTS.md should still be missing (zcp init owns first-write); stat err=%v", statErr)
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
