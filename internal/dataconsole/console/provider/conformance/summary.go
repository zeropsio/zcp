package conformance

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// outcome is one engine case's terminal state — shared by the human-readable
// RunSummary below and the CaseRecord ledger's JSON "outcome" field, so there
// is exactly one vocabulary for "did this pass" across both surfaces.
type outcome string

const (
	outcomePass outcome = "pass"
	outcomeSkip outcome = "skip"
	outcomeFail outcome = "fail"
)

// summaryRecord is one hostname's terminal outcome. family is a plain display
// label (provider.Family stringified by the caller) — summary.go has no
// reason to depend on the provider package for a value it only ever prints.
type summaryRecord struct {
	hostname string
	family   string
	outcome  outcome
	reason   string
}

// RunSummary accumulates per-engine outcomes across one conformance run so a
// partial-profile skip is never silent — TestMain prints it after m.Run(),
// independent of -v.
//
// non-parallel: this package runs with no t.Parallel anywhere (shared live
// services, see doc.go), so a single mutex around the slice append is enough;
// there is no concurrent-test scenario to design for.
type RunSummary struct {
	mu      sync.Mutex
	records []summaryRecord
}

// NewRunSummary constructs an empty summary.
func NewRunSummary() *RunSummary { return &RunSummary{} }

// Record appends one hostname's terminal outcome.
func (s *RunSummary) Record(hostname, family string, o outcome, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, summaryRecord{hostname: hostname, family: family, outcome: o, reason: reason})
}

// Fprint renders the accumulated summary to w, sorted by family then
// hostname for stable, readable output.
func (s *RunSummary) Fprint(w io.Writer) {
	s.mu.Lock()
	recs := make([]summaryRecord, len(s.records))
	copy(recs, s.records)
	s.mu.Unlock()

	_, _ = fmt.Fprintln(w, "\n=== Data Console live conformance — run summary ===")
	if len(recs) == 0 {
		_, _ = fmt.Fprintln(w, "(no cases recorded — DC_LIVE_CONFIG matched nothing any test looked for)")
		_, _ = fmt.Fprintln(w, "====================================================")
		return
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].family != recs[j].family {
			return recs[i].family < recs[j].family
		}
		return recs[i].hostname < recs[j].hostname
	})
	var pass, skip, fail int
	for _, r := range recs {
		switch r.outcome {
		case outcomePass:
			pass++
		case outcomeSkip:
			skip++
		case outcomeFail:
			fail++
		}
		line := fmt.Sprintf("[%-4s] %-9s %-16s", strings.ToUpper(string(r.outcome)), r.family, r.hostname)
		if r.reason != "" {
			line += " — " + r.reason
		}
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintf(w, "--- %d pass, %d skip, %d fail ---\n", pass, skip, fail)
	_, _ = fmt.Fprintln(w, "====================================================")
}

// CaseRecord is one live case's terminal ledger entry — the full per-case
// evidence record serialized to DC_LIVE_SUMMARY (docs/spec-dataconsole-
// testing.md §5). CaseID is the top-level test function name (matching a
// ConformanceCases.TestName, proofs.go), never the "/hostname" subtest path,
// so it cross-references the declared-proof registry directly.
type CaseRecord struct {
	CaseID          string    `json:"caseId"`
	Hostname        string    `json:"hostname"`
	BaseType        string    `json:"baseType"`
	Family          string    `json:"family"`
	ProofIDs        []ProofID `json:"proofIds,omitempty"`
	Outcome         outcome   `json:"outcome"`
	Reason          string    `json:"reason,omitempty"`
	DurationMs      int64     `json:"durationMs"`
	DeclaredVersion string    `json:"declaredVersion,omitempty"`
	RunID           string    `json:"runId"`
	Revision        string    `json:"revision,omitempty"`
}

// Ledger accumulates CaseRecord entries across a run — the JSON evidence
// artifact (DC_LIVE_SUMMARY) and the matrix gate's input.
//
// non-parallel: same no-t.Parallel contract as RunSummary (see its doc), so
// one mutex around the slice append is enough.
type Ledger struct {
	mu      sync.Mutex
	records []CaseRecord
}

// NewLedger constructs an empty Ledger.
func NewLedger() *Ledger { return &Ledger{} }

// Record appends one case's terminal ledger entry.
func (l *Ledger) Record(rec CaseRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
}

// Records returns a copy of every recorded entry, in recording order.
func (l *Ledger) Records() []CaseRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]CaseRecord, len(l.records))
	copy(out, l.records)
	return out
}

// WriteLedgerJSON serializes records to path — the DC_LIVE_SUMMARY evidence
// artifact, written unconditionally (pass or fail) from TestMain.
func WriteLedgerJSON(path string, records []CaseRecord) error {
	if records == nil {
		records = []CaseRecord{}
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("conformance: marshal ledger: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("conformance: write %s: %w", path, err)
	}
	return nil
}

// RequiredCell is one (hostname, proofID) pair the full profile's matrix gate
// requires a passing ledger entry for — docs/spec-dataconsole-testing.md §5.
type RequiredCell struct {
	Hostname string
	BaseType string
	ProofID  ProofID
}

// DeriveRequiredCells computes the full profile's required (hostname,
// proofID) matrix: for each manifest entry, every RequiredProofs(profile) of
// its baseType's registered profile. A manifest entry whose baseType is not
// found in profiles contributes nothing (ValidateManifestAgainstConfig +
// ParseManifest's registered-baseType check already make this unreachable in
// the live harness; this is a defensive no-op, not a silent success).
func DeriveRequiredCells(manifest []ManifestEntry, profiles []provider.ServiceProfile) []RequiredCell {
	index := make(map[string]provider.ServiceProfile, len(profiles))
	for _, p := range profiles {
		index[p.BaseType] = p
	}
	var cells []RequiredCell
	for _, m := range manifest {
		p, ok := index[m.BaseType]
		if !ok {
			continue
		}
		for _, proof := range RequiredProofs(p) {
			cells = append(cells, RequiredCell{Hostname: m.Hostname, BaseType: m.BaseType, ProofID: proof})
		}
	}
	return cells
}

// GateResult is the matrix gate's outcome — OK once every required cell (full
// profile only) has a passing ledger entry; Failures lists each unmet cell,
// sorted for stable, greppable output.
type GateResult struct {
	OK       bool
	Failures []string
}

// EvaluateMatrixGate applies §5's full-profile gate: derive required cells
// from manifest × registry, and fail when any required cell has no passing
// ledger record — missing (no record at all), failed, or skipped. The
// partial profile never gates (§5: "no cell derivation") — it always returns
// OK.
func EvaluateMatrixGate(profile Profile, manifest []ManifestEntry, profiles []provider.ServiceProfile, records []CaseRecord) GateResult {
	if profile != ProfileFull {
		return GateResult{OK: true}
	}
	required := DeriveRequiredCells(manifest, profiles)

	type cellKey struct {
		hostname string
		proof    ProofID
	}
	proven := make(map[cellKey]bool)
	latest := make(map[cellKey]CaseRecord)
	for _, r := range records {
		for _, p := range r.ProofIDs {
			key := cellKey{hostname: r.Hostname, proof: p}
			latest[key] = r
			if r.Outcome == outcomePass {
				proven[key] = true
			}
		}
	}

	var failures []string
	for _, c := range required {
		key := cellKey{hostname: c.Hostname, proof: c.ProofID}
		if proven[key] {
			continue
		}
		if rec, ok := latest[key]; ok {
			failures = append(failures, fmt.Sprintf("%s (%s): proof %q %s", c.Hostname, c.BaseType, c.ProofID, strings.ToUpper(string(rec.Outcome))))
		} else {
			failures = append(failures, fmt.Sprintf("%s (%s): proof %q MISSING", c.Hostname, c.BaseType, c.ProofID))
		}
	}
	sort.Strings(failures)
	return GateResult{OK: len(failures) == 0, Failures: failures}
}
