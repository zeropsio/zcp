package conformance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// caseOutcome maps a subtest's terminal (failed, skipped) status to an
// outcome — the exact decision RecordOnCleanup's t.Cleanup applies. Factored
// out as a pure function so the fail branch is unit-testable directly: a
// REAL failing subtest propagates its failure up to ITS parent (confirmed
// behavior of testing.T — a t.Run child that fails also marks the parent
// failed), which would incorrectly turn this package's own test suite red
// just to prove the mapping. The pass/skip branches ARE additionally proven
// end-to-end below via real subtests, since neither propagates failure.
func caseOutcome(failed, skipped bool) outcome {
	switch {
	case failed:
		return outcomeFail
	case skipped:
		return outcomeSkip
	default:
		return outcomePass
	}
}

// RecordOnCleanup registers a t.Cleanup on t that appends a CaseRecord to
// ledger reflecting t's terminal outcome (pass/fail/skip) and the elapsed
// time since this call — the mechanism setupService (harness_test.go) relies
// on so every live case is recorded automatically, including a bare
// t.Fatalf/t.Error path inside the case body that never calls a record
// function itself. base carries every field known up front (hostname,
// baseType, family, proofIDs, declaredVersion, runID, revision); Outcome and
// DurationMs are filled in at cleanup time. It lives in a test file (not
// summary.go) because it is the one piece of the ledger that genuinely needs
// *testing.T — everything else (CaseRecord, Ledger, the matrix gate) is
// plain, non-test package code.
func RecordOnCleanup(t *testing.T, ledger *Ledger, base CaseRecord) {
	t.Helper()
	start := time.Now()
	t.Cleanup(func() {
		rec := base
		rec.DurationMs = time.Since(start).Milliseconds()
		rec.Outcome = caseOutcome(t.Failed(), t.Skipped())
		ledger.Record(rec)
	})
}

// ---- caseOutcome: the fail branch, proven directly (see the doc comment
// above for why a real failing subtest can't be used here). ----

func TestCaseOutcome_MapsFailedSkippedToOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		failed, skipped bool
		want            outcome
	}{
		{"failed wins even if also skipped", true, true, outcomeFail},
		{"failed alone", true, false, outcomeFail},
		{"skipped, not failed", false, true, outcomeSkip},
		{"neither — pass", false, false, outcomePass},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := caseOutcome(c.failed, c.skipped); got != c.want {
				t.Errorf("caseOutcome(%v, %v) = %q, want %q", c.failed, c.skipped, got, c.want)
			}
		})
	}
}

// ---- Ledger / RecordOnCleanup: the automatic per-case recording mechanism
// setupService relies on (harness_test.go) so a t.Fatalf path inside a live
// case — one that never explicitly calls a record function — still lands a
// "fail" entry in the ledger. The pass/skip wiring (Cleanup registration,
// duration capture, ledger append) is exercised here through REAL subtests
// whose outcome never propagates to this test: a pass and a skip are both
// safe (confirmed above), so this stays fully offline — no live engine, no
// e2e build tag needed. The fail *mapping* is covered by
// TestCaseOutcome_MapsFailedSkippedToOutcome instead of a real failing
// subtest here, for the reason in RecordOnCleanup's doc comment. ----

func TestLedger_CleanupRecordsPassAndSkip(t *testing.T) {
	var ledger Ledger

	okPass := t.Run("passing", func(t *testing.T) {
		RecordOnCleanup(t, &ledger, CaseRecord{
			CaseID: "TestFake", Hostname: "db", BaseType: "postgresql", Family: "tabular",
			ProofIDs: []ProofID{ProofBrowseTree, ProofReadValue}, DeclaredVersion: "18", RunID: "run-abc", Revision: "deadbeef",
		})
	})
	if !okPass {
		t.Fatal("the passing subtest must not itself fail")
	}

	okSkip := t.Run("skipping", func(t *testing.T) {
		RecordOnCleanup(t, &ledger, CaseRecord{CaseID: "TestFake", Hostname: "storage", BaseType: "object-storage", Family: "object"})
		t.Skip("no engine configured")
	})
	if !okSkip {
		t.Fatal("a skipped subtest must not report failure to t.Run")
	}

	recs := ledger.Records()
	if len(recs) != 2 {
		t.Fatalf("Records() = %d entries, want 2: %+v", len(recs), recs)
	}
	byHostname := make(map[string]CaseRecord, len(recs))
	for _, r := range recs {
		byHostname[r.Hostname] = r
	}

	pass, ok := byHostname["db"]
	if !ok || pass.Outcome != outcomePass {
		t.Errorf("db record = %+v, want outcome %q", pass, outcomePass)
	}
	if pass.DurationMs < 0 {
		t.Errorf("db record DurationMs = %d, want >= 0", pass.DurationMs)
	}
	if pass.CaseID != "TestFake" || pass.BaseType != "postgresql" || pass.Family != "tabular" {
		t.Errorf("db record didn't carry through base fields: %+v", pass)
	}
	if len(pass.ProofIDs) != 2 || pass.DeclaredVersion != "18" || pass.RunID != "run-abc" || pass.Revision != "deadbeef" {
		t.Errorf("db record didn't carry through proof/version/run fields: %+v", pass)
	}

	skip, ok := byHostname["storage"]
	if !ok || skip.Outcome != outcomeSkip {
		t.Errorf("storage record = %+v, want outcome %q", skip, outcomeSkip)
	}

	// ---- JSON round-trips the full record shape ----
	raw, err := json.Marshal(recs)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded []CaseRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded) != len(recs) {
		t.Fatalf("round-trip: got %d records, want %d", len(decoded), len(recs))
	}
	for i := range recs {
		if !reflect.DeepEqual(decoded[i], recs[i]) {
			t.Errorf("round-trip[%d] = %+v, want %+v", i, decoded[i], recs[i])
		}
	}
}

// ---- EvaluateMatrixGate: the full-profile engine × proof matrix decision.
// Independent oracle: the expected valkey (kv, full) required-proof COUNT (9)
// is hand-derived from proofs.go's own documented baseRequiredProofs entry
// for {FamilyKV, SupportFull} — ProofBrowseTree, ProofReadValue, ProofDownloadContent,
// ProofWriteRoundtrip, ProofDelete, ProofTTL, ProofCreateCollision,
// ProofWrongTypeGuard, ProofValueFidelity — not recomputed by calling
// RequiredProofs and asserting against its own output. ----

func TestMatrixGate_Decision(t *testing.T) {
	t.Parallel()
	manifest := []ManifestEntry{{Hostname: "cache", BaseType: "valkey"}}
	profiles := provider.ServiceProfiles()
	const valkeyRequiredProofCount = 9 // see doc comment above

	t.Run("full + no records at all: every required cell missing", func(t *testing.T) {
		t.Parallel()
		got := EvaluateMatrixGate(ProfileFull, manifest, profiles, nil)
		if got.OK {
			t.Fatal("want gate NOT ok when nothing was ever recorded")
		}
		if len(got.Failures) != valkeyRequiredProofCount {
			t.Fatalf("Failures = %d, want %d (one per required valkey proof): %v", len(got.Failures), valkeyRequiredProofCount, got.Failures)
		}
		for _, f := range got.Failures {
			if !strings.Contains(f, "cache") || !strings.Contains(f, "MISSING") {
				t.Errorf("failure line %q must name the hostname and MISSING", f)
			}
		}
	})

	t.Run("full + one required proof recorded FAIL: exactly that cell fails", func(t *testing.T) {
		t.Parallel()
		// Every OTHER required proof passes; ProofDelete's ONLY record is a
		// fail (never also recorded pass elsewhere — a proof recorded pass
		// anywhere counts as proven per §5, so this must be its sole record).
		records := allValkeyProofsExceptAs(ProofDelete, outcomePass)
		records = append(records, CaseRecord{Hostname: "cache", ProofIDs: []ProofID{ProofDelete}, Outcome: outcomeFail})
		got := EvaluateMatrixGate(ProfileFull, manifest, profiles, records)
		if got.OK {
			t.Fatal("want gate NOT ok when a required proof's only record is a fail")
		}
		if len(got.Failures) != 1 {
			t.Fatalf("Failures = %v, want exactly 1 (delete)", got.Failures)
		}
		if !strings.Contains(got.Failures[0], "delete") || !strings.Contains(got.Failures[0], "FAIL") {
			t.Errorf("failure line %q must name the proof (delete) and FAIL", got.Failures[0])
		}
	})

	t.Run("full + every required proof recorded pass: clean", func(t *testing.T) {
		t.Parallel()
		got := EvaluateMatrixGate(ProfileFull, manifest, profiles, allValkeyProofsAs(outcomePass))
		if !got.OK || len(got.Failures) != 0 {
			t.Fatalf("GateResult = %+v, want OK with no failures", got)
		}
	})

	t.Run("partial profile never gates, even with zero records", func(t *testing.T) {
		t.Parallel()
		got := EvaluateMatrixGate(ProfilePartial, manifest, profiles, nil)
		if !got.OK || len(got.Failures) != 0 {
			t.Fatalf("GateResult = %+v, want OK (partial never derives required cells)", got)
		}
	})
}

// allValkeyProofsAs builds one CaseRecord per valkey-required ProofID
// (independent oracle list, matching TestMatrixGate_Decision's doc comment),
// each carrying outcome o for hostname "cache".
func allValkeyProofsAs(o outcome) []CaseRecord {
	return allValkeyProofsExceptAs("", o)
}

// allValkeyProofsExceptAs is allValkeyProofsAs, omitting the one proof named
// except (pass "" to omit none) — for a test that needs every OTHER required
// proof settled one way while asserting on that one proof's own, separately
// recorded outcome.
func allValkeyProofsExceptAs(except ProofID, o outcome) []CaseRecord {
	proofs := []ProofID{
		ProofBrowseTree, ProofReadValue, ProofDownloadContent, ProofWriteRoundtrip, ProofDelete,
		ProofTTL, ProofCreateCollision, ProofWrongTypeGuard, ProofValueFidelity,
	}
	out := make([]CaseRecord, 0, len(proofs))
	for _, p := range proofs {
		if p == except {
			continue
		}
		out = append(out, CaseRecord{Hostname: "cache", ProofIDs: []ProofID{p}, Outcome: o})
	}
	return out
}
