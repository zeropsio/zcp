//go:build e2e

package conformance

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/factory"
	"github.com/zeropsio/zcp/internal/dataconsole/console/seed"
)

const (
	// reachAttempts/reachDelay bound the reachability RETRY (VPN/dial jitter
	// is not a finding). reachTimeout/assertTimeout bound each individual
	// call; semantic assertions after a successful reachability probe never
	// retry — see doc.go "Retry policy".
	reachAttempts = 3
	reachDelay    = 2 * time.Second
	reachTimeout  = 20 * time.Second
	assertTimeout = 30 * time.Second
)

// Harness state, loaded once by TestMain — every test reads it read-only
// (no t.Parallel anywhere in this package, see doc.go, so there is no data
// race to guard against).
var (
	activeConfig     *LiveConfig
	activeConfigErr  error
	activeProfile    Profile
	activeProfileErr error
	activeManifest   []string
	activeNamespace  string
	globalSummary    = NewRunSummary()
)

// TestMain loads the DC_LIVE_CONFIG/PROFILE/MANIFEST/NAMESPACE environment
// once, sweeps activeNamespace (recovery after an interrupted prior run —
// see sweepNamespace), runs the suite, then ALWAYS prints the run summary —
// independent of -v, so a partial-profile skip is never silent.
func TestMain(m *testing.M) {
	activeConfig, activeConfigErr = LoadFromEnv()
	activeProfile, activeProfileErr = ProfileFromEnv()
	activeManifest = ManifestFromEnv()
	activeNamespace = NamespaceFromEnv()

	sweepNamespace()

	code := m.Run()
	globalSummary.Fprint(os.Stdout)
	os.Exit(code)
}

// sweepNamespace removes any activeNamespace fixtures left behind by an
// earlier interrupted run, for every configured service, BEFORE any test
// seeds fresh ones (S10b recovery). It is deliberately best-effort: TestMain
// has no *testing.T to skip/fail through, so a sweep failure (an engine
// simply absent/unreachable — the ordinary case locally under the partial
// profile) is logged to stderr and never fatal; the per-test
// setupService/skipOrFail gate is what actually enforces reachability once
// the real tests run. A nil/empty activeConfig (DC_LIVE_CONFIG unset, or
// malformed — activeConfigErr covers that per-test via requireHarness)
// makes this a fast no-op.
func sweepNamespace() {
	if activeConfig == nil {
		return
	}
	for _, entry := range activeConfig.Services {
		desc, err := entry.Descriptor()
		if err != nil {
			continue // config-shape errors are surfaced per-test by requireHarness/validate
		}
		ctx, cancel := context.WithTimeout(context.Background(), reachTimeout)
		if err := seed.Cleanup(ctx, entry.Type, desc, activeNamespace); err != nil {
			fmt.Fprintf(os.Stderr, "sweep %s (namespace %s): %v\n", entry.Hostname, activeNamespace, err)
		}
		cancel()
	}
}

// seedNamespacedFixture seeds entry's namespaced fixture (console/seed,
// under activeNamespace) so a family smoke test always has real data to
// read rather than skipping on an unseeded engine. A seed failure routes
// through skipOrFail — same profile/manifest gate as an unreachable engine,
// since "seeding failed" is a setup-phase condition, not a semantic
// assertion — and never returns to its caller; a nil return means the
// calling subtest already terminated (mirrors setupService's contract).
// On success it returns a teardown func the caller must defer; teardown is
// itself best-effort (logged, not fatal — sweepNamespace is the safety net
// for anything a defer doesn't reach, e.g. a hard process kill).
func seedNamespacedFixture(t *testing.T, entry ServiceEntry, desc provider.ConnectionDescriptor) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
	defer cancel()
	if err := seed.Service(ctx, entry.Type, desc, seed.Options{Namespace: activeNamespace}); err != nil {
		skipOrFail(t, entry, fmt.Sprintf("seed fixture: %v", err))
		return nil
	}
	return func() {
		cctx, ccancel := context.WithTimeout(context.Background(), assertTimeout)
		defer ccancel()
		if err := seed.Cleanup(cctx, entry.Type, desc, activeNamespace); err != nil {
			t.Logf("%s: teardown fixture: %v (best-effort; next run's startup sweep will retry)", entry.Hostname, err)
		}
	}
}

// requireHarness fails the calling test HARD (never skip) when the
// environment itself is misconfigured — a malformed DC_LIVE_CONFIG or an
// invalid DC_LIVE_PROFILE is an operator mistake, not "engine absent", and a
// full profile with no manifest has nothing to gate. Every test calls this
// first.
func requireHarness(t *testing.T) {
	t.Helper()
	if activeConfigErr != nil {
		t.Fatalf("%s: %v", EnvConfig, activeConfigErr)
	}
	if activeProfileErr != nil {
		t.Fatalf("%s: %v", EnvProfile, activeProfileErr)
	}
	if activeProfile == ProfileFull && len(activeManifest) == 0 {
		t.Fatalf("%s is required when %s=full", EnvManifest, EnvProfile)
	}
}

// TestManifest_FullProfileCoverage is the config-PRESENCE half of manifest
// enforcement: a manifest hostname that is entirely absent from
// DC_LIVE_CONFIG never reaches any per-family loop below (ByFamily can only
// iterate what IS configured), so this dedicated case is what actually makes
// "absent" fail the run under the full profile. The per-family tests provide
// the complementary reachability/assertion half via skipOrFail.
func TestManifest_FullProfileCoverage(t *testing.T) {
	requireHarness(t)
	if activeProfile != ProfileFull {
		t.Skip("manifest config-presence coverage only enforced under the full profile")
	}
	for _, hostname := range activeManifest {
		if _, ok := activeConfig.Lookup(hostname); !ok {
			reason := fmt.Sprintf("required by %s but absent from %s", EnvManifest, EnvConfig)
			globalSummary.Record(hostname, "?", outcomeFail, reason)
			t.Errorf("%s: %s", hostname, reason)
		}
	}
}

// skipOrFail applies the profile/manifest decision (RequiredByManifest) and
// terminates the calling (sub)test: t.Fatalf under a required full-profile
// miss, t.Skipf otherwise. It never returns.
func skipOrFail(t *testing.T, entry ServiceEntry, reason string) {
	t.Helper()
	family := string(entry.Family())
	if RequiredByManifest(activeProfile, activeManifest, entry.Hostname) {
		globalSummary.Record(entry.Hostname, family, outcomeFail, reason)
		t.Fatalf("%s: %s (required by %s under the full profile)", entry.Hostname, reason, EnvManifest)
	}
	globalSummary.Record(entry.Hostname, family, outcomeSkip, reason)
	t.Skipf("%s: %s", entry.Hostname, reason)
}

// buildProvider constructs entry's provider through factory.New — the SAME
// single construction path production uses (cmd/zcp/studio_console.go's
// per-family factories delegate to it too), armed=true: the release posture,
// exactly how a real --allow-writes launch builds every family. There is no
// second, harness-local switch to drift out of lockstep with production
// anymore — a provider's Config wiring changes in exactly one place. A
// view-only/read-only-by-nature engine (ClickHouse, qdrant, stream) stays
// read-only regardless: its OWN constructor is the ultimate posture owner
// (factory.New never re-decides that), so arming it here proves nothing
// about those constructors' read-only guarantees — see
// tabular/dialect_test.go's TestNew_Clickhouse_ForcesNoEditUnderArmedPosture
// for that characterization.
func buildProvider(entry ServiceEntry) (provider.Provider, error) {
	desc, err := entry.Descriptor()
	if err != nil {
		return nil, fmt.Errorf("descriptor: %w", err)
	}
	return factory.New(desc, true)
}

// retryHealth probes Health with brief retries — reachability/setup MAY
// retry (VPN/dial jitter), per the S10a retry policy; semantic assertions
// called AFTER this succeeds never do.
func retryHealth(ctx context.Context, prov provider.Provider, attempts int, delay time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		hctx, cancel := context.WithTimeout(ctx, reachTimeout)
		err := prov.Health(hctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if i < attempts-1 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}

// setupService builds the provider for entry and gates on reachability
// (retried) via skipOrFail. On any failure it calls skipOrFail (which never
// returns to its caller) and returns nil; callers must treat a nil return as
// "this subtest already terminated".
//
// The second parameter is intentionally unused: buildProvider always arms
// (see its doc), so every call site's former readOnly/writable choice no
// longer changes construction — a per-case-file posture toggle would only
// diverge from what production actually builds. It stays in the signature
// because every case file (outside this slice's write-set) still calls this
// positionally with a bool.
func setupService(t *testing.T, entry ServiceEntry, _ bool) provider.Provider {
	t.Helper()
	prov, err := buildProvider(entry)
	if err != nil {
		skipOrFail(t, entry, fmt.Sprintf("build provider: %v", err))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), reachTimeout*time.Duration(reachAttempts))
	defer cancel()
	if err := retryHealth(ctx, prov, reachAttempts, reachDelay); err != nil {
		_ = prov.Close()
		skipOrFail(t, entry, fmt.Sprintf("unreachable: %v", err))
		return nil
	}
	return prov
}
