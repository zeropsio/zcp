package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq/hqtest"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestObserve_UnderTheKey_ReadsAndFailures(t *testing.T) {
	// non-parallel: the Mate's identity is the process environment.
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterObserve(srv, nil, "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(listed.Tools) != 1 {
		t.Fatalf("list observe tool: result=%+v err=%v", listed, err)
	}
	tool := listed.Tools[0]
	wantDescription := "Observe stage and production through HQ. environments lists project IDs and known change status; changesAsLastRead marks unrefreshed status. Merge advice requires current status; absent changes or changesAsLastRead mean unknown. status reads services and versions; logs reads up to 100 entries. Read only, without secrets. Failures say fix input, retry later, or accept HQ's refusal."
	if tool == nil || tool.Description != wantDescription {
		t.Fatalf("observe tool = %+v, want description %q", tool, wantDescription)
	}
	for _, tt := range []struct {
		name, action, project, service, path, body string
		status                                     int
		// want is the failure text; a no-answer row pins its start and end,
		// the transport's words between them.
		want, wantEnd string
	}{
		{"environments", "environments", "", "", "/api/mate/environments", `{"appId":"a1","environments":[{"projectId":"P_STAGE","name":"stage","tier":"stage"}]}`, 200, "", ""},
		{"status", "status", "P_STAGE", "", "/api/mate/environments/P_STAGE", `{"environment":{"projectId":"P_STAGE","name":"stage","tier":"stage"},"services":[{"id":"S1","name":"web","status":"ACTIVE","activeVersion":{"id":"V1","activatedAt":null}}]}`, 200, "", ""},
		{"logs", "logs", "P_STAGE", "S1", "/api/mate/environments/P_STAGE/services/S1/logs?limit=2", `{"projectId":"P_STAGE","serviceId":"S1","entries":[{"timestamp":"now","severity":"info","message":"serving"}]}`, 200, "", ""},
		{"forbidden", "status", "P_STAGE", "", "/api/mate/environments/P_STAGE", `{"code":"observation_refused","reason":"forbidden"}`, 403,
			"HQ refused the observation: 403 observation_refused (forbidden); the same call gets the same answer.", ""},
		{"unavailable", "status", "P_STAGE", "", "/api/mate/environments/P_STAGE", `{"code":"zerops_unavailable"}`, 503,
			"HQ could not read Zerops right now (zerops_unavailable); try again later — nothing to fix on your side.", ""},
		{"an environment HQ does not know is named as HQ's refusal", "logs", "P_GONE", "S1", "/api/mate/environments/P_GONE/services/S1/logs?limit=2", `{"code":"environment_not_found","reason":"environment_not_found"}`, 404,
			"HQ refused the observation: 404 environment_not_found; the same call gets the same answer.", ""},
		{"a standby HQ asks the agent to try again later with nothing to fix", "environments", "", "", "/api/mate/environments", `{"code":"not_active"}`, 503,
			"HQ is not serving right now (not_active); try again later — nothing to fix on your side.", ""},
		{"an HQ that drops the call asks the agent to try again later with nothing to fix", "status", "P_STAGE", "", "/api/mate/environments/P_STAGE", "", 0,
			"HQ is not serving right now (hq /api/mate/environments/P_STAGE: ", "); try again later — nothing to fix on your side."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var reads atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				if r.Method != http.MethodGet || r.URL.String() != tt.path || r.Header.Get("Authorization") != "Project good" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				if tt.status == 0 {
					conn, _, err := http.NewResponseController(w).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				w.WriteHeader(tt.status)
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Error(err)
				}
			}))
			defer fake.Close()
			hqtest.Link(t, fake.URL, "P_MATE", "good")
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
			result := callTool(t, srv, "zerops_observe", map[string]any{"action": tt.action, "projectId": tt.project, "serviceId": tt.service, "limit": 2})
			if reads.Load() != 1 {
				t.Fatalf("reads = %d, want one attempt", reads.Load())
			}
			if result.StructuredContent != nil {
				t.Fatal("typed content must stay nil")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if strings.Contains(text, "good") {
				t.Fatal("key leaked")
			}
			switch {
			case tt.status == 200:
				if result.IsError || !json.Valid([]byte(text)) {
					t.Fatalf("success: %+v", result)
				}
			case tt.wantEnd == "":
				if !result.IsError || text != tt.want {
					t.Fatalf("failure = %q (error %v), want %q", text, result.IsError, tt.want)
				}
			default:
				if !result.IsError || !strings.HasPrefix(text, tt.want) || !strings.HasSuffix(text, tt.wantEnd) {
					t.Fatalf("failure = %q (error %v), want %q…%q", text, result.IsError, tt.want, tt.wantEnd)
				}
			}
		})
	}
}

// The environment read already made by the agent must name a change merged
// since its last pass as merged; an unread standing is explicitly as last read.
func TestObserve_Environments_CurrentChanges(t *testing.T) {
	// non-parallel: the Mate's identity is the process environment.
	for _, tt := range []struct {
		name, state    string
		status         int
		merged, unread bool
		labels         []string
	}{
		{"merged since the last pass", "merged", http.StatusOK, true, false, nil},
		{"open with current standing", "open", http.StatusOK, false, false, []string{"ready", "behind main"}},
		{"unknown while HQ cannot refresh", "", http.StatusServiceUnavailable, false, true, []string{"draft"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			ready := false
			if err := workflow.RecordHQChange(stateDir, workflow.HQChange{
				AppID: "a1", Repo: "group", Number: 1, Branch: "setup",
				Told: &workflow.HQChangeTold{Ready: &ready},
			}); err != nil {
				t.Fatal(err)
			}
			var environmentReads, selfReads atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Project good" {
					t.Errorf("unexpected request method or authorization")
				}
				switch r.URL.Path {
				case "/api/mate/environments":
					environmentReads.Add(1)
					writeHQJSON(w, http.StatusOK, map[string]any{"appId": "a1", "environments": []any{}})
				case "/api/mate/self":
					selfReads.Add(1)
					if tt.status != http.StatusOK {
						writeHQJSON(w, tt.status, map[string]any{"code": "not_active"})
						return
					}
					writeHQJSON(w, http.StatusOK, map[string]any{
						"appId": "a1", "projectId": "P_MATE", "openPerBranch": true,
						"changes": []map[string]any{{"repo": "group", "number": 1, "branch": "setup", "state": tt.state, "head": "abc123", "ready": true, "mergeability": "clean", "behind": true}},
					})
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer fake.Close()
			hqtest.Link(t, fake.URL, "P_MATE", "good")
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterObserve(srv, fake.Client(), "P_MATE", stateDir, runtime.Info{InContainer: true, MateEnabled: true})
			result := callTool(t, srv, "zerops_observe", map[string]any{"action": "environments"})
			text := result.Content[0].(*mcp.TextContent).Text
			if result.IsError || result.StructuredContent != nil {
				t.Fatalf("environment observation failed or used structured content: %s", text)
			}
			var got struct {
				AppID        string `json:"appId"`
				Environments []any  `json:"environments"`
				Changes      []struct {
					Repo    string `json:"repo"`
					Changes []struct {
						Number int      `json:"number"`
						Branch string   `json:"branch"`
						Merged bool     `json:"merged"`
						Labels []string `json:"labels"`
					} `json:"changes"`
				} `json:"changes"`
				AsLastRead *bool `json:"changesAsLastRead"`
			}
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatal(err)
			}
			if got.AppID != "a1" || got.Environments == nil || len(got.Environments) != 0 {
				t.Fatalf("environment facts changed: %s", text)
			}
			if len(got.Changes) != 1 || got.Changes[0].Repo != "group" || len(got.Changes[0].Changes) != 1 || got.AsLastRead == nil {
				t.Fatalf("environment result lacks known change and freshness: %s", text)
			}
			change := got.Changes[0].Changes[0]
			if change.Number != 1 || change.Branch != "setup" || change.Merged != tt.merged || !reflect.DeepEqual(change.Labels, tt.labels) || *got.AsLastRead != tt.unread {
				t.Errorf("change standing = %s, want merged=%v labels=%v asLastRead=%v", text, tt.merged, tt.labels, tt.unread)
			}
			if environmentReads.Load() != 1 || selfReads.Load() != 1 {
				t.Errorf("reads: environments=%d self=%d, want one each", environmentReads.Load(), selfReads.Load())
			}
			kept, err := workflow.ListHQChanges(stateDir)
			if err != nil || len(kept) != 1 || kept[0].Told == nil || kept[0].Told.Ready == nil || *kept[0].Told.Ready {
				t.Errorf("read changed the ledger: %+v, err=%v", kept, err)
			}
		})
	}
}

func TestObserve_InvalidInputOrNoKey_NoRequest(t *testing.T) {
	// non-parallel: the Mate's identity is the process environment.
	hqtest.Link(t, "", "", "")
	for _, args := range []map[string]any{
		{"action": "status"}, {"action": "logs", "projectId": "P"}, {"action": "delete", "projectId": "P"},
		{"action": "logs", "projectId": "P", "serviceId": "S", "limit": 101}, {"action": "status", "projectId": "../P"},
		{"action": "environments"},
	} {
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		RegisterObserve(srv, nil, "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
		result := callTool(t, srv, "zerops_observe", args)
		if !result.IsError {
			t.Errorf("input %v should fail visibly", args)
		}
	}
}

func TestObserve_InputError_AsksToFixAndCallAgain(t *testing.T) {
	t.Parallel()
	t.Run("a mistyped action asks the agent to fix it and call zerops_observe again", func(t *testing.T) {
		t.Parallel()
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		RegisterObserve(srv, nil, "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
		result := callTool(t, srv, "zerops_observe", map[string]any{"action": "delete", "projectId": "P"})
		want := "Unknown observation action; fix it and call zerops_observe again."
		if text := result.Content[0].(*mcp.TextContent).Text; !result.IsError || text != want {
			t.Fatalf("failure = %q (error %v), want %q", text, result.IsError, want)
		}
	})
}

// HQ answers a running version as Zerops reads it, `{id, activatedAt}` with
// no name; the agent receives exactly that, the timestamp kept or null.
func TestObserve_StatusActiveVersion_PassesIDAndActivationThrough(t *testing.T) {
	// non-parallel: the Mate's identity is the process environment.
	for _, tt := range []struct {
		name, version string
	}{
		{"an activated version keeps its activation time", `{"id":"V1","activatedAt":"2026-10-16T08:43:10.000Z"}`},
		{"a version not yet activated keeps a null activation time", `{"id":"V1","activatedAt":null}`},
		{"a service without a running version stays null", `null`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"environment":{"projectId":"P_STAGE","name":"stage","tier":"stage"},"services":[{"id":"S1","name":"web","status":"ACTIVE","activeVersion":` + tt.version + `}]}`
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			}))
			defer fake.Close()
			hqtest.Link(t, fake.URL, "P_MATE", "good")
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
			result := callTool(t, srv, "zerops_observe", map[string]any{"action": "status", "projectId": "P_STAGE"})
			if text := result.Content[0].(*mcp.TextContent).Text; result.IsError || text != body {
				t.Fatalf("status = %q (error %v), want %q", text, result.IsError, body)
			}
		})
	}
}
