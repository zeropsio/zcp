// Tests for: env.go, env_request.go — the Mate's own vault: mate=true names
// the service zcp runs in, its ask is the service ask, nothing restarts for
// it, and its answers say how the value is used (docs/spec-mate.md §5.8a,
// MF-19).

package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

// fenContainer is the hostname of the service Fen's zcp runs in.
const fenContainer = "zcp"

// fenMock is the project of a Mate called Fen: its container zcp, whose own
// variables hold a key for Fen's tools beside the names Mate and zcp keep for
// themselves and the container's own machinery, and the app api, running,
// whose deployed entry names the container's TEST_ACCOUNTS and which holds a
// GIT_TOKEN and a VSCODE_PASSWORD of its own.
func fenMock() *platform.Mock {
	runtime := func(id, name string) platform.ServiceStack {
		return platform.ServiceStack{ID: id, Name: name, ProjectID: "proj-1", Status: statusActive,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
			ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-" + name}}
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "Shop - Fen", Status: statusActive}).
		WithServices([]platform.ServiceStack{runtime("svc-api", "api"), runtime("svc-zcp", fenContainer)}).
		WithServiceEnv("svc-zcp", []platform.ServiceEnvVar{
			{ID: "u-zcp-key", Key: "ZCP_API_KEY", Content: "k", Type: platform.ServiceEnvUser},
			{ID: "u-mate-setup", Key: "MATE_SETUP", Content: "1", Type: platform.ServiceEnvUser},
			{ID: "u-gitea", Key: "GITEA_TOKEN", Content: "g", Type: platform.ServiceEnvUser},
			{ID: "u-git", Key: "GIT_TOKEN", Content: "t", Type: platform.ServiceEnvUser},
			{ID: "u-t3", Key: "T3CODE_ZEROPS_HQ_ORIGIN", Content: "https://hq.example.test", Type: platform.ServiceEnvUser},
			{ID: "u-vscode", Key: "VSCODE_PASSWORD", Content: "v", Type: platform.ServiceEnvUser},
			{ID: "u-lease", Key: "appdev_LEASE_TOKEN", Content: "${appdev_API_TOKEN}", Type: platform.ServiceEnvUser},
			{ID: "u-tool", Key: "TOOL_KEY", Content: "REDACTED", Type: platform.ServiceEnvUser, Sensitive: true},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{
			{ID: "u-api-git", Key: "GIT_TOKEN", Content: "t", Type: platform.ServiceEnvUser},
			{ID: "u-api-vscode", Key: "VSCODE_PASSWORD", Content: "v", Type: platform.ServiceEnvUser},
		}).
		WithAppVersionUserData("av-api", []platform.ServiceEnvVar{{Key: "ACCOUNTS", Content: "${zcp_TEST_ACCOUNTS}"}}).
		WithProcess(&platform.Process{ID: "proc-envset-svc-zcp", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-envdel-u-tool", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-envdel-u-vscode", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-api", Status: statusFinished})
}

// fenServer is zerops_env as Fen's zcp registers it: asks offered, the
// container's hostname its own.
func fenServer(t *testing.T, mock *platform.Mock) *mcp.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", fenContainer, WithVaultAsks(t.TempDir()))
	return srv
}

// ownVaultWire is an answer of zerops_env as the agent and Mate read it.
type ownVaultWire struct {
	Service *struct {
		Hostname string `json:"hostname"`
	} `json:"service"`
	Envs []struct {
		Key string `json:"key"`
	} `json:"envs"`
	Scope           string `json:"scope"`
	ServiceHostname string `json:"serviceHostname"`
	Requested       *struct {
		Key             string `json:"key"`
		Scope           string `json:"scope"`
		ServiceHostname string `json:"serviceHostname"`
	} `json:"requested"`
	AlreadySet        map[string]any  `json:"alreadySet"`
	AlreadyAsked      string          `json:"alreadyAsked"`
	Readers           json.RawMessage `json:"readers"`
	RestartAfterSave  []string        `json:"restartAfterSave"`
	RestartedServices []string        `json:"restartedServices"`
	NextActions       string          `json:"nextActions"`
}

func parseOwnVault(t *testing.T, result *mcp.CallToolResult) ownVaultWire {
	t.Helper()
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("the call failed: %s", text)
	}
	var got ownVaultWire
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return got
}

// containerKeys are the keys Fen's container holds now.
func containerKeys(t *testing.T, mock *platform.Mock) []string {
	t.Helper()
	envs, err := mock.GetServiceEnv(context.Background(), "svc-zcp")
	if err != nil {
		t.Fatalf("read the container's variables: %v", err)
	}
	keys := make([]string, 0, len(envs))
	for _, env := range envs {
		keys = append(keys, env.Key)
	}
	return keys
}

// TestEnvTool_MateTrue_NamesTheServiceZcpRunsIn: mate=true, with no hostname
// given, reads, writes, removes and asks in the variables of the service zcp
// runs in — the agent never has to know its container's hostname.
// witness: internal/tools/env.go | input.ServiceHostname = selfHostname →
func TestEnvTool_MateTrue_NamesTheServiceZcpRunsIn(t *testing.T) {
	t.Parallel()

	mock := fenMock()
	srv := fenServer(t, mock)

	got := parseOwnVault(t, callTool(t, srv, "zerops_env", map[string]any{"action": "get", "mate": true}))
	if got.Service == nil || got.Service.Hostname != fenContainer {
		t.Errorf("get mate=true read %+v, want the service zcp runs in", got.Service)
	}

	got = parseOwnVault(t, callTool(t, srv, "zerops_env", map[string]any{"action": "set", "mate": true, "variables": []any{"TEST_ACCOUNTS=a:b"}}))
	if got.Scope != "service" || got.ServiceHostname != fenContainer {
		t.Errorf("set mate=true answered scope %q, serviceHostname %q; want service, %s", got.Scope, got.ServiceHostname, fenContainer)
	}
	if held := containerKeys(t, mock); !slices.Contains(held, "TEST_ACCOUNTS") {
		t.Errorf("set mate=true did not write the container's variables: %v", held)
	}

	parseOwnVault(t, callTool(t, srv, "zerops_env", map[string]any{"action": "delete", "mate": true, "variables": []any{"TOOL_KEY"}}))
	if held := containerKeys(t, mock); slices.Contains(held, "TOOL_KEY") {
		t.Errorf("delete mate=true left the key in the container's variables: %v", held)
	}

	got = parseOwnVault(t, callTool(t, srv, "zerops_env", map[string]any{"action": "request", "mate": true, "key": "SIGNING_KEY", "why": "to sign the test build"}))
	if got.Requested == nil || got.Requested.Scope != "service" || got.Requested.ServiceHostname != fenContainer {
		t.Errorf("request mate=true asked %+v, want a service ask on %s", got.Requested, fenContainer)
	}
}

// TestEnvTool_MateTrue_RefusedWithAnotherTargetOrOutsideAMate: mate=true
// beside project=true or another service's hostname names two vaults and is
// refused, never resolved to one of them; outside a Mate there is no such
// vault, so it is neither offered nor taken. A call that names the
// container's hostname by hand is left as it came, a hidden name included:
// it reaches that service's variables as it always did.
// witness: internal/tools/env.go | case input.Project.Bool(): → case false:
func TestEnvTool_MateTrue_RefusedWithAnotherTargetOrOutsideAMate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    EnvInput
		inMate   bool
		refused  bool
		wantHost string
	}{
		{name: "beside project=true", input: EnvInput{Mate: true, Project: true}, inMate: true, refused: true},
		{name: "beside another service", input: EnvInput{Mate: true, ServiceHostname: "api"}, inMate: true, refused: true},
		{name: "outside a Mate", input: EnvInput{Mate: true}, refused: true},
		{name: "alone", input: EnvInput{Mate: true}, inMate: true, wantHost: fenContainer},
		{name: "beside the container's own hostname", input: EnvInput{Mate: true, ServiceHostname: fenContainer}, inMate: true, wantHost: fenContainer},
		{name: "the container's hostname by hand, a hidden name",
			input: EnvInput{ServiceHostname: fenContainer, Variables: []string{"T3CODE_ZEROPS_HQ_ORIGIN=x"}}, inMate: true, wantHost: fenContainer},
		{name: "another service", input: EnvInput{ServiceHostname: "api"}, inMate: true, wantHost: "api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveEnvTarget(tt.input, fenContainer, tt.inMate)
			if (err != nil) != tt.refused {
				t.Fatalf("refused = %v (%v), want %v", err != nil, err, tt.refused)
			}
			if !tt.refused && got.ServiceHostname != tt.wantHost {
				t.Errorf("target = %q, want %q", got.ServiceHostname, tt.wantHost)
			}
		})
	}

	if _, offered := envInputSchema(false, false).Properties["mate"]; offered {
		t.Error("mate is offered outside a Mate")
	}
	if _, offered := envInputSchema(false, true).Properties["mate"]; !offered {
		t.Error("mate is not offered in a Mate")
	}
	result := callTool(t, fenServer(t, fenMock()), "zerops_env", map[string]any{"action": "get", "mate": true, "project": true})
	if text := getTextContent(t, result); !result.IsError || !strings.Contains(text, platform.ErrInvalidUsage) {
		t.Errorf("get mate=true project=true was not refused as invalid usage: %s", text)
	}
}

// TestEnvTool_Request_OwnVault_IsTheServiceAskAndSaysHowToUseTheValue: an
// ask for the Mate's own vault is the service ask on the container's
// hostname, kept as any open ask, and names no service to restart — not even
// one whose entry names the key — while its next step says how the value is
// used, never to reference it in run.envVariables, which would hand it to an
// app.
// witness: internal/tools/env_request.go | case plan.ownVault: → case false:
func TestEnvTool_Request_OwnVault_IsTheServiceAskAndSaysHowToUseTheValue(t *testing.T) {
	t.Parallel()

	srv := fenServer(t, fenMock())
	ask := map[string]any{"action": "request", "mate": true, "key": "TEST_ACCOUNTS",
		"label": "Test account", "why": "to check the page behind the sign-in, an account made for testing"}

	got := parseOwnVault(t, callTool(t, srv, "zerops_env", ask))
	if got.Requested == nil || got.Requested.Key != "TEST_ACCOUNTS" || got.Requested.Scope != "service" || got.Requested.ServiceHostname != fenContainer {
		t.Fatalf("requested = %+v, want the service ask for TEST_ACCOUNTS on %s", got.Requested, fenContainer)
	}
	if len(got.Readers) != 0 || len(got.RestartAfterSave) != 0 {
		t.Errorf("the ask names readers %s and restarts %v; want none", got.Readers, got.RestartAfterSave)
	}
	if !strings.Contains(got.NextActions, "`zcp with TEST_ACCOUNTS -- <command>`") {
		t.Errorf("the next step does not say how the value is used: %s", got.NextActions)
	}
	for _, wrong := range []string{"run.envVariables", `zerops_manage action="restart"`} {
		if strings.Contains(got.NextActions, wrong) {
			t.Errorf("the next step names %q: %s", wrong, got.NextActions)
		}
	}

	again := parseOwnVault(t, callTool(t, srv, "zerops_env", ask))
	if again.AlreadyAsked == "" {
		t.Errorf("a repeat while the ask is open was asked anew: %+v", again)
	}

	held := parseOwnVault(t, callTool(t, srv, "zerops_env", map[string]any{"action": "request", "mate": true, "key": "TOOL_KEY", "why": "for the linter"}))
	if held.AlreadySet == nil || !strings.Contains(held.NextActions, "`zcp with TOOL_KEY -- <command>`") || strings.Contains(held.NextActions, "run.envVariables") {
		t.Errorf("a held key's answer does not say how it is used: %+v", held)
	}
}

// TestEnvTool_Get_OwnVault_LeavesTheHiddenNamesOut: the Mate's own vault
// lists what the person sees in it — never a hidden name — while another
// service's own GIT_TOKEN is listed as before.
// witness: internal/tools/env.go | got.Envs = withoutHiddenNames(got.Envs) →
func TestEnvTool_Get_OwnVault_LeavesTheHiddenNamesOut(t *testing.T) {
	t.Parallel()

	srv := fenServer(t, fenMock())
	keysOf := func(args map[string]any) []string {
		got := parseOwnVault(t, callTool(t, srv, "zerops_env", args))
		keys := make([]string, 0, len(got.Envs))
		for _, env := range got.Envs {
			keys = append(keys, env.Key)
		}
		return keys
	}

	if own := keysOf(map[string]any{"action": "get", "mate": true}); !slices.Equal(own, []string{"TOOL_KEY"}) {
		t.Errorf("the Mate's own vault lists %v, want TOOL_KEY alone", own)
	}
	if api := keysOf(map[string]any{"action": "get", "serviceHostname": "api"}); !slices.Contains(api, "GIT_TOKEN") {
		t.Errorf("api's own vault lists %v, want its GIT_TOKEN", api)
	}
}

// TestEnvChange_OwnVault_RestartsNothing: a key of the Mate's own vault
// restarts no service when set or deleted — not a running app whose deployed
// entry names it — and the next step says how the value is used, never that
// nothing reads it yet.
// witness: internal/tools/env.go | ownVault: input.Mate.Bool()} → ownVault: false}
func TestEnvChange_OwnVault_RestartsNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     map[string]any
		wantNext string
	}{
		{name: "a set by mate=true",
			args:     map[string]any{"action": "set", "mate": true, "variables": []any{"TEST_ACCOUNTS=a:b"}},
			wantNext: "`zcp with TEST_ACCOUNTS -- <command>`"},
		{name: "a set of two keys",
			args:     map[string]any{"action": "set", "mate": true, "variables": []any{"TEST_ACCOUNTS=a:b", "OTHER=c"}},
			wantNext: "`zcp with TEST_ACCOUNTS,OTHER -- <command>`"},
		{name: "a set told to skip the restart",
			args:     map[string]any{"action": "set", "mate": true, "skipRestart": true, "variables": []any{"TEST_ACCOUNTS=a:b"}},
			wantNext: "`zcp with TEST_ACCOUNTS -- <command>`"},
		{name: "a delete",
			args:     map[string]any{"action": "delete", "mate": true, "variables": []any{"TOOL_KEY"}},
			wantNext: "removed from your own vault"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseOwnVault(t, callTool(t, fenServer(t, fenMock()), "zerops_env", tt.args))
			if len(got.RestartedServices) != 0 {
				t.Errorf("restarted %v for a key of the Mate's own vault", got.RestartedServices)
			}
			if !strings.Contains(got.NextActions, tt.wantNext) {
				t.Errorf("the next step lacks %q: %s", tt.wantNext, got.NextActions)
			}
			for _, wrong := range []string{"run.envVariables", "No zerops.yaml entry", "skipRestart"} {
				if strings.Contains(got.NextActions, wrong) {
					t.Errorf("the next step names %q: %s", wrong, got.NextActions)
				}
			}
		})
	}
}

// TestEnvChange_OwnVault_RefusesAHiddenName: a name Mate and zcp keep for
// themselves is neither written, removed nor asked for in the Mate's own
// vault — a stored one the person could never see, a removed one is a key
// the Mate runs on — and the refusal carries no value.
// witness: internal/tools/env.go | if ops.IsOwnVaultHidden(key) { → if false {
func TestEnvChange_OwnVault_RefusesAHiddenName(t *testing.T) {
	t.Parallel()

	const value = "s3cr3t-of-the-test"
	tests := []struct {
		name string
		args map[string]any
	}{
		{name: "a set", args: map[string]any{"action": "set", "mate": true, "variables": []any{"MATE_PROBE=" + value}}},
		{name: "a set beside a listed name", args: map[string]any{"action": "set", "mate": true, "variables": []any{"TEST_ACCOUNTS=a:b", "GIT_TOKEN=" + value}}},
		{name: "a delete", args: map[string]any{"action": "delete", "mate": true, "variables": []any{"ZCP_API_KEY"}}},
		{name: "a request", args: map[string]any{"action": "request", "mate": true, "key": "GITEA_TOKEN", "why": "for the mirror"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := fenMock()
			before := containerKeys(t, mock)
			result := callTool(t, fenServer(t, mock), "zerops_env", tt.args)
			text := getTextContent(t, result)
			if !result.IsError {
				t.Errorf("a hidden name was taken: %s", text)
			}
			if strings.Contains(text, value) {
				t.Errorf("the refusal carries the value: %s", text)
			}
			if after := containerKeys(t, mock); !slices.Equal(before, after) {
				t.Errorf("the container's variables changed from %v to %v", before, after)
			}
		})
	}
}

// The names of the container's own machinery — the Mate server's launch
// environment, the editor's password, a pool's bearer reference.
var fenMachinery = []string{"T3CODE_ZEROPS_HQ_ORIGIN", "VSCODE_PASSWORD", "appdev_LEASE_TOKEN"}

// TestEnvTool_OwnVault_HidesTheContainersOwnMachinery: what Mate and zcp
// wrote on the container for themselves under names the first rule does not
// cover is no part of the Mate's own vault either — not listed, and not
// written, removed or asked for through mate=true — or the agent would list
// the Mate server's launch environment as its own values and could remove
// what the Mate runs on.
// witness: internal/ops/env_request.go | return IsVaultHidden(key) || isContainerMachinery(key) → return IsVaultHidden(key)
func TestEnvTool_OwnVault_HidesTheContainersOwnMachinery(t *testing.T) {
	t.Parallel()

	got := parseOwnVault(t, callTool(t, fenServer(t, fenMock()), "zerops_env", map[string]any{"action": "get", "mate": true}))
	for _, env := range got.Envs {
		if slices.Contains(fenMachinery, env.Key) {
			t.Errorf("the Mate's own vault lists %s", env.Key)
		}
	}
	for _, key := range fenMachinery {
		for _, args := range []map[string]any{
			{"action": "set", "mate": true, "variables": []any{key + "=x"}},
			{"action": "delete", "mate": true, "variables": []any{key}},
			{"action": "request", "mate": true, "key": key, "why": "for the test"},
		} {
			mock := fenMock()
			before := containerKeys(t, mock)
			result := callTool(t, fenServer(t, mock), "zerops_env", args)
			if !result.IsError {
				t.Errorf("%s of %s was taken: %s", args["action"], key, getTextContent(t, result))
			}
			if after := containerKeys(t, mock); !slices.Equal(before, after) {
				t.Errorf("%s of %s changed the container's variables from %v to %v", args["action"], key, before, after)
			}
		}
	}
}

// TestEnvTool_TheContainersMachineryNames_StayOrdinaryInSharedAndAnAppsVault:
// the second rule is the Mate's own vault's alone. An app may have a
// VSCODE_PASSWORD or a *_LEASE_TOKEN of its own, and the Shared vault a
// T3CODE_ value: they are listed and asked for like any name, and the
// container's own are still reached by a call that names its hostname.
// witness: internal/ops/env_request.go | return slices.Contains(vaultHiddenNames, key) → return slices.Contains(vaultHiddenNames, key) || isContainerMachinery(key)
func TestEnvTool_TheContainersMachineryNames_StayOrdinaryInSharedAndAnAppsVault(t *testing.T) {
	t.Parallel()

	srv := fenServer(t, fenMock())
	keysOf := func(hostname string) []string {
		got := parseOwnVault(t, callTool(t, srv, "zerops_env", map[string]any{"action": "get", "serviceHostname": hostname}))
		keys := make([]string, 0, len(got.Envs))
		for _, env := range got.Envs {
			keys = append(keys, env.Key)
		}
		return keys
	}
	if api := keysOf("api"); !slices.Contains(api, "VSCODE_PASSWORD") {
		t.Errorf("api's own vault lists %v, want its VSCODE_PASSWORD", api)
	}
	if byHand := keysOf(fenContainer); !slices.Contains(byHand, "T3CODE_ZEROPS_HQ_ORIGIN") || !slices.Contains(byHand, "ZCP_API_KEY") {
		t.Errorf("the container's variables, named by hostname, list %v; want every name as before", byHand)
	}
	for _, args := range []map[string]any{
		{"action": "request", "project": true, "key": "T3CODE_THEME", "why": "for the editor's theme"},
		{"action": "request", "serviceHostname": "api", "key": "worker_LEASE_TOKEN", "why": "for the worker's lease"},
	} {
		if got := parseOwnVault(t, callTool(t, srv, "zerops_env", args)); got.Requested == nil {
			t.Errorf("%v was not asked: %+v", args, got)
		}
	}
	mock := fenMock()
	parseOwnVault(t, callTool(t, fenServer(t, mock), "zerops_env", map[string]any{"action": "delete", "serviceHostname": fenContainer, "variables": []any{"VSCODE_PASSWORD"}}))
	if held := containerKeys(t, mock); slices.Contains(held, "VSCODE_PASSWORD") {
		t.Errorf("a delete that names the container's hostname left VSCODE_PASSWORD: %v", held)
	}
}
