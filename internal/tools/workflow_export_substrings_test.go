package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestHandleExport_ExpectedSubstringsPostMigration is the contract test
// for the handleExport→atom-synthesis migration (Phase 0b). For each of
// the 7 export statuses, it asserts that:
//
//  1. response.guidance contains every "survived" phrase from the audit
//     in workflow_export_phrase_audit.md (presence). These are concepts
//     the post-migration rendered guidance MUST surface.
//  2. response.guidance does NOT contain any "dropped-pre-migration"
//     phrase (absence). These are plan / amendment cites that were
//     inline today but must be stripped during atom migration.
//
// Scope of the absence check is `guidance` only. Other surfaces that
// might mention the same cite (e.g. `bundle.warnings` from
// `ops.BuildBundle`) are out of Phase 0b scope — that's an ops-layer
// composer, not handler-inline prose. Removal of redundant prose-only
// ancillary response fields like `protocolRef` is implicit in the
// migration commit's response-shape change, not pinned by this test.
//
// RED today: the absence assertions fail because today's inline guidance
// strings still embed the dropped plan / amendment cites ("§3.3", "§3.4").
// After Phase 0b.6 (handleExport renders atoms with service context,
// atoms migrated in 0b.2-0b.5), the cites are gone from `guidance` and
// the test goes GREEN.
//
// Loose substring matching (single words / short phrases) is intentional
// — atom-body editorial improvements during Phase 2 must not break the
// migration contract. The contract is "these concepts surface", not
// "these exact strings appear".
func TestHandleExport_ExpectedSubstringsPostMigration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		drive          func(t *testing.T) *mcp.CallToolResult
		wantPresent    []string
		wantAbsent     []string
		todoPhase2Note string
	}{
		{
			name: "scope_prompt",
			drive: func(t *testing.T) *mcp.CallToolResult {
				mock := newExportMock([]platform.ServiceStack{
					runtimeService("appdev", "php-apache@8.4", false),
					managedService("db", "postgresql@16"),
				}, nil)
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, t.TempDir(), "", nil, nil, runtime.Info{InContainer: true})
				return callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "export"})
			},
			wantPresent: []string{
				"Pick the runtime",
				"targetService",
			},
		},
		{
			name: "variant_prompt",
			drive: func(t *testing.T) *mcp.CallToolResult {
				mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)
				dir := t.TempDir()
				writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, nil, runtime.Info{InContainer: true})
				return callTool(t, srv, "zerops_workflow", map[string]any{
					"workflow":      "export",
					"targetService": "appdev",
				})
			},
			wantPresent: []string{
				"pair",
				"dev",
				"stage",
				"NON_HA",
				"destination project",
			},
			wantAbsent: []string{
				"§3.3",  // dropped-pre-migration 2.7 — plan reference
			},
		},
		{
			name: "scaffold_required",
			drive: func(t *testing.T) *mcp.CallToolResult {
				mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)
				dir := t.TempDir()
				writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)
				ssh := &routedSSH{responses: map[string]string{
					"cat /var/www/zerops.yaml": "", // empty body forces scaffold-required
				}}
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})
				return callTool(t, srv, "zerops_workflow", map[string]any{
					"workflow":      "export",
					"targetService": "appdev",
					"variant":       "dev",
				})
			},
			wantPresent: []string{
				"/var/www/zerops.yaml",     // 3.1 (TODO Phase 2 — wording precision around "missing or empty")
				"minimal",                   // 3.2 (TODO Phase 2 — self-ref clarity)
				"re-call",
			},
			todoPhase2Note: "scaffold-required: revisit 3.1 (literal 'is missing' vs 'missing or empty') + 3.2 (drop self-ref 'Run the X atom flow' phrasing) during Phase 2 review pass.",
		},
		{
			name: "git_push_setup_required",
			drive: func(t *testing.T) *mcp.CallToolResult {
				mock := newExportMock([]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)}, nil)
				dir := t.TempDir()
				writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushUnconfigured)
				ssh := &routedSSH{responses: map[string]string{
					"cat /var/www/zerops.yaml": exportTestZeropsYAML,
					"git remote get-url":       "", // empty remote → git-push-setup-required
				}}
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})
				return callTool(t, srv, "zerops_workflow", map[string]any{
					"workflow":      "export",
					"targetService": "appdev",
					"variant":       "dev",
				})
			},
			wantPresent: []string{
				"GitPushState=configured",
				"git-push-setup",
				"re-call",
			},
		},
		{
			name: "classify_prompt",
			drive: func(t *testing.T) *mcp.CallToolResult {
				mock := newExportMock(
					[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
					[]platform.EnvVar{
						{Key: "APP_KEY", Content: "old-key"},
						{Key: "DB_HOST", Content: "${db_hostname}"},
					},
				)
				dir := t.TempDir()
				writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)
				ssh := &routedSSH{responses: map[string]string{
					"cat /var/www/zerops.yaml": exportTestZeropsYAML,
					"git remote get-url":       "https://github.com/example/demo.git",
				}}
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})
				return callTool(t, srv, "zerops_workflow", map[string]any{
					"workflow":      "export",
					"targetService": "appdev",
					"variant":       "dev",
				})
			},
			wantPresent: []string{
				"Classify",
				"infrastructure",
				"auto-secret",
				"external-secret",
				"plain-config",
				"zerops_discover",
				"envClassifications",
			},
			wantAbsent: []string{
				"§3.4", // dropped-pre-migration 5.6 — plan reference (in `guidance` today; ops.BuildBundle warnings are out of scope)
			},
		},
		{
			name: "validation_failed",
			drive: func(t *testing.T) *mcp.CallToolResult {
				const invalidZeropsYAML = `zerops:
  - setup: appdev
    build:
      base: php@8.4
    run:
      base: php-apache@8.4
  - run:
      base: nodejs@22
`
				mock := newExportMock(
					[]platform.ServiceStack{runtimeService("appdev", "php-apache@8.4", false)},
					[]platform.EnvVar{{Key: "LOG_LEVEL", Content: "info"}},
				)
				dir := t.TempDir()
				writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)
				ssh := &routedSSH{responses: map[string]string{
					"cat /var/www/zerops.yaml": invalidZeropsYAML,
					"git remote get-url":       "https://github.com/example/demo.git",
				}}
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})
				return callTool(t, srv, "zerops_workflow", map[string]any{
					"workflow":      "export",
					"targetService": "appdev",
					"variant":       "dev",
					"envClassifications": map[string]any{
						"LOG_LEVEL": "plain-config",
					},
				})
			},
			wantPresent: []string{
				"Schema validation",
				"path",
				"message",
				"re-call",
			},
		},
		{
			name: "publish_ready",
			drive: func(t *testing.T) *mcp.CallToolResult {
				mock := newExportMock(
					[]platform.ServiceStack{
						runtimeService("appdev", "php-apache@8.4", true),
						managedService("db", "postgresql@16"),
					},
					[]platform.EnvVar{
						{Key: "APP_KEY", Content: "old-key"},
						{Key: "DB_HOST", Content: "${db_hostname}"},
						{Key: "LOG_LEVEL", Content: "info"},
					},
				)
				dir := t.TempDir()
				writeBootstrappedMeta(t, dir, topology.ModeStandard, topology.GitPushConfigured)
				ssh := &routedSSH{responses: map[string]string{
					"cat /var/www/zerops.yaml": exportTestZeropsYAML,
					"git remote get-url":       "https://github.com/example/demo.git",
				}}
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
				RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, nil, nil, dir, "", nil, ssh, runtime.Info{InContainer: true})
				return callTool(t, srv, "zerops_workflow", map[string]any{
					"workflow":      "export",
					"targetService": "appdev",
					"variant":       "dev",
					"envClassifications": map[string]any{
						"APP_KEY":   "auto-secret",
						"DB_HOST":   "infrastructure",
						"LOG_LEVEL": "plain-config",
					},
				})
			},
			wantPresent: []string{
				"Bundle composed",
				"git-push",
				"commit",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := tc.drive(t)
			if result.IsError {
				t.Fatalf("status %s: handler errored unexpectedly: %s", tc.name, getTextContent(t, result))
			}
			body := decodeExportJSON(t, result)
			guidance, _ := body["guidance"].(string)
			if guidance == "" {
				t.Fatalf("status %s: response.guidance is empty; want atom-rendered body", tc.name)
			}
			for _, want := range tc.wantPresent {
				if !strings.Contains(guidance, want) {
					t.Errorf("status %s: guidance missing required substring %q\n--- guidance:\n%s", tc.name, want, guidance)
				}
			}
			for _, deny := range tc.wantAbsent {
				if strings.Contains(guidance, deny) {
					t.Errorf("status %s: guidance contains dropped-pre-migration substring %q (must be stripped during atom migration)\n--- guidance:\n%s", tc.name, deny, guidance)
				}
			}
		})
	}
}
