// Tests for: tool annotations — verify all tools have correct metadata.
package tools_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// browserAnnotationStub satisfies ops.browserRunner via the exported
// override so the annotations test can register zerops_browser without
// requiring agent-browser installed locally.
type browserAnnotationStub struct{}

func (browserAnnotationStub) LookPath() (string, error) { return "/fake/agent-browser", nil }
func (browserAnnotationStub) Run(_ context.Context, _ string, _ time.Duration) (string, string, bool, error) {
	return "", "", false, nil
}
func (browserAnnotationStub) RecoverFork(_ context.Context) {}

// nopMounter satisfies ops.Mounter for annotation tests (never called).
type nopMounter struct{}

var _ ops.Mounter = (*nopMounter)(nil)

func (*nopMounter) CheckMount(_ context.Context, _ string) (platform.MountState, error) {
	return platform.MountStateNotMounted, nil
}
func (*nopMounter) Mount(_ context.Context, _, _ string) error           { return nil }
func (*nopMounter) Unmount(_ context.Context, _, _ string) error         { return nil }
func (*nopMounter) ForceUnmount(_ context.Context, _, _ string) error    { return nil }
func (*nopMounter) IsWritable(_ context.Context, _ string) (bool, error) { return false, nil }
func (*nopMounter) ListMountDirs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (*nopMounter) HasUnit(_ context.Context, _ string) (bool, error) { return false, nil }
func (*nopMounter) CleanupUnit(_ context.Context, _ string) error     { return nil }

// nopSSH satisfies ops.SSHDeployer for annotation tests (never called).
type nopSSH struct{}

func (*nopSSH) ExecSSH(_ context.Context, _, _ string) ([]byte, error) { return nil, nil }
func (*nopSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

func TestAnnotations_AllToolsHaveTitleAndAnnotations(t *testing.T) {
	t.Parallel()

	toolMap := listAllTools(t, runtime.Info{})

	tests := []struct {
		name        string
		title       string
		readOnly    bool
		idempotent  bool
		destructive *bool
		openWorld   *bool
	}{
		// Read-only tools
		{name: "zerops_workflow", title: "Workflow orchestration", openWorld: boolPtr(false)},
		{name: "zerops_discover", title: "Discover project and services", readOnly: true, idempotent: true},
		{name: "zerops_knowledge", title: "Zerops knowledge access", readOnly: true, idempotent: true},
		{name: "zerops_logs", title: "Fetch service logs", readOnly: true, idempotent: true},
		{name: "zerops_events", title: "Fetch project activity timeline", readOnly: true, idempotent: true},
		{name: "zerops_verify", title: "Verify service health", readOnly: true, idempotent: true},
		{name: "zerops_export", title: "Export project/service configuration", readOnly: true, idempotent: true},

		// Mutating tools
		{name: "zerops_process", title: "Wait for, check, or cancel async process", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_manage", title: "Manage service lifecycle", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_scale", title: "Scale a service", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_delete", title: "Delete a service", destructive: boolPtr(true)},
		{name: "zerops_subdomain", title: "Enable or disable subdomain", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_deploy", title: "Deploy code to a service", destructive: boolPtr(true)},
		{name: "zerops_env", title: "Manage environment variables", destructive: boolPtr(true)},
		{name: "zerops_import", title: "Import services from YAML", destructive: boolPtr(true)},
		{name: "zerops_mount", title: "Mount/unmount service filesystems", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_dev_server", title: "Manage dev server lifecycle", idempotent: true, destructive: boolPtr(false)},
		{name: "zerops_deploy_batch", title: "Deploy batch — parallel deploys", destructive: boolPtr(true)},

		// Knowledge / workflow-adjacent
		{name: "zerops_preprocess", title: "Expand Zerops preprocessor expressions", readOnly: true},
		{name: "zerops_record_fact", title: "Record deploy-time fact"},
		{name: "zerops_workspace_manifest", title: "Workspace manifest (read/update)"},
	}

	// Completeness: every registered tool must have a table entry (so a new
	// tool can't ship with nil annotations or wrong hints unnoticed). The
	// table name "AllTools" is now enforced, not aspirational.
	// zerops_browser is exempt — it is container-only (absent from
	// listAllTools under a bare runtime.Info{}) and covered by the dedicated
	// TestAnnotations_BrowserTool. The authoring tools are exempt — they
	// register only under ZCP_AUTHORING=1 (this test is t.Parallel, so it
	// cannot pin the env) and are covered by the dedicated non-parallel
	// TestAnnotations_AuthoringTools.
	tabled := make(map[string]bool, len(tests))
	for _, tt := range tests {
		tabled[tt.name] = true
	}
	exempt := map[string]bool{
		"zerops_browser": true, // container-only, TestAnnotations_BrowserTool
		"zerops_recipe":  true, // ZCP_AUTHORING-gated, TestAnnotations_AuthoringTools
	}
	for name := range toolMap {
		if exempt[name] {
			continue
		}
		if !tabled[name] {
			t.Errorf("registered tool %q has no annotations table entry — add one (this is how wrong destructive/read-only hints ship unnoticed)", name)
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tool, ok := toolMap[tt.name]
			if !ok {
				t.Fatalf("tool %s not found", tt.name)
			}

			// All tools must have non-empty description.
			if tool.Description == "" {
				t.Errorf("tool %s has empty description", tt.name)
			}

			if tool.Annotations == nil {
				t.Fatalf("tool %s has nil annotations", tt.name)
			}

			ann := tool.Annotations

			if ann.Title != tt.title {
				t.Errorf("tool %s: Title = %q, want %q", tt.name, ann.Title, tt.title)
			}
			if ann.ReadOnlyHint != tt.readOnly {
				t.Errorf("tool %s: ReadOnlyHint = %v, want %v", tt.name, ann.ReadOnlyHint, tt.readOnly)
			}
			if ann.IdempotentHint != tt.idempotent {
				t.Errorf("tool %s: IdempotentHint = %v, want %v", tt.name, ann.IdempotentHint, tt.idempotent)
			}
			if !equalBoolPtr(ann.DestructiveHint, tt.destructive) {
				t.Errorf("tool %s: DestructiveHint = %v, want %v", tt.name, ptrStr(ann.DestructiveHint), ptrStr(tt.destructive))
			}
			if !equalBoolPtr(ann.OpenWorldHint, tt.openWorld) {
				t.Errorf("tool %s: OpenWorldHint = %v, want %v", tt.name, ptrStr(ann.OpenWorldHint), ptrStr(tt.openWorld))
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func equalBoolPtr(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func ptrStr(p *bool) string {
	if p == nil {
		return "<nil>"
	}
	if *p {
		return "true"
	}
	return "false"
}

func TestAnnotations_DescriptionWordCount(t *testing.T) {
	t.Parallel()

	toolMap := listAllTools(t, runtime.Info{})

	const maxWords = 60

	// Phase 6.2: cap applies to every registered tool, with explicit
	// debt tracking instead of silent grandfathering. The pre-Phase-6.2
	// allowlist excluded six tools "because they were not part of the
	// trim plan" — opaque exemption that let descriptions grow without
	// review. The new shape inverts it: every tool is checked, and
	// each known oversize is named in `untrimmedTools` with its
	// current word count and the reason its trim is non-trivial. Each
	// entry is technical debt; trim the description AND remove the
	// exemption in the same PR. Do NOT add new entries — descriptions
	// for new tools must fit the cap from day one.
	untrimmedTools := map[string]string{
		"zerops_workflow":           "~210 words — orchestration tool spans five workflows + three deploy-axis actions + multi-language trigger phrases for natural-language routing of launch-production (Czech + English); trim path is to move per-workflow blurbs + trigger-phrase tables into an atom-corpus discoverability surface so the description holds just the tool's contract",
		"zerops_dev_server":         "173 words — explains the historical hand-rolled pattern this tool replaces + the reason-code taxonomy; substantial rewrite needed to keep load-bearing parts under 60",
		"zerops_deploy_batch":       "68 words — borderline; one-pass copy edit",
		"zerops_record_fact":        "81 words — explains workflow integration semantics; trim by moving workflow context to atom corpus",
		"zerops_preprocess":         "88 words — explains motivation + invariants; trim by referencing spec instead of restating",
		"zerops_workspace_manifest": "66 words — borderline; one-pass copy edit",
	}

	for name, tool := range toolMap {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, exempt := untrimmedTools[name]; exempt {
				t.Skipf("tool %s: documented untrimmed debt — see untrimmedTools map", name)
			}
			words := strings.Fields(tool.Description)
			if len(words) > maxWords {
				t.Errorf("tool %s: description has %d words (max %d). "+
					"Either trim the description, OR add an entry to `untrimmedTools` "+
					"with a one-line rationale (then file a follow-up issue to actually trim it).\n%s",
					name, len(words), maxWords, tool.Description)
			}
		})
	}
}

func TestAnnotations_DescriptionKeywords(t *testing.T) {
	t.Parallel()

	toolMap := listAllTools(t, runtime.Info{})

	tests := []struct {
		name     string
		keywords []string
	}{
		{name: "zerops_discover", keywords: []string{"env var", "includeEnvs", "includeEnvValues"}},
		{name: "zerops_deploy", keywords: []string{"SSH", "zerops.yaml", "deployFiles"}},
		{name: "zerops_import", keywords: []string{"workflow", "YAML"}},
		{name: "zerops_manage", keywords: []string{"reload", "restart", "connect-storage", "/mnt/"}},
		{name: "zerops_env", keywords: []string{"set", "delete", "restart"}},
		{name: "zerops_subdomain", keywords: []string{"enable", "disable", "subdomain"}},
		{name: "zerops_knowledge", keywords: []string{"briefing", "scope", "query", "recipe"}},
		{name: "zerops_verify", keywords: []string{"health", "pass", "fail", "info"}},
		{name: "zerops_process", keywords: []string{"wait", "cancel", "status"}},
		{name: "zerops_export", keywords: []string{"export", "yaml", "service"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool, ok := toolMap[tt.name]
			if !ok {
				t.Fatalf("tool %s not found", tt.name)
			}
			desc := strings.ToLower(tool.Description)
			for _, kw := range tt.keywords {
				if !strings.Contains(desc, strings.ToLower(kw)) {
					t.Errorf("tool %s: description missing keyword %q:\n%s",
						tt.name, kw, tool.Description)
				}
			}
		})
	}
}

// listAllTools creates a test MCP server for the given runtime.Info and
// returns all registered tools by name. The authoring gate is driven by
// rt.Authoring (single owner — runtime.Detect), so callers select gate
// state via runtime.Info{Authoring: ...}, not env.
func listAllTools(t *testing.T, rt runtime.Info) map[string]*mcp.Tool {
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

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{}, rt)

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
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	toolMap := make(map[string]*mcp.Tool)
	for _, tool := range result.Tools {
		toolMap[tool.Name] = tool
	}
	return toolMap
}

// TestAnnotations_BrowserTool locks the metadata for the container-only
// zerops_browser tool. The tool is gated on InContainer + PATH, so the
// default annotations test (which runs with runtime.Info{}) can never
// see it. This dedicated test brings up a server with InContainer=true
// and a stub runner, then asserts the same title/hint invariants the
// general test enforces for every other tool.
func TestAnnotations_BrowserTool(t *testing.T) {
	// Not parallel: overrides ops.browserRun global. Sequential blocks
	// run before the parallel group in this file.
	restore := ops.OverrideBrowserRunnerForTest(browserAnnotationStub{})
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

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{},
		runtime.Info{InContainer: true, ServiceID: "s1"})

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
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var tool *mcp.Tool
	for _, tl := range result.Tools {
		if tl.Name == "zerops_browser" {
			tool = tl
			break
		}
	}
	if tool == nil {
		t.Fatal("zerops_browser should be registered when InContainer=true and agent-browser is on PATH")
	}

	if tool.Description == "" {
		t.Error("zerops_browser has empty description")
	}
	if tool.Annotations == nil {
		t.Fatal("zerops_browser has nil annotations")
	}
	ann := tool.Annotations
	if ann.Title != "Drive browser via agent-browser" {
		t.Errorf("Title = %q, want %q", ann.Title, "Drive browser via agent-browser")
	}
	if ann.ReadOnlyHint {
		t.Error("browser tool should not be marked ReadOnlyHint (it mutates Chrome state)")
	}
	if ann.IdempotentHint {
		t.Error("browser tool should not be marked IdempotentHint (walks mutate page state)")
	}
	if ann.DestructiveHint == nil || *ann.DestructiveHint {
		t.Errorf("DestructiveHint must be false (tool only drives browser, does not touch Zerops resources), got %v", ann.DestructiveHint)
	}
	if ann.OpenWorldHint == nil || !*ann.OpenWorldHint {
		t.Errorf("OpenWorldHint must be true (browser walks arbitrary URLs), got %v", ann.OpenWorldHint)
	}

	// Description must mention the key safety points that agents rely on.
	keywords := []string{"batch", "lifecycle", "close"}
	desc := strings.ToLower(tool.Description)
	for _, kw := range keywords {
		if !strings.Contains(desc, kw) {
			t.Errorf("description missing keyword %q:\n%s", kw, tool.Description)
		}
	}
}

// TestAnnotations_AuthoringTools locks the metadata for the
// authoring-gated surface (docs/spec-authoring-boundary.md §gate). The
// general annotations test runs gate-off and can never see these tools;
// this dedicated test enables the gate via runtime.Info{Authoring:true}
// and asserts the same title invariant the general test enforces for
// every other tool.
func TestAnnotations_AuthoringTools(t *testing.T) {
	// Not parallel: t.Setenv (recipe mount root). The gate itself is
	// driven by the rt flag, not env.
	t.Setenv("ZCP_RECIPE_MOUNT_ROOT", t.TempDir())

	toolMap := listAllTools(t, runtime.Info{Authoring: true})

	authoring := []struct {
		name  string
		title string
	}{
		{name: "zerops_recipe", title: "Run a Zerops recipe (v3)"},
		{name: "zerops_port", title: "Port OSS software to Zerops (authoring)"},
	}
	for _, tt := range authoring {
		tool, ok := toolMap[tt.name]
		if !ok {
			t.Errorf("%s should be registered when ZCP_AUTHORING=1", tt.name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("%s has empty description", tt.name)
		}
		if tool.Annotations == nil {
			t.Errorf("%s has nil annotations", tt.name)
			continue
		}
		if tool.Annotations.Title != tt.title {
			t.Errorf("%s: Title = %q, want %q", tt.name, tool.Annotations.Title, tt.title)
		}
	}
}

// TestAnnotations_AuthoringToolsAbsentByDefault pins the other half of
// the gate: rt.Authoring=false registers NO authoring tool, so end
// users never pay the schema context cost.
func TestAnnotations_AuthoringToolsAbsentByDefault(t *testing.T) {
	t.Parallel()

	toolMap := listAllTools(t, runtime.Info{})
	for _, name := range []string{"zerops_recipe", "zerops_port"} {
		if _, ok := toolMap[name]; ok {
			t.Errorf("%s registered without ZCP_AUTHORING=1 — the gate leaked", name)
		}
	}
}

// TestAnnotations_DeployLocalTool locks the zerops_deploy annotations on
// the LOCAL registration path (RegisterDeployLocal), which every other
// annotations test in this file never exercises: listAllTools always
// passes a non-nil &nopSSH{}, so the server always wires
// RegisterDeploySSH and registers zerops_deploy from deploy_ssh.go. When
// zcp runs outside a Zerops container (the common laptop case), cmd
// wiring passes a nil SSH deployer and server.New falls back to
// RegisterDeployLocal (deploy_local.go) instead — a second, independent
// registration of the same tool name. This is a drift guard: the two
// registrations must never disagree on safety-relevant annotations
// (Title/ReadOnlyHint/IdempotentHint/DestructiveHint/OpenWorldHint), so
// the expected values here are the same ones the SSH-path table entry
// in TestAnnotations_AllToolsHaveTitleAndAnnotations pins for
// "zerops_deploy" — title "Deploy code to a service",
// destructive=boolPtr(true), all other hints zero-value.
func TestAnnotations_DeployLocalTool(t *testing.T) {
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

	// sshDeployer=nil is the load-bearing difference from listAllTools:
	// it steers server.New's registerTools onto RegisterDeployLocal
	// instead of RegisterDeploySSH (mirrors internal/server/server_test.go's
	// listServerTools, which passes nil, nil for the same reason).
	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, nil, &nopMounter{}, runtime.Info{})

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
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var tool *mcp.Tool
	for _, tl := range result.Tools {
		if tl.Name == "zerops_deploy" {
			tool = tl
			break
		}
	}
	if tool == nil {
		t.Fatal("zerops_deploy should be registered in local mode (sshDeployer=nil)")
	}
	if tool.Description == "" {
		t.Error("zerops_deploy (local) has empty description")
	}
	if tool.Annotations == nil {
		t.Fatal("zerops_deploy (local) has nil annotations")
	}

	// Expected values mirror the SSH-path table entry for "zerops_deploy"
	// in TestAnnotations_AllToolsHaveTitleAndAnnotations — the two
	// registrations of one tool name must never drift on these.
	const wantTitle = "Deploy code to a service"
	wantDestructive := boolPtr(true)
	var wantReadOnly, wantIdempotent bool
	var wantOpenWorld *bool

	ann := tool.Annotations
	if ann.Title != wantTitle {
		t.Errorf("zerops_deploy (local): Title = %q, want %q", ann.Title, wantTitle)
	}
	if ann.ReadOnlyHint != wantReadOnly {
		t.Errorf("zerops_deploy (local): ReadOnlyHint = %v, want %v", ann.ReadOnlyHint, wantReadOnly)
	}
	if ann.IdempotentHint != wantIdempotent {
		t.Errorf("zerops_deploy (local): IdempotentHint = %v, want %v", ann.IdempotentHint, wantIdempotent)
	}
	if !equalBoolPtr(ann.DestructiveHint, wantDestructive) {
		t.Errorf("zerops_deploy (local): DestructiveHint = %v, want %v", ptrStr(ann.DestructiveHint), ptrStr(wantDestructive))
	}
	if !equalBoolPtr(ann.OpenWorldHint, wantOpenWorld) {
		t.Errorf("zerops_deploy (local): OpenWorldHint = %v, want %v", ptrStr(ann.OpenWorldHint), ptrStr(wantOpenWorld))
	}
}

func TestAnnotations_DeleteToolRequiresExplicitApproval(t *testing.T) {
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

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{}, runtime.Info{})

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
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var deleteTool *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "zerops_delete" {
			deleteTool = tool
			break
		}
	}
	if deleteTool == nil {
		t.Fatal("zerops_delete tool not found")
	}

	// Delete tool description must require explicit user approval.
	if !strings.Contains(deleteTool.Description, "explicit user approval") {
		t.Errorf("zerops_delete description should contain 'explicit user approval', got: %s", deleteTool.Description)
	}
}
