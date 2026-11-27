package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// boardHQ is a fake HQ for the board tools: it answers every request with
// status and body and records each one.
type boardHQ struct {
	mu       sync.Mutex
	status   int
	body     string
	requests []string
}

func (h *boardHQ) doer() checkLogDoer {
	return func(r *http.Request) (*http.Response, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.requests = append(h.requests, fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization")))
		return &http.Response{StatusCode: h.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(h.body))}, nil
	}
}

func (h *boardHQ) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.requests...)
}

// boardRuntime is a Mate's container linked to HQ, its state heard: HQ holds
// it in an application when app is true, in none when false.
func boardRuntime(t *testing.T, app bool) runtime.Info {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	var appID any
	if app {
		appID = "app-1"
	}
	raw, err := json.Marshal(map[string]any{"link": "linked", "mate": map[string]any{
		"rev": 1, "projectId": "mate-1", "appId": appID, "repos": []any{}, "changes": []any{}, "chief": nil,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return runtime.Info{InContainer: true, MateEnabled: true,
		HQ: &runtime.HQLink{Origin: "https://hq.example", ProjectID: "mate-1", Key: "unit-key", StatePath: path}}
}

// callBoard calls one board tool with args on a server RegisterBoard alone
// registers.
func callBoard(t *testing.T, rt runtime.Info, h *boardHQ, carries bool, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterBoard(srv, h.doer(), rt, func() bool { return carries })
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result == nil || result.StructuredContent != nil {
		t.Fatalf("call %s: %+v, %v", name, result, err)
	}
	return result
}

// witness: internal/ops/board_proposal.go | case utf8.RuneCountInString(brief) > BoardBriefMax: → case len(brief) > BoardBriefMax:
func TestBoardPropose_BD1_AnswersTheCheckedProposalAndWritesNothing(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 8000)
	for _, tc := range []struct {
		name         string
		title, brief string
		want         string // the proposal's title and brief as answered, "" for a refusal
	}{
		{name: "trimmed", title: "  Fix the invoice total rounding  ", brief: "\nThe invoice total rounds each line first.\n", want: "Fix the invoice total rounding|The invoice total rounds each line first."},
		{name: "a brief of 8000 characters", title: "Rounding", brief: long, want: "Rounding|" + long},
		{name: "a title over two lines", title: "Fix\nrounding", brief: "b"},
		{name: "a title of 121 characters", title: strings.Repeat("t", 121), brief: "b"},
		{name: "no brief", title: "Rounding", brief: "  "},
		{name: "a brief of 8001 characters", title: "Rounding", brief: long + "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &boardHQ{status: http.StatusOK, body: `{}`}
			result := callBoard(t, boardRuntime(t, true), h, true, "zerops_board_propose", map[string]any{"title": tc.title, "brief": tc.brief})
			if n := len(h.seen()); n != 0 {
				t.Errorf("the proposal asked HQ %d times, want none", n)
			}
			if tc.want == "" {
				if !result.IsError {
					t.Errorf("proposed outside the bounds: %s", extractText(result))
				}
				return
			}
			var answer struct {
				Proposed struct {
					Board struct{ Title, Brief string } `json:"board"`
				} `json:"proposed"`
				NextActions string `json:"nextActions"`
			}
			if result.IsError || json.Unmarshal([]byte(extractText(result)), &answer) != nil {
				t.Fatalf("answer: %s", extractText(result))
			}
			if got := answer.Proposed.Board.Title + "|" + answer.Proposed.Board.Brief; got != tc.want || !strings.Contains(answer.NextActions, "Nothing was written") {
				t.Errorf("answer = %q, %q; want %q", got, answer.NextActions, tc.want)
			}
		})
	}
}

// witness: internal/mate/manifest.go | return carriesParts(core, BoardMateVersion) → return carriesParts(installed, BoardMateVersion)
// witness: internal/tools/board.go | if !carriesBoard() { → if carriesBoard() {
func TestBoardPropose_BD2_RefusedBelowTheMateThatDrawsTheCard(t *testing.T) {
	t.Parallel()
	for installed, want := range map[string]bool{
		mate.BoardMateVersion:                 true,
		mate.BoardMateVersion + "-dev.a1b2c3": true,
		"0.16.0":                              false,
		"":                                    false,
	} {
		if got := mate.CarriesBoard(installed); got != want {
			t.Errorf("CarriesBoard(%q) = %v, want %v", installed, got, want)
		}
	}
	for _, carries := range []bool{true, false} {
		result := callBoard(t, boardRuntime(t, true), &boardHQ{status: http.StatusOK}, carries, "zerops_board_propose",
			map[string]any{"title": "Rounding", "brief": "The invoice total rounds each line first."})
		if refused := result.IsError && strings.Contains(extractText(result), "nothing was proposed"); refused == carries {
			t.Errorf("a Mate that draws the card = %v: refused = %v: %s", carries, refused, extractText(result))
		}
	}
}

// witness: internal/tools/board.go | case t.Taker.Kind == hq.TakerPerson && t.Taker.MateProjectID == own: → case t.Taker.Kind == hq.TakerPerson && t.Taker.MateProjectID == "":
func TestBoardList_BD3_TellsEachOpenTaskAndWhoHasIt(t *testing.T) {
	t.Parallel()
	h := &boardHQ{status: http.StatusOK, body: `{"tasks":[
		{"id":"t1","title":"Fix rounding","state":"queued","label":"","mate":null,"taker":null,"source":{"kind":"conversation","mateProjectId":"mate-1"}},
		{"id":"t2","title":"Sort orders","state":"taken","label":"","mate":{"projectId":"mate-1","name":"Ivo"},"taker":{"kind":"person","userId":"u1","mateProjectId":"mate-1"},"source":{"kind":"conversation","mateProjectId":"mate-2"}},
		{"id":"t3","title":"Login fails","state":"working","label":"web#12","mate":{"projectId":"mate-3","name":"Pool One"},"taker":{"kind":"pool","mateProjectId":"mate-3"},"source":{"kind":"github-issue"}},
		{"id":"t4","title":"Nightly report","state":"working","label":"the nightly schedule","mate":{"projectId":"mate-4","name":"Pool Two"}},
		{"id":"t5","title":"Dark mode","state":"taken","label":"","mate":{"projectId":"mate-5","name":"Ada"},"taker":{"kind":"person","userId":"u2","mateProjectId":"mate-5"},"source":{"kind":"conversation","mateProjectId":"mate-5"}}
	]}`}
	result := callBoard(t, boardRuntime(t, true), h, true, "zerops_board_list", map[string]any{})
	if got := h.seen(); len(got) != 1 || got[0] != "GET /api/mate/tasks Project unit-key" {
		t.Errorf("requests = %q, want one GET /api/mate/tasks under the Mate's key", got)
	}
	var answer boardListAnswer
	if result.IsError || json.Unmarshal([]byte(extractText(result)), &answer) != nil {
		t.Fatalf("answer: %s", extractText(result))
	}
	want := []boardTask{
		{ID: "t1", Title: "Fix rounding", State: "queued", From: "a conversation in this Mate"},
		{ID: "t2", Title: "Sort orders", State: "taken", With: "your person, in this Mate", From: "a conversation in another Mate"},
		{ID: "t3", Title: "Login fails", State: "working", With: "Pool One, an automated Mate", From: "web#12"},
		{ID: "t4", Title: "Nightly report", State: "working", With: "Pool Two", From: "the nightly schedule"},
		{ID: "t5", Title: "Dark mode", State: "taken", With: "a person, in Ada", From: "a conversation in another Mate"},
	}
	if fmt.Sprint(answer.Tasks) != fmt.Sprint(want) || answer.More != 0 {
		t.Errorf("tasks = %+v, more %d; want %+v", answer.Tasks, answer.More, want)
	}
	tasks := make([]string, 0, 130)
	for i := range 130 {
		tasks = append(tasks, fmt.Sprintf(`{"id":"t%d","title":"Task %d","state":"queued"}`, i, i))
	}
	h = &boardHQ{status: http.StatusOK, body: `{"tasks":[` + strings.Join(tasks, ",") + `]}`}
	if err := json.Unmarshal([]byte(extractText(callBoard(t, boardRuntime(t, true), h, true, "zerops_board_list", map[string]any{}))), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Tasks) != boardListMax || answer.More != 30 {
		t.Errorf("a board of 130 tells %d tasks and %d more, want %d and 30", len(answer.Tasks), answer.More, boardListMax)
	}
}

// witness: internal/tools/board.go | refused.Status == http.StatusNotFound → refused.Status == http.StatusConflict
func TestBoardList_BD4_SaysEachFailureAndWhetherToAskAgain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "HQ not answering", status: http.StatusServiceUnavailable, body: `{"code":"unavailable"}`, want: "HQ did not answer, so the board could not be read. Try again"},
		{name: "a refused key", status: http.StatusUnauthorized, body: `{"code":"key_refused"}`, want: "Asking again will not help until that access is back"},
		{name: "no board yet", status: http.StatusNotFound, body: `{"code":"not_found"}`, want: "keeps no board for it yet"},
		{name: "another refusal", status: http.StatusConflict, body: `{"code":"conflict","reason":"other"}`, want: "HQ refused to list the board's tasks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &boardHQ{status: tc.status, body: tc.body}
			result := callBoard(t, boardRuntime(t, true), h, true, "zerops_board_list", map[string]any{})
			if !result.IsError || !strings.Contains(extractText(result), tc.want) {
				t.Errorf("answer = %q, want an error saying %q", extractText(result), tc.want)
			}
			if n := len(h.seen()); n != 1 {
				t.Errorf("HQ was asked %d times, want once", n)
			}
		})
	}
}

// witness: internal/tools/board.go | if err != nil || state.AppID != nil { → if err != nil || state.AppID == nil {
func TestBoard_BD5_AMateInNoApplicationHasNoBoard(t *testing.T) {
	t.Parallel()
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"zerops_board_propose", map[string]any{"title": "Rounding", "brief": "The invoice total rounds each line first."}},
		{"zerops_board_list", map[string]any{}},
	} {
		for _, app := range []bool{true, false} {
			h := &boardHQ{status: http.StatusOK, body: `{"tasks":[]}`}
			result := callBoard(t, boardRuntime(t, app), h, true, call.name, call.args)
			if refused := result.IsError && strings.Contains(extractText(result), "no application"); refused == app {
				t.Errorf("%s in an application = %v: refused = %v: %s", call.name, app, refused, extractText(result))
			}
			if !app && len(h.seen()) != 0 {
				t.Errorf("%s asked HQ in no application: %q", call.name, h.seen())
			}
		}
	}
}
