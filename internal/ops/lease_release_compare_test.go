package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// compareFixture is a leased devpool-3 of repo whose checkout counts one
// commit on no remote branch until a fetch of origin goes through — origin
// already has it, but the container's remote-tracking refs are stale — and
// whose fetches answer fetches in turn (nil: goes through). mate makes zcp a
// Mate, whose git against HQ runs relayed (relayedThrough).
func compareFixture(t *testing.T, repo string, mate bool, fetches ...error) (*hostFixture, func() int) {
	t.Helper()
	hf := newHostFixture(t)
	var (
		mu      sync.Mutex
		armed   bool
		fetched bool
		tries   int
	)
	ssh := &leaseSSH{fn: func(_, command string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case !armed:
			// The acquire's own checkout goes through.
			return nil, nil
		case strings.Contains(command, "fetch -q --prune origin"):
			tries++
			if len(fetches) > 0 {
				err := fetches[0]
				fetches = fetches[1:]
				if err != nil {
					return nil, err
				}
			}
			fetched = true
			return nil, nil
		case strings.Contains(command, "rev-list --count"):
			if fetched {
				return []byte("0\n"), nil
			}
			return []byte("1\n"), nil
		}
		return nil, nil
	}}
	hf.deps.SSH = ssh
	hf.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, testRepo, "feature"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	rec, err := loadLeaseRecord(hf.stateDir, "devpool-3")
	if err != nil {
		t.Fatal(err)
	}
	rec.Repo = repo
	if err := writeLeaseRecord(hf.stateDir, rec); err != nil {
		t.Fatal(err)
	}
	hf.deps.HQAddress = func() string { return leaseHQURL }
	if mate {
		hf.deps.HQGit = relayedThrough{deps: &hf.deps}
	}
	ssh.reset()
	hf.events = nil
	mu.Lock()
	armed = true
	mu.Unlock()
	return hf, func() int { mu.Lock(); defer mu.Unlock(); return tries }
}

// authRefused is a fetch the remote refused for its credential.
func authRefused(t *testing.T, repo string) error {
	t.Helper()
	return sshFailure("fatal: Authentication failed for '"+repo+"/'", exitError(t, 128))
}

// TestLeaseRelease_ComparesWithOriginBeforeCountingCommitsUnpushed (D9, live
// 2026-10-27): a commit pushed from elsewhere is on origin while the lease's
// remote-tracking refs still predate it, so counted against them it reads as
// unpushed and the release is refused for work it would not lose. The guard
// fetches origin first — relayed for a repository on the Mate's HQ — and
// counts again. HQ refusing that fetch, or a repository outside HQ whose host
// refused the pool's token, the guard says it could not compare and why,
// never that the commit is unpushed, and releases nothing.
func TestLeaseRelease_ComparesWithOriginBeforeCountingCommitsUnpushed(t *testing.T) {
	t.Parallel()
	t.Run("the commit is on origin: released", func(t *testing.T) {
		t.Parallel()
		hf, tries := compareFixture(t, leaseHQRepo, true)
		if _, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false); err != nil {
			t.Fatalf("release of a lease whose commit origin has: %v", err)
		}
		if tries() != 1 || len(hf.swarm.releases) != 1 {
			t.Errorf("fetches = %d, releases = %+v; want one fetch and the release", tries(), hf.swarm.releases)
		}
		if !slices.ContainsFunc(hf.deps.SSH.(*leaseSSH).commands(), func(c string) bool {
			return strings.HasPrefix(c, relayedMark) && strings.Contains(c, "fetch -q --prune origin")
		}) {
			t.Error("the guard's fetch of a repository on HQ did not run relayed")
		}
	})
	// witness: internal/ops/lease_checkout.go | _, suggestion = classifyCheckoutFailure(err, onHQ) → _, _ = classifyCheckoutFailure(err, onHQ)
	t.Run("HQ refuses the fetch: could not compare", func(t *testing.T) {
		t.Parallel()
		hf, _ := compareFixture(t, leaseHQRepo, true, authRefused(t, leaseHQRepo))
		_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
		pe := assertRefusedAndKept(t, hf, err)
		if !strings.Contains(pe.Message, "could not compare") || strings.Contains(pe.Message, "would lose") {
			t.Errorf("message %q: want it could not compare, not that releasing would lose work", pe.Message)
		}
		for _, want := range []string{"project key", "zcp git <alias>", "zerops_lease action=release lease=devpool-3 force=true"} {
			if !strings.Contains(pe.Suggestion, want) {
				t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
			}
		}
		if strings.Contains(pe.Suggestion, "Acquire again") {
			t.Errorf("suggestion %q says to acquire again", pe.Suggestion)
		}
	})
	t.Run("a repository outside HQ whose host refused the token: could not compare", func(t *testing.T) {
		t.Parallel()
		hf, tries := compareFixture(t, testRepo, true, authRefused(t, testRepo))
		_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
		pe := assertRefusedAndKept(t, hf, err)
		if !strings.Contains(pe.Message, "could not compare") || pe.Code == platform.ErrGitTokenMissing || pe.Code == platform.ErrGitTokenInvalid {
			t.Errorf("refusal = %s %q: want could not compare, no token code (the release asks for none)", pe.Code, pe.Message)
		}
		if tries() != 1 {
			t.Errorf("fetches = %d, want one: zcp renews no token outside HQ", tries())
		}
	})
	t.Run("origin cannot be fetched otherwise: counted as before", func(t *testing.T) {
		t.Parallel()
		hf, _ := compareFixture(t, testRepo, false, sshFailure("fatal: unable to access: Could not resolve host", exitError(t, 128)))
		_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message, "1 commit") {
			t.Errorf("err = %v, want the refusal counting the commit as before", err)
		}
	})
}
