// Tests for: ops/deploy_ssh.go + ops/deploy_lease_stage.go — a stage tracks
// main: a deploy from a lease to an app runtime fetches origin/main before it
// resolves what it ships, and says so when that is not on it
// (docs/spec-workflows.md §8 DP-8).
package ops

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	stageTestHead = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	stageTestMain = "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"
)

// stageRepoSSH answers the commands a deploy sends to the repository of its
// source — a lease or a dev service — as a checkout on branch at head whose
// origin/main is main (none when ""). onMain is what `git merge-base
// --is-ancestor` answers; fetchErr fails the fetch of origin/main with git's
// stderr.
type stageRepoSSH struct {
	stageRepo
	mu    sync.Mutex
	calls []sshCall
}

// stageRepo is the checkout stageRepoSSH answers for.
type stageRepo struct {
	head     string
	branch   string
	dirty    bool
	main     string
	onMain   bool
	fetchErr string
}

func (s *stageRepoSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, sshCall{hostname: hostname, command: command})
	switch {
	case strings.Contains(command, "git fetch"):
		if s.fetchErr != "" {
			return []byte(s.fetchErr), &platform.SSHExecError{Hostname: hostname, Output: s.fetchErr, Err: errors.New("exit status 128")}
		}
		return nil, nil
	case strings.Contains(command, "merge-base --is-ancestor"):
		switch {
		case s.main == "":
			return []byte("ZCP:MAIN:none\n"), nil
		case s.onMain:
			return []byte("ZCP:MAIN:on " + s.main + "\n"), nil
		default:
			return []byte("ZCP:MAIN:off " + s.main + "\n"), nil
		}
	case strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain"):
		out := s.head + "\nZCP:BRANCH:" + s.branch + "\n"
		if s.dirty {
			out += "M"
		}
		return []byte(out), nil
	case strings.Contains(command, "rev-parse --verify"):
		if strings.Contains(command, "origin/main") {
			return []byte(s.main + "\n"), nil
		}
		return []byte(s.head + "\n"), nil
	case strings.Contains(command, "git show"):
		return []byte(validCommitZeropsYaml), nil
	case strings.Contains(command, "mktemp -d"):
		return []byte("/tmp/zcp-extract-1\n"), nil
	}
	return []byte("ok"), nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *stageRepoSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *stageRepoSSH) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

// index is the position of the first call whose command contains sub, -1
// when none does.
func (s *stageRepoSSH) index(sub string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.calls {
		if strings.Contains(c.command, sub) {
			return i
		}
	}
	return -1
}

func (s *stageRepoSSH) command(sub string) (sshCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if strings.Contains(c.command, sub) {
			return c, true
		}
	}
	return sshCall{}, false
}

// stageDeployServices is a project holding a dev service, the part's stage
// and the dev pool, each typed as the platform reports it.
func stageDeployServices() []platform.ServiceStack {
	return []platform.ServiceStack{
		{ID: "svc-dev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-stage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-pool", Name: "webpool", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
	}
}

// stageNote is the result's stage-tracks-main warning, "" when it has none.
func stageNote(r *DeployResult) string {
	for _, w := range r.Warnings {
		if strings.Contains(w, "Stage tracks main") {
			return w
		}
	}
	return ""
}

func TestDeploySSH_StageTracksMain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		number   int64
		source   string // "" = the lease
		target   string
		sha      string
		repo     stageRepo
		wantNote []string // nil = no stage note
		banNote  []string
		// wantFetch: origin/main is fetched in the source before what ships
		// is resolved.
		wantFetch bool
	}{
		{
			name: "a branch commit not on main deploys and says so", number: 61, target: "appstage",
			repo:      stageRepo{head: stageTestHead, branch: "feat-x", main: stageTestMain},
			wantFetch: true,
			wantNote: []string{
				"Stage tracks main", "ships feat-x a1b2c3d", "to appstage", "not on origin/main", "9f8e7d6",
				"preview URL", `zerops_dev_server action=status hostname="webpool-61"`, "previewUrl",
				`zerops_deploy sourceService="webpool-61" targetService="appstage" sha="origin/main"`,
			},
		},
		{
			name: "a commit on main says nothing more", number: 62, target: "appstage",
			repo:      stageRepo{head: stageTestHead, branch: "main", main: stageTestMain, onMain: true},
			wantFetch: true,
		},
		{
			name: "uncommitted changes on main are no commit on main", number: 63, target: "appstage",
			repo:      stageRepo{head: stageTestHead, branch: "main", dirty: true, main: stageTestMain, onMain: true},
			wantFetch: true,
			wantNote:  []string{"Stage tracks main", "main a1b2c3d plus uncommitted changes", "not on origin/main"},
		},
		{
			name: "an explicit sha not on main deploys and says so", number: 64, target: "appstage", sha: "a1b2c3d",
			repo:      stageRepo{head: stageTestHead, branch: "feat-x", main: stageTestMain},
			wantFetch: true,
			wantNote:  []string{"Stage tracks main", "a1b2c3d", "not on origin/main", `sha="origin/main"`},
		},
		{
			name: "sha origin/main ships main as fetched", number: 65, target: "appstage", sha: "origin/main",
			repo:      stageRepo{head: stageTestMain, branch: "feat-x", main: stageTestMain, onMain: true},
			wantFetch: true,
		},
		{
			name: "a fetch that fails compares against the copy fetched last", number: 66, target: "appstage",
			repo: stageRepo{head: stageTestHead, branch: "feat-x", main: stageTestMain,
				fetchErr: "fatal: could not read Username for 'https://github.com': terminal prompts disabled"},
			wantFetch: true,
			wantNote:  []string{"Stage tracks main", "not on origin/main", "could not be fetched", "terminal prompts disabled", "fetched last"},
		},
		{
			name: "no origin/main cannot tell", number: 67, target: "appstage",
			repo:      stageRepo{head: stageTestHead, branch: "feat-x"},
			wantFetch: true,
			wantNote:  []string{"Stage tracks main", "could not tell", "feat-x a1b2c3d", "no origin/main"},
			banNote:   []string{"not on origin/main"},
		},
		{
			name: "no origin/main and a failed fetch names the failure", number: 71, target: "appstage",
			repo:      stageRepo{head: stageTestHead, branch: "feat-x", fetchErr: "fatal: couldn't find remote ref refs/heads/main"},
			wantFetch: true,
			wantNote:  []string{"Stage tracks main", "could not tell", "no origin/main, and fetching it failed: fatal: couldn't find remote ref"},
			banNote:   []string{"fetched last"},
		},
		{
			name: "a dev service to its stage fetches nothing and says nothing more", number: 68, source: "appdev", target: "appstage",
			repo: stageRepo{head: stageTestHead, branch: "feat-x", main: stageTestMain},
		},
		{
			name: "a lease to its own pool fetches nothing and says nothing more", number: 69, target: "webpool",
			repo: stageRepo{head: stageTestHead, branch: "feat-x", main: stageTestMain},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir, alias := leaseDeployState(t, tt.number)
			source := cmp.Or(tt.source, alias)
			mock := platform.NewMock().WithServices(stageDeployServices())
			ssh := &stageRepoSSH{stageRepo: tt.repo}

			result, err := DeploySSHWithLeases(context.Background(), mock, "proj-1", ssh, testAuthInfo(),
				stateDir, source, tt.target, "app", "", tt.sha)
			if err != nil {
				t.Fatalf("a deploy is never refused for the commit it ships: %v", err)
			}
			if ssh.index("zcli push") < 0 {
				t.Fatal("the push did not run")
			}

			fetch := ssh.index("git fetch")
			if !tt.wantFetch {
				if fetch >= 0 {
					t.Errorf("a deploy that is no lease-to-stage deploy fetched: %q", ssh.calls[fetch].command)
				}
			} else {
				if fetch < 0 {
					t.Fatal("origin/main was not fetched")
				}
				c, _ := ssh.command("git fetch")
				if c.hostname != alias {
					t.Errorf("fetch ran on %q, want the lease %q", c.hostname, alias)
				}
				for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "'+refs/heads/main:refs/remotes/origin/main'"} {
					if !strings.Contains(c.command, want) {
						t.Errorf("fetch %q lacks %q", c.command, want)
					}
				}
				resolve := ssh.index("rev-parse --verify")
				if resolve >= 0 && resolve < fetch {
					t.Errorf("what ships was resolved (call %d) before origin/main was fetched (call %d)", resolve, fetch)
				}
			}

			note := stageNote(result)
			if tt.wantNote == nil {
				if note != "" {
					t.Errorf("unexpected stage note: %s", note)
				}
				return
			}
			if note == "" {
				t.Fatalf("no stage note in warnings %q", result.Warnings)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(note, want) {
					t.Errorf("note %q lacks %q", note, want)
				}
			}
			for _, banned := range append(tt.banNote, "now runs") {
				if strings.Contains(note, banned) {
					t.Errorf("note %q must not say %q", note, banned)
				}
			}
		})
	}
}

// TestDeploySSH_StageTracksMain_ChecksTheShippedCommit: the main check asks
// about exactly the commit the deploy ships, against origin/main.
func TestDeploySSH_StageTracksMain_ChecksTheShippedCommit(t *testing.T) {
	t.Parallel()

	stateDir, alias := leaseDeployState(t, 70)
	mock := platform.NewMock().WithServices(stageDeployServices())
	ssh := &stageRepoSSH{stageRepo: stageRepo{head: stageTestHead, branch: "feat-x", main: stageTestMain}}

	if _, err := DeploySSHWithLeases(context.Background(), mock, "proj-1", ssh, testAuthInfo(),
		stateDir, alias, "appstage", "app", "", ""); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	check, ok := ssh.command("merge-base --is-ancestor")
	if !ok {
		t.Fatal("the shipped commit was not checked against origin/main")
	}
	for _, want := range []string{"'" + stageTestHead + "'", "refs/remotes/origin/main"} {
		if !strings.Contains(check.command, want) {
			t.Errorf("check %q lacks %q", check.command, want)
		}
	}
	if check.hostname != alias {
		t.Errorf("check ran on %q, want the lease %q", check.hostname, alias)
	}
	if push := ssh.index("zcli push"); ssh.index("merge-base --is-ancestor") > push {
		t.Error("the check ran after the push: it must run while the git lock is held, before the push")
	}
}
