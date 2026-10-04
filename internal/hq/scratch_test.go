// Tests for: hq/scratch.go — git on this container against HQ carries the
// Mate credential in its environment only, scoped to HQ.
package hq

import (
	"encoding/base64"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestGitEnv_CarriesTheCredentialScopedToHQ: the credential reaches git as an
// Authorization header for HQ's address alone, through the environment —
// never as an argument a process listing shows, nor in a URL.
func TestGitEnv_CarriesTheCredentialScopedToHQ(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		wantKey string
	}{
		{"HQ on 443", "https://hq.example.com", "http.https://hq.example.com/.extraHeader"},
		{"HQ on another port, trailing slash", "https://hq.example.com:8443/", "http.https://hq.example.com:8443/.extraHeader"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := Client{enrollment: Enrollment{HQ: tt.address, Credential: "the-credential"}}
			env := c.gitEnv()
			want := []string{
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=" + tt.wantKey,
				"GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(GitUser+":the-credential")),
				"GIT_TERMINAL_PROMPT=0",
			}
			for _, w := range want {
				if !slices.Contains(env, w) {
					t.Errorf("env lacks %q", w)
				}
			}
			if url := c.RepoURL("app-1", "group"); strings.Contains(url, "the-credential") {
				t.Errorf("the repository's URL %q carries the credential", url)
			}
		})
	}
}

// TestScratch_File: a file is read as rev holds it, byte for byte; a path rev
// does not hold is absent, not an error.
func TestScratch_File(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := t.Context()
	c := Client{enrollment: Enrollment{HQ: "https://hq.example.com", Credential: "the-credential"}}
	scratch, err := c.OpenScratch(ctx, "app-1", RecipeRepo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scratch.Close)
	empty, err := scratch.git(ctx, strings.NewReader(""), "mktree")
	if err != nil {
		t.Fatal(err)
	}
	const body = "services:\n  - hostname: search\n"
	tree, err := scratch.TreeWith(ctx, empty, map[string]string{"3 — Stage/import.yaml": body})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := scratch.Commit(ctx, CommitSpec{Tree: tree, Message: "tiers", Name: "t", Email: "t@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		path      string
		wantBody  string
		wantFound bool
	}{
		{"a file rev holds", "3 — Stage/import.yaml", body, true},
		{"a path rev does not hold", "4 — Small Production/import.yaml", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, found, err := scratch.File(t.Context(), rev, tt.path)
			if err != nil || found != tt.wantFound || got != tt.wantBody {
				t.Errorf("File(%q) = %q, %v, %v; want %q, %v", tt.path, got, found, err, tt.wantBody, tt.wantFound)
			}
		})
	}
}
