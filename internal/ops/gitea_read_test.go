package ops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestReadGiteaFile reads one file of a branch the way a Mate reads its
// group's tier: a path with spaces and an em dash, at a named ref, answered
// "not there" rather than an error when the branch or the file is missing.
func TestReadGiteaFile(t *testing.T) {
	t.Parallel()
	const tier = "0 — AI Agent/import.yaml"
	tests := []struct {
		name      string
		seed      map[string]string
		ref       string
		path      string
		wantFound bool
		wantBody  string
	}{
		{name: "the tier on main", seed: map[string]string{tier: "services: []\n"}, ref: "main", path: tier, wantFound: true, wantBody: "services: []\n"},
		{name: "main without the tier", seed: map[string]string{"README.md": "# acme\n"}, ref: "main", path: tier},
		{name: "a branch that is not there", seed: map[string]string{tier: "x\n"}, ref: "develop", path: tier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.seed("acme/group", "main", maps2(tt.seed))
			srv := httptest.NewServer(fake)
			defer srv.Close()

			body, found, err := ReadGiteaFile(context.Background(), srv.Client(), srv.URL, "tok", "acme/group", tt.ref, tt.path)
			if err != nil {
				t.Fatalf("ReadGiteaFile: %v", err)
			}
			if found != tt.wantFound || body != tt.wantBody {
				t.Errorf("ReadGiteaFile = (%q, %v), want (%q, %v)", body, found, tt.wantBody, tt.wantFound)
			}
		})
	}
}

// TestReadGiteaFile_Refusals: a Gitea that fails, a directory where a file
// was asked for, and a missing input are errors — never an empty tier.
func TestReadGiteaFile_Refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		repo    string
		path    string
	}{
		{name: "a 500", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, repo: "acme/group", path: "a.yaml"},
		{name: "a directory", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"type":"file","path":"x/a.yaml"}]`))
		}, repo: "acme/group", path: "x"},
		{name: "no repository", handler: func(w http.ResponseWriter, _ *http.Request) {}, path: "a.yaml"},
		{name: "no path", handler: func(w http.ResponseWriter, _ *http.Request) {}, repo: "acme/group"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			if _, _, err := ReadGiteaFile(context.Background(), srv.Client(), srv.URL, "tok", tt.repo, "main", tt.path); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// TestReadGiteaFile_EscapesEachSegment: the tier's directory has spaces and
// an em dash, so every segment is escaped on the wire and the separators
// stay.
func TestReadGiteaFile_EscapesEachSegment(t *testing.T) {
	t.Parallel()
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, _, err := ReadGiteaFile(context.Background(), srv.Client(), srv.URL, "tok", "acme/group", "main", "0 — AI Agent/import.yaml"); err != nil {
		t.Fatalf("ReadGiteaFile: %v", err)
	}
	want := "/api/v1/repos/acme/group/contents/0%20%E2%80%94%20AI%20Agent/import.yaml"
	if raw != want {
		t.Errorf("path on the wire = %q, want %q", raw, want)
	}
}

// TestGiteaGroupOrgs names the orgs whose group repository the token's user
// reads — how a Mate's bot finds its group before any of its pairs names it,
// with a token that cannot list its orgs (the fake refuses GET /user/orgs).
func TestGiteaGroupOrgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		orgs []string
		want string
	}{
		{name: "one group", orgs: []string{"acme"}, want: "acme"},
		{name: "none", want: ""},
		{name: "two", orgs: []string{"acme", "beviro"}, want: "acme,beviro"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.orgs = tt.orgs
			srv := httptest.NewServer(fake)
			defer srv.Close()
			got, err := GiteaGroupOrgs(context.Background(), srv.Client(), srv.URL, "tok")
			if err != nil {
				t.Fatalf("GiteaGroupOrgs: %v", err)
			}
			if strings.Join(got, ",") != tt.want {
				t.Errorf("GiteaGroupOrgs = %v, want %s", got, tt.want)
			}
		})
	}
}

func TestGiteaGroupOrgs_AFailedReadIsAnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := GiteaGroupOrgs(context.Background(), srv.Client(), srv.URL, "tok"); err == nil {
		t.Fatal("a 401 must be an error, not a user in no org")
	}
}

// TestGiteaRepositoryExists tells a repository the token can read from one
// that is not there, so a Mate never asks the broker for — and so creates —
// a repository its recipe names by mistake.
func TestGiteaRepositoryExists(t *testing.T) {
	t.Parallel()
	fake := newFakeGitea(t)
	fake.seed("acme/appdev", "main", map[string]string{"README.md": "x"})
	srv := httptest.NewServer(fake)
	defer srv.Close()

	for repo, want := range map[string]bool{"acme/appdev": true, "acme/nothere": false} {
		got, err := GiteaRepositoryExists(context.Background(), srv.Client(), srv.URL, "tok", repo)
		if err != nil {
			t.Fatalf("GiteaRepositoryExists(%s): %v", repo, err)
		}
		if got != want {
			t.Errorf("GiteaRepositoryExists(%s) = %v, want %v", repo, got, want)
		}
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failing.Close()
	if _, err := GiteaRepositoryExists(context.Background(), failing.Client(), failing.URL, "tok", "acme/appdev"); err == nil {
		t.Error("a 502 must be an error, not a missing repository")
	}
}
