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
	"github.com/zeropsio/zcp/internal/workflow"
)

// originHQ is a fake HQ answering GET /api/mate/self with repos ("" for an
// HQ older than them); it records every other request it is sent.
func originHQ(t *testing.T, repos string) (srv *mcp.Server, others func() []string) {
	t.Helper()
	srv, others, _ = originHQIn(t, t.TempDir(), repos)
	return srv, others
}

// originHQIn is originHQ for a project whose service metas live in stateDir;
// it also returns the fake HQ's address.
func originHQIn(t *testing.T, stateDir, repos string) (srv *mcp.Server, others func() []string, address string) {
	t.Helper()
	body := `{"projectId":"P_MATE","name":"Ada","face":"f","standupRequestedBy":null,"appId":"a1","changes":[]`
	if repos != "" {
		body += `,"repos":` + repos
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
	registerOrigin(srv, fake.Client(), path, "P_MATE", stateDir)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), unexpected...)
	}, fake.URL
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
	for _, want := range []string{
		"Mate asks where the code of appdev is kept — on GitHub at https://github.com/zeropsio/mate, or in Mate, only following it",
		"Never ask for a token in the chat",
		"Either way take the code from HQ first: " + originFromHQ("/var/www/appdev"),
		"Kept on GitHub: ZCP points the checkout's origin at GitHub, never a second remote; work on it in your own shell, never over ssh: `cd /var/www/appdev && git fetch origin`",
		"`git push origin <branch>`, and open its change with `zerops_workflow action=\"open-change\"",
		"HQ opens its pull request on GitHub from that branch and the person merges it in Mate",
		"Never push `main` and never force-push",
		"Kept in Mate: HQ stays the origin and GitHub is only followed — in your own shell, never over ssh: `cd /var/www/appdev && git remote add upstream https://github.com/zeropsio/mate && git remote set-url --push upstream no_push && " + hq.GitHubHelperConfig + " && git fetch upstream`",
		"a private one is read with HQ's read token for it, and when HQ holds none, say so",
	} {
		if !strings.Contains(answer.NextActions, want) {
			t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
		}
	}
	if strings.Contains(answer.NextActions, "allow-unrelated-histories") {
		t.Errorf("nextActions joins unrelated histories: %s", answer.NextActions)
	}
	// The wiring is the one writer of a wired checkout's origin.
	if strings.Contains(answer.NextActions, "set-url origin") {
		t.Errorf("nextActions has the agent point origin itself: %s", answer.NextActions)
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

// A refused origin request never echoes the URL: not a token in its query or
// fragment, nor a credential in an address that does not parse.
func TestOriginRequest_RefusalNeverEchoesTheURL(t *testing.T) {
	t.Parallel()
	for _, url := range []string{
		"https://gitlab.com/acme/app?private_token=glpat-SECRET",
		"https://github.com/acme/app#SECRET",
		"https://u:tok-SECRET@github.com/acme/%zz",
		"https://SECRET@github.com/acme/app",
		"http://github.com/acme/SECRET",
		"https://github.com/SECRET",
	} {
		srv, others := originHQ(t, `[]`)

		result := callTool(t, srv, "zerops_origin", map[string]any{
			"action": "request", "url": url, "repo": "appdev", "why": "to deliver finished work back",
		})

		text := getTextContent(t, result)
		if !result.IsError || !strings.Contains(text, "INVALID_PARAMETER") {
			t.Errorf("%s: want an INVALID_PARAMETER refusal, got: %s", url, text)
		}
		if strings.Contains(text, "SECRET") {
			t.Errorf("%s: the refusal echoes the URL: %s", url, text)
		}
		if got := others(); len(got) != 0 {
			t.Errorf("%s: a refused request reached HQ: %v", url, got)
		}
	}
}

// An origin request refuses an address past the repository, such as a page
// inside it, and says to give the repository's own address.
func TestOriginRequest_AddressPastTheRepository_RefusedAskingForTheRepositoryAddress(t *testing.T) {
	t.Parallel()
	for _, url := range []string{
		"https://github.com/acme/app/tree/develop",
		"https://github.com/acme/app/pull/7",
		"https://github.com/acme/app.git/info",
	} {
		srv, others := originHQ(t, `[]`)

		result := callTool(t, srv, "zerops_origin", map[string]any{
			"action": "request", "url": url, "repo": "appdev", "why": "to deliver finished work back",
		})

		text := getTextContent(t, result)
		if !result.IsError || !strings.Contains(text, "INVALID_PARAMETER") || strings.Contains(text, `"requested"`) {
			t.Errorf("%s: want an INVALID_PARAMETER refusal, got: %s", url, text)
		}
		if !strings.Contains(text, "give the repository address, e.g. https://github.com/acme/app") {
			t.Errorf("%s: the refusal does not say to give the repository address: %s", url, text)
		}
		if got := others(); len(got) != 0 {
			t.Errorf("%s: a refused request reached HQ: %v", url, got)
		}
	}
}

// An origin request for an origin already connected answers connected.
func TestOriginRequest_OriginAlreadyConnected_AnswersConnected(t *testing.T) {
	t.Parallel()
	srv, _ := originHQ(t,
		`[{"name":"appdev","origin":{"url":"https://github.com/zeropsio/mate"}},{"name":"web","origin":{"url":"https://github.com/zeropsio/web"}}]`)

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
	if got := answer.Connected; got.Origin.Repo != "appdev" || got.Repo != "appdev" {
		t.Errorf("connected = %+v, want appdev's own origin", got)
	}

	// Another repository's origin is not this one.
	result = callTool(t, srv, "zerops_origin", map[string]any{
		"action": "request", "url": "https://github.com/zeropsio/api", "repo": "apidev", "why": "to deliver finished work back",
	})
	if text := getTextContent(t, result); !strings.Contains(text, `"requested"`) {
		t.Errorf("a repository no origin is connected to answered: %s", text)
	}
}

// originAnswerTo asks zerops_origin for url under appdev and decodes its
// answer.
func originAnswerTo(t *testing.T, srv *mcp.Server, url string) (originAnswer, string) {
	t.Helper()
	result := callTool(t, srv, "zerops_origin", map[string]any{
		"action": "request", "url": url, "repo": "appdev", "why": "to deliver finished work back",
	})
	text := getTextContent(t, result)
	var answer originAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil || result.IsError {
		t.Fatalf("want an answer, got: %s", text)
	}
	return answer, text
}

// An origin already connected to the repository answers connected.
func TestOriginRequest_OriginConnectedToTheRepository_AnswersConnected(t *testing.T) {
	t.Parallel()
	srv, others := originHQ(t, `[{"name":"appdev","origin":{"url":"https://github.com/Acme/App"}},{"name":"web"}]`)

	answer, text := originAnswerTo(t, srv, "https://github.com/acme/app.git/")

	if answer.Requested != nil || answer.Connected == nil {
		t.Fatalf("want connected only: %s", text)
	}
	if got := answer.Connected; got.Repo != "appdev" || got.Origin.Repo != "appdev" || got.Origin.URL != "https://github.com/acme/app" {
		t.Errorf("connected = %+v, want https://github.com/acme/app held by appdev", got)
	}
	for _, want := range []string{
		"Nothing was asked: https://github.com/acme/app is already the origin of appdev in HQ.",
		"never ask for a token",
		originFromHQ("/var/www/appdev"),
		"HQ does not say which way the person keeps it: ask them in one sentence",
		"Kept on GitHub: ZCP points the checkout's origin at GitHub",
		"Kept in Mate: HQ stays the origin and GitHub is only followed",
	} {
		if !strings.Contains(answer.NextActions, want) {
			t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
		}
	}
	if got := others(); len(got) != 0 {
		t.Errorf("a request sent HQ more than its read: %v", got)
	}

	// Another address for the same repository is asked.
	if answer, text := originAnswerTo(t, srv, "https://github.com/acme/api"); answer.Requested == nil || answer.Connected != nil {
		t.Errorf("an address that is not appdev's origin answered: %s", text)
	}
}

// The same origin connected under another repository names that repository.
func TestOriginRequest_OriginConnectedUnderAnotherRepository_NamesThatRepository(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{Hostname: "web", BootstrapSession: "test", BootstrappedAt: "2026-10-17"}); err != nil {
		t.Fatal(err)
	}
	srv, _, _ := originHQIn(t, stateDir,
		`[{"name":"appdev"},{"name":"web","origin":{"url":"https://github.com/acme/app"}}]`)

	answer, text := originAnswerTo(t, srv, "https://github.com/acme/app")

	if answer.Requested != nil || answer.Connected == nil {
		t.Fatalf("want connected only: %s", text)
	}
	if got := answer.Connected; got.Repo != "web" || got.Origin.Repo != "appdev" {
		t.Errorf("connected = %+v, want the origin held by web", got)
	}
	for _, want := range []string{
		"Nothing was asked: https://github.com/acme/app is already the origin of web in HQ, not of appdev.",
		"Its code comes from that repository, web",
		"never ask for a token",
		originFromHQ("/var/www/web"),
	} {
		if !strings.Contains(answer.NextActions, want) {
			t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
		}
	}
}

// An older HQ without repos asks: nothing says which origin is connected.
func TestOriginRequest_OlderHQWithoutRepos_Asks(t *testing.T) {
	t.Parallel()
	older, _ := originHQ(t, "")

	answer, text := originAnswerTo(t, older, "https://github.com/acme/app")

	if answer.Requested == nil || answer.Connected != nil {
		t.Errorf("an older HQ: want requested, got: %s", text)
	}
}

// The same origin connected under a repository that is no service of this
// project — a sibling Mate's — names no host for it: its code is fetched from
// HQ into the asked repository.
func TestOriginRequest_OriginConnectedUnderAnotherProjectsRepository_FetchesItFromHQ(t *testing.T) {
	t.Parallel()
	srv, _, address := originHQIn(t, t.TempDir(),
		`[{"name":"appdev"},{"name":"web","origin":{"url":"https://github.com/acme/app"}}]`)

	answer, text := originAnswerTo(t, srv, "https://github.com/acme/app")

	if answer.Requested != nil || answer.Connected == nil || answer.Connected.Repo != "web" {
		t.Fatalf("want connected, held by web: %s", text)
	}
	if strings.Contains(answer.NextActions, "ssh web") {
		t.Errorf("nextActions names web, no service of this project, as a host: %s", answer.NextActions)
	}
	want := originTake("/var/www/appdev", "git fetch "+address+"/git/a1/web.git main")
	if !strings.Contains(answer.NextActions, want) {
		t.Errorf("nextActions lacks the fetch from HQ %s: %s", want, answer.NextActions)
	}
}

// The same origin connected under a sibling Mate's repository whose code the
// person keeps on GitHub is fetched from GitHub, where that code is, through
// zcp's GitHub helper.
func TestOriginRequest_OriginKeptOnGitHubUnderAnotherProjectsRepository_FetchesItFromGitHub(t *testing.T) {
	t.Parallel()
	srv, _, address := originHQIn(t, t.TempDir(),
		`[{"name":"appdev"},{"name":"web","origin":{"url":"https://github.com/acme/app","mode":"github"}}]`)

	answer, text := originAnswerTo(t, srv, "https://github.com/acme/app")

	if answer.Connected == nil || answer.Connected.Repo != "web" {
		t.Fatalf("want connected, held by web: %s", text)
	}
	want := originTake("/var/www/appdev", hq.GitHubHelperConfig+" && git fetch https://github.com/acme/app main")
	if !strings.Contains(answer.NextActions, want) {
		t.Errorf("nextActions lacks the fetch from GitHub %s: %s", want, answer.NextActions)
	}
	if strings.Contains(answer.NextActions, address) {
		t.Errorf("nextActions fetches from HQ code kept on GitHub: %s", answer.NextActions)
	}
}

// A connected origin's checkout is set up the way HQ says the person keeps
// its code, and only that way.
func TestOriginRequest_OriginConnectedToTheRepository_SetsUpTheWayHQSaysItIsKept(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode        string
		want, never string
	}{
		{mode: "github", want: "Kept on GitHub: ZCP points the checkout's origin at GitHub", never: "Kept in Mate"},
		{mode: "follow", want: "Kept in Mate: HQ stays the origin and GitHub is only followed", never: "Kept on GitHub"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			srv, _ := originHQ(t, `[{"name":"appdev","origin":{"url":"https://github.com/acme/app","mode":"`+tt.mode+`"}}]`)

			answer, text := originAnswerTo(t, srv, "https://github.com/acme/app")

			if answer.Connected == nil {
				t.Fatalf("want connected: %s", text)
			}
			for _, want := range []string{tt.want, originFromHQ("/var/www/appdev")} {
				if !strings.Contains(answer.NextActions, want) {
					t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
				}
			}
			for _, never := range []string{tt.never, "ask them"} {
				if strings.Contains(answer.NextActions, never) {
					t.Errorf("nextActions says %q though HQ says how the code is kept: %s", never, answer.NextActions)
				}
			}
		})
	}
}

// A code-volume pair's checkout is the volume below the dev service's mount,
// never the runtime's own /var/www: the origin's commands run there.
func TestOriginRequest_CodeVolumePair_RunsInTheVolumeCheckout(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeVariantBPair(t, stateDir)
	srv, _, _ := originHQIn(t, stateDir, `[]`)

	result := callTool(t, srv, "zerops_origin", map[string]any{
		"action": "request", "url": "https://github.com/acme/app", "repo": "webdev", "why": "to deliver finished work back",
	})

	var answer originAnswer
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &answer); err != nil || result.IsError {
		t.Fatalf("want an answer, got: %s", getTextContent(t, result))
	}
	text := answer.NextActions
	for _, want := range []string{"`cd /var/www/webdev/ws && git fetch origin main", "`cd /var/www/webdev/ws && git fetch origin`", "`cd /var/www/webdev/ws && git remote add upstream"} {
		if !strings.Contains(text, want) {
			t.Errorf("answer lacks %q: %s", want, text)
		}
	}
	if strings.Contains(text, "cd /var/www/webdev &&") {
		t.Errorf("answer runs git in the runtime's own /var/www: %s", text)
	}
}

// A repository outside GitHub is only ever followed: HQ mounts and mints
// tokens for GitHub alone, so the ask offers no push back and no GitHub
// helper.
func TestOriginRequest_RepositoryOutsideGitHub_IsOnlyFollowed(t *testing.T) {
	t.Parallel()
	srv, _ := originHQ(t, `[]`)

	answer, text := originAnswerTo(t, srv, "https://gitlab.com/acme/app")

	if answer.Requested == nil || answer.Connected != nil {
		t.Fatalf("want requested only: %s", text)
	}
	for _, want := range []string{
		"Mate asks whether appdev follows https://gitlab.com/acme/app — a repository outside GitHub is only followed, never pushed to",
		"`cd /var/www/appdev && git remote add upstream https://gitlab.com/acme/app && git remote set-url --push upstream no_push && git fetch upstream`",
		"only a public one can be fetched",
	} {
		if !strings.Contains(answer.NextActions, want) {
			t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
		}
	}
	for _, never := range []string{"Kept on GitHub", "zcp hq git-credential", "gh pr create", "set-url origin"} {
		if strings.Contains(answer.NextActions, never) {
			t.Errorf("nextActions offers %q for a repository outside GitHub: %s", never, answer.NextActions)
		}
	}
}
