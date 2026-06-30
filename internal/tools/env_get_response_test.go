package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestEnvGet_ResponseStructurallyExcludesAdoptionState pins the
// plan §"Wire-leak fix" invariant: env-get response MUST NOT contain
// any DiscoverResult enrichment fields. Pre-fix env-get returned
// ops.DiscoverResult verbatim → any new enrichment (adoptionState,
// future Warnings, etc.) would leak silently. Post-refactor it
// returns focused EnvGetResponse that has no field for adoptionState
// at all — structural impossibility, not omitempty trick.
//
// This regression test fails compile-time first (if adoptionState is
// somehow re-added to EnvGetServiceInfo) and runtime second (if
// project envs / services array / unmanagedRuntimes / adoptRecovery
// somehow leak through projection).
func TestEnvGet_ResponseStructurallyExcludesAdoptionState(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "db"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "serviceHostname": "db",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)

	// Field-name leak regression checks. None of these may appear in
	// env-get's JSON output — they are DiscoverResult concerns, not
	// EnvGetResponse fields.
	leakFields := []string{
		`"adoptionState"`,
		`"managedByZcp"`, // dropped field; absence is required
		`"isInfrastructure"`,
		`"mountPath"`,
		`"subdomainEnabled"`,
		`"subdomainUrl"`,
		`"containers"`,
		`"resources"`,
		`"ports"`,
		`"refs"`,
		`"activity"`, // discover-only live-activity field — never on env-get
		`"services"`, // discover-style array — should NOT be here
		`"unmanagedRuntimes"`,
		`"adoptRecovery"`,
	}
	for _, f := range leakFields {
		if strings.Contains(text, f) {
			t.Errorf("EnvGetResponse leaked field %s; got: %s", f, text)
		}
	}

	// Sanity: required fields present.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := parsed["service"]; !ok {
		t.Errorf("required field `service` missing; got: %v", parsed)
	}
	if _, ok := parsed["envs"]; !ok {
		t.Errorf("required field `envs` missing; got: %v", parsed)
	}
}

// TestEnvGet_ProjectScope_ReturnsProjectEnvs pins the project=true
// branch: top-level `envs` carries the project env list; `project`
// block carries identity only; `service` is absent.
func TestEnvGet_ProjectScope_ReturnsProjectEnvs(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "APP_KEY", Content: "base64:fakekey"},
			{ID: "pe2", Key: "API_BASE", Content: "https://api.example/"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "project": true,
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}

	// service block MUST be absent on project scope.
	if _, ok := parsed["service"]; ok {
		t.Errorf("service block must be absent on project=true; got: %v", parsed["service"])
	}

	// project identity present, NO envs inside project block (envs
	// canonically live at top-level).
	proj, ok := parsed["project"].(map[string]any)
	if !ok {
		t.Fatalf("project block required on project=true; got: %v", parsed)
	}
	if proj["name"] != "myproject" {
		t.Errorf("project.name: got %v, want myproject", proj["name"])
	}
	if _, ok := proj["envs"]; ok {
		t.Errorf("project.envs must NOT duplicate top-level envs; got: %v", proj["envs"])
	}

	// Top-level envs carries the project env list.
	envs, ok := parsed["envs"].([]any)
	if !ok || len(envs) == 0 {
		t.Fatalf("top-level envs[] must carry project envs on project=true; got: %v", parsed["envs"])
	}
}

// TestEnvGet_PreservesWarnings pins that env-fetch warnings (emitted
// by ops.Discover when per-service env reads fail partially) survive
// the EnvGetResponse projection. Without this preservation, agents
// would lose visibility into "env-get returned partial data because
// fetch failed for X" diagnostics.
//
// Synthesized by seeding the mock service env successfully but
// crafting a Notes string the projection should pass through. Since
// the mock client's success path doesn't emit warnings naturally,
// this test exercises the projection itself by constructing a
// DiscoverResult with warnings and verifying projection preserves
// them — done via the helper directly.
func TestProjectEnvGetResponse_PreservesWarnings(t *testing.T) {
	t.Parallel()
	in := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "db", ServiceID: "svc-1", Type: "postgresql@17", Status: statusActive,
				Envs: []map[string]any{{"key": "hostname", "value": "db"}}},
		},
		Warnings: []string{"env fetch for db failed: timeout"},
	}

	resp := projectEnvGetResponse(in, false)

	if len(resp.Warnings) != 1 || resp.Warnings[0] != "env fetch for db failed: timeout" {
		t.Errorf("Warnings not preserved through projection; got %v", resp.Warnings)
	}
}
