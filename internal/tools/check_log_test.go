package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
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
		{"superseded log", "/api/mate/changes/app/7/checks/42/log", `{"check":{"id":42,"name":"build"},"head":"old","whole":true,"entries":[]}`, map[string]any{"repo": "app", "change": 7, "check": 42}, 200, false},
		{"review setup", "/api/mate/changes/app/7/checks", `{"head":"new","checks":[],"reviewBuild":{"head":"new","parts":[{"setup":"api","stage":"stage.example","outcome":"failed","phase":"health","reason":"unhealthy","probe":{"url":"https://stage.example/health","status":503,"at":"2026-11-26T20:00:00Z"}}]}}`, map[string]any{"repo": "app", "change": 7, "setup": "api"}, 200, false},
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
				if _, isCheck := tt.args["check"]; isCheck && r.URL.RequestURI() == "/api/mate/changes/app/7/checks" && tt.status == 200 {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(listAnswer))}, nil
				}
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.response))}, nil
			})
			rt := runtime.Info{InContainer: true, MateEnabled: true, HQ: &runtime.HQLink{Origin: "https://hq.example", ProjectID: "p1", Key: "unit-key"}}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterCheckLog(srv, doer, rt, nil, nil, "p1")
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
			if tt.name == "superseded log" && !strings.Contains(extractText(result), "This log is for commit old; the change is now at abc123.") {
				t.Errorf("superseded answer: %q", extractText(result))
			}
			if tt.name == "review setup" && !strings.Contains(extractText(result), "https://stage.example/health") {
				t.Errorf("setup answer: %q", extractText(result))
			}
			mu.Lock()
			defer mu.Unlock()
			wantCount := 1
			if _, isCheck := tt.args["check"]; isCheck && tt.status == 200 {
				wantCount = 2
			}
			if len(requests) != wantCount || requests[0].method != http.MethodGet || requests[0].path != tt.path || !requests[0].authorized {
				t.Errorf("requests = %+v, want authorized GET %s and, after a log, the current checks", requests, tt.path)
			}
			if wantCount == 2 && len(requests) == 2 && (requests[1].method != http.MethodGet || requests[1].path != "/api/mate/changes/app/7/checks" || !requests[1].authorized) {
				t.Errorf("current checks request = %+v", requests[1])
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
			RegisterCheckLog(srv, nil, tt.rt, nil, nil, "p1")
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
		{"merged", &hq.RefusedError{Status: 409, Code: "conflict", Reason: "change_not_open", State: "merged"}, "change #7 was merged."},
		{"closed", &hq.RefusedError{Status: 409, Code: "conflict", Reason: "change_not_open", State: "closed"}, "change #7 was closed."},
		{"not open", &hq.RefusedError{Status: 409, Code: "conflict", Reason: "change_not_open"}, "change #7 is not open."},
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
			if got := checkLogRefusal(7, tt.err); got != tt.want {
				t.Errorf("refusal = %q, want %q", got, tt.want)
			}
		})
	}
}

// witness: internal/tools/hq_check_log.go | if checks.ReviewBuild != nil && len(checks.ReviewBuild.Parts) > 0 { → if checks.ReviewBuild == nil && len(checks.ReviewBuild.Parts) > 0 {
func TestCheckLog_KL4_ListsReviewPartsBesideGitHubAndNamesSupersededHeads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, answer string
		want         []string
	}{
		{"both sources", `{"head":"new","checks":[{"id":9,"name":"test","conclusion":"failure","url":"https://github.example/9"}],"reviewBuild":{"head":"new","parts":[{"setup":"api","stage":"api.example","outcome":"failed","phase":"build","appVersionId":"v1","reason":"compile failed"}]}}`, []string{"test: failure, check run 9", "Review build failed", "setup api, stage api.example, phase build, app version v1: compile failed"}},
		{"superseded review", `{"head":"new","checks":[],"reviewBuild":{"head":"old","parts":[{"setup":"api","stage":"api","outcome":"failed","phase":"deploy","appVersionId":"v2","reason":"deploy failed"}]}}`, []string{"Review build is for commit old; the change is now at new", "setup api"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var checks hq.ChecksAnswer
			if err := json.Unmarshal([]byte(tt.answer), &checks); err != nil {
				t.Fatal(err)
			}
			got := checkLogText("app", 7, checks, nil)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("answer %q lacks %q", got, want)
				}
			}
		})
	}
}

// witness: internal/tools/hq_check_log.go | if part.Setup != setup { → if part.Setup == setup {
func TestCheckLog_KL5_ReadsTheNamedReviewPartAndKeepsReadFailuresDistinct(t *testing.T) {
	t.Parallel()
	status := 503
	head := "new"
	checks := hq.ChecksAnswer{Head: &head, ReviewBuild: &hq.CheckReviewBuild{Head: "old", Parts: []hq.CheckReviewPart{
		{Setup: "api", Stage: "stage", Outcome: "failed", Phase: "build", AppVersionID: "v1", Reason: "build failed"},
		{Setup: "web", Stage: "web.example", Outcome: "failed", Phase: "health", AppVersionID: "v2", Reason: "unhealthy", Probe: &hq.CheckReviewProbe{URL: "https://web.example/health", Status: &status, At: "2026-11-26T20:00:00Z"}},
	}}}
	tests := []struct {
		name, setup string
		fetcher     *platform.MockLogFetcher
		want        []string
	}{
		{"build lines", "api", platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{{Facility: "local0", Tag: "zbuilder@v1", Message: "compile error"}}), []string{"setup api", "compile error", "change is now at new"}},
		{"no log", "api", platform.NewMockLogFetcher(), []string{"platform keeps no log for this version"}},
		{"read error", "api", platform.NewMockLogFetcher().WithError(errors.New("backend down")), []string{"could not be read", "backend down"}},
		{"health", "web", platform.NewMockLogFetcher(), []string{"setup web", "https://web.example/health", "HTTP 503"}},
		{"missing", "other", platform.NewMockLogFetcher(), []string{"No failing Review build part for setup other"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc", Name: "stage"}}).
				WithServiceAppVersions("svc", []platform.AppVersionEvent{{ID: "v1"}}).
				WithLogAccess(&platform.LogAccess{URL: "https://logs.example"})
			got := reviewBuildLog(context.Background(), client, tt.fetcher, "p1", tt.setup, checks)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("answer %q lacks %q", got, want)
				}
			}
			if tt.name == "read error" && strings.Contains(got, "no log") {
				t.Errorf("read error was called no log: %q", got)
			}
		})
	}
}

// witness: internal/hq/client.go | State: refusal.State → State: ""
func TestCheckLog_KL6_SaysTheChangeStateInHQsRefusal(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ state, want string }{
		{"merged", "change #7 was merged."},
		{"closed", "change #7 was closed."},
		{"", "change #7 is not open."},
	} {
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			body := `{"code":"conflict","reason":"change_not_open","state":"` + tt.state + `"}`
			doer := checkLogDoer(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusConflict, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			client, err := hq.Open(doer, hq.Identity{Origin: "https://hq.example", ProjectID: "p1", Key: "unit-key"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.FailingChecks(context.Background(), "app", 7)
			if got := checkLogRefusal(7, err); got != tt.want {
				t.Errorf("refusal = %q, want %q", got, tt.want)
			}
		})
	}
}

// witness: internal/tools/hq_check_log.go | if log.Head != nil && checks.Head != nil && *log.Head != *checks.Head { → if log.Head != nil && checks.Head != nil && *log.Head == *checks.Head {
func TestCheckLog_KL7_LabelsTheLogWithItsOwnHead(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, current, logged string
		wantSuperseded        bool
	}{
		{"current", "new", "new", false},
		{"superseded", "new", "old", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkLogText("app", 7, hq.ChecksAnswer{Head: &tt.current}, &hq.CheckLogAnswer{Head: &tt.logged, Whole: true})
			if !strings.Contains(got, "commit "+tt.logged+":") || strings.Contains(got, "This log is for commit old; the change is now at new.") != tt.wantSuperseded {
				t.Errorf("answer = %q", got)
			}
		})
	}
}
