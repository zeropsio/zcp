package ops

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// non-parallel: these tests observe timing around the package-level
// per-source deploy git lock.

func TestDeployBatchSSH_SerializesPerSource(t *testing.T) {
	source := "batch-f4-source"
	mock := platform.NewMock().
		WithServices(deployBatchTestServices(source, "batch-f4-stage", "batch-f4-worker"))
	ssh := newBatchConcurrencySSH(50 * time.Millisecond)

	result := DeployBatchSSH(
		context.Background(),
		mock,
		"proj-1",
		ssh,
		testAuthInfo(),
		[]DeployBatchTarget{
			{SourceService: source, TargetService: "batch-f4-stage", Setup: "prod"},
			{SourceService: source, TargetService: "batch-f4-worker", Setup: "worker"},
		},
		nil,
		nil,
		nil,
	)

	assertDeployBatchNoKickoffErrors(t, result)
	if got := ssh.peakForSource(source); got != 1 {
		t.Fatalf("peak concurrent ExecSSH calls for source %q = %d, want 1", source, got)
	}
}

func TestDeployBatchSSH_DistinctSourcesOverlap(t *testing.T) {
	sourceA := "batch-f4-source-a"
	sourceB := "batch-f4-source-b"
	mock := platform.NewMock().
		WithServices(deployBatchTestServices(sourceA, sourceB, "batch-f4-stage-a", "batch-f4-stage-b"))
	ssh := newBatchConcurrencySSH(0)
	ssh.waitForCalls = 2

	result := DeployBatchSSH(
		context.Background(),
		mock,
		"proj-1",
		ssh,
		testAuthInfo(),
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
	delay            time.Duration
	waitForCalls     int
	entered          int
	allEnteredClosed bool
	allEntered       chan struct{}
	inFlight         int
	peakInFlight     int
	inFlightBySource map[string]int
	peakBySource     map[string]int
}

func newBatchConcurrencySSH(delay time.Duration) *batchConcurrencySSH {
	return &batchConcurrencySSH{
		delay:            delay,
		allEntered:       make(chan struct{}),
		inFlightBySource: make(map[string]int),
		peakBySource:     make(map[string]int),
	}
}

func (s *batchConcurrencySSH) ExecSSH(ctx context.Context, hostname, _ string) ([]byte, error) {
	s.enter(hostname)
	defer s.leave(hostname)

	if s.waitForCalls > 0 {
		select {
		case <-s.allEntered:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []byte("ok"), nil
}

func (s *batchConcurrencySSH) ExecSSHBackground(_ context.Context, _ string, _ string, _ time.Duration) ([]byte, error) {
	return []byte("ok"), nil
}

func (s *batchConcurrencySSH) enter(hostname string) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *batchConcurrencySSH) leave(hostname string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.inFlight--
	s.inFlightBySource[hostname]--
}

func (s *batchConcurrencySSH) peakForSource(hostname string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakBySource[hostname]
}

func (s *batchConcurrencySSH) peak() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakInFlight
}
