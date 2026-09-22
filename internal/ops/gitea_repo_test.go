// Tests for: ops/gitea_repo.go — the broker's repository call, the Mate's
// branch, and the idempotent pull request (guide 2.1, 2.3).
package ops

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGiteaWiring_ReadyAndMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		env         map[string]string
		wantReady   bool
		wantMissing []string
	}{
		{
			name:      "all three",
			env:       map[string]string{GiteaURLEnvKey: "https://g", MateBrokerURLEnvKey: "https://b", GiteaTokenEnvKey: "t"},
			wantReady: true,
		},
		{
			name:        "nothing yet",
			env:         map[string]string{},
			wantMissing: []string{GiteaURLEnvKey, MateBrokerURLEnvKey, GiteaTokenEnvKey},
		},
		{
			// The app writes the three in one pass, but they reach a
			// container's env store as separate entries: a partial read is a
			// real state, and acting on it would half-configure a pair.
			name:        "token has not landed",
			env:         map[string]string{GiteaURLEnvKey: "https://g", MateBrokerURLEnvKey: "https://b"},
			wantMissing: []string{GiteaTokenEnvKey},
		},
		{
			name:        "whitespace is not a value",
			env:         map[string]string{GiteaURLEnvKey: "  ", MateBrokerURLEnvKey: "https://b", GiteaTokenEnvKey: "t"},
			wantMissing: []string{GiteaURLEnvKey},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := ReadGiteaWiring(func(k string) string { return tt.env[k] })
			if got := w.Ready(); got != tt.wantReady {
				t.Errorf("Ready() = %v, want %v", got, tt.wantReady)
			}
			got := strings.Join(w.MissingKeys(), ",")
			want := strings.Join(tt.wantMissing, ",")
			if got != want {
				t.Errorf("MissingKeys() = %q, want %q", got, want)
			}
		})
	}
}

func TestReadGiteaWiring_NilLookup(t *testing.T) {
	t.Parallel()
	if w := ReadGiteaWiring(nil); w.Ready() {
		t.Errorf("a nil lookup must read as nothing present, got %+v", w)
	}
}

func TestGiteaMateBranch(t *testing.T) {
	t.Parallel()
	if got := GiteaMateBranch("mate-p1"); got != "mate/mate-p1" {
		t.Errorf("GiteaMateBranch = %q, want mate/mate-p1", got)
	}
	if got := GiteaMateBranch(""); got != "" {
		t.Errorf("an unknown bot has no branch, got %q", got)
	}
}

// TestRequestMateRepository pins the broker contract (gitea-mate
// docs/broker-api.md): the bot's token in Gitea's `token` scheme, only a name
// in the body, and 409/403 as distinguishable, REPORTABLE outcomes — a Mate
// whose repository the broker refuses still has a working dev pair.
func TestRequestMateRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		body      string
		wantErr   error
		wantClone string
		wantMade  bool
	}{
		{
			name:      "created",
			status:    http.StatusOK,
			body:      `{"fullName":"acme/api","cloneUrl":"https://gitea.example/acme/api","defaultBranch":"main","created":true}`,
			wantClone: "https://gitea.example/acme/api",
			wantMade:  true,
		},
		{
			name:      "already ours",
			status:    http.StatusOK,
			body:      `{"fullName":"acme/api","cloneUrl":"https://gitea.example/acme/api","defaultBranch":"main","created":false}`,
			wantClone: "https://gitea.example/acme/api",
		},
		{
			name:      "no default branch named",
			status:    http.StatusOK,
			body:      `{"fullName":"acme/api","cloneUrl":"https://gitea.example/acme/api"}`,
			wantClone: "https://gitea.example/acme/api",
		},
		{name: "taken", status: http.StatusConflict, body: `{"error":"taken"}`, wantErr: ErrRepositoryTaken},
		{name: "not registered", status: http.StatusForbidden, body: `{"error":"not_registered"}`, wantErr: ErrNotRegistered},
		{name: "not a bot", status: http.StatusUnauthorized, body: `{"error":"not_a_bot"}`},
		{name: "broker down", status: http.StatusBadGateway, body: `<html>502</html>`},
		{name: "answer is not JSON", status: http.StatusOK, body: `<html>a proxy page</html>`},
		{name: "answer names no repository", status: http.StatusOK, body: `{"created":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotAuth, gotPath, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				gotPath = r.URL.Path
				buf := make([]byte, r.ContentLength)
				_, _ = r.Body.Read(buf)
				gotBody = string(buf)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			repo, err := RequestMateRepository(context.Background(), srv.Client(), srv.URL+"/", "bot-token", "api")

			if gotAuth != "token bot-token" {
				t.Errorf("Authorization = %q, want %q", gotAuth, "token bot-token")
			}
			if gotPath != "/mate/repository" {
				t.Errorf("path = %q, want /mate/repository", gotPath)
			}
			if gotBody != `{"name":"api"}` {
				t.Errorf("body = %q, want {\"name\":\"api\"}", gotBody)
			}

			if tt.wantClone == "" {
				if err == nil {
					t.Fatalf("expected an error, got %+v", repo)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("error = %v, want %v", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "bot-token") {
					t.Errorf("error leaked the token: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("RequestMateRepository: %v", err)
			}
			if repo.CloneURL != tt.wantClone {
				t.Errorf("CloneURL = %q, want %q", repo.CloneURL, tt.wantClone)
			}
			if repo.Created != tt.wantMade {
				t.Errorf("Created = %v, want %v", repo.Created, tt.wantMade)
			}
			if repo.DefaultBranch != "main" {
				t.Errorf("DefaultBranch = %q, want main (the contract's default)", repo.DefaultBranch)
			}
		})
	}
}

func TestRequestMateRepository_Degenerate(t *testing.T) {
	t.Parallel()
	if _, err := RequestMateRepository(context.Background(), nil, "https://b", "t", "api"); err == nil {
		t.Error("a nil HTTP client must be an error, not a panic")
	}
	if _, err := RequestMateRepository(context.Background(), http.DefaultClient, "", "t", "api"); err == nil {
		t.Error("an unset broker URL must be an error")
	}
	if _, err := RequestMateRepository(context.Background(), http.DefaultClient, "https://b", "t", " "); err == nil {
		t.Error("an empty name must be an error")
	}
}

// TestEnsureGiteaPullRequest pins A5's landing path: the Mate pushes its own
// branch and lands through a pull request, and running the step again finds
// the one it already opened instead of piling up duplicates.
func TestEnsureGiteaPullRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		openList    string
		createCode  int
		wantNumber  int
		wantCreated bool
		wantPosts   int
		wantErr     bool
	}{
		{
			name:        "none open yet",
			openList:    `[]`,
			createCode:  http.StatusCreated,
			wantNumber:  7,
			wantCreated: true,
			wantPosts:   1,
		},
		{
			name:       "ours is already open",
			openList:   `[{"number":4,"state":"open","head":{"ref":"mate/mate-p1","repo":{"full_name":"acme/api"}},"base":{"ref":"main"}}]`,
			wantNumber: 4,
		},
		{
			name:       "someone else's branch is open, ours is not",
			openList:   `[{"number":4,"state":"open","head":{"ref":"mate/other","repo":{"full_name":"acme/api"}},"base":{"ref":"main"}}]`,
			createCode: http.StatusCreated,
			wantNumber: 7, wantCreated: true, wantPosts: 1,
		},
		{
			// A repository's own pull requests are same-repo; one whose head
			// repo is gone (a deleted fork) is not ours to reuse.
			name:       "our branch name, another repository's head",
			openList:   `[{"number":4,"state":"open","head":{"ref":"mate/mate-p1","repo":null},"base":{"ref":"main"}}]`,
			createCode: http.StatusCreated,
			wantNumber: 7, wantCreated: true, wantPosts: 1,
		},
		{
			// A concurrent pass opened it between the list and the create.
			name:       "raced",
			openList:   `[]`,
			createCode: http.StatusConflict,
			wantNumber: 0,
			wantPosts:  1,
		},
		{
			name:       "gitea refuses the create",
			openList:   `[]`,
			createCode: http.StatusUnprocessableEntity,
			wantPosts:  1,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var posts int
			var createBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "token bot-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.URL.Path != "/api/v1/repos/acme/api/pulls" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.Method == http.MethodPost {
					posts++
					buf := make([]byte, r.ContentLength)
					_, _ = r.Body.Read(buf)
					createBody = string(buf)
					w.WriteHeader(tt.createCode)
					_, _ = w.Write([]byte(`{"number":7,"state":"open"}`))
					return
				}
				_, _ = w.Write([]byte(tt.openList))
			}))
			defer srv.Close()

			number, created, err := EnsureGiteaPullRequest(
				context.Background(), srv.Client(), srv.URL, "bot-token",
				"acme/api", "acme/api", "mate/mate-p1", "main", "Mate: api",
			)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got number=%d created=%v", number, created)
				}
				return
			}
			if err != nil {
				t.Fatalf("EnsureGiteaPullRequest: %v", err)
			}
			if number != tt.wantNumber || created != tt.wantCreated {
				t.Errorf("= (%d, %v), want (%d, %v)", number, created, tt.wantNumber, tt.wantCreated)
			}
			if posts != tt.wantPosts {
				t.Errorf("POST /pulls count = %d, want %d", posts, tt.wantPosts)
			}
			if tt.wantPosts > 0 && !strings.Contains(createBody, `"base":"main"`) {
				t.Errorf("create body must target main: %q", createBody)
			}
		})
	}
}

func TestEnsureGiteaPullRequest_Degenerate(t *testing.T) {
	t.Parallel()
	if _, _, err := EnsureGiteaPullRequest(context.Background(), nil, "https://g", "t", "acme/api", "acme/api", "b", "main", "t"); err == nil {
		t.Error("a nil HTTP client must be an error, not a panic")
	}
	if _, _, err := EnsureGiteaPullRequest(context.Background(), http.DefaultClient, "", "t", "acme/api", "acme/api", "b", "main", "t"); err == nil {
		t.Error("an unset GITEA_URL must be an error")
	}
	if _, _, err := EnsureGiteaPullRequest(context.Background(), http.DefaultClient, "https://g", "t", "acme/api", "acme/api", "", "main", "t"); err == nil {
		t.Error("an empty head must be an error")
	}
}

func TestReadGiteaPullRequestOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantOpen   bool
		wantMerged bool
		wantErr    bool
	}{
		{
			name:     "still waiting on somebody",
			status:   http.StatusOK,
			body:     `{"number":4,"state":"open","merged":false}`,
			wantOpen: true,
		},
		{
			// The whole point: the work landed, and nothing in this process
			// did the merging or was told about it.
			name:       "merged",
			status:     http.StatusOK,
			body:       `{"number":4,"state":"closed","merged":true}`,
			wantMerged: true,
		},
		{
			// Closed and merged mean opposite things to the Mate that opened
			// it — work delivered against work refused.
			name:   "closed without merging",
			status: http.StatusOK,
			body:   `{"number":4,"state":"closed","merged":false}`,
		},
		{
			name:   "gitea spells the state in capitals",
			status: http.StatusOK,
			body:   `{"number":4,"state":"OPEN","merged":false}`, wantOpen: true,
		},
		{
			// A request somebody deleted is not a failure to read.
			name:   "gone",
			status: http.StatusNotFound,
			body:   `{}`,
		},
		{
			name:    "gitea refuses the read",
			status:  http.StatusInternalServerError,
			body:    `{}`,
			wantErr: true,
		},
		{
			name:    "not valid JSON",
			status:  http.StatusOK,
			body:    `<html>`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/repos/acme/api/pulls/4" || r.Method != http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			got, err := ReadGiteaPullRequestOutcome(
				context.Background(), srv.Client(), srv.URL, "bot-token", "acme/api", 4)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Open != tt.wantOpen || got.Merged != tt.wantMerged {
				t.Errorf("outcome = %+v, want open=%v merged=%v", got, tt.wantOpen, tt.wantMerged)
			}
		})
	}
}

func TestReadGiteaPullRequestOutcome_NeedsARepositoryAndANumber(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no request should be made without a repository and a number")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		name     string
		fullName string
		number   int
	}{
		{name: "no repository", number: 4},
		{name: "no number", fullName: "acme/api"},
		{name: "a number Gitea never issues", fullName: "acme/api", number: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ReadGiteaPullRequestOutcome(
				context.Background(), srv.Client(), srv.URL, "tok", tc.fullName, tc.number); err == nil {
				t.Error("want an error, got none")
			}
		})
	}
}
