// Package console: the findings model (docs/spec-eval-farm.md §8.3 item 3,
// §8.8 vocabulary) — cause-class vocabulary shared by RunRow, BatchRow and
// Problem, plus FindingRow: one row per finding of a current ok
// observation, everything a list/detail view needs already resolved.
package console

import (
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// Observation.Status literals (§7.5) — observer.go keeps its own copies
// unexported (statusError/statusUnparsed); the console reads the same wire
// values, so it names them itself rather than comparing to raw strings at
// every call site.
const (
	observationStatusOK       = "ok"
	observationStatusUnparsed = "unparsed"
	observationStatusError    = "error"
)

// Cause classes (§8.8): "Cause class: ZCP (guidance, tool) · Test
// (scenario, check) · Agent · Platform." Lists order causes ZCP first —
// causeClassOrder is that fixed order, used everywhere a cause class is
// aggregated or listed (RunRow.CauseCounts, BatchRow's ZCP counts, the
// `cause` list filter, §8.6's problem cause labels).
const (
	CauseClassZCP      = "zcp"
	CauseClassTest     = "test"
	CauseClassAgent    = "agent"
	CauseClassPlatform = "platform"
)

var causeClassOrder = []string{CauseClassZCP, CauseClassTest, CauseClassAgent, CauseClassPlatform}

// causeLabelByOwner and causeClassByOwner map a finding's owner (§7.5:
// zcp-guidance|zcp-tool|platform|agent|scenario|evaluator —
// observer.Finding.Owner) to §8.8's display label ("Cause") and cause
// class. "evaluator" is §8.8's "Test check": the finding that says a
// deterministic check itself is wrong or missing (observer.go's
// reconcileChecks).
var causeLabelByOwner = map[string]string{
	"zcp-guidance": "ZCP guidance",
	"zcp-tool":     "ZCP tool",
	"platform":     "Zerops platform",
	"agent":        "Agent mistake",
	"scenario":     "Test scenario",
	"evaluator":    "Test check",
}

var causeClassByOwner = map[string]string{
	"zcp-guidance": CauseClassZCP,
	"zcp-tool":     CauseClassZCP,
	"platform":     CauseClassPlatform,
	"agent":        CauseClassAgent,
	"scenario":     CauseClassTest,
	"evaluator":    CauseClassTest,
}

// CauseLabel and CauseClass return §8.8's display label / cause class for
// a finding's owner — "" for an owner outside the vocabulary (never
// produced by a repaired observation, §7.5's validOwner, but tolerated
// here rather than panicking on an unexpected value).
func CauseLabel(owner string) string { return causeLabelByOwner[owner] }
func CauseClass(owner string) string { return causeClassByOwner[owner] }

// CauseClassCount is one cause class's finding counts by severity —
// RunRow's and BatchRow's "per-cause-class finding counts by severity"
// (§8.3 items 1/2). newCauseClassCounts always returns one zero-valued
// entry per class, in causeClassOrder, so a caller never has to special-
// case a class with no findings.
type CauseClassCount struct {
	Class  string
	High   int
	Medium int
	Low    int
}

func newCauseClassCounts() []CauseClassCount {
	out := make([]CauseClassCount, len(causeClassOrder))
	for i, c := range causeClassOrder {
		out[i] = CauseClassCount{Class: c}
	}
	return out
}

// addFindingToCounts increments counts' entry for f's cause class by f's
// severity — a no-op when f's owner or severity falls outside the
// vocabulary (never produced by a repaired observation, but tolerated).
func addFindingToCounts(counts []CauseClassCount, f observer.Finding) {
	class := CauseClass(f.Owner)
	for i := range counts {
		if counts[i].Class != class {
			continue
		}
		switch f.Severity {
		case observer.SeverityHigh:
			counts[i].High++
		case observer.SeverityMedium:
			counts[i].Medium++
		case observer.SeverityLow:
			counts[i].Low++
		}
		return
	}
}

// mergeCauseClassCounts returns the element-wise sum of a and b (both in
// causeClassOrder) — BatchRow aggregates its runs' CauseCounts this way.
func mergeCauseClassCounts(a, b []CauseClassCount) []CauseClassCount {
	out := newCauseClassCounts()
	for i := range out {
		out[i].High = a[i].High + b[i].High
		out[i].Medium = a[i].Medium + b[i].Medium
		out[i].Low = a[i].Low + b[i].Low
	}
	return out
}

// formatVersionNum maps an observation's stored formatVersion (§7.5) to 1
// or 2 — problems.go's clustering key (§8.6) and FindingRow.FormatVersion
// both need to distinguish the two, and an int compares more cheaply than
// the formatVersion string at every call site that does.
func formatVersionNum(v string) int {
	if v == observer.ObservationFormat2 {
		return 2
	}
	return 1
}

// dedupeEvidence drops a later evidence entry citing a step number already
// cited earlier in ev, keeping the first occurrence — item 3's "step lists
// de-duplicated".
func dedupeEvidence(ev []observer.Evidence) []observer.Evidence {
	if len(ev) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(ev))
	out := make([]observer.Evidence, 0, len(ev))
	for _, e := range ev {
		if seen[e.Step] {
			continue
		}
		seen[e.Step] = true
		out = append(out, e)
	}
	return out
}

// FindingRow is one finding of a current ok observation (§8.3 item 3),
// with everything a list or detail view needs already resolved: batch,
// scenario, run id, started, build, severity, owner, cause label/class,
// surface, anchor, title, what, evidence, span, causedVerdict, lookAt,
// fix, the finding's index in its observation (for #f<n>, n = Index+1),
// and the observation's format version (1 or 2).
type FindingRow struct {
	RunID         string
	Batch         string
	Scenario      string
	StartedAt     time.Time
	Build         BuildInfo
	Severity      string
	Owner         string
	CauseLabel    string
	CauseClass    string
	Surface       string
	Anchor        string
	Title         string
	What          string
	Evidence      []observer.Evidence
	Span          *observer.Span
	CausedVerdict bool
	LookAt        string
	Fix           string
	Index         int
	FormatVersion int
}

// BuildFindingRows resolves every FindingRow in scope from rows: one per
// finding of each run's current observation, when that observation's
// status is "ok" (§7.5/§8.6: problems and findings are clustered "over the
// current observations (status ok)" only — a failed or unparsed
// observation names no findings a maintainer should act on). Findings
// stay in their observation's own order (severity/importance ordering, if
// any, is the caller's — §8.6's problem members and §8.7's /findings sort
// both reorder from here).
func BuildFindingRows(rows []RunRow) []FindingRow {
	var out []FindingRow
	for _, row := range rows {
		obs := row.Observation
		if obs == nil || obs.Status != observationStatusOK {
			continue
		}
		fv := formatVersionNum(obs.FormatVersion)
		for i, f := range obs.Findings {
			out = append(out, FindingRow{
				RunID: row.RunID, Batch: row.Batch, Scenario: row.Scenario, StartedAt: row.StartedAt,
				Build: row.Build, Severity: f.Severity, Owner: f.Owner,
				CauseLabel: CauseLabel(f.Owner), CauseClass: CauseClass(f.Owner),
				Surface: f.Surface, Anchor: f.Anchor, Title: f.Title, What: f.What,
				Evidence: dedupeEvidence(f.Evidence), Span: f.Span, CausedVerdict: f.CausedVerdict,
				LookAt: f.LookAt, Fix: f.Fix, Index: i, FormatVersion: fv,
			})
		}
	}
	return out
}

// --- /findings list (§8.7 item 5) ------------------------------------------

// findingListSpec is the /findings list's query surface.
func findingListSpec() ListSpec {
	return ListSpec{
		Closed: []ClosedFilter{
			{Name: paramCause, Allowed: []string{CauseClassZCP, CauseClassTest, CauseClassAgent, CauseClassPlatform}},
			{Name: paramSeverity, Allowed: []string{observer.SeverityHigh, observer.SeverityMedium, observer.SeverityLow}, Min: true},
		},
		Open:        []string{paramSurface, paramScenario, paramBatch, paramBuild},
		Sorts:       []SortKey{{Name: "severity", DefaultDir: "desc"}, {Name: "newest", DefaultDir: "desc"}, {Name: paramCause, DefaultDir: "asc"}},
		DefaultSort: "severity",
		HasSince:    true, DefaultSince: "7d",
	}
}

// causeClassRankIndex is causeClassOrder's index for class — §8.7's
// `cause` sort key ("ZCP-first order").
func causeClassRankIndex(class string) int {
	for i, c := range causeClassOrder {
		if c == class {
			return i
		}
	}
	return len(causeClassOrder)
}

// findingNewestThenIDsTiebreak is `severity`'s tie-break (§8.7): newest,
// then run id, then finding index.
func findingNewestThenIDsTiebreak(a, b FindingRow) int {
	if c := cmpTime(b.StartedAt, a.StartedAt); c != 0 { // newest first
		return c
	}
	if c := cmpString(a.RunID, b.RunID); c != 0 {
		return c
	}
	return cmpInt(a.Index, b.Index)
}

// findingEngine wires findingListSpec to FindingRow.
func findingEngine() Engine[FindingRow] {
	return Engine[FindingRow]{
		Spec: findingListSpec(),
		Match: func(f FindingRow, name, value string) bool {
			switch name {
			case paramCause:
				return f.CauseClass == value
			case paramSurface:
				return OpenMatch(f.Surface, value)
			case paramScenario:
				return OpenMatch(f.Scenario, value)
			case paramBatch:
				return OpenMatch(f.Batch, value)
			case paramBuild:
				return OpenMatch(f.Build.Sha12(), value)
			}
			return false
		},
		Severity: func(f FindingRow) string { return f.Severity },
		Time:     func(f FindingRow) time.Time { return f.StartedAt },
		Sorts: map[string]SortSpec[FindingRow]{
			"severity": {
				Primary: func(a, b FindingRow) int {
					return cmpInt(problemSeverityRank(a.Severity), problemSeverityRank(b.Severity))
				},
				Tiebreak: findingNewestThenIDsTiebreak,
			},
			"newest": {
				Primary: func(a, b FindingRow) int { return cmpTime(a.StartedAt, b.StartedAt) },
				Tiebreak: func(a, b FindingRow) int {
					return cmpInt(problemSeverityRank(a.Severity), problemSeverityRank(b.Severity))
				},
			},
			paramCause: {
				Primary: func(a, b FindingRow) int {
					return cmpInt(causeClassRankIndex(a.CauseClass), causeClassRankIndex(b.CauseClass))
				},
				Tiebreak: func(a, b FindingRow) int {
					return cmpInt(problemSeverityRank(a.Severity), problemSeverityRank(b.Severity))
				},
			},
		},
	}
}
