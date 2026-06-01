package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// seedLaunchGateReadyMeta writes a complete ServiceMeta into stateDir
// that passes the launch source-control gate's checks 1-2 (and check
// 3 when paired with installFakeLiveRemoteReader returning the same
// URL).
//
// Used by every launch test that exercises the post-scope-prompt
// pipeline (classify / ready / launching / launched). Tests that
// intentionally exercise gate-failure paths construct the meta
// themselves with the missing field.
//
// Fields seeded:
//   - Hostname, Mode (default ModeDev unless overridden)
//   - GitPushState=configured, RemoteURL (the supplied url)
//   - BootstrapSession="" (adopted shape; gate treats either way)
//   - BootstrappedAt=now (IsComplete() true)
//   - FirstDeployedAt=now (avoids develop-flow blocker on first deploy)
//   - BuildIntegration=actions (skips build-integration-recommended
//     warn for the canonical happy path)
//
// Callers that need a different mode / pair shape pass options.
//
//nolint:unparam // return value preserved so call sites can introspect the seeded meta in-line; current tests discard it but the API is documented
func seedLaunchGateReadyMeta(t *testing.T, stateDir string, hostname, remoteURL string, opts ...seedMetaOption) *workflow.ServiceMeta {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02")
	meta := &workflow.ServiceMeta{
		Hostname:                 hostname,
		Mode:                     topology.ModeDev,
		GitPushState:             topology.GitPushConfigured,
		RemoteURL:                remoteURL,
		BuildIntegration:         topology.BuildIntegrationActions,
		CloseDeployMode:          topology.CloseModeAuto,
		CloseDeployModeConfirmed: true,
		BootstrappedAt:           now,
		FirstDeployedAt:          time.Now().UTC().Format(time.RFC3339),
	}
	for _, o := range opts {
		o(meta)
	}
	servicesDir := filepath.Join(stateDir, "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	path := filepath.Join(servicesDir, meta.Hostname+".json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	return meta
}

// seedMetaOption mutates a ServiceMeta before write. Used for tests
// that need a non-default shape (pair mode, missing build-integration,
// etc.).
type seedMetaOption func(*workflow.ServiceMeta)

// withMetaMode sets Mode on the seeded meta. Default is ModeDev.
func withMetaMode(mode topology.Mode) seedMetaOption {
	return func(m *workflow.ServiceMeta) { m.Mode = mode }
}

// withMetaStageHostname sets StageHostname on the seeded meta (pair
// shape).
func withMetaStageHostname(stageHost string) seedMetaOption {
	return func(m *workflow.ServiceMeta) { m.StageHostname = stageHost }
}

// withMetaGitPushUnconfigured sets GitPushState=unconfigured to exercise
// the gate's unconfigured branch (the only state these gate tests need).
func withMetaGitPushUnconfigured() seedMetaOption {
	return func(m *workflow.ServiceMeta) { m.GitPushState = topology.GitPushUnconfigured }
}

// withMetaBuildIntegration overrides BuildIntegration. Default is
// `actions` (so the warn blocker is suppressed); pass
// BuildIntegrationNone to exercise the warn-blocker path.
func withMetaBuildIntegration(bi topology.BuildIntegration) seedMetaOption {
	return func(m *workflow.ServiceMeta) { m.BuildIntegration = bi }
}

// installFakeLiveRemoteReader swaps the source-control gate's live-
// remote reader for a closure that returns the supplied URL for the
// supplied hostname (and empty for everything else). Tests pair this
// with seedLaunchGateReadyMeta(t, stateDir, hostname, sameURL) so
// the gate's check 3 (live == meta) passes.
//
// Also installs a happy-path push-proof reader returning DirtyTree=false
// + LocalHead==RemoteHead==<canonical sha> for every hostname in the
// map, so P3 checks 4-5 also pass for the canonical happy path. Tests
// that need to exercise the P3 failure branches override via
// installFakePushProofReader after this call.
//
// Cleanup registers via t.Cleanup so the swap reverts even on test
// failure / panic.
func installFakeLiveRemoteReader(t *testing.T, urlByHostname map[string]string) {
	t.Helper()
	cleanup := setLaunchLiveRemoteReader(func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, hostname string) (string, error) {
		if url, ok := urlByHostname[hostname]; ok {
			return url, nil
		}
		return "", nil
	})
	t.Cleanup(cleanup)
	// Happy-path push-proof default: every hostname listed is "clean +
	// pushed". Tests that exercise P3 failure paths override.
	installFakePushProofReader(t, defaultGateReadyPushProof(urlByHostname))
}

// canonicalLaunchTestHeadSHA is the SHA the happy-path push-proof
// reader returns for both LocalHead and RemoteHead. Stable across runs
// so tests can match without re-deriving.
const canonicalLaunchTestHeadSHA = "deadbeefcafebabefacefeedbaadf00dba5eba11"

// defaultGateReadyPushProof builds the canonical "clean + pushed"
// push-proof map for the canonical happy path. Every hostname that
// has a remote URL gets DirtyTree=false + matching LocalHead/RemoteHead.
func defaultGateReadyPushProof(urlByHostname map[string]string) map[string]LaunchPushProofResult {
	out := make(map[string]LaunchPushProofResult, len(urlByHostname))
	for host := range urlByHostname {
		out[host] = LaunchPushProofResult{
			DirtyTree:  false,
			LocalHead:  canonicalLaunchTestHeadSHA,
			RemoteHead: canonicalLaunchTestHeadSHA,
		}
	}
	return out
}

// installFakePushProofReader swaps the gate's push-proof reader with a
// per-hostname lookup. Tests that exercise P3 failure paths supply a
// map with the failure shape (DirtyTree=true OR LocalHead!=RemoteHead).
func installFakePushProofReader(t *testing.T, proofByHostname map[string]LaunchPushProofResult) {
	t.Helper()
	cleanup := setLaunchPushProofReader(func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, hostname string, _ string) (LaunchPushProofResult, error) {
		if p, ok := proofByHostname[hostname]; ok {
			return p, nil
		}
		// Hostname not in the map = no remote URL set on the meta →
		// gate's P3 branch never runs (checks 1-3 already failed).
		// Return empty so the gate handles the empty-LocalHead branch
		// uniformly.
		return LaunchPushProofResult{}, nil
	})
	t.Cleanup(cleanup)
}

// installLaunchGateReady seeds the most common test fixture: one meta
// for the runtime hostname plus the matching live-remote stub. Returns
// the stateDir created (caller passes to handleLaunchProduction).
// Equivalent to:
//
//	stateDir := withTempState(t)
//	seedLaunchGateReadyMeta(t, stateDir, hostname, remoteURL)
//	installFakeLiveRemoteReader(t, map[string]string{hostname: remoteURL})
func installLaunchGateReady(t *testing.T, stateDir, hostname, remoteURL string, opts ...seedMetaOption) {
	t.Helper()
	seedLaunchGateReadyMeta(t, stateDir, hostname, remoteURL, opts...)
	installFakeLiveRemoteReader(t, map[string]string{hostname: remoteURL})
}

// stateDirWithLaunchGate returns a fresh temp state-dir with the
// canonical happy-path launch-gate setup seeded: one meta for `hostname`
// (RemoteURL=remoteURL, GitPushState=configured, BuildIntegration=actions)
// + the matching fake live-remote reader. One-liner for the dozens of
// launch tests that exercise post-gate behavior (classify-prompt,
// ready-to-launch, mutation, etc.) but don't care about the gate
// machinery.
//
//nolint:unparam // hostname parameterized so multi-runtime tests in P2 can pass "appdev"/"workerstage" through the same helper; today's tests all use "app"
func stateDirWithLaunchGate(t *testing.T, hostname, remoteURL string, opts ...seedMetaOption) string {
	t.Helper()
	dir := t.TempDir()
	installLaunchGateReady(t, dir, hostname, remoteURL, opts...)
	return dir
}

// canonicalLaunchTestRemoteURL is the source-repo URL the dozens of
// launch happy-path tests use when seeding gate-ready metas + the
// matching live-remote stub. Keeps a single constant so updates flow
// through all helpers consistently.
const canonicalLaunchTestRemoteURL = "https://github.com/example/myapp.git"
