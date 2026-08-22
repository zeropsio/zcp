// Tests for: env.go — zerops_env MCP tool handler.

package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestEnvSchema_GenerateDotenv_PrefersSetupOverHostname pins the
// Phase 0C documentation contract: serviceHostname is deprecated for
// generate-dotenv (description steers callers to setup), and the
// setup property names zerops.yaml run.envVariables as the canonical
// schema location. The earlier "Ignored by generate-dotenv" wording
// pre-dated the run.envVariables resolution path; agents got
// non-actionable schema errors.
func TestEnvSchema_GenerateDotenv_PrefersSetupOverHostname(t *testing.T) {
	t.Parallel()
	schema := envInputSchema()

	hostnameProp, ok := schema.Properties["serviceHostname"]
	if !ok || hostnameProp == nil {
		t.Fatalf("serviceHostname property missing from envInputSchema")
	}
	if strings.Contains(hostnameProp.Description, "Ignored by generate-dotenv") {
		t.Errorf("description still claims generate-dotenv ignores serviceHostname; got: %s", hostnameProp.Description)
	}
	if !strings.Contains(hostnameProp.Description, "deprecated") {
		t.Errorf("serviceHostname description should mark deprecated for generate-dotenv; got: %s", hostnameProp.Description)
	}
	if !strings.Contains(hostnameProp.Description, "setup") {
		t.Errorf("serviceHostname description should steer callers to setup parameter; got: %s", hostnameProp.Description)
	}

	setupProp, ok := schema.Properties["setup"]
	if !ok || setupProp == nil {
		t.Fatalf("setup property missing from envInputSchema")
	}
	if !strings.Contains(setupProp.Description, "zerops.yaml") {
		t.Errorf("setup description should name zerops.yaml; got: %s", setupProp.Description)
	}
	if !strings.Contains(setupProp.Description, "auto-pick") {
		t.Errorf("setup description should explain single-block auto-pick behavior; got: %s", setupProp.Description)
	}
}

// TestEnvTool_GetAction_Success is the new happy path for `get` — the
// action is now first-class, delegating to the same discover path that
// zerops_discover uses. Before the v7 post-mortem fix, agents tried
// `get` five times in a row because it's the natural action name for
// "read env vars"; exposing it here eliminates that whole failure mode.
func TestEnvTool_GetAction_Success(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17"}},
		}).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "db"},
			{Key: "port", Content: "5432"},
			{Key: "user", Content: "dbuser"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "serviceHostname": "db",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	// Post-refactor: env-get returns focused EnvGetResponse shape, not
	// full DiscoverResult. Service identity at top-level `service`,
	// envs at top-level `envs`.
	svc, ok := parsed["service"].(map[string]any)
	if !ok {
		t.Fatalf("expected service object in EnvGetResponse, got: %v", parsed)
	}
	if svc["hostname"] != "db" {
		t.Errorf("service.hostname: got %v, want db", svc["hostname"])
	}
	envs, ok := parsed["envs"].([]any)
	if !ok {
		t.Fatalf("expected envs[] at top-level, got: %v", parsed)
	}
	if len(envs) == 0 {
		t.Error("get returned zero env vars — expected hostname/port/user")
	}
}

// TestEnvGet_ServiceScoped_NoProjectEnvLeak pins the caller-safety
// regression that drove the includeProjectEnvs option on ops.Discover.
// zerops_env action="get" serviceHostname=X delegates to Discover scoped
// to that service and asks for env VALUES (includeEnvValues=true). If
// the scoped path implicitly attached project envs, get would silently
// broaden to return raw project-level secret values — a contract +
// safety expansion. Phase 1 patches ops.Discover to require an explicit
// includeProjectEnvs opt-in, and env.go keeps it false. See plan
// Risk R1 in plans/archive/env-discover-three-changes-2026-05-20.md.
func TestEnvGet_ServiceScoped_NoProjectEnvLeak(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17"}},
		}).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "db"},
			{Key: "port", Content: "5432"},
		}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "PROJECT_SECRET", Content: "must-not-leak-via-service-get"},
			{ID: "pe2", Key: "SESSION_SECRET", Content: "must-not-leak-either"},
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
	if strings.Contains(text, "PROJECT_SECRET") || strings.Contains(text, "SESSION_SECRET") || strings.Contains(text, "must-not-leak") {
		t.Fatalf("scoped env get leaked project envs: %s", text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	// Project block must be absent on service-scoped get (EnvGetResponse
	// uses Project only when project=true).
	if _, ok := parsed["project"]; ok {
		t.Fatalf("project must be absent on scoped get, got: %v", parsed["project"])
	}
	// Sanity: the service envs we asked for must still be present.
	svc, ok := parsed["service"].(map[string]any)
	if !ok {
		t.Fatalf("expected service object in EnvGetResponse, got: %v", parsed)
	}
	if svc["hostname"] != "db" {
		t.Errorf("service.hostname: got %v, want db", svc["hostname"])
	}
	envs, _ := parsed["envs"].([]any)
	if len(envs) == 0 {
		t.Fatalf("expected service envs at top-level envs[], got none")
	}
}

// TestEnvTool_GetAction_RequiresTarget verifies the handler's own
// guard: get without serviceHostname AND without project=true is a
// user error that must come back with an actionable suggestion. This
// is the one get-action failure that cannot be caught at the schema
// layer (both fields are optional — one or the other has to be set).
func TestEnvTool_GetAction_RequiresTarget(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{"action": "get"})

	if !result.IsError {
		t.Fatal("expected IsError when get has neither serviceHostname nor project=true")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "serviceHostname") {
		t.Errorf("error should name the missing parameter, got: %s", text)
	}
	if !strings.Contains(text, "zerops_discover") {
		t.Errorf("error should point at zerops_discover for bulk reads, got: %s", text)
	}
}

// TestEnvTool_GetAction_StringifiedBoolProject verifies the same
// FlexBool acceptance we tested for discover: an agent sending
// `project: "true"` (stringified) must succeed, not bounce off the
// schema. This is the direct regression for LOG.txt line 65 where a
// stringified `project` argument failed with a non-actionable MCP
// schema error.
func TestEnvTool_GetAction_StringifiedBoolProject(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", Status: statusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action":  "get",
		"project": "true",
	})

	if result.IsError {
		t.Fatalf("unexpected IsError with stringified project=\"true\": %s", getTextContent(t, result))
	}
}

func TestEnvTool_Set_PollsToFinished(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-envset-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action":          "set",
		"serviceHostname": "api",
		"variables":       []any{"PORT=8080"},
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	proc, ok := parsed["process"].(map[string]any)
	if !ok {
		t.Fatalf("expected process in result, got: %v", parsed)
	}
	if proc["status"] != statusFinished {
		t.Errorf("process status = %v, want FINISHED", proc["status"])
	}
}

func TestEnvTool_Delete_PollsToFinished(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{{ID: "env-1", Key: "OLD_VAR", Content: "old"}}).
		WithProcess(&platform.Process{
			ID:     "proc-envdel-env-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action":          "delete",
		"serviceHostname": "api",
		"variables":       []any{"OLD_VAR"},
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	proc, ok := parsed["process"].(map[string]any)
	if !ok {
		t.Fatalf("expected process in result, got: %v", parsed)
	}
	if proc["status"] != statusFinished {
		t.Errorf("process status = %v, want FINISHED", proc["status"])
	}
}

// TestEnvTool_EmptyAction: the schema enum rejects an empty action
// at the protocol layer before it reaches the handler. This is the
// preferred form of early-exit for a required field — the agent
// sees a crisp "enum: does not equal any of [get set delete
// generate-dotenv]" error with the valid set included.
func TestEnvTool_EmptyAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	err := callToolMayError(t, srv, "zerops_env", map[string]any{
		"action": "", "serviceHostname": "api",
	})
	if err == nil {
		t.Fatal("expected schema rejection for empty action")
	}
	if !strings.Contains(err.Error(), "action") {
		t.Errorf("error should reference the action field, got: %v", err)
	}
}

// TestEnvTool_InvalidAction: same mechanism as EmptyAction — the
// enum-based schema rejects unknown actions and includes the full
// valid-action list in the error. `wipe` is the standin for any
// bogus action an agent might try (e.g. the old "get" attempt from
// LOG.txt is now a valid action, so we cannot use it here).
func TestEnvTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	err := callToolMayError(t, srv, "zerops_env", map[string]any{
		"action": "wipe", "serviceHostname": "api",
	})
	if err == nil {
		t.Fatal("expected schema rejection for invalid action 'wipe'")
	}
	msg := err.Error()
	if !strings.Contains(msg, "wipe") {
		t.Errorf("error should echo the bad value, got: %v", err)
	}
	// The enum list must be in the error so the agent can recover
	// without a second lookup.
	for _, wanted := range []string{"get", "set", "delete", "generate-dotenv"} {
		if !strings.Contains(msg, wanted) {
			t.Errorf("error should list valid action %q, got: %v", wanted, err)
		}
	}
}

// TestEnvTool_GenerateDotenv_LegacyHostname_AddsDeprecationWarning pins
// the Phase 0C deprecation path: when an MCP caller passes the legacy
// serviceHostname for generate-dotenv, the result MUST carry a
// deprecation warning steering the caller to the setup parameter.
// Recipe / multi-setup yaml uses setup names that aren't always
// service hostnames; the warning is the migration signal.
func TestEnvTool_GenerateDotenv_LegacyHostname_AddsDeprecationWarning(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: legacy-via-hostname
`
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: statusActive},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "p1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action":          "generate-dotenv",
		"serviceHostname": "app",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	warnings, _ := parsed["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatalf("expected deprecation warning in result.warnings, got: %v", parsed)
	}
	first, _ := warnings[0].(string)
	for _, want := range []string{"deprecated", "setup"} {
		if !strings.Contains(first, want) {
			t.Errorf("warning should mention %q; got: %s", want, first)
		}
	}
}

// TestEnvTool_GenerateDotenv_SetupParam_NoWarning pins the new happy
// path: when caller passes setup explicitly, no deprecation warning
// is emitted (the call site is correct). Companion to the legacy
// test above.
func TestEnvTool_GenerateDotenv_SetupParam_NoWarning(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: setup-param
`
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: statusActive},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "p1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "generate-dotenv",
		"setup":  "app",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if warnings, ok := parsed["warnings"].([]any); ok && len(warnings) > 0 {
		t.Errorf("setup parameter should not trigger deprecation warning, got: %v", warnings)
	}
	if got, _ := parsed["setup"].(string); got != "app" {
		t.Errorf("result.setup = %v, want %q", parsed["setup"], "app")
	}
}

// shadowSetMock builds a project with one live runtime service "api" whose
// yaml-baked run.envVariables + slim userData are seeded, plus the set/restart
// processes so a project-scope set polls cleanly. baked is appended to api's
// app-version userData (the yaml-baked layer); slim is its service userData.
func shadowSetMock(baked, slim []platform.ServiceEnvVar) *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-api", Name: "api", Status: statusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"}},
		}).
		WithServiceEnv("svc-api", slim).
		WithAppVersionUserData("av-api", baked).
		WithProcess(&platform.Process{ID: "proc-projenvset", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-envset-svc-api", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-api", Status: statusFinished})
}

// TestEnvSet_ProjectScope_ShadowedByYaml_WarnsNotLive — a project-scope set
// of a key a live runtime service bakes in zerops.yaml run.envVariables is
// SILENTLY shadowed: the project value is stored but the container reads the
// yaml value (spec §2). The handler must surface shadowWarnings and must NOT
// claim the value is "live".
func TestEnvSet_ProjectScope_ShadowedByYaml_WarnsNotLive(t *testing.T) {
	t.Parallel()
	mock := shadowSetMock(
		[]platform.ServiceEnvVar{{Key: "LOG_LEVEL", Content: "info"}}, // yaml-baked
		[]platform.ServiceEnvVar{{Key: "PORT", Content: "3000"}},      // slim
	)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "set", "project": true, "variables": []any{"LOG_LEVEL=debug"},
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	warns, _ := parsed["shadowWarnings"].([]any)
	if len(warns) == 0 {
		t.Fatalf("expected shadowWarnings for project LOG_LEVEL shadowed by api yaml-baked, got: %v", parsed)
	}
	w, _ := warns[0].(string)
	for _, want := range []string{"LOG_LEVEL", "api", "zerops.yaml"} {
		if !strings.Contains(w, want) {
			t.Errorf("shadowWarning should mention %q; got: %s", want, w)
		}
	}
	if next, _ := parsed["nextActions"].(string); strings.Contains(next, "are live") {
		t.Errorf("nextActions must not claim values are live when shadowed; got: %s", next)
	}
}

// TestEnvSet_ProjectScope_NoShadow_Live — a project-scope set of a key NO
// service bakes is genuinely live after restart; no shadowWarnings, and the
// success text states the values are live.
func TestEnvSet_ProjectScope_NoShadow_Live(t *testing.T) {
	t.Parallel()
	mock := shadowSetMock(
		[]platform.ServiceEnvVar{{Key: "LOG_LEVEL", Content: "info"}},
		[]platform.ServiceEnvVar{{Key: "PORT", Content: "3000"}},
	)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "set", "project": true, "variables": []any{"NEW_VAR=x"},
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if warns, _ := parsed["shadowWarnings"].([]any); len(warns) != 0 {
		t.Errorf("no shadow expected for unbaked NEW_VAR, got: %v", warns)
	}
	if next, _ := parsed["nextActions"].(string); !strings.Contains(next, "are live") {
		t.Errorf("nextActions should state values are live when not shadowed; got: %s", next)
	}
}

// TestEnvSet_ServiceScope_NoShadowDetection — service-scope set is never
// silently shadowed: a yaml-owned key 400s (handled in ops.EnvSet) and
// service userData outranks project, so no shadow scan runs even when the
// same key exists at project scope.
func TestEnvSet_ServiceScope_NoShadowDetection(t *testing.T) {
	t.Parallel()
	mock := shadowSetMock(
		[]platform.ServiceEnvVar{{Key: "LOG_LEVEL", Content: "info"}},
		[]platform.ServiceEnvVar{{Key: "PORT", Content: "3000"}},
	).WithProjectEnv([]platform.ProjectEnvVar{{ID: "pe1", Key: "FOO", Content: "fromproject"}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "set", "serviceHostname": "api", "variables": []any{"FOO=fromservice"},
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if warns, _ := parsed["shadowWarnings"].([]any); len(warns) != 0 {
		t.Errorf("service-scope set must not run cross-layer shadow detection, got: %v", warns)
	}
}

// TestEnvSet_ProjectScope_ShadowRedaction_IsKeyBased pins the layered-shadow
// message redaction under the key-based masking model (the plan's "never
// Sensitive"): the winning (shadowing) value is redacted iff its KEY is a
// ZCP-owned credential — routed through the single owner RedactCredentialValue
// — NOT when the platform Sensitive flag is set. A generic-keyed value, even
// one the platform classified SECRET, is now SHOWN: the Sensitive flag is not
// authoritative (it does not persist), so only key ownership drives masking.
func TestEnvSet_ProjectScope_ShadowRedaction_IsKeyBased(t *testing.T) {
	t.Parallel()
	mock := shadowSetMock(
		// Type:"USER" — the 2026-08 app-version model classifies every
		// yaml-baked record Sensitive=false regardless of Type (no SECRET
		// value to derive from any more). GIT_TOKEN is a ZCP-owned
		// credential key → its winning value masks; API_SECRET is generic
		// → its winning value is shown despite looking secret-ish — proof
		// the masking never depended on the (now-gone) Sensitive derivation.
		[]platform.ServiceEnvVar{
			{Key: ops.GitTokenEnvKey, Content: "ghp_BAKED_SECRET", Type: platform.ServiceEnvUser},
			{Key: "API_SECRET", Content: "topsecret-baked", Type: platform.ServiceEnvUser},
		},
		nil,
	)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "set", "project": true,
		"variables": []any{"GIT_TOKEN=mytoken", "API_SECRET=myval"},
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	// ZCP-owned credential key → winning value redacted, never echoed.
	if strings.Contains(text, "ghp_BAKED_SECRET") {
		t.Fatalf("shadowWarning leaked the GIT_TOKEN winning value: %s", text)
	}
	// JSON marshaling escapes '<' → <, so match the unescaped marker body.
	if !strings.Contains(text, "redacted: ZCP-managed credential") {
		t.Fatalf("expected the GIT_TOKEN winning value to be redacted: %s", text)
	}
	// Generic key (even SECRET-classified) → winning value shown, because the
	// Sensitive flag no longer drives redaction.
	if !strings.Contains(text, "topsecret-baked") {
		t.Fatalf("expected the generic API_SECRET winning value to be shown: %s", text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	warns, _ := parsed["shadowWarnings"].([]any)
	if len(warns) < 2 {
		t.Fatalf("expected shadowWarnings for both shadowed keys, got: %v", parsed)
	}
}

// TestEnvSet_ProjectScope_ShadowScanUnavailable_DoesNotClaimLive is the RC2
// centerpiece for env-set (E4): when a service's higher-layer read fails
// transiently, the shadow scan can't confirm the set isn't silently shadowed —
// so the success text must NOT claim "env values are live"; it reports the
// service as unverified with a VPN-retry hint.
func TestEnvSet_ProjectScope_ShadowScanUnavailable_DoesNotClaimLive(t *testing.T) {
	t.Parallel()
	mock := shadowSetMock(
		[]platform.ServiceEnvVar{{Key: "FOO", Content: "bar"}},
		nil,
	).WithError("GetServiceEnv", errors.New("vpn down"))
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "set", "project": true, "variables": []any{"FOO=bar"},
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if strings.Contains(text, "env values are live") {
		t.Errorf("must NOT claim 'env values are live' when a shadow scan failed transiently; got: %s", text)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if unver, _ := parsed["shadowUnverified"].([]any); len(unver) == 0 {
		t.Errorf("expected shadowUnverified to list the unread service; got: %v", parsed)
	}
}

// TestEnvGet_LiveRuntime_ShowsYamlBaked_NoProjectLeak — Phase 3 + the Codex
// seam: service-scoped env get surfaces a live runtime's yaml-baked
// run.envVariables (source="zerops.yaml", from the app-version userDataList)
// yet must STILL NOT leak project env values.
func TestEnvGet_LiveRuntime_ShowsYamlBaked_NoProjectLeak(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-api", Name: "api", Status: statusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"}},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{Key: "PORT", Content: "3000"}}).
		WithAppVersionUserData("av-api", []platform.ServiceEnvVar{{Key: "APP_NAME", Content: "fromyaml"}}).
		WithProjectEnv([]platform.ProjectEnvVar{{ID: "pe1", Key: "PROJECT_SECRET", Content: "must-not-leak"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", map[string]any{
		"action": "get", "serviceHostname": "api",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if strings.Contains(text, "PROJECT_SECRET") || strings.Contains(text, "must-not-leak") {
		t.Fatalf("service-scoped env get leaked project envs: %s", text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	envs, _ := parsed["envs"].([]any)
	var appName map[string]any
	for _, e := range envs {
		if m, ok := e.(map[string]any); ok && m["key"] == "APP_NAME" {
			appName = m
		}
	}
	if appName == nil {
		t.Fatalf("yaml-baked APP_NAME missing from env get: %v", parsed)
	}
	if appName["source"] != "zerops.yaml" {
		t.Errorf("APP_NAME source = %v, want zerops.yaml", appName["source"])
	}
}
