//go:build e2e

package conformance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

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
	// seedTimeout bounds the SETUP phase (fixture seed/teardown), not any
	// semantic assertion. It is deliberately wider than assertTimeout:
	// meilisearch applies writes through a serial task queue, and the
	// task-CONFIRMED seed (seed.waitMeiliTask) legitimately waits out queue
	// residue from a directly preceding run — observed live at 20-50s drain
	// on the eval testbed. Semantic assertions keep assertTimeout; a seed
	// that cannot confirm within this budget still fails honestly.
	seedTimeout = 120 * time.Second

	// lockAcquireBudget bounds runMain's own wait for RunLock.Acquire — a bit
	// more than the lock's own internal timeout so a real timeout error (not
	// a context cancellation) is what surfaces.
	lockAcquireBudget = defaultLockTimeout + 10*time.Second
)

// Harness state, loaded once by TestMain (via runMain) — every test reads it
// read-only (no t.Parallel anywhere in this package, see doc.go, so there is
// no data race to guard against).
var (
	activeConfig      *LiveConfig
	activeConfigErr   error
	activeProfile     Profile
	activeProfileErr  error
	activeManifest    []ManifestEntry
	activeManifestErr error
	activeNamespace   string
	activeRunID       string
	activeRevision    string
	globalSummary     = NewRunSummary()
	globalLedger      = NewLedger()
)

// TestMain delegates to runMain and exits with its result. Splitting the two
// matters: runMain returns normally (running every deferred cleanup — the run
// lock's Release, notably) before THIS calls os.Exit, which bypasses defers
// entirely. os.Exit must only ever be called here, exactly once.
func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

// runMain loads the DC_LIVE_CONFIG/PROFILE/MANIFEST/NAMESPACE/REVISION/SUMMARY
// environment once, validates the typed manifest against the config, acquires
// the cross-process run lock (§5), THEN sweeps activeNamespace inside the
// mutual-exclusion window (recovery after an interrupted prior run — see
// sweepNamespace), runs the suite, and applies the full-profile engine ×
// proof matrix gate. The JSON ledger evidence artifact and the run summary
// are ALWAYS emitted — every exit path, lock failures included, independent
// of -v — so a failed or refused run still leaves evidence and a
// partial-profile skip is never silent.
func runMain(m *testing.M) int {
	activeConfig, activeConfigErr = LoadFromEnv()
	activeProfile, activeProfileErr = ProfileFromEnv()
	activeManifest, activeManifestErr = ManifestFromEnv()
	if activeManifestErr == nil && activeConfigErr == nil {
		activeManifestErr = ValidateManifestAgainstConfig(activeManifest, activeConfig)
	}
	activeNamespace = NamespaceFromEnv()
	activeRunID = newRunID()
	activeRevision = RevisionFromEnv()

	// The ledger artifact + run summary are written on EVERY exit path —
	// including a lost lock race or a refused full profile — so a failed run
	// always leaves evidence for dc-live-remote's unconditional retrieval
	// (an empty ledger with a stderr cause is honest; a missing file is not).
	defer func() {
		if err := WriteLedgerJSON(SummaryPathFromEnv(), globalLedger.Records()); err != nil {
			fmt.Fprintf(os.Stderr, "conformance: write ledger: %v\n", err)
		}
		globalSummary.Fprint(os.Stdout)
	}()

	release, err := acquireRunLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conformance: %v\n", err)
		return 1
	}
	defer release()

	// Sweep INSIDE the mutual-exclusion window: an unlocked sweep would
	// destroy a concurrently running suite's live fixtures before this
	// process ever blocked on the lock (review finding: fixture mutation
	// belongs after Acquire, §5).
	sweepNamespace()

	code := m.Run()

	gate := EvaluateMatrixGate(activeProfile, activeManifest, provider.ServiceProfiles(), globalLedger.Records())
	if !gate.OK {
		fmt.Fprintln(os.Stderr, "\n=== Data Console live conformance — engine × proof matrix gate FAILED ===")
		for _, f := range gate.Failures {
			fmt.Fprintln(os.Stderr, "  "+f)
		}
		fmt.Fprintln(os.Stderr, "==========================================================================")
		if code == 0 {
			code = 1
		}
	}

	return code
}

// newRunID generates a random per-process identifier stamped onto every
// ledger record and used as the cross-process lock's value — so a losing
// Acquire's timeout error can name the holder.
func newRunID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("pid-%d", os.Getpid()) // best-effort fallback — never fatal over an identifier
	}
	return hex.EncodeToString(buf[:])
}

// acquireRunLock implements §5's cross-process lock policy: with a kv service
// configured, build a real client against it and Acquire (bounded); with none
// configured, the full profile refuses to start (the release manifest always
// carries valkey) while the partial profile proceeds lockless with a logged
// warning. The returned release func is always safe to defer — a no-op when
// no lock was ever taken.
func acquireRunLock() (release func(), err error) {
	noop := func() {}
	if activeConfig == nil {
		return noop, nil
	}
	kvEntries := activeConfig.ByFamily(provider.FamilyKV)
	if len(kvEntries) == 0 {
		if activeProfile == ProfileFull {
			return noop, fmt.Errorf("run lock: full profile requires a configured kv service for the cross-process lock (%s), refusing to start", EnvConfig)
		}
		fmt.Fprintln(os.Stderr, "conformance: run lock: no kv service configured — proceeding lockless (partial profile only)")
		return noop, nil
	}
	kv := kvEntries[0].KV
	if kv == nil {
		return noop, fmt.Errorf("run lock: %q classifies kv but carries no \"kv\" descriptor block", kvEntries[0].Hostname)
	}
	cli := goredis.NewClient(&goredis.Options{Addr: net.JoinHostPort(kv.Host, kv.Port), Password: kv.Password})

	lock := NewRunLock(cli, activeRunID)
	ctx, cancel := context.WithTimeout(context.Background(), lockAcquireBudget)
	defer cancel()
	if err := lock.Acquire(ctx); err != nil {
		_ = cli.Close()
		return noop, fmt.Errorf("run lock: %w", err)
	}
	return func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := lock.Release(dctx); err != nil {
			fmt.Fprintf(os.Stderr, "conformance: run lock: release: %v\n", err)
		}
		dcancel()
		_ = cli.Close()
	}, nil
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
	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()
	if err := seed.Service(ctx, entry.Type, desc, seed.Options{Namespace: activeNamespace}); err != nil {
		skipOrFail(t, entry, fmt.Sprintf("seed fixture: %v", err))
		return nil
	}
	return func() {
		cctx, ccancel := context.WithTimeout(context.Background(), seedTimeout)
		defer ccancel()
		if err := seed.Cleanup(cctx, entry.Type, desc, activeNamespace); err != nil {
			t.Logf("%s: teardown fixture: %v (best-effort; next run's startup sweep will retry)", entry.Hostname, err)
		}
	}
}

// requireHarness fails the calling test HARD (never skip) when the
// environment itself is misconfigured — a malformed DC_LIVE_CONFIG, an
// invalid DC_LIVE_PROFILE, or a typed DC_LIVE_MANIFEST entry that mismatches
// DC_LIVE_CONFIG (wrong baseType, or a hostname absent from it) is an
// operator mistake, not "engine absent" — and a full profile with no
// manifest has nothing to gate. Every test calls this first.
func requireHarness(t *testing.T) {
	t.Helper()
	if activeConfigErr != nil {
		t.Fatalf("%s: %v", EnvConfig, activeConfigErr)
	}
	if activeProfileErr != nil {
		t.Fatalf("%s: %v", EnvProfile, activeProfileErr)
	}
	if activeManifestErr != nil {
		t.Fatalf("%s: %v", EnvManifest, activeManifestErr)
	}
	if activeProfile == ProfileFull && len(activeManifest) == 0 {
		t.Fatalf("%s is required when %s=full", EnvManifest, EnvProfile)
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

// topLevelCaseID strips a subtest's "/hostname" path down to the top-level
// test function name (t.Name() for the "db" subtest of TestTabular_Smoke is
// "TestTabular_Smoke/db") — the form ConformanceCases.TestName + the ledger's
// caseID both use.
func topLevelCaseID(name string) string {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return name
}

// setupService builds the provider for entry and gates on reachability
// (retried) via skipOrFail. It ALSO registers the ledger's automatic
// per-case recording (RecordOnCleanup, summary_test.go): every family case
// calls this exactly once per subtest, so this one seam is what makes every
// live case — pass, skip, or a bare t.Fatalf deep in the case body — land a
// CaseRecord in globalLedger with no per-case-file recording code (§5). On
// any setup failure it calls skipOrFail (which never returns to its caller)
// and returns nil; callers must treat a nil return as "this subtest already
// terminated".
func setupService(t *testing.T, entry ServiceEntry) provider.Provider {
	t.Helper()
	caseID := topLevelCaseID(t.Name())
	baseType := provider.BaseType(entry.Type)
	RecordOnCleanup(t, globalLedger, CaseRecord{
		CaseID:          caseID,
		Hostname:        entry.Hostname,
		BaseType:        baseType,
		Family:          string(entry.Family()),
		ProofIDs:        proofIDsFor(caseID, baseType),
		DeclaredVersion: DeclaredVersion(entry.Type),
		RunID:           activeRunID,
		Revision:        activeRevision,
	})

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

// proofIDsFor cross-references ConformanceCases (proofs.go) for every proof
// caseID (the top-level test name) declares for baseType — the ledger's
// proofIDs field. A (caseID, baseType) pair absent from the registry (a case
// that doesn't declare a proof for this engine) yields nil, not an error —
// recording a ledger entry never depends on a proof declaration existing.
func proofIDsFor(caseID, baseType string) []ProofID {
	out := proofIDsDeclared(caseID, baseType)
	if out != nil {
		return out
	}
	// ProvenBy equivalence (spec §2): a profile whose live proof is carried
	// by an equivalent engine (mysql → mariadb) has NO direct ConformanceCases
	// rows — its records earn the proofs the EQUIVALENT base type declares,
	// because the case genuinely ran against this host; only the declaration
	// is dialect-shared. Without this, a mysql manifest entry's matrix cells
	// could never be marked proven and the release gate could never pass.
	for _, p := range provider.ServiceProfiles() {
		if p.BaseType == baseType && p.ProvenBy != "" {
			return proofIDsDeclared(caseID, p.ProvenBy)
		}
	}
	return nil
}

// proofIDsDeclared returns the proofs caseID directly declares for baseType.
func proofIDsDeclared(caseID, baseType string) []ProofID {
	var out []ProofID
	for _, c := range ConformanceCases {
		if c.TestName != caseID {
			continue
		}
		for _, bt := range c.BaseTypes {
			if bt == baseType {
				out = append(out, c.Proof)
				break
			}
		}
	}
	return out
}

// TestProofIDsFor_ProvenByFallback pins review finding 3: a base type whose
// profile declares ProvenBy earns the proofs its EQUIVALENT declares, so a
// mysql manifest entry's matrix cells are provable at all. Pure — no engine.
func TestProofIDsFor_ProvenByFallback(t *testing.T) {
	direct := proofIDsFor("TestTabular_FullEngineWriteAndFidelity", "mariadb")
	if len(direct) == 0 {
		t.Fatal("mariadb declares no proofs for TestTabular_FullEngineWriteAndFidelity — fixture assumption broken")
	}
	viaEquiv := proofIDsFor("TestTabular_FullEngineWriteAndFidelity", "mysql")
	if len(viaEquiv) != len(direct) {
		t.Fatalf("mysql (ProvenBy mariadb) proofs = %v, want mariadb's %v", viaEquiv, direct)
	}
	if got := proofIDsFor("TestTabular_FullEngineWriteAndFidelity", "no-such-type"); got != nil {
		t.Fatalf("unregistered base type proofs = %v, want nil", got)
	}
}

// recordSummary records the operator-facing per-case run-summary line for
// hostname/family, HONESTLY: a case that reached its end with t.Errorf
// failures records fail, never pass (review finding 6 — the summary printed
// [PASS] for engines whose assertions failed non-fatally; the ledger already
// handled this via t.Failed() in its cleanup hook).
func recordSummary(t *testing.T, hostname, family string) {
	t.Helper()
	if t.Failed() {
		globalSummary.Record(hostname, family, outcomeFail, "assertion failure (see test output)")
		return
	}
	globalSummary.Record(hostname, family, outcomePass, "")
}
