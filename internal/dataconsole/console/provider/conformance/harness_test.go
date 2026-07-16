//go:build e2e

package conformance

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/document"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/kv"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/object"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/stream"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/tabular"
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
	globalSummary    = NewRunSummary()
)

// TestMain loads the DC_LIVE_CONFIG/PROFILE/MANIFEST environment once, runs
// the suite, then ALWAYS prints the run summary — independent of -v, so a
// partial-profile skip is never silent.
func TestMain(m *testing.M) {
	activeConfig, activeConfigErr = LoadFromEnv()
	activeProfile, activeProfileErr = ProfileFromEnv()
	activeManifest = ManifestFromEnv()

	code := m.Run()
	globalSummary.Fprint(os.Stdout)
	os.Exit(code)
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

// buildProvider replicates cmd/zcp/studio_console.go's per-family factory
// construction INSIDE the island (studio_console.go lives in cmd/zcp, outside
// console/, so it cannot be imported here — see doc.go "Island"). Kept in
// lockstep with those factories by inspection; a drift here means this suite
// is no longer testing what production actually builds.
func buildProvider(entry ServiceEntry, readOnly bool) (provider.Provider, error) {
	desc, err := entry.Descriptor()
	if err != nil {
		return nil, fmt.Errorf("descriptor: %w", err)
	}
	switch d := desc.(type) {
	case provider.SQLConn:
		return tabular.New(tabular.Config{Conn: d, ReadOnly: readOnly})
	case provider.KVConn:
		return kv.New(kv.Config{Addr: net.JoinHostPort(d.Host, d.Port), Password: d.Password, ReadOnly: readOnly})
	case provider.ObjectConn:
		return object.New(object.Config{
			Endpoint: d.Endpoint, AccessKey: d.AccessKey, SecretKey: d.SecretKey,
			Bucket: d.Bucket, Secure: d.Secure, ReadOnly: readOnly,
		})
	case provider.DocumentConn:
		return document.New(document.Config{Engine: d.Engine, BaseURL: d.BaseURL, User: d.User, APIKey: d.APIKey, ReadOnly: readOnly})
	case provider.StreamConn:
		return stream.New(stream.Config{Engine: d.Engine, Addr: net.JoinHostPort(d.Host, d.Port), User: d.User, Password: d.Password})
	default:
		return nil, fmt.Errorf("unhandled descriptor type %T", desc)
	}
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
func setupService(t *testing.T, entry ServiceEntry, readOnly bool) provider.Provider {
	t.Helper()
	prov, err := buildProvider(entry, readOnly)
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
