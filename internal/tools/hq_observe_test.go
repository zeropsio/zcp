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
	"github.com/zeropsio/zcp/internal/hq"
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
	wantDescription := "Read stage/production status, processes, versions and complete native logs through HQ with explicit profile observe authorization and current target permissions. For failed builds, follow process.versionId to build_logs for build/preparation; active runtime is separate. Log from/till accept record IDs or ISO timestamps. environments includes known changes; absent/changesAsLastRead means unknown, so refresh before merge advice. Errors distinguish input, unavailability and refusal."
	if tool == nil || tool.Description != wantDescription {
		t.Fatalf("observe tool = %+v, want description %q", tool, wantDescription)
	}
	schemaJSON, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties) != 8 {
		t.Fatalf("observe schema has unexpected authority/transport inputs: %s", schemaJSON)
	}
	for _, name := range []string{"action", "projectId", "serviceId", "processId", "versionId", "limit", "from", "till"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Fatalf("missing fixed input %s", name)
		}
	}
	for _, name := range []string{"from", "till"} {
		property, ok := schema.Properties[name].(map[string]any)
		if !ok || property["type"] != "string" {
			t.Fatalf("native log boundary %s must be a string input", name)
		}
	}
	for _, tt := range []struct {
		name, action, project, service, path, body string
		status                                     int
		// want is the failure text; a no-answer row pins its start and end,
		// the transport's words between them.
		want, wantEnd string
	}{
		{"environments", "environments", "", "", "/api/mate/observe", `{"appId":"a1","environments":[{"projectId":"P_STAGE","name":"stage","tier":"stage"}]}`, 200, "", ""},
		{"status", "status", "P_STAGE", "", "/api/mate/observe", `{"environment":{"projectId":"P_STAGE","name":"stage","tier":"stage"},"services":[{"id":"S1","name":"web","status":"ACTIVE","activeVersion":{"id":"V1","activatedAt":null}}]}`, 200, "", ""},
		{"logs", "logs", "P_STAGE", "S1", "/api/mate/observe", `{"projectId":"P_STAGE","serviceId":"S1","entries":[{"timestamp":"now","severity":"info","message":"serving"}]}`, 200, "", ""},
		{"forbidden", "status", "P_STAGE", "", "/api/mate/observe", `{"code":"observation_refused","reason":"forbidden"}`, 403,
			"HQ refused the observation: 403 observation_refused (forbidden); the same call gets the same answer.", ""},
		{"unavailable", "status", "P_STAGE", "", "/api/mate/observe", `{"code":"zerops_unavailable"}`, 503,
			"HQ could not read Zerops right now (zerops_unavailable); try again later — nothing to fix on your side.", ""},
		{"an environment HQ does not know is named as HQ's refusal", "logs", "P_GONE", "S1", "/api/mate/observe", `{"code":"environment_not_found","reason":"environment_not_found"}`, 404,
			"HQ refused the observation: 404 environment_not_found; the same call gets the same answer.", ""},
		{"a standby HQ asks the agent to try again later with nothing to fix", "environments", "", "", "/api/mate/observe", `{"code":"not_active"}`, 503,
			"HQ is not serving right now (not_active); try again later — nothing to fix on your side.", ""},
		{"an HQ that drops the call asks the agent to try again later with nothing to fix", "status", "P_STAGE", "", "/api/mate/observe", "", 0,
			"HQ is not serving right now (HQ observation request did not complete", "); try again later — nothing to fix on your side."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var reads atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				if r.Method != http.MethodPost || r.URL.String() != tt.path || r.Header.Get("Authorization") != "Observe "+observeTestToken {
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
				if _, err := w.Write([]byte(observeTestWire(t, tt.action, tt.body, tt.status))); err != nil {
					t.Error(err)
				}
			}))
			defer fake.Close()
			hqtest.Link(t, fake.URL, "P_MATE", "good")
			observeTestProfile(t)
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
				if r.URL.Path == "/api/mate/self" && (r.Method != http.MethodGet || r.Header.Get("Authorization") != "Project good") || r.URL.Path == "/api/mate/observe" && (r.Method != http.MethodPost || r.Header.Get("Authorization") != "Observe "+observeTestToken) {
					t.Errorf("unexpected request method or authorization")
				}
				switch r.URL.Path {
				case "/api/mate/observe":
					environmentReads.Add(1)
					_, _ = w.Write([]byte(observeTestWire(t, "environments", `{"appId":"a1","environments":[]}`, 200)))
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
			observeTestProfile(t)
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
			legacyBody := `{"environment":{"projectId":"P_STAGE","name":"stage","tier":"stage"},"services":[{"id":"S1","name":"web","status":"ACTIVE","activeVersion":` + tt.version + `}]}`
			body := observeTestWire(t, "status", legacyBody, 200)
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			}))
			defer fake.Close()
			hqtest.Link(t, fake.URL, "P_MATE", "good")
			observeTestProfile(t)
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
			result := callTool(t, srv, "zerops_observe", map[string]any{"action": "status", "projectId": "P_STAGE"})
			if text := result.Content[0].(*mcp.TextContent).Text; result.IsError || text != body {
				t.Fatalf("status = %q (error %v), want %q", text, result.IsError, body)
			}
		})
	}
}

// A project integration key does not prove personal observe authorization.
func TestObserve_MissingCredential_ProjectKeyCannotAuthorize(t *testing.T) {
	// non-parallel: isolates the fixed profile path and project environment.
	t.Setenv("HOME", t.TempDir())
	var reads atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		writeHQJSON(w, http.StatusOK, map[string]any{"appId": "a1", "environments": []any{}})
	}))
	defer fake.Close()
	hqtest.Link(t, fake.URL, "P_MATE", "good")
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
	result := callTool(t, srv, "zerops_observe", map[string]any{"action": "environments"})
	if !result.IsError || reads.Load() != 0 {
		t.Fatalf("missing observe credential must fail closed before HTTP; error=%v requests=%d", result.IsError, reads.Load())
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "observe authorization required") || strings.Contains(text, "good") {
		t.Fatalf("missing credential must ask for observe authorization without exposing a key: %q", text)
	}
}

const observeTestToken = "zobs.org.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func observeTestProfile(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	hqtest.GitCredential(t, hq.ObserveCredentialPath(), observeTestToken)
}

// Original fact fixtures retain their scenario values inside the new HQ envelope.
func observeTestWire(t *testing.T, action, body string, status int) string {
	t.Helper()
	if status != http.StatusOK {
		return body
	}
	var facts map[string]any
	if err := json.Unmarshal([]byte(body), &facts); err != nil {
		t.Fatal(err)
	}
	facts["action"] = action
	if _, ok := facts["appId"]; !ok {
		facts["appId"] = "a1"
	}
	if environment, ok := facts["environment"].(map[string]any); ok {
		facts["projectId"] = environment["projectId"]
		delete(facts, "environment")
	}
	if _, ok := facts["projectId"]; !ok {
		facts["projectId"] = nil
	}
	delete(facts, "serviceId")
	if environments, ok := facts["environments"].([]any); ok {
		for _, v := range environments {
			environment := v.(map[string]any)
			environment["kind"] = environment["tier"]
			delete(environment, "tier")
		}
	}
	facts["coverage"] = map[string]any{"source": "hq", "observedAt": "2026-10-29T12:00:00Z", "freshness": "fresh", "scope": "authorized current facts", "truncated": false}
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestObserve_AllActions_FixedInputsAndCredentialRotationReachMCP(t *testing.T) {
	// non-parallel: fixed profile HOME and project identity are process state.
	observeTestProfile(t)
	var expectedAuthorization string
	var requests int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/mate/observe" || r.Header.Get("Authorization") != expectedAuthorization {
			t.Error("wrong observation route or authority")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
			return
		}
		action, _ := in["action"].(string)
		field := map[string]string{"environments": "environments", "status": "services", "deployments": "deployments", "processes": "processes", "process": "processes", "versions": "versions", "version": "versions", "logs": "entries", "build_logs": "entries"}[action]
		if field == "" {
			t.Error("unknown action reached HQ")
			return
		}
		if in["limit"] != float64(100) {
			t.Error("default limit was not normalized")
		}
		if _, ok := in["credential"]; ok {
			t.Error("credential entered input")
		}
		out := map[string]any{"action": action, "appId": "a1", "projectId": in["projectId"], "coverage": map[string]any{"source": "hq", "observedAt": "2026-10-29T12:00:00Z", "freshness": "fresh", "scope": "visible current facts", "truncated": false}, field: []any{}}
		if action == "process" {
			out[field] = []any{map[string]any{"id": "PR1", "status": "ACTIVE"}}
		}
		if action == "version" {
			out[field] = []any{map[string]any{"id": "V1", "status": "ACTIVE"}}
		}
		writeHQJSON(w, 200, out)
	}))
	defer fake.Close()
	hqtest.Link(t, fake.URL, "P_MATE", "")
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
	for _, action := range []string{"environments", "status", "deployments", "processes", "process", "versions", "version", "logs", "build_logs"} {
		// Replacing the profile file changes authority on the same registered tool.
		token := "zobs.org." + strings.Repeat("A", 43)
		if requests%2 == 1 {
			token = "zobs.org." + strings.Repeat("B", 42) + "A"
		}
		hqtest.GitCredential(t, hq.ObserveCredentialPath(), token)
		expectedAuthorization = "Observe " + token
		args := map[string]any{"action": action}
		if action != "environments" && action != "deployments" {
			args["projectId"] = "P_STAGE"
		}
		if action == "logs" || action == "versions" {
			args["serviceId"] = "S1"
		}
		if action == "process" {
			args["processId"] = "PR1"
		}
		if action == "version" || action == "build_logs" {
			args["versionId"] = "V1"
		}
		result := callTool(t, srv, "zerops_observe", args)
		text := result.Content[0].(*mcp.TextContent).Text
		if result.IsError || result.StructuredContent != nil || strings.Contains(text, token) {
			t.Fatalf("%s did not deliver safe text facts: %s", action, text)
		}
		var got struct {
			Action string `json:"action"`
			AppID  string `json:"appId"`
		}
		if err := json.Unmarshal([]byte(text), &got); err != nil || got.Action != action || got.AppID != "a1" {
			t.Fatalf("%s facts changed: %s", action, text)
		}
	}
	if requests != 9 {
		t.Fatalf("requests=%d want exactly 9", requests)
	}
}

func TestObserve_InvalidFixedSelectors_NoHTTPRequest(t *testing.T) {
	// non-parallel: profile path and project configuration are process state.
	observeTestProfile(t)
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer fake.Close()
	hqtest.Link(t, fake.URL, "P_MATE", "project-key")
	for _, args := range []map[string]any{
		{"action": "process", "projectId": "P"},
		{"action": "version", "projectId": "P"},
		{"action": "build_logs", "projectId": "P"},
		{"action": "versions", "projectId": "P"},
		{"action": "processes"},
		{"action": "logs", "projectId": "P", "serviceId": "S", "limit": -1},
		{"action": "environments", "versionId": "../escape"},
		{"action": "status", "projectId": "P", "from": "record-id"},
		{"action": "processes", "projectId": "P", "till": "2026-10-29T12:00:00Z"},
		{"action": "logs", "projectId": "P", "serviceId": "S", "from": ""},
		{"action": "logs", "projectId": "P", "serviceId": "S", "till": ""},
		{"action": "logs", "projectId": "P", "serviceId": "S", "from": "line\nbreak"},
		{"action": "logs", "projectId": "P", "serviceId": "S", "till": "control\x7f"},
		{"action": "logs", "projectId": "P", "serviceId": "S", "from": "nonascii-é"},
		{"action": "build_logs", "projectId": "P", "versionId": "V", "till": strings.Repeat("r", 257)},
		{"action": "logs", "projectId": "P", "serviceId": "S", "from": nil},
		{"action": "logs", "projectId": "P", "serviceId": "S", "till": nil},
	} {
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
		if result := callTool(t, srv, "zerops_observe", args); !result.IsError {
			t.Errorf("invalid input accepted: %v", args)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid selectors reached HTTP %d times", requests.Load())
	}
}

func TestObserve_AuthorizedLogs_PreserveHQCoverageAndOpaqueRecordsThroughMCP(t *testing.T) {
	// non-parallel: profile HOME and project identity are process state.
	observeTestProfile(t)
	for _, action := range []string{"logs", "build_logs"} {
		for _, scenario := range []string{"HQ log coverage", "opaque source record", "escaped opaque source record"} {
			t.Run(action+"/"+scenario, func(t *testing.T) {
				body := `{"action":"` + action + `","appId":"app","projectId":"target","coverage":{"source":"zerops_logger","observedAt":"2026-10-29T12:00:00Z","freshness":"fresh","scope":"authorized service","truncated":false},"entries":[]}`
				if scenario == "HQ log coverage" {
					body = strings.Replace(body, `"truncated":false`, `"truncated":false,"logWindow":{"rawCount":0,"rawLimit":100,"rawWindowFull":false,"nextPage":false}`, 1)
				} else {
					record := `{"id":"native-record-1","tag":"zbuilder/version","hostname":"web","content":"` + observeTestToken + strings.Repeat("x", 5000) + `","structuredData":{"vendor":{"nested":[false,null,42]}},"arbitrarySourceField":"unchanged"}`
					if scenario == "escaped opaque source record" {
						record = strings.Replace(record, "zobs.", `zobs\u002e`, 1)
					}
					body = strings.Replace(body, `"entries":[]`, `"entries":[`+record+`]`, 1)
				}
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(body))
				}))
				defer fake.Close()
				hqtest.Link(t, fake.URL, "P_MATE", "")
				srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
				RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
				result := callTool(t, srv, "zerops_observe", map[string]any{"action": action, "projectId": "target", "serviceId": "service", "versionId": "version"})
				text := result.Content[0].(*mcp.TextContent).Text
				if result.IsError || result.StructuredContent != nil || text != body {
					t.Fatalf("authorized HQ log page must reach MCP unchanged: error=%v unchanged=%v", result.IsError, text == body)
				}
			})
		}
	}
}

func TestObserve_FailedAttempt_ExactVersionBuildLogsSeparateFromActiveRuntime(t *testing.T) {
	// non-parallel: profile HOME and project identity are process state.
	observeTestProfile(t)
	var requests int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/mate/observe" || r.Header.Get("Authorization") != "Observe "+observeTestToken {
			t.Error("wrong flow route or authority")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
			return
		}
		if in["projectId"] != "target" {
			t.Error("flow left the target project")
		}
		action, _ := in["action"].(string)
		var body string
		switch action {
		case "status":
			body = `{"projectId":"target","services":[{"id":"service","status":"ACTIVE","activeVersion":{"id":"old-active-version"}}]}`
		case "processes":
			body = `{"projectId":"target","processes":[{"id":"failed-process","serviceId":"service","status":"FAILED","versionId":"failed-version"}]}`
		case "version":
			if in["versionId"] != "failed-version" {
				t.Error("version detail substituted active runtime for failed attempt")
			}
			body = `{"projectId":"target","versions":[{"id":"failed-version","status":"BUILD_FAILED"}]}`
		case "build_logs":
			if in["versionId"] != "failed-version" {
				t.Error("build log substituted active runtime for failed attempt")
			}
			body = `{"projectId":"target","entries":[{"id":"prepare-record","tag":"zbuilder@failed-version","hostname":"prepare-runtime","content":"failure in preparation","structuredData":{"native":true}}]}`
		default:
			t.Error("unexpected flow action")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(observeTestWire(t, action, body, http.StatusOK)))
	}))
	defer fake.Close()
	hqtest.Link(t, fake.URL, "P_MATE", "")
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
	read := func(action, versionID string) map[string]json.RawMessage {
		t.Helper()
		result := callTool(t, srv, "zerops_observe", map[string]any{"action": action, "projectId": "target", "versionId": versionID})
		if result.IsError || result.StructuredContent != nil {
			t.Fatal("failed-attempt navigation did not return text facts")
		}
		var facts map[string]json.RawMessage
		if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &facts); err != nil {
			t.Fatal(err)
		}
		return facts
	}
	var services []struct {
		ActiveVersion struct {
			ID string `json:"id"`
		} `json:"activeVersion"`
	}
	if err := json.Unmarshal(read("status", "")["services"], &services); err != nil || len(services) != 1 || services[0].ActiveVersion.ID != "old-active-version" {
		t.Fatal("active runtime identity was lost")
	}
	var processes []struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		VersionID string `json:"versionId"`
	}
	if err := json.Unmarshal(read("processes", "")["processes"], &processes); err != nil || len(processes) != 1 || processes[0].VersionID != "failed-version" || processes[0].Status != "FAILED" {
		t.Fatal("failed process must retain its exact versionId")
	}
	var versions []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(read("version", processes[0].VersionID)["versions"], &versions); err != nil || len(versions) != 1 || versions[0].ID != "failed-version" || versions[0].Status != "BUILD_FAILED" {
		t.Fatal("failed version detail must stay separate from active runtime")
	}
	var entries []struct {
		ID       string `json:"id"`
		Tag      string `json:"tag"`
		Hostname string `json:"hostname"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(read("build_logs", versions[0].ID)["entries"], &entries); err != nil || len(entries) != 1 || entries[0].Tag != "zbuilder@failed-version" || entries[0].Content != "failure in preparation" || entries[0].Hostname != "prepare-runtime" {
		t.Fatal("exact failed attempt did not retain its preparation log record")
	}
	if requests != 4 {
		t.Fatalf("flow requests=%d, want 4", requests)
	}
}

func TestObserve_LogBounds_NativePaginationReachesMCP(t *testing.T) {
	// non-parallel: profile HOME and project identity are process state.
	observeTestProfile(t)
	for _, action := range []string{"logs", "build_logs"} {
		t.Run(action, func(t *testing.T) {
			var requests int
			var expectedTill = "2026-10-29T13:00:00Z"
			bodies := []string{
				`{"action":"` + action + `","appId":"app","projectId":"target","coverage":{"source":"zerops_logger","observedAt":"2026-10-29T12:00:00Z","freshness":"fresh","scope":"authorized source","truncated":true,"logWindow":{"rawCount":1,"rawLimit":1,"rawWindowFull":true,"nextPage":true}},"entries":[{"id":"page-1","content":"first","sourceExtra":{"n":1}}]}`,
				`{"action":"` + action + `","appId":"app","projectId":"target","coverage":{"source":"zerops_logger","observedAt":"2026-10-29T12:00:00Z","freshness":"fresh","scope":"authorized source","truncated":false,"logWindow":{"rawCount":1,"rawLimit":1,"rawWindowFull":true,"nextPage":false}},"entries":[{"id":"page-2","content":"last","sourceExtra":{"n":2}}]}`,
			}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in map[string]any
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
					return
				}
				if in["action"] != action || in["from"] != "2026-10-29T11:00:00Z" || in["till"] != expectedTill || in["limit"] != float64(1) || r.Header.Get("Authorization") != "Observe "+observeTestToken {
					t.Error("native page bounds or authorization were not forwarded exactly")
				}
				if requests >= len(bodies) {
					t.Error("unexpected page request")
					return
				}
				_, _ = w.Write([]byte(bodies[requests]))
				requests++
			}))
			defer fake.Close()
			hqtest.Link(t, fake.URL, "P_MATE", "")
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
			for page := range bodies {
				result := callTool(t, srv, "zerops_observe", map[string]any{"action": action, "projectId": "target", "serviceId": "service", "versionId": "version", "limit": 1, "from": "2026-10-29T11:00:00Z", "till": expectedTill})
				if result.IsError || result.StructuredContent != nil {
					t.Fatal("native log page failed to reach MCP as text")
				}
				text := result.Content[0].(*mcp.TextContent).Text
				if text != bodies[page] {
					t.Fatal("native records or boolean nextPage changed in MCP output")
				}
				var out struct {
					Entries []struct {
						ID string `json:"id"`
					} `json:"entries"`
				}
				if err := json.Unmarshal([]byte(text), &out); err != nil || len(out.Entries) != 1 {
					t.Fatal("page lost its native continuation record ID")
				}
				expectedTill = out.Entries[0].ID
			}
			if requests != 2 {
				t.Fatalf("page requests=%d, want 2", requests)
			}
		})
	}
}

func TestObserve_ReflectedToken_FailedResponsesDoNotReachMCP(t *testing.T) {
	// non-parallel: profile HOME and project identity are process state.
	observeTestProfile(t)
	for _, tt := range []struct {
		name, body string
		status     int
	}{
		{"reflected auth", `{"code":"` + observeTestToken + `","reason":"` + observeTestToken + `"}`, http.StatusUnauthorized},
		{"reflected denial", `{"code":"forbidden","reason":"` + observeTestToken + `"}`, http.StatusForbidden},
		{"reflected unavailable", `{"code":"` + observeTestToken + `"}`, http.StatusServiceUnavailable},
		{"invalid success", `{"entries":[{"content":"` + observeTestToken + `"}]}`, http.StatusOK},
		{"oversized source record", `{"action":"logs","appId":"app","projectId":"target","coverage":{"source":"zerops_logger","observedAt":"2026-10-29T12:00:00Z","freshness":"fresh","scope":"authorized source","truncated":false},"entries":[{"content":"` + observeTestToken + strings.Repeat("x", hq.ObserveOutputLimit) + `"}]}`, http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer fake.Close()
			hqtest.Link(t, fake.URL, "P_MATE", "")
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterObserve(srv, fake.Client(), "P_MATE", t.TempDir(), runtime.Info{InContainer: true, MateEnabled: true})
			result := callTool(t, srv, "zerops_observe", map[string]any{"action": "logs", "projectId": "target", "serviceId": "service"})
			text := result.Content[0].(*mcp.TextContent).Text
			if !result.IsError || result.StructuredContent != nil || strings.Contains(text, observeTestToken) || strings.Contains(text, `"content"`) {
				t.Fatal("failed or invalid HQ response must expose no source body or reflected token")
			}
		})
	}
}
