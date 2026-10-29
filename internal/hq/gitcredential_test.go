package hq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// git asks the helper with the remote's protocol and host; only the HQ this
// Mate's key names gets the Git credential its link carries, under the user
// HQ's git reads — never the key.
func TestGitCredential_AnswersOnlyTheLinkedHQ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hq      string
		linked  bool
		request string
		want    string
		wantOK  bool
	}{
		{"the enrolled HQ", "https://hq.example", true, "protocol=https\nhost=hq.example\npath=git/app/appdev.git\n\n", "secret", true},
		{"https's own port named", "https://hq.example", true, "protocol=https\nhost=hq.example:443\n\n", "secret", true},
		{"an HQ on a port of its own", "http://127.0.0.1:8080/", true, "protocol=http\nhost=127.0.0.1:8080\n\n", "secret", true},
		{"the host in another case", "https://HQ.example", true, "protocol=https\nhost=hq.EXAMPLE\n\n", "secret", true},
		{"git's other attributes ignored", "https://hq.example", true, "capability[]=authtype\nprotocol=https\nhost=hq.example\nwwwauth[]=Basic realm=\"hq\"\n\n", "secret", true},
		{"another port on the HQ's host", "https://hq.example", true, "protocol=https\nhost=hq.example:8443\n\n", "", false},
		{"the HQ's host over plain http", "https://hq.example", true, "protocol=http\nhost=hq.example\n\n", "", false},
		{"another host", "https://hq.example", true, "protocol=https\nhost=gitlab.com\npath=team/app.git\n\n", "", false},
		{"no host at all", "https://hq.example", true, "protocol=https\n\n", "", false},
		{"no Git credential linked", "https://hq.example", false, "protocol=https\nhost=hq.example\n\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := linkedCredential(t, tt.linked)
			id := Identity{Origin: tt.hq, ProjectID: "p1", Key: "the-key", GitCredentialPath: path}
			got, err := AnswerGit(context.Background(), strings.NewReader(tt.request), id, http.DefaultClient)
			if got.Password != tt.want || (err == nil) != tt.wantOK {
				t.Errorf("AnswerGit = %q, %v; want %q, ok %v", got.Password, err, tt.want, tt.wantOK)
			}
			if tt.wantOK && got.Username != GitUser {
				t.Errorf("username = %q, want %q", got.Username, GitUser)
			}
		})
	}
}

// A repository on GitHub gets the token HQ mints for it at that moment — the
// Mate's own credential never leaves for GitHub, and nothing is kept between
// calls; HQ's refusal is said in words, never answered with a password.
func TestAnswerGit_AGitHubRepositoryGetsTheTokenHQMintsForIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		request    string
		status     int
		body       string
		wantToken  string
		wantAsked  bool
		wantReason string
	}{
		{"a mounted repository gets HQ's token", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 200,
			`{"username":"x-access-token","password":"ghs_write","expiresAt":"2026-10-21T13:00:00Z"}`, "ghs_write", true, ""},
		{"a followed repository gets HQ's read token", "protocol=https\nhost=github.com\npath=acme/lib\n\n", 200,
			`{"username":"x-access-token","password":"ghs_read","expiresAt":"2026-10-21T13:00:00Z"}`, "ghs_read", true, ""},
		{"a repository HQ does not mount", "protocol=https\nhost=github.com\npath=acme/other.git\n\n", 404,
			`{"reason":"not_mounted"}`, "", true, "mounts no repository github.com/acme/other.git"},
		{"a Mate whose project access is gone", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 403,
			`{"reason":"no_access"}`, "", true, "no longer has access"},
		{"an installation that no longer reaches it", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 409,
			`{"reason":"app_not_installed"}`, "", true, "GitHub App no longer reaches"},
		{"GitHub not answering HQ", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 503,
			`{}`, "", true, "try again"},
		{"an answer with no password", "protocol=https\nhost=github.com\npath=acme/shop.git\n\n", 200,
			`{"username":"x-access-token","password":""}`, "", true, "no password"},
		{"git named no repository", "protocol=https\nhost=github.com\n\n", 200,
			`{}`, "", false, "credential.https://github.com.useHttpPath"},
		{"GitHub over plain http", "protocol=http\nhost=github.com\npath=acme/shop.git\n\n", 200,
			`{}`, "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var asked atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked.Store(true)
				if r.Method != http.MethodGet || r.URL.Path != "/api/mate/git-credential" || r.Header.Get("Authorization") != "Project secret" {
					t.Errorf("asked %s %s as %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
				}
				if got := r.URL.Query().Get("host"); got != "github.com" {
					t.Errorf("host = %q", got)
				}
				if got, want := r.URL.Query().Get("path"), requestPath(tt.request); got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)
			id := Identity{Origin: srv.URL, ProjectID: "p1", Key: "secret", GitCredentialPath: linkedCredential(t, true)}

			got, err := AnswerGit(context.Background(), strings.NewReader(tt.request), id, srv.Client())
			if asked.Load() != tt.wantAsked {
				t.Errorf("HQ asked = %v, want %v", asked.Load(), tt.wantAsked)
			}
			if got.Password != tt.wantToken {
				t.Errorf("password = %q, want %q", got.Password, tt.wantToken)
			}
			if tt.wantToken != "" {
				if err != nil || got.Username != "x-access-token" {
					t.Errorf("answer = %q, %v", got.Username, err)
				}
				return
			}
			var declined *DeclinedError
			if !errors.As(err, &declined) {
				t.Fatalf("err = %v, want a decline", err)
			}
			if !strings.Contains(declined.Reason, tt.wantReason) {
				t.Errorf("reason = %q, want it to say %q", declined.Reason, tt.wantReason)
			}
			if strings.Contains(declined.Reason, "secret") {
				t.Errorf("reason %q carries the key", declined.Reason)
			}
		})
	}
}

// git waits on the helper: an HQ that never answers, once the call is given
// up, is said as "try again" and answered with nothing.
func TestAnswerGit_AnHQThatNeverAnswersIsGivenUp(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	id := Identity{Origin: srv.URL, ProjectID: "p1", Key: "secret", GitCredentialPath: linkedCredential(t, true)}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := AnswerGit(ctx, strings.NewReader("protocol=https\nhost=github.com\npath=acme/shop.git\n\n"), id, srv.Client())
	var declined *DeclinedError
	if !errors.As(err, &declined) || !strings.Contains(declined.Reason, "try again") {
		t.Fatalf("err = %v, want a decline saying to try again", err)
	}
}

// requestPath is the path attribute of a credential request, "" without one.
func requestPath(request string) string {
	for line := range strings.SplitSeq(request, "\n") {
		if value, ok := strings.CutPrefix(line, "path="); ok {
			return value
		}
	}
	return ""
}

// The checkout's GitHub helper is the only one git asks for github.com: a
// helper the machine or the person configured (`gh auth setup-git`, a
// credential store) never answers ahead of it with the person's own token,
// nor keeps the minted one.
func TestGitHubHelperConfig_OnlyZcpAnswersGitHub(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	repo := filepath.Join(root, "repo")
	global := filepath.Join(root, "gitconfig")
	for _, dir := range []string{bin, repo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fakeZcp := "#!/bin/sh\ncat >/dev/null\necho username=x-access-token\necho password=minted\n"
	if err := os.WriteFile(filepath.Join(bin, "zcp"), []byte(fakeZcp), 0o755); err != nil {
		t.Fatal(err)
	}
	persons := "[credential]\n\thelper = \"!f() { echo username=person; echo password=persons-token; }; f\"\n"
	if err := os.WriteFile(global, []byte(persons), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"HOME="+root, "GIT_CONFIG_GLOBAL="+global, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	run := func(stdin, script string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
		cmd.Dir, cmd.Env, cmd.Stdin = repo, env, strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", script, err, out)
		}
		return string(out)
	}
	run("", "git init -q && "+GitHubHelperConfig+" && "+GitHubHelperConfig) // a second run changes nothing

	got := run("protocol=https\nhost=github.com\npath=acme/app.git\n\n", "git credential fill")
	if !strings.Contains(got, "password=minted") || strings.Contains(got, "persons-token") {
		t.Errorf("git credential fill = %q, want only the minted token", got)
	}
}

// TestRevokeGitHubToken_SendsDeleteWithTheToken: a token minted for one call
// is revoked as HQ revokes the tokens it mints — `DELETE /installation/token`
// at GitHub's API, authenticated by the token itself; GitHub's 204 and its
// 401 (already gone) end it, anything else is an error that never carries
// the token.
func TestRevokeGitHubToken_SendsDeleteWithTheToken(t *testing.T) {
	t.Parallel()
	const token = "ghs_FAKErevoke0001"
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"revoked", http.StatusNoContent, false},
		{"already gone", http.StatusUnauthorized, false},
		{"GitHub failing", http.StatusInternalServerError, true},
		{"forbidden", http.StatusForbidden, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var asked atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				asked.Add(1)
				if r.Method != http.MethodDelete || r.URL.Path != "/installation/token" {
					t.Errorf("asked %s %s, want DELETE /installation/token", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+token {
					t.Errorf("Authorization = %q, want the token as a bearer", got)
				}
				if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
					t.Errorf("Accept = %q", got)
				}
				if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
					t.Errorf("X-GitHub-Api-Version = %q", got)
				}
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)
			err := RevokeGitHubToken(context.Background(), srv.Client(), srv.URL, token)
			if (err != nil) != tc.wantErr {
				t.Errorf("RevokeGitHubToken = %v, want error %v", err, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), token) {
				t.Errorf("the error carries the token: %v", err)
			}
			if asked.Load() != 1 {
				t.Errorf("GitHub asked %d times, want once", asked.Load())
			}
		})
	}
	t.Run("GitHub unreachable", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.NotFoundHandler())
		addr := srv.URL
		srv.Close()
		err := RevokeGitHubToken(context.Background(), http.DefaultClient, addr, token)
		if err == nil || strings.Contains(err.Error(), token) {
			t.Errorf("RevokeGitHubToken against a closed server = %v, want an error without the token", err)
		}
	})
	if GitHubAPIOrigin != "https://api.github.com" {
		t.Errorf("GitHubAPIOrigin = %q, want GitHub's REST API", GitHubAPIOrigin)
	}
}

// accessHQ is an HQ minting GitHub tokens that records the access each mint
// asked for — "<none>" when the query carried no access at all — answering
// status and body.
func accessHQ(t *testing.T, status int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mate/git-credential" || r.Header.Get("Authorization") != "Project secret" {
			t.Errorf("asked %s as %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		access := "<none>"
		if values, ok := r.URL.Query()["access"]; ok {
			access = strings.Join(values, ",")
		}
		mu.Lock()
		asked = append(asked, access)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

const githubShopRequest = "protocol=https\nhost=github.com\npath=acme/shop.git\n\n"

// TestGitHubCredentialFor_SendsAccess: a token asked for read, write or
// workflows carries that access on HQ's query; none asked sends none.
func TestGitHubCredentialFor_SendsAccess(t *testing.T) {
	t.Parallel()
	for _, access := range []string{GitHubAccessRead, GitHubAccessWrite, GitHubAccessWorkflows, ""} {
		srv, asked := accessHQ(t, http.StatusOK, `{"username":"x-access-token","password":"ghs_FAKEaccess"}`)
		id := Identity{Origin: srv.URL, ProjectID: "p1", Key: "secret", GitCredentialPath: linkedCredential(t, true)}
		client, err := Open(srv.Client(), id)
		if err != nil {
			t.Fatal(err)
		}
		got, err := client.GitHubCredentialFor(context.Background(), "acme/shop.git", access)
		if err != nil || got.Password != "ghs_FAKEaccess" {
			t.Errorf("GitHubCredentialFor(%q) = %v, %v", access, got.Username, err)
		}
		want := access
		if access == "" {
			want = "<none>"
		}
		if len(*asked) != 1 || (*asked)[0] != want {
			t.Errorf("GitHubCredentialFor(%q) asked HQ with access %q, want %q", access, *asked, want)
		}
		answer, err := AnswerGitFor(context.Background(), strings.NewReader(githubShopRequest), id, srv.Client(), access)
		if err != nil || answer.Password != "ghs_FAKEaccess" {
			t.Errorf("AnswerGitFor(%q) = %v", access, err)
		}
		if len(*asked) != 2 || (*asked)[1] != want {
			t.Errorf("AnswerGitFor(%q) asked HQ with access %q, want %q", access, *asked, want)
		}
	}
}

// TestGitHubCredentialFor_BadAccess_Declined: HQ refusing the access asked
// (400 bad_access) is a decline naming it — never a password.
func TestGitHubCredentialFor_BadAccess_Declined(t *testing.T) {
	t.Parallel()
	srv, asked := accessHQ(t, http.StatusBadRequest, `{"code":"git_credential_refused","reason":"bad_access"}`)
	id := Identity{Origin: srv.URL, ProjectID: "p1", Key: "secret", GitCredentialPath: linkedCredential(t, true)}
	client, err := Open(srv.Client(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GitHubCredentialFor(context.Background(), "acme/shop.git", GitHubAccessRead); !refusedAs(err, "git_credential_refused") {
		t.Errorf("GitHubCredentialFor = %v, want HQ's refusal", err)
	}
	got, err := AnswerGitFor(context.Background(), strings.NewReader(githubShopRequest), id, srv.Client(), GitHubAccessRead)
	var declined *DeclinedError
	if !errors.As(err, &declined) || !strings.Contains(declined.Reason, "bad_access") || got.Password != "" {
		t.Errorf("AnswerGitFor = %q, %v; want a decline naming bad_access", got.Password, err)
	}
	if len(*asked) != 2 {
		t.Errorf("HQ asked %d times, want once per call", len(*asked))
	}
}

// TestGitHubCredentialFor_ReadOnlyRemote_DeclinedNamingIt: a push to a
// remote HQ keeps connected with a person's token (409 read_only_remote) is
// declined saying the repository is read-only for this Mate — and HQ is not
// asked again with another access.
func TestGitHubCredentialFor_ReadOnlyRemote_DeclinedNamingIt(t *testing.T) {
	t.Parallel()
	for _, access := range []string{GitHubAccessWrite, GitHubAccessWorkflows} {
		srv, asked := accessHQ(t, http.StatusConflict, `{"code":"git_credential_refused","reason":"read_only_remote"}`)
		id := Identity{Origin: srv.URL, ProjectID: "p1", Key: "secret", GitCredentialPath: linkedCredential(t, true)}
		got, err := AnswerGitFor(context.Background(), strings.NewReader(githubShopRequest), id, srv.Client(), access)
		var declined *DeclinedError
		if !errors.As(err, &declined) || got.Password != "" {
			t.Fatalf("AnswerGitFor(%s) = %q, %v; want a decline", access, got.Password, err)
		}
		if !strings.Contains(declined.Reason, "read-only for this Mate") || !strings.Contains(declined.Reason, "github.com/acme/shop.git") {
			t.Errorf("AnswerGitFor(%s) reason = %q, want it to name the repository as read-only for this Mate", access, declined.Reason)
		}
		if !slices.Equal(*asked, []string{access}) {
			t.Errorf("HQ asked with %q, want once with %q and no retry", *asked, access)
		}
	}
}

// TestAnswerGit_StillSendsNoAccess: the `zcp hq git-credential` fallback
// asks HQ for today's token, with no access on the query.
func TestAnswerGit_StillSendsNoAccess(t *testing.T) {
	t.Parallel()
	srv, asked := accessHQ(t, http.StatusOK, `{"username":"x-access-token","password":"ghs_FAKEbroad"}`)
	id := Identity{Origin: srv.URL, ProjectID: "p1", Key: "secret", GitCredentialPath: linkedCredential(t, true)}
	got, err := AnswerGit(context.Background(), strings.NewReader(githubShopRequest), id, srv.Client())
	if err != nil || got.Password != "ghs_FAKEbroad" {
		t.Fatalf("AnswerGit = %v", err)
	}
	if !slices.Equal(*asked, []string{"<none>"}) {
		t.Errorf("AnswerGit asked HQ with access %q, want none", *asked)
	}
}

// linkedCredential is the path of the Git credential file the Mate's server
// would write: holding "secret" when linked, absent otherwise.
func linkedCredential(t *testing.T, linked bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "git-credential")
	if linked {
		if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
