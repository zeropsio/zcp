package hq

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestProjectClient_ProjectRoutes_UseProjectKeyAndAssertIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, method, path, body, answer string
		run                              func(*testing.T, ProjectClient) error
	}{
		{"self", "GET", "/api/project/self?projectId=p%2Fone", "", `{"orgId":"org-hq","projectId":"p/one","appId":"app-hq"}`, func(t *testing.T, c ProjectClient) error {
			t.Helper()
			got, err := c.Self(context.Background())
			if err == nil && got != (ProjectIdentity{OrgID: "org-hq", ProjectID: "p/one", AppID: "app-hq"}) {
				t.Errorf("identity = %+v", got)
			}
			return err
		}},
		{"repo", "POST", "/api/project/repos", `{"name":"appdev","projectId":"p/one"}`, `{"appId":"app-hq","name":"appdev"}`, func(t *testing.T, c ProjectClient) error {
			t.Helper()
			got, err := c.EnsureRepo(context.Background(), "appdev")
			if err == nil && got != (Repo{AppID: "app-hq", Name: "appdev"}) {
				t.Errorf("repo = %+v", got)
			}
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			count := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				raw, _ := io.ReadAll(r.Body)
				if r.Method != tt.method || r.URL.RequestURI() != tt.path || string(raw) != tt.body {
					t.Errorf("request = %s %s %s", r.Method, r.URL.RequestURI(), raw)
				}
				if r.Header.Get("Authorization") != "Project test-key" {
					t.Error("wrong project authorization")
				}
				if tt.body != "" && r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing JSON content type")
				}
				_, _ = io.WriteString(w, tt.answer)
			}))
			defer srv.Close()
			c, err := OpenProject(srv.Client(), Identity{Origin: srv.URL, Key: "test-key", ProjectID: "p/one", OrgID: "untrusted-org"})
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.run(t, c); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Errorf("calls = %d, want 1", count)
			}
		})
	}
}

func TestProjectClient_Refusals_ReportOnceAndRecheckOnlyRefusedKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		code    string
		recheck bool
	}{
		{"missing key", 401, "project_key_required", false}, {"refused key", 401, "key_refused", true},
		{"wide key", 403, "key_too_wide", false}, {"public key", 403, "key_not_sensitive", false},
		{"mate project", 403, "not_plain", false}, {"assertion mismatch", 403, "zerops_refused", false},
		{"gone enrollment", 404, "project_gone", false}, {"gone repository", 404, "project_not_found", false},
		{"invalid repo", 400, "invalid", false}, {"Zerops unavailable", 503, "zerops_unavailable", false},
		{"Zerops unexpected", 502, "zerops_unexpected", false}, {"refusal code with different status", 403, "key_refused", false},
	}
	for _, tt := range tests {
		for _, op := range []string{"self", "repo", "git"} {
			t.Run(tt.name+"/"+op, func(t *testing.T) {
				t.Parallel()
				var calls []string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls = append(calls, r.URL.Path)
					if r.Header.Get("Authorization") != "Project test-key" {
						t.Error("wrong project authorization")
					}
					if r.URL.Path == "/api/project/key-recheck" {
						if r.Method != http.MethodPost {
							t.Error("recheck is not POST")
						}
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, `{"code":"key_refused"}`)
						return
					}
					w.Header().Set("Retry-After", "10")
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, `{"code":"`+tt.code+`","reason":"reason"}`)
				}))
				defer srv.Close()
				c, err := OpenProject(srv.Client(), Identity{Origin: srv.URL, Key: "test-key", ProjectID: "p"})
				if err != nil {
					t.Fatal(err)
				}
				switch op {
				case "self":
					_, err = c.Self(context.Background())
				case "repo":
					_, err = c.EnsureRepo(context.Background(), "appdev")
				case "git":
					err = c.ProbeRepo(context.Background(), srv.URL+"/git/org/app/appdev.git")
				}
				if err == nil {
					t.Fatal("expected refusal")
				}
				if strings.Contains(err.Error(), "test-key") {
					t.Fatal("error contains credential")
				}
				var refused *RefusedError
				var unavailable *UnavailableError
				if tt.status == 503 {
					if !errors.As(err, &unavailable) || unavailable.Code != tt.code {
						t.Errorf("unavailable = %v", err)
					}
				} else if !errors.As(err, &refused) || refused.Status != tt.status || refused.Code != tt.code || refused.Reason != "reason" {
					t.Errorf("refusal = %v", err)
				}
				want := 1
				if tt.recheck {
					want = 2
				}
				if len(calls) != want {
					t.Errorf("calls = %v, want %d", calls, want)
				}
			})
		}
	}
}

func TestProjectClient_RecheckKey_ReportsVerdictWithoutRecursion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status int
		body   string
		want   KeyVerdict
		fails  bool
	}{
		{204, "", KeyHeld, false}, {401, `{"code":"key_refused"}`, KeyRefused, false},
		{401, `{"code":"project_key_required"}`, "", true}, {503, `{"code":"zerops_unanswered"}`, "", true},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status)+tt.body, func(t *testing.T) {
			t.Parallel()
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/project/key-recheck" || r.Header.Get("Authorization") != "Project test-key" {
					t.Error("wrong recheck request")
				}
				raw, _ := io.ReadAll(r.Body)
				if len(raw) != 0 {
					t.Error("recheck carries a body")
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			c, err := OpenProject(srv.Client(), Identity{Origin: srv.URL, Key: "test-key", ProjectID: "p"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.RecheckKey(context.Background())
			if got != tt.want || (err != nil) != tt.fails {
				t.Errorf("verdict = %q, error = %v", got, err)
			}
			if calls != 1 {
				t.Errorf("calls = %d", calls)
			}
		})
	}
}

func TestProjectClient_Git_UsesScopedEphemeralAuthorization(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Project test-key" || r.URL.RequestURI() != "/git/org/app/appdev.git/info/refs?service=git-upload-pack" {
			t.Error("wrong Git request")
		}
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n0000")
	}))
	defer srv.Close()
	c, err := OpenProject(srv.Client(), Identity{Origin: srv.URL, Key: "test-key", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := c.RepoURL("org", "app", "appdev")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ProbeRepo(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	for _, remote := range []string{"https://external.example/git/org/app/repo.git", srv.URL + "@external.example/git/org/app/repo.git", srv.URL + "/unrelated"} {
		if err := c.ProbeRepo(context.Background(), remote); err == nil {
			t.Errorf("accepted unscoped URL %q", remote)
		}
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	env := c.GitEnv()
	relevant := env[len(env)-4:]
	want := []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http." + srv.URL + "/.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Project test-key", "GIT_TERMINAL_PROMPT=0"}
	if !reflect.DeepEqual(relevant, want) {
		t.Error("Git environment is not exactly scoped to HQ with an ephemeral Project header")
	}
}

func TestProjectClient_Redirect_DoesNotForwardProjectKey(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"self", "repo", "recheck", "git"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			forwarded := 0
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded++; w.WriteHeader(http.StatusNoContent) }))
			defer destination.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL+"/redirected")
				w.WriteHeader(http.StatusTemporaryRedirect)
			}))
			defer source.Close()
			original := source.Client()
			client, err := OpenProject(original, Identity{Origin: source.URL, ProjectID: "p", Key: "test-key"})
			if err != nil {
				t.Fatal(err)
			}
			switch op {
			case "self":
				_, err = client.Self(context.Background())
			case "repo":
				_, err = client.EnsureRepo(context.Background(), "appdev")
			case "recheck":
				_, err = client.RecheckKey(context.Background())
			case "git":
				err = client.ProbeRepo(context.Background(), source.URL+"/git/org/app/appdev.git")
			}
			if err == nil {
				t.Error("redirect must be refused")
			}
			if err != nil && strings.Contains(err.Error(), "test-key") {
				t.Error("refusal leaks project key")
			}
			if forwarded != 0 {
				t.Errorf("forwarded %d requests outside HQ origin", forwarded)
			}
			if original.CheckRedirect != nil {
				t.Error("changed caller's HTTP client")
			}
		})
	}
}

func TestProjectClient_RepoURL_EncodesEachSegmentLikeHQ(t *testing.T) {
	t.Parallel()
	client, err := OpenProject(http.DefaultClient, Identity{Origin: "https://hq.example/", ProjectID: "p", Key: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, org, app, repo, want string
		missingOrg                 bool
	}{
		{"reserved characters", "org:+@", "app/?#", "repo name!\"'()*+", "https://hq.example/git/org%3A%2B%40/app%2F%3F%23/repo%20name!%22'()*%2B.git", false},
		{"unicode and safe characters", "žluť", "app-_.~", "web", "https://hq.example/git/%C5%BElu%C5%A5/app-_.~/web.git", false},
		{"missing organisation", "", "app", "web", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := client.RepoURL(tt.org, tt.app, tt.repo)
			if tt.missingOrg {
				if !errors.Is(err, ErrNoOrganisation) {
					t.Errorf("error = %v, want missing organisation", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("URL = %q, want %q", got, tt.want)
			}
		})
	}
}
