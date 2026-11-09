// Tests for: internal/tools/app_propose.go — a Mate proposes restarting a
// service or setting a variable in its application's stage or production,
// checked against HQ's observation; nothing runs from here.
package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq/hqtest"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
)

// appObserveHQ answers a Mate's observations of its application: stage and
// production, and production's services. It records each observation asked.
// It also answers the production tier of the recipe on main, which declares
// the api production runs and a db it lacks.
func appObserveHQ(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/mate/recipe/production" {
			_, _ = w.Write([]byte(`{"state":"present","mainHead":"4444444444444444444444444444444444444444","importYaml":"services:\n  - hostname: api\n    type: nodejs@22\n  - hostname: db\n    type: postgresql@18\n"}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/mate/observe" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var in struct {
			Action    string `json:"action"`
			ProjectID string `json:"projectId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		mu.Lock()
		asked = append(asked, strings.TrimSpace(in.Action+" "+in.ProjectID))
		mu.Unlock()
		var body string
		switch in.Action {
		case "environments":
			body = observeTestWire(t, "environments", `{"environments":[{"projectId":"P_STAGE","name":"stage","tier":"stage"},{"projectId":"P_PROD","name":"production","tier":"production"}],"siblingMates":[]}`, http.StatusOK)
		case "status":
			body = observeTestWire(t, "status", `{"environment":{"projectId":"`+in.ProjectID+`","name":"production","tier":"production"},"services":[{"id":"S_API","name":"api","status":"ACTIVE","activeVersion":null}]}`, http.StatusOK)
		default:
			t.Errorf("unexpected observation %q", in.Action)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(fake.Close)
	return fake, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
}

func appProposal(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError {
		t.Fatalf("tool error: %s", text)
	}
	var doc struct {
		Proposal map[string]any `json:"proposal"`
		Note     string         `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("result is not a proposal: %v\n%s", err, text)
	}
	if !strings.HasPrefix(doc.Note, ops.ProposalNote) {
		t.Errorf("note = %q, want it to start with the proposal note", doc.Note)
	}
	return doc.Proposal
}

// TestAppPropose_OverMCP_ProposesFromObservationAndRunsNothing: each tool
// reads the environments and the target's services through HQ's observation,
// and answers a proposal of the Chief's shape — a card the person decides.
func TestAppPropose_OverMCP_ProposesFromObservationAndRunsNothing(t *testing.T) {
	// non-parallel: the Mate's identity and profile are the process environment.
	tests := []struct {
		name string
		tool string
		args map[string]any
		want map[string]any
	}{
		{
			name: "a restart in production",
			tool: "zerops_propose_service",
			args: map[string]any{"project": "production", "service": "api", "action": "restart", "reason": "502s since the release"},
			want: map[string]any{"kind": "service.restart", "summary": "Restart api in Production", "projectId": "P_PROD", "serviceId": "S_API"},
		},
		{
			name: "a variable on production",
			tool: "zerops_propose_env",
			args: map[string]any{"project": "P_PROD", "scope": "project", "key": "LOG_LEVEL", "value": "debug", "reason": "find the 502s"},
			want: map[string]any{"kind": "env.set", "summary": "Set LOG_LEVEL in Production", "projectId": "P_PROD"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, asked := appObserveHQ(t)
			hqtest.Link(t, fake.URL, "P_MATE", "good")
			observeTestProfile(t)
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterAppPropose(srv, fake.Client(), "P_MATE", runtime.Info{InContainer: true, MateEnabled: true})
			proposal := appProposal(t, callTool(t, srv, tt.tool, tt.args))
			for key, want := range tt.want {
				if proposal[key] != want {
					t.Errorf("proposal.%s = %v, want %v", key, proposal[key], want)
				}
			}
			if id, _ := proposal["id"].(string); id == "" {
				t.Error("proposal has no id")
			}
			if got := asked(); !slices.Equal(got, []string{"environments", "status P_PROD"}) {
				t.Errorf("observations = %v, want the environments, then production's status only", got)
			}
		})
	}
}

// TestAppPropose_NoObserveAuthorization_SaysSoWithoutARequest: a Mate the
// person has not authorized to observe proposes nothing and says why, as
// zerops_observe does.
func TestAppPropose_NoObserveAuthorization_SaysSoWithoutARequest(t *testing.T) {
	// non-parallel: isolates the fixed profile path and project environment.
	t.Setenv("HOME", t.TempDir())
	var reads atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reads.Add(1) }))
	defer fake.Close()
	hqtest.Link(t, fake.URL, "P_MATE", "good")
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterAppPropose(srv, fake.Client(), "P_MATE", runtime.Info{InContainer: true, MateEnabled: true})
	result := callTool(t, srv, "zerops_propose_service", map[string]any{"project": "production", "service": "api", "action": "restart", "reason": "stuck"})
	text := result.Content[0].(*mcp.TextContent).Text
	if !result.IsError || reads.Load() != 0 {
		t.Fatalf("want a refusal before any request; error=%v requests=%d: %s", result.IsError, reads.Load(), text)
	}
	if !strings.Contains(text, "observe authorization required") || strings.Contains(text, "good") {
		t.Errorf("refusal = %q, want it to ask for observe authorization without the key", text)
	}
}

// TestRevealPropose_OverMCP_ProposesANameNeverAValue: zerops_propose_reveal
// answers an env.reveal proposal naming the key and where it lives, read
// through HQ's observation, with a note that the agent is never shown it; a
// call that hands it a value is refused before anything is read.
func TestRevealPropose_OverMCP_ProposesANameNeverAValue(t *testing.T) {
	// non-parallel: the Mate's identity and profile are the process environment.
	fake, asked := appObserveHQ(t)
	hqtest.Link(t, fake.URL, "P_MATE", "good")
	observeTestProfile(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterAppPropose(srv, fake.Client(), "P_MATE", runtime.Info{InContainer: true, MateEnabled: true})

	result := callTool(t, srv, "zerops_propose_reveal", map[string]any{
		"environment": "production", "service": "api", "key": "DATABASE_PASSWORD", "reason": "to connect from the laptop",
	})
	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError {
		t.Fatalf("tool error: %s", text)
	}
	var doc struct {
		Proposal map[string]any `json:"proposal"`
		Note     string         `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("result is not a proposal: %v\n%s", err, text)
	}
	for key, want := range map[string]any{"kind": "env.reveal", "summary": "Show DATABASE_PASSWORD from Production", "projectId": "P_PROD", "serviceId": "S_API"} {
		if doc.Proposal[key] != want {
			t.Errorf("proposal.%s = %v, want %v", key, doc.Proposal[key], want)
		}
	}
	if doc.Note != ops.RevealNote || strings.Contains(text, `"value"`) {
		t.Errorf("result %s, want the reveal's note and no value", text)
	}
	if got := asked(); !slices.Equal(got, []string{"environments", "status P_PROD"}) {
		t.Errorf("observations = %v, want the environments, then production's status only", got)
	}

	secret := strings.Join([]string{"hK7q", "2mVz", "P9xR"}, "-")
	refused := callTool(t, srv, "zerops_propose_reveal", map[string]any{
		"environment": "production", "key": "DATABASE_PASSWORD", "value": secret, "reason": "r",
	})
	if !refused.IsError || len(asked()) != 2 {
		t.Errorf("a call with a value: error=%v observations=%v, want a refusal before any read", refused.IsError, asked())
	}
}

// TestAppPropose_CreateService_OverMCP_ProposesWhatProductionLacks:
// zerops_propose_create_service answers a service.create proposal for a
// service the production tier on main declares and production's observed
// status lacks, with the recipe head the tier was read at, and refuses one
// production already has by its name.
// witness: internal/tools/app_propose.go | ops.CreateServiceInput{Services: in.Services, Reason: in.Reason} → ops.CreateServiceInput{Reason: in.Reason}
func TestAppPropose_CreateService_OverMCP_ProposesWhatProductionLacks(t *testing.T) {
	// non-parallel: the Mate's identity and profile are the process environment.
	fake, asked := appObserveHQ(t)
	hqtest.Link(t, fake.URL, "P_MATE", "good")
	observeTestProfile(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterAppPropose(srv, fake.Client(), "P_MATE", runtime.Info{InContainer: true, MateEnabled: true})

	proposal := appProposal(t, callTool(t, srv, "zerops_propose_create_service", map[string]any{
		"services": []string{"db"}, "reason": "The People change keeps invitations in a database",
	}))
	for key, want := range map[string]any{"kind": "service.create", "summary": "Create db in Production", "projectId": "P_PROD", "projectName": "Production"} {
		if proposal[key] != want {
			t.Errorf("proposal.%s = %v, want %v", key, proposal[key], want)
		}
	}
	wantParams := map[string]any{"services": []any{"db"}, "recipeHead": "4444444444444444444444444444444444444444"}
	if !reflect.DeepEqual(proposal["params"], wantParams) {
		t.Errorf("proposal.params = %v, want %v", proposal["params"], wantParams)
	}
	if got := asked(); !slices.Equal(got, []string{"environments", "status P_PROD"}) {
		t.Errorf("observations = %v, want the environments, then production's status only", got)
	}

	refused := callTool(t, srv, "zerops_propose_create_service", map[string]any{"services": []string{"api"}, "reason": "r"})
	if text := refused.Content[0].(*mcp.TextContent).Text; !refused.IsError || !strings.Contains(text, "api is already in production") {
		t.Errorf("a service production has: error=%v %s, want a refusal naming it", refused.IsError, text)
	}
}
