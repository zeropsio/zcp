// Tests for: hq/scratch.go — git on this container against HQ carries the
// Mate credential in its environment only, scoped to HQ.
package hq

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestGitEnv_DoesNotExposeAuthorizationThroughInheritedTracing(t *testing.T) {
	// non-parallel: t.Setenv changes the process environment.
	for _, name := range []string{"GIT_TRACE", "GIT_TRACE_PACKET", "GIT_TRACE_CURL", "GIT_TRACE_CURL_NO_DATA", "GIT_TRACE2_EVENT", "GIT_TRACE2_ENV_VARS", "GIT_CURL_VERBOSE"} {
		t.Setenv(name, "trace-enabled")
	}
	t.Setenv("HQ_GIT_TEST_KEEP", "ordinary environment")
	c := Client{id: Identity{Origin: "https://hq.example", ProjectID: "project-1", Key: "trace-test-key"}}
	env := c.gitEnv()
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_TRACE") || name == "GIT_CURL_VERBOSE" {
			t.Errorf("Git inherits credential tracing through %s", name)
		}
	}
	if !slices.Contains(env, "HQ_GIT_TEST_KEEP="+os.Getenv("HQ_GIT_TEST_KEEP")) {
		t.Error("Git lost ordinary inherited environment")
	}
	if !slices.Contains(env, "GIT_CONFIG_VALUE_0=Authorization: Project trace-test-key") {
		t.Error("Git lost scoped project authorization")
	}
}

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
			c := Client{id: Identity{Origin: tt.address, Key: "the-key", OrgID: "org-1"}}
			env := c.gitEnv()
			want := []string{
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=" + tt.wantKey,
				"GIT_CONFIG_VALUE_0=Authorization: Project the-key",
				"GIT_TERMINAL_PROMPT=0",
			}
			for _, w := range want {
				if !slices.Contains(env, w) {
					t.Errorf("env lacks %q", w)
				}
			}
			if url, err := c.RepoURL("app-1", "group"); err != nil || strings.Contains(url, "the-key") {
				t.Errorf("the repository's URL %q (%v) is missing or carries the key", url, err)
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
	c := Client{id: Identity{Origin: "https://hq.example.com", Key: "the-key", OrgID: "org-1"}}
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

// TestScratch_History: who wrote what a file holds is read off the history
// of the rev a scratch fetched — each commit that changed the path, newest
// first, by its author's address and its subject — so the recipe can tell a
// tier only this Mate's recipe proposals ever wrote (Core's squash of a
// Mate's change is authored SquashAuthorEmail) from one anyone else touched.
func TestScratch_History(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := t.Context()
	c := Client{id: Identity{Origin: "https://hq.example.com", Key: "the-key", OrgID: "org-1"}}
	scratch, err := c.OpenScratch(ctx, "app-1", RecipeRepo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scratch.Close)
	empty, err := scratch.git(ctx, strings.NewReader(""), "mktree")
	if err != nil {
		t.Fatal(err)
	}
	commit := func(parent string, files map[string]string, email, subject string) string {
		t.Helper()
		base := empty
		var parents []string
		if parent != "" {
			base, parents = parent, []string{parent}
		}
		tree, err := scratch.TreeWith(ctx, base, files)
		if err != nil {
			t.Fatal(err)
		}
		rev, err := scratch.Commit(ctx, CommitSpec{Tree: tree, Parents: parents, Message: subject, Name: "Mate", Email: email})
		if err != nil {
			t.Fatal(err)
		}
		return rev
	}
	first := commit("", map[string]string{"3 — Stage/import.yaml": "a\n", "README.md": "r\n"}, SquashAuthorEmail("p-mate"), RecipeProposalTitle+" (#1)")
	second := commit(first, map[string]string{"README.md": "s\n"}, "person@example.invalid", "Words (#2)")
	third := commit(second, map[string]string{"3 — Stage/import.yaml": "b\n"}, SquashAuthorEmail("p-other"), "More memory (#3)")

	got, err := scratch.History(ctx, third, "3 — Stage/import.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []Commit{
		{AuthorEmail: "p-other@mate.hq.invalid", Subject: "More memory (#3)"},
		{AuthorEmail: "p-mate@mate.hq.invalid", Subject: RecipeProposalTitle + " (#1)"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("History = %+v, want %+v", got, want)
	}
	if none, err := scratch.History(ctx, third, "4 — Small Production/import.yaml"); err != nil || len(none) != 0 {
		t.Errorf("History of a path no commit touched = %+v, %v; want none", none, err)
	}
}

// TestScratch_MergeBase: the common ancestor of two revs, which tells what a
// change's branch holds beyond the main it last took in; none for an unborn
// rev or two histories that share no commit.
func TestScratch_MergeBase(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := t.Context()
	c := Client{id: Identity{Origin: "https://hq.example.com", Key: "the-key", OrgID: "org-1"}}
	scratch, err := c.OpenScratch(ctx, "app-1", RecipeRepo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scratch.Close)
	empty, err := scratch.git(ctx, strings.NewReader(""), "mktree")
	if err != nil {
		t.Fatal(err)
	}
	commit := func(subject string, parents ...string) string {
		t.Helper()
		tree, err := scratch.TreeWith(ctx, empty, map[string]string{"README.md": subject + "\n"})
		if err != nil {
			t.Fatal(err)
		}
		rev, err := scratch.Commit(ctx, CommitSpec{Tree: tree, Parents: parents, Message: subject, Name: "t", Email: "t@example.invalid"})
		if err != nil {
			t.Fatal(err)
		}
		return rev
	}
	main := commit("main")
	change := commit("change", main)
	moved := commit("moved", main)
	unrelated := commit("unrelated")
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"a change on main", change, main, main},
		{"a change behind a moved main", change, moved, main},
		{"unrelated histories", change, unrelated, ""},
		{"an unborn main", change, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, err := scratch.MergeBase(t.Context(), tt.a, tt.b); err != nil || got != tt.want {
				t.Errorf("MergeBase = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
