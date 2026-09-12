// Package console: §8.6 problems — findings clustered across runs
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md item 4).
package console

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// Problem statuses (§8.6/§8.8). live = recurring, new, first seen, or
// still-emitted.
const (
	StatusRecurring    = "recurring"
	StatusNew          = "new"
	StatusFirstSeen    = "first-seen"
	StatusGone         = "gone"
	StatusUnconfirmed  = "unconfirmed"
	StatusStillEmitted = "still-emitted"
)

func isLiveStatus(s string) bool {
	return s == StatusRecurring || s == StatusNew || s == StatusFirstSeen || s == StatusStillEmitted
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

// zcpFarmPath matches one ".zcp-farm/<segment>/" path component — a run's
// own disposable working directory (§2.1) — anywhere in an anchor's text.
var zcpFarmPath = regexp.MustCompile(`\.zcp-farm/[^/]+/`)

// maskRunSpecific implements FIX2.md's FIX2-DATA item 1: run BEFORE norm's
// own masking (below), so this finding's own run id, batch id and scenario
// (whole-token, §8.6's boundary rule) collapse to "<runpath>" wherever they
// occur in s, and any ".zcp-farm/<anything>/" path segment — a stray
// reference to some OTHER run's own working directory (a cross-deploy
// scenario's error can name its source run, not just itself) — collapses
// the same way. Without this, the SAME bug reported from N independent
// runs clusters into N problems instead of one: each anchor carries its own
// run's disposable path, and that path alone made every anchor unique.
func maskRunSpecific(s, runID, batch, scenario string) string {
	for _, id := range []string{runID, batch, scenario} {
		s = maskLiteralToken(s, id, "<runpath>")
	}
	return zcpFarmPath.ReplaceAllString(s, "<runpath>/")
}

// maskLiteralToken replaces every non-overlapping, token-bounded occurrence
// of needle in s with replacement. Unlike replaceTokens/norm's own token
// masking, needle need not itself be one maximal [a-z0-9] run — a run id
// such as "final1-api-node-postgres-classic-dev" has '-' inside it — so
// this matches the literal needle and checks only that its two edges sit on
// a token boundary (§8.6: "a token is bounded by a character outside
// [a-z0-9] or the text's edge"), the same boundary rule generalized to a
// multi-token literal. A false (non-boundary) match advances one byte and
// keeps scanning, so a later, boundary-true occurrence starting inside it
// is still found. needle == "" is a no-op (never masks everything).
func maskLiteralToken(s, needle, replacement string) string {
	if needle == "" {
		return s
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(s[i:], needle)
		if j == -1 {
			b.WriteString(s[i:])
			break
		}
		start := i + j
		end := start + len(needle)
		boundedBefore := start == 0 || !isTokenByte(s[start-1])
		boundedAfter := end == len(s) || !isTokenByte(s[end])
		if boundedBefore && boundedAfter {
			b.WriteString(s[i:start])
			b.WriteString(replacement)
			i = end
			continue
		}
		b.WriteString(s[i : start+1])
		i = start + 1
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
		anchor := maskRunSpecific(f.Anchor, f.RunID, f.Batch, f.Scenario)
		return "anchor|" + prefix + "|" + norm(anchor, hostnames)
	case f.FormatVersion == 2 && f.Anchor == "" && clusterableNoAnchorPrefix(prefix):
		return "noanchor|" + prefix + "|" + f.Owner + "|" + f.Scenario
	default:
		// format-2 agent/platform/empty surface, and every format-1
		// finding: a problem of its own (§8.6) — RunID+Index is unique
		// across the current-observation set this is built from.
		return "solo|" + f.RunID + "|" + strconv.Itoa(f.Index)
	}
}

// splitAnchorKey parses an anchor-kind problem key ("anchor|<prefix>|
// <norm>", problemKey's own format) back into its surface prefix and
// normalized anchor text; ok is false for a noanchor/solo key. The
// "|"-cut only ever splits on the FIRST two pipes: prefix (a surface, e.g.
// "tool:zerops_deploy") never itself contains "|", so any further "|" in
// the normalized anchor text stays intact in the third piece.
func splitAnchorKey(key string) (prefix, anchor string, ok bool) {
	rest, found := strings.CutPrefix(key, "anchor|")
	if !found {
		return "", "", false
	}
	prefix, anchor, found = strings.Cut(rest, "|")
	return prefix, anchor, found
}

// containmentMinLen is the round-2 addendum to item 1 (FIX2.md FIX2-DATA,
// final verification round): the shorter of two normalized anchors must be
// at least this many characters before containment alone is trusted to
// mean "the same bug" — short generic phrases (a bare status word) would
// otherwise merge unrelated problems.
const containmentMinLen = 12

// mergeContainedAnchorClusters implements that addendum: two anchor-kind
// clusters under the SAME surface prefix merge into one when one's
// normalized anchor is a substring of the other's and the shorter is at
// least containmentMinLen characters. item 1's own masking (above) already
// unifies anchors that differ ONLY by run-specific path/id; this catches
// the wider case a live bucket actually showed — the model wrapping the
// same invariant sentence in different surrounding text run to run (e.g. a
// leading "zerops.yaml not found or invalid:" and/or a trailing "—
// scaffold zerops.yaml for service ... there"), so the shorter wording is
// a substring of the longer one. Merging is transitive (union-find): three
// wordings A⊂B, A⊂C (but B⊄C directly) still end up in one group via the
// shared A. The merged group's surviving key keeps the LONGEST anchor
// (ties broken by the key string) so Problem's displayed Title/Fix/Anchor
// — chosen from Members by severity/recency, unaffected by this — and the
// key's own anchor text stay the most complete wording seen.
func mergeContainedAnchorClusters(clusters map[string][]ProblemMember, order []string) (map[string][]ProblemMember, []string) {
	type info struct{ key, prefix, anchor string }
	infos := make([]info, 0, len(order))
	for _, k := range order {
		if prefix, anchor, ok := splitAnchorKey(k); ok {
			infos = append(infos, info{key: k, prefix: prefix, anchor: anchor})
		}
	}

	parent := make(map[string]string, len(infos))
	for _, in := range infos {
		parent[in.key] = in.key
	}
	var find func(string) string
	find = func(k string) string {
		if parent[k] != k {
			parent[k] = find(parent[k])
		}
		return parent[k]
	}
	union := func(a, b string) {
		if ra, rb := find(a), find(b); ra != rb {
			parent[ra] = rb
		}
	}

	byPrefix := make(map[string][]info, len(infos))
	for _, in := range infos {
		byPrefix[in.prefix] = append(byPrefix[in.prefix], in)
	}
	for _, group := range byPrefix {
		for i := range group {
			for j := i + 1; j < len(group); j++ {
				shorter, longer := group[i].anchor, group[j].anchor
				if len(longer) < len(shorter) {
					shorter, longer = longer, shorter
				}
				if len(shorter) >= containmentMinLen && strings.Contains(longer, shorter) {
					union(group[i].key, group[j].key)
				}
			}
		}
	}

	byKeyInfo := make(map[string]info, len(infos))
	for _, in := range infos {
		byKeyInfo[in.key] = in
	}
	rootKeys := make(map[string][]string, len(infos))
	for _, in := range infos {
		root := find(in.key)
		rootKeys[root] = append(rootKeys[root], in.key)
	}

	newClusters := make(map[string][]ProblemMember, len(clusters))
	newOrder := make([]string, 0, len(order))
	replaced := make(map[string]bool, len(infos))
	for _, keys := range rootKeys {
		if len(keys) < 2 {
			continue // no merge needed for this group
		}
		sort.Slice(keys, func(i, j int) bool {
			ai, aj := byKeyInfo[keys[i]], byKeyInfo[keys[j]]
			if len(ai.anchor) != len(aj.anchor) {
				return len(ai.anchor) > len(aj.anchor) // longest anchor survives
			}
			return ai.key < aj.key
		})
		canonical := keys[0]
		combinedLen := 0
		for _, k := range keys {
			combinedLen += len(clusters[k])
		}
		combined := make([]ProblemMember, 0, combinedLen)
		for _, k := range keys {
			combined = append(combined, clusters[k]...)
			replaced[k] = true
		}
		newClusters[canonical] = combined
		newOrder = append(newOrder, canonical)
	}
	for _, k := range order {
		if replaced[k] {
			continue
		}
		newClusters[k] = clusters[k]
		newOrder = append(newOrder, k)
	}
	return newClusters, newOrder
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

	HitOnNewestBuild     int // a: distinct runs among members on the newest build
	RunsAssessedOnNewest int // b: runs of this problem's scenarios assessed on the newest build
	RunsTotal            int // n: distinct runs among members
	BatchesTotal         int // m: distinct batches among members
	BuildsTotal          int // k: distinct builds among members
	FirstSeen, LastSeen  time.Time

	// RunsHitByBatch and RunsAssessedByBatch are item 7's batch-local "hit N
	// of M runs in this batch" counts (FIX2.md FIX2-DATA item 7), keyed by
	// batch id, for every batch this problem has at least one member in:
	// RunsHitByBatch is the number of DISTINCT runs of that one batch the
	// problem hit; RunsAssessedByBatch is the number of that batch's runs,
	// among this problem's scenarios, that carry a current ok observation
	// (the denominator). Unlike HitOnNewestBuild/RunsAssessedOnNewest —
	// scoped to "the newest build" across every batch in the since window —
	// these are scoped to one batch, so a batch page can say "hit 5 of 12
	// runs in this batch" instead of the cross-batch newest-build figure.
	RunsHitByBatch      map[string]int
	RunsAssessedByBatch map[string]int

	// InScopeHit and InScopeAssessed implement item 3 (FIX2 round 2,
	// final verification round): "status must not depend on the request's
	// scope" — Status and every OTHER total above are computed over the
	// FULL run history BuildProblemsScoped is given, regardless of the
	// caller's own scope (a since window, or one batch); only these two
	// fields, and WHICH problems are returned at all, depend on scope.
	// InScopeHit is the number of distinct in-scope runs that are members;
	// InScopeAssessed is the number of in-scope runs of this problem's
	// scenarios that carry a current ok observation — so a page can print
	// "hit 1/1 in scope · seen in 4 runs / 4 batches / 2 builds · status
	// recurring". BuildProblems (every run given is its own whole scope)
	// sets both from that same run set, unchanged from before this item.
	InScopeHit      int
	InScopeAssessed int

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

// StepTextFinder returns runID's full, searchable step text — the task
// prompt plus every step's agent/thinking text and every tool call's input
// JSON and result, undecoded exactly as §7.3's digest quotes it — for
// item 2's still-emitted search (FIX2 round 2, final verification round);
// ok is false when the run's bundle isn't available (never an error: a
// missing bundle just narrows the search, like a missing optional file
// elsewhere in the observer). A nil StepTextFinder disables the search
// entirely — statusFor then returns exactly gone/unconfirmed as before
// item 2, so BuildProblems (which passes nil) is unaffected.
type StepTextFinder func(runID string) (text string, ok bool)

// stepTextCacheEntry memoizes one run's normalizedStepText call within a
// single BuildProblemsScoped invocation ("do it once per request over the
// candidate runs, and cache what you can") — present-in-map is "already
// computed" regardless of ok, so a run whose bundle failed to load is
// never retried for a second problem that also wants to search it.
type stepTextCacheEntry struct {
	text string
	ok   bool
}

// normalizedStepText fetches and caches runID's step text via findStepText,
// normalized through the SAME pipeline problemKey's anchor branch uses —
// maskRunSpecific (this run's own id/batch/scenario, plus any stray
// ".zcp-farm/<anything>/" path) then norm (this run's own service
// hostnames, hex/digit tokens) — so a problem's already-normalized anchor
// (an anchor-kind Key already carries it) can be found by a plain
// substring search against a DIFFERENT run's own step text: both sides
// collapse the same run-specific noise to the same placeholders.
func normalizedStepText(runID string, findStepText StepTextFinder, runByID map[string]ProblemsRun, cache map[string]stepTextCacheEntry) (string, bool) {
	if e, done := cache[runID]; done {
		return e.text, e.ok
	}
	raw, ok := findStepText(runID)
	if !ok {
		cache[runID] = stepTextCacheEntry{}
		return "", false
	}
	var batch, scenario string
	var hostnames []string
	if r, found := runByID[runID]; found {
		batch, scenario, hostnames = r.Row.Batch, r.Row.Scenario, r.Row.ServiceHostnames
	}
	normalized := norm(maskRunSpecific(raw, runID, batch, scenario), hostnames)
	cache[runID] = stepTextCacheEntry{text: normalized, ok: true}
	return normalized, true
}

// stillEmitted implements item 2 (FIX2 round 2): before a problem's status
// settles on gone or unconfirmed, search its own normalized anchor (an
// anchor-kind problem's Key already carries "anchor|<prefix>|<norm>") in
// the normalized step text of the newest build's OWN runs of the problem's
// scenarios — the "candidate" runs, from runsByBuildScenario. Found in any
// of them → true (the caller reports StatusStillEmitted instead of
// gone/unconfirmed): the anchor's underlying text is still being produced
// on the current build even though no CURRENT OBSERVATION happened to
// flag it as a finding there, so "gone" would be unprovable — exactly the
// live-data defect this item fixes (`NOT supervised`/`no recipe template`
// still emitted in most merge-ready-1 runs while reading gone/unconfirmed).
// A no-anchor/solo problem (splitAnchorKey fails) or an empty anchor keeps
// today's behavior untouched, per the item's own scope.
func stillEmitted(key string, scenarios map[string]bool, newestBuild string, runsByBuildScenario map[string]map[string][]string, findStepText StepTextFinder, runByID map[string]ProblemsRun, cache map[string]stepTextCacheEntry) bool {
	return stillEmittedAnchors([]string{anchorFromProblemKey(key)}, scenarios, newestBuild, runsByBuildScenario, findStepText, runByID, cache)
}

func anchorFromProblemKey(key string) string {
	_, anchor, ok := splitAnchorKey(key)
	if !ok {
		return ""
	}
	return anchor
}

// stillEmittedAnchors is the merged-cluster form of stillEmitted. Each
// member's anchor is normalized with that member run's metadata before it is
// compared to candidate text normalized with the candidate's metadata.
func stillEmittedAnchors(anchors []string, scenarios map[string]bool, newestBuild string, runsByBuildScenario map[string]map[string][]string, findStepText StepTextFinder, runByID map[string]ProblemsRun, cache map[string]stepTextCacheEntry) bool {
	if findStepText == nil || newestBuild == "" {
		return false
	}
	if len(anchors) == 0 {
		return false
	}
	for s := range scenarios {
		for _, runID := range runsByBuildScenario[newestBuild][s] {
			text, ok := normalizedStepText(runID, findStepText, runByID, cache)
			if !ok {
				continue
			}
			for _, anchor := range anchors {
				if anchor != "" && strings.Contains(text, anchor) {
					return true
				}
			}
		}
	}
	return false
}

// statusFor implements §8.6's status for one problem, given the build
// index and newest build sha over the full run set, plus item 2's
// still-emitted search inputs (a nil findStepText disables it, restoring
// the plain five-way gone/unconfirmed rule).
func statusFor(p Problem, bi *buildIndex, newestBuild string, runsByBuildScenario map[string]map[string][]string, findStepText StepTextFinder, runByID map[string]ProblemsRun, stepCache map[string]stepTextCacheEntry) string {
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

	candidate := StatusUnconfirmed
	for s := range scenarios {
		if newestBuild != "" && bi.hasFormat(s, newestBuild, memberFormat) {
			candidate = StatusGone
			break
		}
	}
	anchors := make([]string, 0, len(p.Members))
	for _, m := range p.Members {
		if m.Anchor == "" {
			continue
		}
		anchors = append(anchors, norm(maskRunSpecific(m.Anchor, m.RunID, m.Batch, m.Scenario), runByID[m.RunID].Row.ServiceHostnames))
	}
	if stillEmittedAnchors(anchors, scenarios, newestBuild, runsByBuildScenario, findStepText, runByID, stepCache) {
		return StatusStillEmitted
	}
	return candidate
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

// BuildProblems implements §8.6 for a caller with no separate notion of
// scope: runs is treated as both the full history (status/totals) and the
// whole scope (every problem with a member qualifies, and InScopeHit/
// InScopeAssessed count exactly what HitOnNewestBuild/RunsAssessedOnNewest
// would over the SAME set) — i.e. exactly this function's pre-item-3
// behavior. A caller that can supply the FULL run history separately from
// its own narrower scope (a since window, or one batch) should call
// BuildProblemsScoped directly so status stops depending on that scope
// (item 3, FIX2 round 2, final verification round).
func BuildProblems(runs []ProblemsRun) []Problem {
	inScope := make(map[string]bool, len(runs))
	for _, r := range runs {
		inScope[r.Row.RunID] = true
	}
	return BuildProblemsScoped(runs, inScope, nil)
}

// BuildProblemsScoped implements §8.6 end to end: clusters every finding
// of allRuns' current ok observations into problems (including the item-1
// round-2 containment merge), computes each problem's status — including
// item 2's still-emitted search, when findStepText is non-nil — and the
// newest-build/all-history totals over the FULL allRuns set regardless of
// scope (item 3: "status must not depend on the request's scope"), and
// returns, in rank order, only the problems with at least one member in
// inScopeRunIDs — that set is the ONLY thing scope is allowed to affect,
// along with the new InScopeHit/InScopeAssessed fields.
func BuildProblemsScoped(allRuns []ProblemsRun, inScopeRunIDs map[string]bool, findStepText StepTextFinder) []Problem {
	rows := make([]RunRow, len(allRuns))
	hostnamesByRun := make(map[string][]string, len(allRuns))
	runByID := make(map[string]ProblemsRun, len(allRuns))
	for i, r := range allRuns {
		rows[i] = r.Row
		hostnamesByRun[r.Row.RunID] = r.Row.ServiceHostnames
		runByID[r.Row.RunID] = r
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
	clusters, order = mergeContainedAnchorClusters(clusters, order)

	bi := newBuildIndex()
	for _, r := range allRuns {
		if r.Row.Outcome == "" || r.Row.Observation == nil {
			continue
		}
		bi.mark(r.Row.Scenario, r.Row.Build.Sha256, formatVersionNum(r.Row.Observation.FormatVersion))
	}
	newestBuild := newestBuildSha(allRuns)

	// Runs assessed on the newest build, per scenario — for "b" in "hit
	// a/b runs on <newest build>".
	assessedOnNewestByScenario := make(map[string]int)
	// Runs assessed per (scenario, batch) — item 7's batch-local "b"
	// (RunsAssessedByBatch's denominator).
	assessedByScenarioBatch := make(map[string]map[string]int)
	// Runs assessed per scenario, restricted to inScopeRunIDs — item 3's
	// InScopeAssessed denominator.
	assessedInScopeByScenario := make(map[string]int)
	// runsByBuildScenario indexes every run by (build, scenario) → run IDs
	// — item 2's still-emitted candidate-run lookup.
	runsByBuildScenario := make(map[string]map[string][]string)
	for _, r := range allRuns {
		if runsByBuildScenario[r.Row.Build.Sha256] == nil {
			runsByBuildScenario[r.Row.Build.Sha256] = make(map[string][]string)
		}
		runsByBuildScenario[r.Row.Build.Sha256][r.Row.Scenario] = append(runsByBuildScenario[r.Row.Build.Sha256][r.Row.Scenario], r.Row.RunID)

		if r.Row.Outcome == "" {
			continue
		}
		if r.Row.Build.Sha256 == newestBuild {
			assessedOnNewestByScenario[r.Row.Scenario]++
		}
		if assessedByScenarioBatch[r.Row.Scenario] == nil {
			assessedByScenarioBatch[r.Row.Scenario] = make(map[string]int)
		}
		assessedByScenarioBatch[r.Row.Scenario][r.Row.Batch]++
		if inScopeRunIDs[r.Row.RunID] {
			assessedInScopeByScenario[r.Row.Scenario]++
		}
	}

	stepCache := make(map[string]stepTextCacheEntry)

	problems := make([]Problem, 0, len(order))
	for _, key := range order {
		p := buildProblemFromMembers(key, clusters[key])
		p.Status = statusFor(p, bi, newestBuild, runsByBuildScenario, findStepText, runByID, stepCache)

		scenarios := make(map[string]bool)
		runsOnNewest := make(map[string]bool)
		runsInScope := make(map[string]bool)
		hitRunsByBatch := make(map[string]map[string]bool)
		for _, m := range p.Members {
			if m.Build.Sha256 == newestBuild {
				runsOnNewest[m.RunID] = true
			}
			if inScopeRunIDs[m.RunID] {
				runsInScope[m.RunID] = true
			}
			scenarios[m.Scenario] = true
			if hitRunsByBatch[m.Batch] == nil {
				hitRunsByBatch[m.Batch] = make(map[string]bool)
			}
			hitRunsByBatch[m.Batch][m.RunID] = true
		}
		// item 7: "a" is the number of DISTINCT runs hit, not the number of
		// findings/members — a problem whose same run contributed more than
		// one clustered finding must not count that run twice.
		p.HitOnNewestBuild = len(runsOnNewest)
		p.InScopeHit = len(runsInScope)
		for s := range scenarios {
			p.RunsAssessedOnNewest += assessedOnNewestByScenario[s]
			p.InScopeAssessed += assessedInScopeByScenario[s]
		}

		p.RunsHitByBatch = make(map[string]int, len(hitRunsByBatch))
		p.RunsAssessedByBatch = make(map[string]int, len(hitRunsByBatch))
		for batch, runSet := range hitRunsByBatch {
			p.RunsHitByBatch[batch] = len(runSet)
			assessed := 0
			for s := range scenarios {
				assessed += assessedByScenarioBatch[s][batch]
			}
			p.RunsAssessedByBatch[batch] = assessed
		}

		if len(runsInScope) == 0 {
			continue // item 3: scope filters which problems are LISTED …
		}
		problems = append(problems, p) // … never their status or totals.
	}

	sort.SliceStable(problems, func(i, j int) bool { return rankLess(problems[i], problems[j]) })
	return problems
}

// --- /problems list (§8.7 item 5) ------------------------------------------

// problemStatusAllowed is §8.7's `status` closed set for /problems.
var problemStatusAllowed = []string{"live", StatusRecurring, StatusNew, StatusFirstSeen, StatusStillEmitted, StatusGone, StatusUnconfirmed, filterAll}

// problemListSpec is the /problems list's query surface.
func problemListSpec() ListSpec {
	return ListSpec{
		Closed: []ClosedFilter{
			{Name: paramCause, Allowed: []string{CauseClassZCP, CauseClassTest, CauseClassAgent, CauseClassPlatform}},
			{Name: paramSeverity, Allowed: []string{observer.SeverityHigh, observer.SeverityMedium, observer.SeverityLow}, Min: true},
			{Name: paramStatus, Allowed: problemStatusAllowed, Single: true},
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
		case filterAll:
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
// is deliberately NOT wired as a post-hoc Time filter here: it narrows
// which runs feed BuildProblemsScoped's own scope (item 3, FIX2 round 2 —
// "status must not depend on the request's scope": status and every OTHER
// total are computed over the FULL run history regardless of scope, only
// WHICH problems list depends on it), never a per-row filter over
// already-built Problem rows — re-applying it here as a LastSeen filter
// would let a later `since` narrow a problem's member list without ever
// touching its Status/totals, silently reintroducing the very
// scope-dependence item 3 rules out. A caller resolves `since` into an
// inScopeRunIDs set (ideally over the FULL history, via
// BuildProblemsScoped — BuildProblems's single-arg compatibility path
// still treats whatever it's given as the whole history, item 3's
// pre-existing limit for callers not yet passing full history), then runs
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
