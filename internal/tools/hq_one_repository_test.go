// Tests for: tools/hq_wiring.go — a Mate's monorepo is one repository in HQ:
// a pair whose dev checkout holds the repository another pair is already
// wired to gets no repository of its own, run against the fake HQ that
// serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestReconcileHQRepositories_ADevHoldingAWiredPairsRepository_GetsNoRepositoryOfItsOwn:
// appdev and webdev are each a dev/stage pair of the Mate, each dev service
// holding a checkout. The pass wires appdev first (by hostname). When
// webdev's checkout is the same repository — the two share a root commit
// that carries code — HQ is asked for no repository named webdev, webdev
// stays unwired, and the pass says so in one line naming both dev services,
// appdev's repository in HQ and the way out: one dev service with a stage
// per part, then pool-migrate. Two repositories, a checkout that cannot be
// read, and a shared root of the empty tree (`zcp init`'s, HQ's seed) are no
// evidence of one repository: webdev is wired as before.
func TestReconcileHQRepositories_ADevHoldingAWiredPairsRepository_GetsNoRepositoryOfItsOwn(t *testing.T) {
	t.Parallel()
	monorepo := map[string]string{"zerops.yml": "zerops:\n  - setup: app\n  - setup: web\n", "README.md": "one repository, two parts\n"}
	tests := []struct {
		name        string
		app, web    []map[string]string
		unreadable  string
		wantRefused bool
	}{
		{name: "one repository on both dev services", app: []map[string]string{monorepo}, web: []map[string]string{monorepo}, wantRefused: true},
		{name: "one repository, the second dev service a commit ahead", app: []map[string]string{monorepo}, web: []map[string]string{monorepo, {"web.txt": "more\n"}}, wantRefused: true},
		{name: "two repositories", app: []map[string]string{{"app.txt": "app\n"}}, web: []map[string]string{{"web.txt": "web\n"}}},
		{name: "the second checkout cannot be read", app: []map[string]string{monorepo}, web: []map[string]string{monorepo}, unreadable: "webdev"},
		{name: "the wired pair's checkout cannot be read", app: []map[string]string{monorepo}, web: []map[string]string{monorepo}, unreadable: "appdev"},
		{name: "a shared root of the empty tree names no code", app: []map[string]string{{}, {"app.txt": "app\n"}}, web: []map[string]string{{}, {"web.txt": "web\n"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			web := lab.addPair("webdev", "webstage")
			labHistory(t, lab.pair, tt.app...)
			labHistory(t, web, tt.web...)
			ssh := &rootsUnreadableSSH{labSSH: lab.ssh, host: tt.unreadable}

			report := reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), ssh, lab.rt, lab.stateDir, hqPassBackedOff)

			if app := lab.meta(); app.HQ == nil || app.HQ.Repo != "appdev" {
				t.Fatalf("appdev is wired to %+v, want its repository appdev; report %q", app.HQ, report)
			}
			webMeta, err := workflow.FindServiceMeta(lab.stateDir, "webdev")
			if err != nil || webMeta == nil {
				t.Fatalf("webdev's record: %v", err)
			}
			line := reportLine(report, "webdev")
			if !tt.wantRefused {
				if webMeta.HQ == nil || webMeta.HQ.Repo != "webdev" {
					t.Errorf("webdev is wired to %+v, want its repository webdev; report %q", webMeta.HQ, report)
				}
				if strings.Contains(line, "pool-migrate") {
					t.Errorf("a wired pair is told to migrate: %q", line)
				}
				return
			}
			if webMeta.HQ != nil || webMeta.GitPushState == topology.GitPushConfigured {
				t.Errorf("webdev was wired: HQ %+v, git-push %s", webMeta.HQ, webMeta.GitPushState)
			}
			if lab.hq.hasRepo(labApp, "webdev") {
				t.Error("HQ was asked for a repository named webdev")
			}
			for _, want := range []string{
				"webdev", "appdev", `wired to "appdev"`, "a monorepo is one repository",
				"a stage per part", `"webstage"`, `zerops_workflow workflow="pool-migrate" service="appdev"`,
				"a dev pool of its own",
			} {
				if !strings.Contains(line, want) {
					t.Errorf("the line lacks %q: %q", want, line)
				}
			}

			// The next pass refuses it again: it never asks HQ for the repository.
			writeHQPairState(lab.stateDir, "webdev", hqPairState{})
			again := reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), ssh, lab.rt, lab.stateDir, hqPassBackedOff)
			if !strings.Contains(reportLine(again, "webdev"), "a monorepo is one repository") || lab.hq.hasRepo(labApp, "webdev") {
				t.Errorf("the next pass did not refuse webdev again: %q", again)
			}
		})
	}
}

// addPair records a second finished dev/stage pair in the lab — dev and
// stage both live, the dev half's checkout a directory of its own the lab's
// SSH stand-in runs its commands in — and answers that directory.
func (l *hqLab) addPair(dev, stage string) string {
	l.t.Helper()
	dir := l.t.TempDir()
	l.ssh.dirs[dev] = dir
	l.mock.WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev"}, {ID: "svc-appstage", Name: "appstage"},
		{ID: "svc-" + dev, Name: dev}, {ID: "svc-" + stage, Name: stage},
	})
	if err := workflow.WriteServiceMeta(l.stateDir, &workflow.ServiceMeta{
		Hostname: dev, Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: stage}},
		BootstrapSession: "test", BootstrappedAt: "2026-10-02",
	}); err != nil {
		l.t.Fatal(err)
	}
	return dir
}

// labHistory makes the checkout at dir hold one history and nothing else: a
// commit per entry, each on the one before, its files added to what the
// working tree holds, at fixed dates — so two checkouts given the same
// entries hold the same commits, as two clones of one repository do. An
// entry with no files and nothing before it is a commit of the empty tree,
// as `zcp init` makes one.
func labHistory(t *testing.T, dir string, commits ...map[string]string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	git := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: a test's own git
		cmd.Env = append(os.Environ(), append(env, "GIT_CONFIG_NOSYSTEM=1")...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git(nil, "init", "-q", "-b", "main")
	for i, files := range commits {
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		date := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		git(nil, "add", "-A")
		git([]string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date},
			"-c", "user.name=dev", "-c", "user.email=dev@example.invalid", "commit", "-q", "--allow-empty", "-m", fmt.Sprintf("commit %d", i+1))
	}
}

// reportLine is the pass's line about host, "" when it says nothing of it.
func reportLine(report []string, host string) string {
	for _, line := range report {
		if strings.HasPrefix(line, host+": ") {
			return line
		}
	}
	return ""
}

// hasRepo reports whether HQ holds the repository app/name.
func (f *fakeHQ) hasRepo(app, name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.repos[app+"/"+name]
}

// rootsUnreadableSSH is the lab's SSH stand-in, except that host's checkout
// cannot be read for its root commits: that read fails as a dropped SSH
// session does, and every other command runs as the lab runs it.
type rootsUnreadableSSH struct {
	*labSSH
	host string
}

func (s *rootsUnreadableSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	if hostname == s.host && strings.Contains(command, "--max-parents=0") {
		return nil, &platform.SSHExecError{Hostname: hostname, Err: fmt.Errorf("connection reset")}
	}
	return s.labSSH.ExecSSH(ctx, hostname, command)
}

var _ ops.SSHDeployer = (*rootsUnreadableSSH)(nil)
