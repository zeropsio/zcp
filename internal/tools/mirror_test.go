// Tests for: zerops_mirror action="request" (mirror.go) — a Mate's agent asks
// the person to connect one of the application's repositories to GitHub.
package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq/hqtest"
)

// mirrorHQ is a fake HQ (not for a parallel test: the identity is the
// process environment) answering GET /api/mate/self for application a1 and
// GET /api/apps/a1/repos with repos; it records every other request it is
// sent.
func mirrorHQ(t *testing.T, repos string) (srv *mcp.Server, others func() []string) {
	t.Helper()
	var mu sync.Mutex
	var unexpected []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.Header.Get("Authorization") == "Project good" {
			switch r.URL.Path {
			case "/api/mate/self":
				_, _ = w.Write([]byte(`{"projectId":"P_MATE","name":"Ada","face":"f","standupRequestedBy":null,"appId":"a1","changes":[]}`))
				return
			case "/api/apps/a1/repos":
				_, _ = w.Write([]byte(`{"repos":` + repos + `}`))
				return
			}
		}
		mu.Lock()
		unexpected = append(unexpected, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(fake.Close)
	hqtest.Link(t, fake.URL, "P_MATE", "good")
	srv = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterMirror(srv, fake.Client(), "P_MATE")
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), unexpected...)
	}
}

// A repository with no remote is asked: the answer names it and the why for
// Mate's card, and tells the agent to say one sentence and end its turn.
// witness: internal/tools/mirror.go | Requested: &mirrorRequested{Mirror: req}, → Connected: &mirrorConnected{Mirror: ops.MirrorConnection{Repo: req.Repo}},
func TestMirrorRequest_NoRemote_AsksThePerson(t *testing.T) {
	srv, others := mirrorHQ(t, `[{"name":"app","remote":null}]`)

	result := callTool(t, srv, "zerops_mirror", map[string]any{
		"action": "request", "repo": "app", "why": "so the team reviews pull requests on GitHub",
	})

	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("want a request, got a refusal: %s", text)
	}
	want := `{"requested":{"mirror":{"repo":"app","why":"so the team reviews pull requests on GitHub"}},"nextActions":"`
	if !strings.HasPrefix(text, want) {
		t.Errorf("answer = %s, want it to start %s", text, want)
	}
	var answer mirrorAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatalf("answer is not JSON: %v\n%s", err, text)
	}
	for _, want := range []string{"connect app to GitHub, in the card above", `action="withdraw"`, "End your turn"} {
		if !strings.Contains(answer.NextActions, want) {
			t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
		}
	}
	if got := others(); len(got) != 0 {
		t.Errorf("a request sent HQ %v", got)
	}
}

// A repository that already has a remote in HQ is not asked: the answer names
// the remote.
// witness: internal/tools/mirror.go | if remote := repos[i].Remote; remote != nil { → if remote := repos[i].Remote; false && remote != nil {
func TestMirrorRequest_RepositoryWithARemote_AnswersConnected(t *testing.T) {
	srv, _ := mirrorHQ(t, `[{"name":"app","remote":{"url":"https://github.com/acme/app","via":"app"}}]`)

	result := callTool(t, srv, "zerops_mirror", map[string]any{"action": "request", "repo": "app"})

	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("want an answer, got a refusal: %s", text)
	}
	want := `{"connected":{"mirror":{"repo":"app","url":"https://github.com/acme/app"}},"nextActions":"`
	if !strings.HasPrefix(text, want) {
		t.Errorf("answer = %s, want it to start %s", text, want)
	}
}
