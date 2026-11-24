package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
)

type checkLogDoer func(*http.Request) (*http.Response, error)

func (d checkLogDoer) Do(r *http.Request) (*http.Response, error) { return d(r) }

// witness: internal/tools/hq_check_log.go | client.FailingChecks(ctx, in.Repo, in.Change) → client.FailingChecks(ctx, in.Repo, in.Change+1)
func TestCheckLog_KL1_AsksHQForTheChecksOrOneChecksLog(t *testing.T) {
	// non-parallel: opening the live identity also reads the process environment.
	const listAnswer = `{"head":"abc123","checks":[]}`
	const logAnswer = `{"check":{"id":138175492335,"name":"build","conclusion":"failure","url":"https://github.com/example/check"},"head":"abc123","lines":400,"whole":true,"selected":41,"entries":[]}`
	tests := []struct {
		name, path, response string
		args                 map[string]any
		status               int
		wantError            bool
	}{
		{"list", "/api/mate/changes/app/7/checks", listAnswer, map[string]any{"repo": "app", "change": 7}, 200, false},
		{"range", "/api/mate/changes/app/7/checks/138175492335/log?from=300&to=340", logAnswer, map[string]any{"repo": "app", "change": 7, "check": int64(138175492335), "from": 300, "to": 340}, 200, false},
		{"last lines", "/api/mate/changes/app/7/checks/138175492335/log", logAnswer, map[string]any{"repo": "app", "change": 7, "check": int64(138175492335)}, 200, false},
		{"pattern", "/api/mate/changes/app/7/checks/138175492335/log?match=fail%7Cerror", logAnswer, map[string]any{"repo": "app", "change": 7, "check": int64(138175492335), "match": "fail|error"}, 200, false},
		{"invalid values reach HQ", "/api/mate/changes/app/7/checks/0/log?from=0&match=", `{"code":"invalid","reason":"check"}`, map[string]any{"repo": "app", "change": 7, "check": 0, "from": 0, "match": ""}, 400, true},
		{"unavailable is not retried", "/api/mate/changes/app/7/checks", `{"code":"unavailable"}`, map[string]any{"repo": "app", "change": 7}, 503, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []struct {
				method, path string
				authorized   bool
			}
			doer := checkLogDoer(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				requests = append(requests, struct {
					method, path string
					authorized   bool
				}{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization") == "Project unit-key"})
				mu.Unlock()
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.response))}, nil
			})
			rt := runtime.Info{InContainer: true, MateEnabled: true, HQ: &runtime.HQLink{Origin: "https://hq.example", ProjectID: "p1", Key: "unit-key"}}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterCheckLog(srv, doer, rt)
			ctx := context.Background()
			st, ct := mcp.NewInMemoryTransports()
			serverSession, err := srv.Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer serverSession.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
			session, err := client.Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil || len(listed.Tools) != 1 {
				t.Fatalf("list tools: %+v, %v", listed, err)
			}
			tool := listed.Tools[0]
			if tool.Name != "zerops_check_log" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
				t.Fatalf("tool annotations: %+v", tool)
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: tt.args})
			if err != nil || result == nil || result.IsError != tt.wantError || result.StructuredContent != nil {
				t.Fatalf("call result: %+v, %v", result, err)
			}
			if tt.wantError && tt.status == 400 && extractText(result) != "HQ says `check` is not valid." {
				t.Errorf("HQ's invalid field: %q", extractText(result))
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != tt.path || !requests[0].authorized {
				t.Errorf("requests = %+v, want one authorized GET %s", requests, tt.path)
			}
		})
	}
	for _, tt := range []struct {
		name string
		rt   runtime.Info
	}{
		{"standalone", runtime.Info{MateEnabled: true, HQ: &runtime.HQLink{Origin: "https://hq.example", ProjectID: "p1", Key: "unit-key"}}},
		{"disabled", runtime.Info{InContainer: true, HQ: &runtime.HQLink{Origin: "https://hq.example", ProjectID: "p1", Key: "unit-key"}}},
		{"no key", runtime.Info{InContainer: true, MateEnabled: true, HQ: &runtime.HQLink{Origin: "https://hq.example", ProjectID: "p1"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterCheckLog(srv, nil, tt.rt)
			ctx := context.Background()
			st, ct := mcp.NewInMemoryTransports()
			ss, err := srv.Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
			session, err := client.Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil || len(listed.Tools) != 0 {
				t.Fatalf("unavailable tool listed: %+v, %v", listed, err)
			}
		})
	}
}

// witness: internal/tools/hq_check_log.go | if !log.Whole { → if log.Whole {
func TestCheckLog_KL2_SaysHQsAnswerAsLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, answer, want string
		log                bool
	}{
		{"checks", `{"head":"abc123","checks":[{"id":138175492335,"name":"build","conclusion":"failure","url":"https://github.com/check/1"},{"id":138175492336,"name":"lint","conclusion":"timed_out","url":"https://github.com/check/2"}]}`, "Failing checks of change #7 in app at commit abc123:\n- build: failure, check run 138175492335, https://github.com/check/1\n- lint: timed_out, check run 138175492336, https://github.com/check/2", false},
		{"none", `{"head":"abc123","checks":[]}`, "No check run is failing on commit abc123.", false},
		{"no head", `{"head":null,"checks":[]}`, "No check run is failing on commit null.", false},
		{"whole log", `{"check":{"id":138175492335,"name":"build","conclusion":"failure","url":"https://github.com/check/1"},"head":"abc123","lines":400,"whole":true,"selected":5,"entries":[{"n":301,"text":"starting"},{"n":302,"text":"long line","cut":true},{"n":303,"text":"failed"}]}`, "build (failure), check run 138175492335, commit abc123: 400 lines read, 5 selected, 3 shown.\n301: starting\n302: long line …[cut]\n303: failed", true},
		{"partial log", `{"check":{"id":138175492335,"name":"build","conclusion":"failure","url":"https://github.com/check/1"},"head":"abc123","lines":400,"whole":false,"selected":5,"entries":[{"n":301,"text":"starting"},{"n":302,"text":"long line","cut":true},{"n":303,"text":"failed"}]}`, "build (failure), check run 138175492335, commit abc123: 400 lines read, 5 selected, 3 shown. The log is longer than HQ reads: these are of its first part.\n301: starting\n302: long line …[cut]\n303: failed", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var checks hq.ChecksAnswer
			var log *hq.CheckLogAnswer
			if tt.log {
				log = &hq.CheckLogAnswer{}
				if err := json.Unmarshal([]byte(tt.answer), log); err != nil {
					t.Fatal(err)
				}
			} else if err := json.Unmarshal([]byte(tt.answer), &checks); err != nil {
				t.Fatal(err)
			}
			if got := checkLogText("app", 7, checks, log); got != tt.want {
				t.Errorf("answer = %q, want %q", got, tt.want)
			}
		})
	}
}

// witness: internal/tools/hq_check_log.go | refusal.Reason == "actions_not_granted" → refusal.Reason == "other"
func TestCheckLog_KL3_SaysEachRefusalAndWhetherToAskAgain(t *testing.T) {
	t.Parallel()
	const own = "This Mate reads the checks of its own open changes only; `status` lists them."
	const later = "GitHub or HQ did not answer; nothing to fix on your side, ask again later."
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"actions grant", &hq.RefusedError{Status: 409, Code: "conflict", Reason: "actions_not_granted"}, "HQ's GitHub app may not read Actions logs for this repository (it lacks Actions: read). Nothing reads the log until a person grants it on GitHub: tell your person. The checks' pages are in the list."},
		{"no log", &hq.RefusedError{Status: 409, Code: "conflict", Reason: "check_has_no_log"}, "GitHub keeps no log for this check run: it is not a GitHub Actions job, is still running, or its log expired."},
		{"not this head", &hq.RefusedError{Status: 404, Code: "not_found", Reason: "check_not_found"}, "No such check run on this change's commit. Call the tool without `check` for the ones failing now."},
		{"another Mate", &hq.RefusedError{Status: 403, Code: "forbidden", Reason: "not_your_change"}, own},
		{"closed", &hq.RefusedError{Status: 409, Code: "conflict", Reason: "change_not_open"}, own},
		{"no change", &hq.RefusedError{Status: 404, Code: "change_not_found", Reason: "change_not_found"}, own},
		{"invalid match", &hq.RefusedError{Status: 400, Code: "invalid", Reason: "match"}, "HQ says `match` is not valid."},
		{"invalid range", &hq.RefusedError{Status: 400, Code: "invalid", Reason: "to"}, "HQ says `to` is not valid."},
		{"github unavailable", &hq.RefusedError{Status: 409, Code: "conflict", Reason: "github_unavailable"}, later},
		{"hq unavailable", &hq.UnavailableError{Code: "unavailable"}, later},
		{"no answer", &hq.UnavailableError{}, later},
		{"503", &hq.RefusedError{Status: 503}, later},
		{"old HQ 404", &hq.RefusedError{Status: 404, Code: "not_found"}, "This HQ has no check reads yet."},
		{"old HQ 405", &hq.RefusedError{Status: 405}, "This HQ has no check reads yet."},
		{"another refusal", &hq.RefusedError{Status: 404, Code: "project_not_found", Reason: "project_not_found"}, "HQ refused the check read (hq refused: 404 project_not_found)."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := checkLogRefusal(tt.err); got != tt.want {
				t.Errorf("refusal = %q, want %q", got, tt.want)
			}
		})
	}
}
