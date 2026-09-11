// Package console: §8.6 problems — findings clustered across runs
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md item 4).
package console

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// Problem statuses (§8.6/§8.8). live = recurring, new or first seen.
const (
	StatusRecurring   = "recurring"
	StatusNew         = "new"
	StatusFirstSeen   = "first-seen"
	StatusGone        = "gone"
	StatusUnconfirmed = "unconfirmed"
)

func isLiveStatus(s string) bool {
	return s == StatusRecurring || s == StatusNew || s == StatusFirstSeen
}

// isTokenByte reports whether b is a §8.6 token character: [a-z0-9] only
// (NOT Go's \b/\w, which also treats "_" as inside a word) — "a token is
// bounded by a character outside [a-z0-9] or the text's edge".
func isTokenByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func isHexToken(tok string) bool {
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isDigitToken(tok string) bool {
	for i := 0; i < len(tok); i++ {
		if tok[i] < '0' || tok[i] > '9' {
			return false
		}
	}
	return true
}

// replaceTokens rewrites every maximal [a-z0-9] run in s via f, copying
// every other byte through unchanged (including multi-byte UTF-8
// sequences, whose continuation bytes never match isTokenByte and so pass
// through one byte at a time, in order, reconstructing the original
// sequence intact).
func replaceTokens(s string, f func(string) string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if isTokenByte(s[i]) {
			j := i + 1
			for j < len(s) && isTokenByte(s[j]) {
				j++
			}
			b.WriteString(f(s[i:j]))
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// norm implements §8.6's anchor normalization: FM-46's decode+collapse+
// strip-punctuation (observer.NormalizeText), lowercased, then — each as
// whole tokens only — the run's service hostnames replaced with "<host>",
// then a 22-character token or an 8-or-more-character hex token replaced
// with "#", then every remaining all-digit token replaced with "#".
func norm(s string, hostnames []string) string {
	s = strings.ToLower(observer.NormalizeText(s))
	hostSet := make(map[string]bool, len(hostnames))
	for _, h := range hostnames {
		hostSet[strings.ToLower(h)] = true
	}
	return replaceTokens(s, func(tok string) string {
		switch {
		case hostSet[tok]:
			return "<host>"
		case len(tok) == 22:
			return "#"
		case len(tok) >= 8 && isHexToken(tok):
			return "#"
		case isDigitToken(tok):
			return "#"
		default:
			return tok
		}
	})
}

// surfacePrefix returns surface up to (not including) its first "/" —
// §8.6: "tool:zerops_deploy/deploy" → "tool:zerops_deploy".
func surfacePrefix(surface string) string {
	prefix, _, _ := strings.Cut(surface, "/")
	return prefix
}

// clusterableNoAnchorPrefix reports whether a surface prefix (surfacePrefix's
// result) is one of §8.6's no-anchor clusterable kinds: tool:, recipe:,
// check:, or the bare "scenario".
func clusterableNoAnchorPrefix(prefix string) bool {
	return strings.HasPrefix(prefix, "tool:") || strings.HasPrefix(prefix, "recipe:") ||
		strings.HasPrefix(prefix, "check:") || prefix == "scenario"
}

// problemKey implements §8.6's clustering key for one finding: hostnames
// is the finding's own run's service hostnames (for norm's host
// replacement).
func problemKey(f FindingRow, hostnames []string) string {
	prefix := surfacePrefix(f.Surface)
	switch {
	case f.FormatVersion == 2 && f.Anchor != "":
		return "anchor|" + prefix + "|" + norm(f.Anchor, hostnames)
	case f.FormatVersion == 2 && f.Anchor == "" && clusterableNoAnchorPrefix(prefix):
		return "noanchor|" + prefix + "|" + f.Owner + "|" + f.Scenario
	default:
		// format-2 agent/platform/empty surface, and every format-1
		// finding: a problem of its own (§8.6) — RunID+Index is unique
		// across the current-observation set this is built from.
		return "solo|" + f.RunID + "|" + strconv.Itoa(f.Index)
	}
}

// ProblemsRun is one run's inputs to problem clustering (§8.6): its
// resolved RunRow plus the batch-level facts (set, created) the
// builds/status computation needs and RunRow itself doesn't carry.
type ProblemsRun struct {
	Row            RunRow
	BatchSet       string
	BatchCreatedAt time.Time
}

// ProblemMember is one finding clustered into a Problem — a thin view
// over FindingRow plus the finding's own build, for members-ordering and
// status computation.
type ProblemMember = FindingRow

// Problem is one §8.6 row: every finding across runs sharing one
// clustering key.
type Problem struct {
	Key    string
	Status string // StatusRecurring | StatusNew | StatusFirstSeen | StatusGone | StatusUnconfirmed

	Severity    string   // highest member severity
	CauseLabels []string // distinct cause labels of members, first-seen order
	Surface     string   // surfacePrefix of the (any) member's surface
	Title       string   // most severe member's title (newest on a tie)
	Fix         string   // most severe member's fix (newest on a tie)
	Anchor      string   // most severe member's anchor (newest on a tie)

	HitOnNewestBuild     int // a: member runs on the newest build
	RunsAssessedOnNewest int // b: runs of this problem's scenarios assessed on the newest build
	RunsTotal            int // n: distinct runs among members
	BatchesTotal         int // m: distinct batches among members
	BuildsTotal          int // k: distinct builds among members
	FirstSeen, LastSeen  time.Time

	Members []ProblemMember // ordered by severity desc, then newest
}

// problemSeverityRank ranks §7.5 severities for comparison: high > medium > low
// > unknown.
func problemSeverityRank(sev string) int {
	switch sev {
	case observer.SeverityHigh:
		return 3
	case observer.SeverityMedium:
		return 2
	case observer.SeverityLow:
		return 1
	default:
		return 0
	}
}

// memberMoreSevereThenNewer reports whether a should sort before b under
// "the title and fix of its most severe member (newest on a tie)" /
// "Members are ordered by severity, then newest" (§8.6): higher severity
// first, ties broken by newer StartedAt first.
func memberMoreSevereThenNewer(a, b ProblemMember) bool {
	if ra, rb := problemSeverityRank(a.Severity), problemSeverityRank(b.Severity); ra != rb {
		return ra > rb
	}
	return a.StartedAt.After(b.StartedAt)
}

// buildProblemFromMembers assembles a Problem's display fields from its
// members (severity/cause/surface/title/fix/anchor/counts/first-last
// seen) — everything except Status, which needs the cross-batch build
// index BuildProblems computes once.
func buildProblemFromMembers(key string, members []ProblemMember) Problem {
	sorted := make([]ProblemMember, len(members))
	copy(sorted, members)
	sort.SliceStable(sorted, func(i, j int) bool { return memberMoreSevereThenNewer(sorted[i], sorted[j]) })

	p := Problem{Key: key, Members: sorted}

	seenLabel := make(map[string]bool)
	runs, batches, builds := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for i, m := range sorted {
		if i == 0 {
			p.Severity, p.Title, p.Fix, p.Anchor = m.Severity, m.Title, m.Fix, m.Anchor
			p.Surface = surfacePrefix(m.Surface)
		}
		if lbl := m.CauseLabel; lbl != "" && !seenLabel[lbl] {
			seenLabel[lbl] = true
			p.CauseLabels = append(p.CauseLabels, lbl)
		}
		runs[m.RunID] = true
		batches[m.Batch] = true
		builds[m.Build.Sha256] = true
		if p.FirstSeen.IsZero() || m.StartedAt.Before(p.FirstSeen) {
			p.FirstSeen = m.StartedAt
		}
		if m.StartedAt.After(p.LastSeen) {
			p.LastSeen = m.StartedAt
		}
	}
	p.RunsTotal, p.BatchesTotal, p.BuildsTotal = len(runs), len(batches), len(builds)
	return p
}

// buildIndex is the per-(scenario,build) assessment ledger BuildProblems
// needs for status: whether the scenario was assessed on that build at
// all, and in which observation format versions.
type buildIndex struct {
	assessed map[string]map[string]bool         // scenario -> build -> assessed
	formats  map[string]map[string]map[int]bool // scenario -> build -> formatVersion -> true
}

func newBuildIndex() *buildIndex {
	return &buildIndex{assessed: map[string]map[string]bool{}, formats: map[string]map[string]map[int]bool{}}
}

func (bi *buildIndex) mark(scenario, build string, format int) {
	if bi.assessed[scenario] == nil {
		bi.assessed[scenario] = map[string]bool{}
	}
	bi.assessed[scenario][build] = true
	if bi.formats[scenario] == nil {
		bi.formats[scenario] = map[string]map[int]bool{}
	}
	if bi.formats[scenario][build] == nil {
		bi.formats[scenario][build] = map[int]bool{}
	}
	bi.formats[scenario][build][format] = true
}

func (bi *buildIndex) hasFormat(scenario, build string, format int) bool {
	return bi.formats[scenario] != nil && bi.formats[scenario][build] != nil && bi.formats[scenario][build][format]
}

// newestBuildSha implements §8.6's "newest build": that of the newest
// gate/all batch with an assessed run, else of the newest batch with one.
func newestBuildSha(runs []ProblemsRun) string {
	type batchInfo struct {
		id       string
		set      string
		created  time.Time
		build    string
		assessed bool
	}
	batches := make(map[string]*batchInfo)
	for _, r := range runs {
		bi, ok := batches[r.Row.Batch]
		if !ok {
			bi = &batchInfo{id: r.Row.Batch, set: r.BatchSet, created: r.BatchCreatedAt, build: r.Row.Build.Sha256}
			batches[r.Row.Batch] = bi
		}
		if r.Row.Outcome != "" {
			bi.assessed = true
		}
	}
	newer := func(a, b *batchInfo) bool {
		if !a.created.Equal(b.created) {
			return a.created.After(b.created)
		}
		return a.id > b.id
	}
	var bestGateAll, bestAny *batchInfo
	for _, bi := range batches {
		if !bi.assessed {
			continue
		}
		if bestAny == nil || newer(bi, bestAny) {
			bestAny = bi
		}
		if bi.set == "gate" || bi.set == "all" {
			if bestGateAll == nil || newer(bi, bestGateAll) {
				bestGateAll = bi
			}
		}
	}
	if bestGateAll != nil {
		return bestGateAll.build
	}
	if bestAny != nil {
		return bestAny.build
	}
	return ""
}

// statusFor implements §8.6's five-way status for one problem, given the
// build index and newest build sha over the full run set.
func statusFor(p Problem, bi *buildIndex, newestBuild string) string {
	hitBuilds := make(map[string]bool)
	scenarios := make(map[string]bool)
	memberFormat := p.Members[0].FormatVersion
	for _, m := range p.Members {
		hitBuilds[m.Build.Sha256] = true
		scenarios[m.Scenario] = true
	}
	newestHit := newestBuild != "" && hitBuilds[newestBuild]
	olderHit := false
	for b := range hitBuilds {
		if b != newestBuild {
			olderHit = true
			break
		}
	}

	if newestHit && olderHit {
		return StatusRecurring
	}
	if newestHit { // && !olderHit
		for s := range scenarios {
			for b := range bi.assessed[s] {
				if b == newestBuild || hitBuilds[b] {
					continue
				}
				return StatusNew
			}
		}
		return StatusFirstSeen
	}

	for s := range scenarios {
		if newestBuild != "" && bi.hasFormat(s, newestBuild, memberFormat) {
			return StatusGone
		}
	}
	return StatusUnconfirmed
}

// rankLess implements §8.6's rank order: live before the rest; then
// highest member severity; then runs hit on the newest build; then runs
// hit in total; then last seen, newest first; then the key.
func rankLess(a, b Problem) bool {
	if la, lb := isLiveStatus(a.Status), isLiveStatus(b.Status); la != lb {
		return la
	}
	if ra, rb := problemSeverityRank(a.Severity), problemSeverityRank(b.Severity); ra != rb {
		return ra > rb
	}
	if a.HitOnNewestBuild != b.HitOnNewestBuild {
		return a.HitOnNewestBuild > b.HitOnNewestBuild
	}
	if a.RunsTotal != b.RunsTotal {
		return a.RunsTotal > b.RunsTotal
	}
	if !a.LastSeen.Equal(b.LastSeen) {
		return a.LastSeen.After(b.LastSeen)
	}
	return a.Key < b.Key
}

// BuildProblems implements §8.6 end to end: clusters every finding of
// runs' current ok observations into problems, computes each problem's
// status and the newest-build "hit a/b" counts over the FULL runs set
// (status is pinned here, over every batch given — a caller narrowing by
// batch/scenario/etc. afterward, per §8.7, must never recompute it), and
// returns them in rank order.
func BuildProblems(runs []ProblemsRun) []Problem {
	rows := make([]RunRow, len(runs))
	hostnamesByRun := make(map[string][]string, len(runs))
	for i, r := range runs {
		rows[i] = r.Row
		hostnamesByRun[r.Row.RunID] = r.Row.ServiceHostnames
	}
	findings := BuildFindingRows(rows)

	clusters := make(map[string][]ProblemMember)
	var order []string
	for _, f := range findings {
		key := problemKey(f, hostnamesByRun[f.RunID])
		if _, ok := clusters[key]; !ok {
			order = append(order, key)
		}
		clusters[key] = append(clusters[key], f)
	}

	bi := newBuildIndex()
	for _, r := range runs {
		if r.Row.Outcome == "" || r.Row.Observation == nil {
			continue
		}
		bi.mark(r.Row.Scenario, r.Row.Build.Sha256, formatVersionNum(r.Row.Observation.FormatVersion))
	}
	newestBuild := newestBuildSha(runs)

	// Runs assessed on the newest build, per scenario — for "b" in "hit
	// a/b runs on <newest build>".
	assessedOnNewestByScenario := make(map[string]int)
	for _, r := range runs {
		if r.Row.Outcome != "" && r.Row.Build.Sha256 == newestBuild {
			assessedOnNewestByScenario[r.Row.Scenario]++
		}
	}

	problems := make([]Problem, 0, len(order))
	for _, key := range order {
		p := buildProblemFromMembers(key, clusters[key])
		p.Status = statusFor(p, bi, newestBuild)

		scenarios := make(map[string]bool)
		for _, m := range p.Members {
			if m.Build.Sha256 == newestBuild {
				p.HitOnNewestBuild++
			}
			scenarios[m.Scenario] = true
		}
		for s := range scenarios {
			p.RunsAssessedOnNewest += assessedOnNewestByScenario[s]
		}

		problems = append(problems, p)
	}

	sort.SliceStable(problems, func(i, j int) bool { return rankLess(problems[i], problems[j]) })
	return problems
}

// --- /problems list (§8.7 item 5) ------------------------------------------

// problemStatusAllowed is §8.7's `status` closed set for /problems.
var problemStatusAllowed = []string{"live", StatusRecurring, StatusNew, StatusFirstSeen, StatusGone, StatusUnconfirmed, "all"}

// problemListSpec is the /problems list's query surface.
func problemListSpec() ListSpec {
	return ListSpec{
		Closed: []ClosedFilter{
			{Name: paramCause, Allowed: []string{CauseClassZCP, CauseClassTest, CauseClassAgent, CauseClassPlatform}},
			{Name: paramSeverity, Allowed: []string{observer.SeverityHigh, observer.SeverityMedium, observer.SeverityLow}, Min: true},
			{Name: paramStatus, Allowed: problemStatusAllowed},
		},
		Open:        []string{paramSurface, paramScenario, paramBatch, paramBuild},
		Sorts:       []SortKey{{Name: "rank", DefaultDir: "asc"}, {Name: "severity", DefaultDir: "desc"}, {Name: "runs", DefaultDir: "desc"}, {Name: "last", DefaultDir: "desc"}, {Name: "first", DefaultDir: "desc"}},
		DefaultSort: "rank",
		Defaults:    map[string]string{paramStatus: "live"},
		HasSince:    true, DefaultSince: "30d",
	}
}

// problemRankCompare is the `rank` sort key's Primary: BuildProblems' own
// rankLess (§8.6), including its final key tie-break — so `sort=rank`
// reproduces BuildProblems' own order exactly.
func problemRankCompare(a, b Problem) int {
	switch {
	case rankLess(a, b):
		return -1
	case rankLess(b, a):
		return 1
	default:
		return 0
	}
}

// problemMatchesOpen and problemMatchesCause/Batch/Scenario check every
// member (§8.6: "cause ... matches an item when any of its findings is in
// that class"; the same "any member" rule applies to the open filters
// here).
func problemMatch(p Problem, name, value string) bool {
	switch name {
	case paramCause:
		for _, m := range p.Members {
			if m.CauseClass == value {
				return true
			}
		}
		return false
	case paramStatus:
		switch value {
		case "all":
			return true
		case "live":
			return isLiveStatus(p.Status)
		case StatusFirstSeen:
			return p.Status == StatusFirstSeen
		default:
			return p.Status == value
		}
	case paramSurface:
		return OpenMatch(p.Surface, value)
	case paramScenario:
		for _, m := range p.Members {
			if OpenMatch(m.Scenario, value) {
				return true
			}
		}
		return false
	case paramBatch:
		for _, m := range p.Members {
			if OpenMatch(m.Batch, value) {
				return true
			}
		}
		return false
	case paramBuild:
		for _, m := range p.Members {
			if OpenMatch(m.Build.Sha12(), value) {
				return true
			}
		}
		return false
	}
	return false
}

// problemEngine wires problemListSpec to Problem. `severity`, `runs`,
// `last` and `first` all tie-break on the full rank order (§8.7's
// "rank" tie-break for those keys).
//
// `since` (declared on problemListSpec for parameter validation/defaults)
// is deliberately NOT wired as a post-hoc Time filter here: §8.6 pins
// status over the since window at BuildProblems time ("Status is computed
// once over the since window ... every other filter narrows rows and
// never changes a status") — the window bounds which runs feed
// BuildProblems in the first place, not which already-built Problem rows
// Apply keeps. Re-applying it here as a per-row LastSeen filter would let
// a later `since` narrow a problem's member list without ever touching
// its Status, silently reintroducing the very inconsistency §8.6 rules
// out. A caller resolves `since` before calling BuildProblems, then runs
// every other filter (cause/severity/status/surface/scenario/batch/build)
// through this Engine.
func problemEngine() Engine[Problem] {
	return Engine[Problem]{
		Spec:     problemListSpec(),
		Match:    problemMatch,
		Severity: func(p Problem) string { return p.Severity },
		Sorts: map[string]SortSpec[Problem]{
			"rank": {Primary: problemRankCompare},
			"severity": {
				Primary: func(a, b Problem) int {
					return cmpInt(problemSeverityRank(a.Severity), problemSeverityRank(b.Severity))
				},
				Tiebreak: problemRankCompare,
			},
			"runs": {
				Primary:  func(a, b Problem) int { return cmpInt(a.RunsTotal, b.RunsTotal) },
				Tiebreak: problemRankCompare,
			},
			"last": {
				Primary:  func(a, b Problem) int { return cmpTime(a.LastSeen, b.LastSeen) },
				Tiebreak: problemRankCompare,
			},
			"first": {
				Primary:  func(a, b Problem) int { return cmpTime(a.FirstSeen, b.FirstSeen) },
				Tiebreak: problemRankCompare,
			},
		},
	}
}
