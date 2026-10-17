// Tests for: zerops_origin action="request" (origin.go) — a Mate's agent
// asks whether finished work goes back to the repository it came from.
package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
)

// originHQ is a fake HQ answering GET /api/mate/self with gitPushes ("" for
// an HQ older than them); it records every other request it is sent.
func originHQ(t *testing.T, gitPushes string) (srv *mcp.Server, others func() []string) {
	t.Helper()
	body := `{"projectId":"P_MATE","name":"Ada","face":"f","standupRequestedBy":null,"appId":"a1","changes":[]`
	if gitPushes != "" {
		body += `,"gitPushes":` + gitPushes
	}
	body += "}"
	var mu sync.Mutex
	var unexpected []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/mate/self" && r.Header.Get("Authorization") == "Mate good" {
			_, _ = w.Write([]byte(body))
			return
		}
		mu.Lock()
		unexpected = append(unexpected, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(fake.Close)
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := hq.SaveEnrollment(path, hq.Enrollment{HQ: fake.URL, ProjectID: "P_MATE", Credential: "good"}); err != nil {
		t.Fatal(err)
	}
	srv = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	registerOrigin(srv, fake.Client(), path, "P_MATE")
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), unexpected...)
	}
}

// A request for an origin writes nothing and names the repository the pair
// will wire.
func TestOriginRequest_WritesNothing_NamesTheRepositoryThePairWillWire(t *testing.T) {
	t.Parallel()
	srv, others := originHQ(t, `[]`)

	result := callTool(t, srv, "zerops_origin", map[string]any{
		"action": "request", "url": "https://github.com/zeropsio/mate.git", "repo": "appdev", "why": "to deliver finished work back",
	})

	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("want a request, got a refusal: %s", text)
	}
	var answer originAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatalf("answer is not JSON: %v\n%s", err, text)
	}
	if answer.Requested == nil || answer.Connected != nil {
		t.Fatalf("want requested only: %s", text)
	}
	if got := answer.Requested.Origin; got.URL != "https://github.com/zeropsio/mate" || got.Repo != "appdev" || got.Why != "to deliver finished work back" {
		t.Errorf("requested.origin = %+v, want the canonical URL, repository appdev and the why", got)
	}
	if !strings.Contains(answer.NextActions, "appdev") {
		t.Errorf("nextActions does not name the repository the pair will wire: %s", answer.NextActions)
	}
	if got := others(); len(got) != 0 {
		t.Errorf("a request sent HQ more than its read: %v", got)
	}
}

// An origin request refuses a URL with credentials.
func TestOriginRequest_URLWithCredentials_Refused(t *testing.T) {
	t.Parallel()
	for _, url := range []string{
		"https://ghp_secret123@github.com/zeropsio/mate",
		"https://x-access-token:ghp_secret123@github.com/zeropsio/mate.git",
	} {
		srv, others := originHQ(t, `[]`)

		result := callTool(t, srv, "zerops_origin", map[string]any{
			"action": "request", "url": url, "repo": "appdev", "why": "to deliver finished work back",
		})

		text := getTextContent(t, result)
		if !result.IsError || !strings.Contains(text, "INVALID_PARAMETER") {
			t.Errorf("%s: want an INVALID_PARAMETER refusal, got: %s", url, text)
		}
		if strings.Contains(text, "ghp_secret123") {
			t.Errorf("the refusal echoes the credential: %s", text)
		}
		if strings.Contains(text, `"requested"`) {
			t.Errorf("a URL with a credential was asked: %s", text)
		}
		if got := others(); len(got) != 0 {
			t.Errorf("a refused request reached HQ: %v", got)
		}
	}
}

// An origin request for an origin already connected answers connected.
func TestOriginRequest_OriginAlreadyConnected_AnswersConnected(t *testing.T) {
	t.Parallel()
	srv, _ := originHQ(t, `[{"environment":"git-prod","tier":"production","branch":"main","repo":"appdev","sha":"`+ciSha+`"},`+
		`{"environment":"git-prod","tier":"production","branch":"main","repo":"web","sha":"`+ciSha+`"}]`)

	result := callTool(t, srv, "zerops_origin", map[string]any{
		"action": "request", "url": "https://github.com/zeropsio/mate", "repo": "appdev", "why": "to deliver finished work back",
	})

	text := getTextContent(t, result)
	var answer originAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil || result.IsError {
		t.Fatalf("want an answer, got: %s", text)
	}
	if answer.Requested != nil || answer.Connected == nil {
		t.Fatalf("want connected only: %s", text)
	}
	if got := answer.Connected; got.Origin.Repo != "appdev" || len(got.Environments) != 1 || got.Environments[0] != "git-prod" {
		t.Errorf("connected = %+v, want appdev through git-prod", got)
	}

	// Another repository of the application delivering is not this one.
	result = callTool(t, srv, "zerops_origin", map[string]any{
		"action": "request", "url": "https://github.com/zeropsio/mate", "repo": "apidev", "why": "to deliver finished work back",
	})
	if text := getTextContent(t, result); !strings.Contains(text, `"requested"`) {
		t.Errorf("a repository no Git environment pushes answered: %s", text)
	}
}
