package ops

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// non-parallel: these tests inspect the package-level per-source deploy git locks.

// TestDeployBatchSSH_SameSourceBuildsOverlap_GitSectionExclusive: two stages
// fed from one dev service wait on their builds at the same time, while every
// git round trip and upload against the source's shared tree runs alone.
func TestDeployBatchSSH_SameSourceBuildsOverlap_GitSectionExclusive(t *testing.T) {
	source := "batch-f4-source"
	mock := platform.NewMock().
		WithServices(deployBatchTestServices(source, "batch-f4-stage", "batch-f4-worker"))
	ssh := newBatchConcurrencySSH()
	ssh.waitForBuilds = 2

	result := DeployBatchSSH(
		context.Background(),
		mock,
		"proj-1",
		ssh,
		testAuthInfo(),
		"",
		nil,
		[]DeployBatchTarget{
			{SourceService: source, TargetService: "batch-f4-stage", Setup: "prod"},
			{SourceService: source, TargetService: "batch-f4-worker", Setup: "worker"},
		},
		nil,
		nil,
		nil,
	)

	assertDeployBatchNoKickoffErrors(t, result)
	if got := ssh.peakForSource(source); got != 2 {
		t.Errorf("peak concurrent ExecSSH calls for source %q = %d, want 2 (both builds awaited at once)", source, got)
	}
	if got := ssh.peakGitForSource(source); got != 1 {
		t.Errorf("peak concurrent git/upload ExecSSH calls for source %q = %d, want 1", source, got)
	}
	if got := ssh.unlockedGitForSource(source); got != 0 {
		t.Errorf("%d git/upload calls for source %q ran without its git lock", got, source)
	}
}

func TestDeployBatchSSH_DistinctSourcesOverlap(t *testing.T) {
	sourceA := "batch-f4-source-a"
	sourceB := "batch-f4-source-b"
	mock := platform.NewMock().
		WithServices(deployBatchTestServices(sourceA, sourceB, "batch-f4-stage-a", "batch-f4-stage-b"))
	ssh := newBatchConcurrencySSH()
	ssh.waitForCalls = 2

	result := DeployBatchSSH(
		context.Background(),
		mock,
		"proj-1",
		ssh,
		testAuthInfo(),
		"",
		nil,
		[]DeployBatchTarget{
			{SourceService: sourceA, TargetService: "batch-f4-stage-a", Setup: "prod"},
			{SourceService: sourceB, TargetService: "batch-f4-stage-b", Setup: "prod"},
		},
		nil,
		nil,
		nil,
	)

	assertDeployBatchNoKickoffErrors(t, result)
	if got := ssh.peak(); got != 2 {
		t.Fatalf("peak concurrent ExecSSH calls across distinct sources = %d, want 2", got)
	}
}

// TestDeployBatchSSH_LeaseSource_PushesFromAlias: a batch entry whose source
// is a recorded lease alias deploys as DeploySSHWithLeases does from it
// (docs/spec-workflows.md §8 DP-1): the alias is never looked up as a
// service, every round trip runs inside the leased container, and two entries
// from it share the alias's git lock while their builds are awaited at once.
// The entry to the lease's own pool reaches only new containers (DP-7).
func TestDeployBatchSSH_LeaseSource_PushesFromAlias(t *testing.T) {
	stateDir, alias := leaseDeployState(t, 51)
	mock := platform.NewMock().WithServices(leaseDeployServices())
	ssh := newBatchConcurrencySSH()
	ssh.waitForBuilds = 2

	result := DeployBatchSSH(
		context.Background(),
		mock,
		"proj-1",
		ssh,
		testAuthInfo(),
		stateDir,
		nil,
		[]DeployBatchTarget{
			{SourceService: alias, TargetService: "appstage", Setup: "app"},
			{SourceService: alias, TargetService: "webpool"},
		},
		nil,
		nil,
		nil,
	)

	assertDeployBatchNoKickoffErrors(t, result)
	if hosts := ssh.hosts(); len(hosts) != 1 || hosts[0] != alias {
		t.Errorf("ssh reached %v, want only the lease alias %q", hosts, alias)
	}
	if got := ssh.peakGitForSource(alias); got != 1 {
		t.Errorf("peak concurrent git/upload ExecSSH calls for the alias = %d, want 1 (one git lock keyed on the alias)", got)
	}
	if got := ssh.unlockedGitForSource(alias); got != 0 {
		t.Errorf("%d git/upload calls for alias %q ran without its git lock", got, alias)
	}
	for _, e := range result.Entries {
		toPool := e.Target.TargetService == "webpool"
		if e.Result.ReachesNewContainersOnly != toPool {
			t.Errorf("entry %s: reachesNewContainersOnly = %v, want %v", e.Target.TargetService, e.Result.ReachesNewContainersOnly, toPool)
		}
		if toPool && !strings.Contains(e.Result.Message, alias+" keeps the version it runs") {
			t.Errorf("pool entry message %q does not name the lease %s", e.Result.Message, alias)
		}
	}
}

func deployBatchTestServices(names ...string) []platform.ServiceStack {
	services := make([]platform.ServiceStack, 0, len(names))
	for _, name := range names {
		services = append(services, platform.ServiceStack{
			ID:     "svc-" + name,
			Name:   name,
			Status: platform.ServiceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeID:          "type-nodejs",
				ServiceStackTypeVersionName: "nodejs@22",
			},
		})
	}
	return services
}

func assertDeployBatchNoKickoffErrors(t *testing.T, result *DeployBatchResult) {
	t.Helper()
	for _, entry := range result.Entries {
		if entry.Error != "" {
			t.Fatalf("deploy kickoff for target %q failed: %s", entry.Target.TargetService, entry.Error)
		}
	}
}

type batchConcurrencySSH struct {
	mu               sync.Mutex
	waitForCalls     int
	entered          int
	allEnteredClosed bool
	allEntered       chan struct{}
	inFlight         int
	peakInFlight     int
	inFlightBySource map[string]int
	peakBySource     map[string]int
	// waitForBuilds holds each build wait until that many have entered;
	// git/upload calls never wait on it.
	waitForBuilds   int
	builds          int
	buildsEntered   chan struct{}
	gitBySource     map[string]int
	peakGitBySource map[string]int
	unlockedGit     map[string]int
}

// isBuildWait reports a call that only awaits a build already uploaded: it
// names the push's run directory and runs neither git nor zcli.
func isBuildWait(command string) bool {
	return strings.Contains(command, "zcp-push-") && !strings.Contains(command, "zcli push") && !strings.Contains(command, "git ")
}

func newBatchConcurrencySSH() *batchConcurrencySSH {
	return &batchConcurrencySSH{
		allEntered:       make(chan struct{}),
		inFlightBySource: make(map[string]int),
		peakBySource:     make(map[string]int),
		buildsEntered:    make(chan struct{}),
		gitBySource:      make(map[string]int),
		peakGitBySource:  make(map[string]int),
		unlockedGit:      make(map[string]int),
	}
}

func (s *batchConcurrencySSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	build := isBuildWait(command)
	s.enter(hostname, build)
	defer s.leave(hostname, build)

	if build {
		if s.waitForBuilds > 0 {
			select {
			case <-s.buildsEntered:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return []byte("ok"), nil
	}

	if s.waitForCalls > 0 {
		select {
		case <-s.allEntered:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []byte("ok"), nil
}

func (s *batchConcurrencySSH) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (s *batchConcurrencySSH) ExecSSHBackground(_ context.Context, _ string, _ string, _ time.Duration) ([]byte, error) {
	return []byte("ok"), nil
}

func (s *batchConcurrencySSH) enter(hostname string, build bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if build {
		s.builds++
		if s.builds == s.waitForBuilds {
			close(s.buildsEntered)
		}
	} else {
		lock, ok := deploySourceGitLocks.locks.Load(hostname)
		if !ok {
			s.unlockedGit[hostname]++
		} else if mu, ok := lock.(*sync.Mutex); !ok {
			s.unlockedGit[hostname]++
		} else if mu.TryLock() {
			mu.Unlock()
			s.unlockedGit[hostname]++
		}
		s.gitBySource[hostname]++
		if s.gitBySource[hostname] > s.peakGitBySource[hostname] {
			s.peakGitBySource[hostname] = s.gitBySource[hostname]
		}
	}

	s.entered++
	if s.waitForCalls > 0 && s.entered >= s.waitForCalls && !s.allEnteredClosed {
		close(s.allEntered)
		s.allEnteredClosed = true
	}

	s.inFlight++
	if s.inFlight > s.peakInFlight {
		s.peakInFlight = s.inFlight
	}
	s.inFlightBySource[hostname]++
	if s.inFlightBySource[hostname] > s.peakBySource[hostname] {
		s.peakBySource[hostname] = s.inFlightBySource[hostname]
	}
}

func (s *batchConcurrencySSH) leave(hostname string, build bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !build {
		s.gitBySource[hostname]--
	}

	s.inFlight--
	s.inFlightBySource[hostname]--
}

func (s *batchConcurrencySSH) peakForSource(hostname string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakBySource[hostname]
}

func (s *batchConcurrencySSH) peakGitForSource(hostname string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakGitBySource[hostname]
}

func (s *batchConcurrencySSH) unlockedGitForSource(hostname string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unlockedGit[hostname]
}

// hosts is every host an ssh call reached, sorted.
func (s *batchConcurrencySSH) hosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.peakBySource))
	for h := range s.peakBySource {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func (s *batchConcurrencySSH) peak() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakInFlight
}
