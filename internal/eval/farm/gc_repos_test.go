package farm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
)

// fakeGitHubRepoListServer is an offline double for GET /user/repos, real
// HTTP end to end (httptest) — RepoGCOptions.ListRepos in each test below
// calls it over the network exactly the way eval.ListGitHubRepos would,
// proving RepoGC's own prefix-filter/age-classification logic against a
// real HTTP round trip rather than a hand-built []eval.GitHubRepoSummary.
type fakeGitHubRepoListServer struct {
	repos []map[string]any
}

func (f *fakeGitHubRepoListServer) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(f.repos); err != nil {
			t.Errorf("fakeGitHubRepoListServer: encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// listReposOverHTTP is the test-only ListRepos implementation: a single GET
// against base (no pagination — every table case here fits one page),
// decoded into []eval.GitHubRepoSummary the same shape ListGitHubRepos
// returns.
func listReposOverHTTP(base string) func(ctx context.Context, token string) ([]eval.GitHubRepoSummary, error) {
	return func(ctx context.Context, _ string) ([]eval.GitHubRepoSummary, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/user/repos", nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var raw []struct {
			Name     string `json:"name"`
			PushedAt string `json:"pushed_at"` //nolint:tagliatelle // upstream GitHub REST schema
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return nil, err
		}
		out := make([]eval.GitHubRepoSummary, 0, len(raw))
		for _, r := range raw {
			pushedAt, _ := time.Parse(time.RFC3339, r.PushedAt)
			out = append(out, eval.GitHubRepoSummary{Owner: r.Owner.Login, Name: r.Name, PushedAt: pushedAt})
		}
		return out, nil
	}
}

func repoEntry(name, pushedAt string) map[string]any {
	return map[string]any{"name": name, "pushed_at": pushedAt, "owner": map[string]any{"login": "krls2020"}}
}

// TestRepoGC_ClassifiesByPrefixAndAge is table-driven over RepoGC's two
// filtering rules: only zcp-farm-*-prefixed repos are ever candidates
// (FM-19/FM-20 posture, mirrored for repos), and --older-than exempts a repo
// until its own pushed_at satisfies the requested age — same shape as
// TestGC's project classification.
func TestRepoGC_ClassifiesByPrefixAndAge(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		repos     []map[string]any
		olderThan time.Duration
		want      []RepoGCCandidate
	}{
		{
			name: "foreign-prefixed repo never listed",
			repos: []map[string]any{
				repoEntry("some-other-repo", "2026-09-01T00:00:00Z"),
			},
			want: nil,
		},
		{
			name: "zcp-farm repo with no --older-than is an eligible candidate",
			repos: []map[string]any{
				repoEntry("zcp-farm-run1", "2026-09-01T00:00:00Z"),
			},
			want: []RepoGCCandidate{{Owner: "krls2020", Name: "zcp-farm-run1"}},
		},
		{
			name: "too recent under --older-than is exempt",
			repos: []map[string]any{
				repoEntry("zcp-farm-run2", "2026-09-15T11:00:00Z"),
			},
			olderThan: 24 * time.Hour,
			want:      []RepoGCCandidate{{Owner: "krls2020", Name: "zcp-farm-run2", Exempt: "too recent"}},
		},
		{
			name: "old enough under --older-than is an eligible candidate",
			repos: []map[string]any{
				repoEntry("zcp-farm-run3", "2026-09-01T00:00:00Z"),
			},
			olderThan: 24 * time.Hour,
			want:      []RepoGCCandidate{{Owner: "krls2020", Name: "zcp-farm-run3"}},
		},
		{
			name: "unparseable pushed_at under --older-than is exempt, never treated as old",
			repos: []map[string]any{
				repoEntry("zcp-farm-run4", "not-a-timestamp"),
			},
			olderThan: 24 * time.Hour,
			want:      []RepoGCCandidate{{Owner: "krls2020", Name: "zcp-farm-run4", Exempt: "unknown finish time"}},
		},
		{
			name:  "empty account",
			repos: nil,
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeGitHubRepoListServer{repos: tc.repos}
			base := f.start(t)

			got, err := RepoGC(context.Background(), RepoGCOptions{
				Token: "test-admin-pat", OlderThan: tc.olderThan,
				Now:       func() time.Time { return fixedNow },
				ListRepos: listReposOverHTTP(base),
			})
			if err != nil {
				t.Fatalf("RepoGC: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("RepoGC = %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("RepoGC[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRepoGC_EmptyToken_NoOp pins the opt-in posture: RepoGC returns
// (nil, nil) — no error, no candidates, no network call — when Token is
// empty. ListRepos is left nil deliberately: a network call here (if RepoGC
// ignored the guard) would panic on the nil func, failing the test loudly.
func TestRepoGC_EmptyToken_NoOp(t *testing.T) {
	t.Parallel()
	got, err := RepoGC(context.Background(), RepoGCOptions{})
	if err != nil {
		t.Fatalf("RepoGC: %v", err)
	}
	if got != nil {
		t.Errorf("RepoGC = %+v, want nil", got)
	}
}

// TestRepoGCApply_DeletesOnlyNonExempt pins RepoGCApply's own filter: it
// calls deleteRepo for every candidate whose Exempt is empty, and for none
// whose Exempt is set, injecting a fake deleteRepo so no network reaches
// GitHub.
func TestRepoGCApply_DeletesOnlyNonExempt(t *testing.T) {
	t.Parallel()
	candidates := []RepoGCCandidate{
		{Owner: "krls2020", Name: "zcp-farm-run1"},
		{Owner: "krls2020", Name: "zcp-farm-run2", Exempt: "too recent"},
		{Owner: "krls2020", Name: "zcp-farm-run3"},
	}

	var deleted []string
	deleteRepo := func(_ context.Context, owner, name, token string) error {
		if token != "test-admin-pat" {
			t.Errorf("deleteRepo token = %q, want test-admin-pat", token)
		}
		deleted = append(deleted, owner+"/"+name)
		return nil
	}

	errs := RepoGCApply(context.Background(), "test-admin-pat", candidates, deleteRepo)
	if len(errs) != 0 {
		t.Fatalf("RepoGCApply errs = %v, want none", errs)
	}
	want := []string{"krls2020/zcp-farm-run1", "krls2020/zcp-farm-run3"}
	if len(deleted) != len(want) {
		t.Fatalf("deleted = %v, want %v", deleted, want)
	}
	for i := range want {
		if deleted[i] != want[i] {
			t.Errorf("deleted[%d] = %q, want %q", i, deleted[i], want[i])
		}
	}
}

// TestRepoGCApply_DeleteFailure_ReportedPerCandidate pins the error
// aggregation: a failing delete is reported, naming the repo, and does not
// stop the remaining candidates from being attempted.
func TestRepoGCApply_DeleteFailure_ReportedPerCandidate(t *testing.T) {
	t.Parallel()
	candidates := []RepoGCCandidate{
		{Owner: "krls2020", Name: "zcp-farm-fails"},
		{Owner: "krls2020", Name: "zcp-farm-ok"},
	}
	var attempted []string
	deleteRepo := func(_ context.Context, owner, name, _ string) error {
		attempted = append(attempted, name)
		if name == "zcp-farm-fails" {
			return fmt.Errorf("simulated failure")
		}
		return nil
	}

	errs := RepoGCApply(context.Background(), "test-admin-pat", candidates, deleteRepo)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly 1", errs)
	}
	if len(attempted) != 2 {
		t.Fatalf("attempted = %v, want both candidates attempted despite the first failing", attempted)
	}
}
