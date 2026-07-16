package conformance

import (
	"fmt"
	"io"
	"sort"
	"sync"
)

// outcome is one engine case's terminal state in the run summary.
type outcome string

const (
	outcomePass outcome = "PASS"
	outcomeSkip outcome = "SKIP"
	outcomeFail outcome = "FAIL"
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
		line := fmt.Sprintf("[%-4s] %-9s %-16s", r.outcome, r.family, r.hostname)
		if r.reason != "" {
			line += " — " + r.reason
		}
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintf(w, "--- %d pass, %d skip, %d fail ---\n", pass, skip, fail)
	_, _ = fmt.Fprintln(w, "====================================================")
}
