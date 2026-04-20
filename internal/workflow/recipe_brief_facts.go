package workflow

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// priorDiscoveriesCap caps the number of downstream-scoped facts
// surfaced in any one dispatch brief. Above this, the oldest entries
// elide with a footer pointing at the on-disk log so a subagent that
// really needs the full set can fetch them.
//
// Calibration: v31 recorded 8 facts across 6 subagent calls. A handful
// of pathological runs may double that — an 8-entry cap surfaces the
// most-recent useful set without inflating the brief past the ~3 KB
// budget that v8.95's content-manifest contract assumes.
const priorDiscoveriesCap = 8

// Fact-scope constants kept in sync with internal/ops/facts_log.go. The
// duplication is sanctioned: workflow cannot import ops (ops imports
// workflow for IsManagedService — cycle), and the facts log is a wire
// contract serialized to disk via JSONL, so a parallel reader-side
// struct in workflow is correct rather than convenient.
const (
	factScopeContent    = "content"
	factScopeDownstream = "downstream"
	factScopeBoth       = "both"
)

// priorDiscoveryRecord mirrors the on-disk JSONL shape written by
// ops.AppendFact. Field tags match ops.FactRecord byte-for-byte. New
// fields added to ops.FactRecord MUST be added here as well when the
// reader-side prior-discoveries renderer needs them.
type priorDiscoveryRecord struct {
	Timestamp   string `json:"ts"`
	Substep     string `json:"substep,omitempty"`
	Codebase    string `json:"codebase,omitempty"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Mechanism   string `json:"mechanism,omitempty"`
	FailureMode string `json:"failureMode,omitempty"`
	FixApplied  string `json:"fixApplied,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	Scope       string `json:"scope,omitempty"`
	// RouteTo mirrors ops.FactRecord.RouteTo added in C-2. Used by the
	// C-5 RouteTo filter to narrow prior-discoveries to facts that
	// specifically target a lane (e.g. writer reads content_* routes;
	// scaffold reads scaffold_preamble; code-review reads claude_md).
	RouteTo string `json:"routeTo,omitempty"`
}

// factLogPathLocal mirrors ops.FactLogPath so the workflow package can
// resolve a path without importing ops (cycle). Callers should prefer
// the exported ops.FactLogPath; this helper exists for the brief-
// rendering path which has no other ops dependency.
func factLogPathLocal(sessionID string) string {
	return filepath.Join(os.TempDir(), "zcp-facts-"+sessionID+".jsonl")
}

// BuildPriorDiscoveriesBlock reads the session facts log and returns a
// markdown block of downstream-scoped facts recorded upstream of the
// given substep. Returns the empty string if no facts apply, the log
// is missing, or the log is unreadable — silent in all three cases so
// the dispatched subagent runs as if the v8.96 mechanism didn't exist.
//
// Behavior contract:
//
//   - Filter by Scope ∈ {downstream, both}. Content-only facts stay in
//     the writer's lane (consumed by the v8.95 manifest contract).
//   - Filter by Substep strictly upstream of currentSubstep. A fact
//     recorded at SubStepReadmes never leaks into a brief delivered at
//     SubStepSubagent (forward-in-time leak class).
//   - Sort newest-first by Timestamp so the most-recent observations
//     anchor the head of the list.
//   - Cap at priorDiscoveriesCap; elide overflow with a footer naming
//     the count and the log path.
func BuildPriorDiscoveriesBlock(sessionID, currentSubstep string) string {
	if sessionID == "" {
		return ""
	}
	return buildPriorDiscoveriesBlockFromPath(factLogPathLocal(sessionID), currentSubstep)
}

// BuildPriorDiscoveriesBlockForLane is the C-5 P5-aware variant that
// filters prior discoveries by the target lane. Lane is one of the
// FactRouteTo* values or empty (accept all RouteTo values, matches
// the legacy BuildPriorDiscoveriesBlock behavior).
//
// Scaffold dispatches pass "scaffold_preamble"; feature dispatches pass
// "feature_preamble"; writer dispatches pass a special sentinel that
// accepts every content_* destination; code-review passes "claude_md"
// + "content_gotcha" + "content_ig". Editorial-review does NOT call
// this function at all (porter-premise requires fresh-reader stance).
//
// Empty lane preserves the pre-C-5 behavior (no routeTo filter). This
// keeps the follow-up "flip" commit additive — existing callers that
// pass sessionID without a lane continue to get the Scope-only filter.
func BuildPriorDiscoveriesBlockForLane(sessionID, currentSubstep, lane string) string {
	if sessionID == "" {
		return ""
	}
	return buildPriorDiscoveriesBlockFromPathForLane(factLogPathLocal(sessionID), currentSubstep, lane)
}

// buildPriorDiscoveriesBlockFromPathForLane is the lane-aware variant
// of buildPriorDiscoveriesBlockFromPath. Lane="" accepts every RouteTo
// (legacy behavior). Non-empty lane accepts RouteTo in the lane's
// allow-list (see laneAcceptsRouteTo).
func buildPriorDiscoveriesBlockFromPathForLane(logPath, currentSubstep, lane string) string {
	facts := readPriorDiscoveries(logPath)
	if len(facts) == 0 {
		return ""
	}

	var eligible []priorDiscoveryRecord
	for _, f := range facts {
		if f.Scope != factScopeDownstream && f.Scope != factScopeBoth {
			continue
		}
		if !substepIsUpstream(f.Substep, currentSubstep) {
			continue
		}
		if lane != "" && !laneAcceptsRouteTo(lane, f.RouteTo) {
			continue
		}
		eligible = append(eligible, f)
	}
	if len(eligible) == 0 {
		return ""
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].Timestamp > eligible[j].Timestamp
	})
	elided := 0
	if len(eligible) > priorDiscoveriesCap {
		elided = len(eligible) - priorDiscoveriesCap
		eligible = eligible[:priorDiscoveriesCap]
	}
	var b strings.Builder
	b.WriteString("## Prior discoveries (recorded earlier this session)\n\n")
	b.WriteString("These facts were surfaced by upstream subagents during the current deploy run. ")
	b.WriteString("They do NOT belong in published content — they are framework / tooling observations ")
	b.WriteString("that would otherwise cost you investigation time. Use them as background; do not ")
	b.WriteString("re-investigate the same surface unless you have a specific reason to verify.\n")
	for _, f := range eligible {
		b.WriteString("\n- ")
		b.WriteString(formatPriorDiscoveryBullet(f))
	}
	if elided > 0 {
		fmt.Fprintf(&b, "\n\n_… and %d more earlier discoveries (see %s)_", elided, logPath)
	}
	return b.String()
}

// laneAcceptsRouteTo reports whether the given lane should see a fact
// with the named RouteTo value. Legacy empty-RouteTo (pre-C-2 records)
// is accepted by every lane — treated as "unclassified, broadcast".
//
// Lane aliases (stable across C-5 follow-up):
//   - "scaffold": scaffold_preamble + content_env_comment + zerops_yaml_comment
//   - "feature":  feature_preamble + content_env_comment + zerops_yaml_comment
//   - "writer":   all content_* + claude_md + discarded
//   - "code-review": claude_md + content_gotcha + content_ig + content_env_comment
//
// Unknown lane values fall through to accept every RouteTo (safe default).
func laneAcceptsRouteTo(lane, routeTo string) bool {
	if routeTo == "" {
		return true
	}
	allow, known := laneRouteToAllowlist[lane]
	if !known {
		return true
	}
	_, ok := allow[routeTo]
	return ok
}

// laneRouteToAllowlist is the allow-list per lane. Edits here must stay
// aligned with the FactRouteTo* constants in internal/ops/facts_log.go.
var laneRouteToAllowlist = map[string]map[string]bool{
	"scaffold": {
		"scaffold_preamble":   true,
		"content_env_comment": true,
		"zerops_yaml_comment": true,
	},
	"feature": {
		"feature_preamble":    true,
		"content_env_comment": true,
		"zerops_yaml_comment": true,
	},
	"writer": {
		"content_gotcha":      true,
		"content_intro":       true,
		"content_ig":          true,
		"content_env_comment": true,
		"claude_md":           true,
		"zerops_yaml_comment": true,
		"discarded":           true,
	},
	"code-review": {
		"claude_md":           true,
		"content_gotcha":      true,
		"content_ig":          true,
		"content_env_comment": true,
	},
}

// buildPriorDiscoveriesBlockFromPath is the path-injectable seam used
// by tests. The exported wrapper resolves the path via factLogPathLocal;
// tests pass the path directly so they don't depend on os.TempDir().
func buildPriorDiscoveriesBlockFromPath(logPath, currentSubstep string) string {
	facts := readPriorDiscoveries(logPath)
	if len(facts) == 0 {
		return ""
	}

	var eligible []priorDiscoveryRecord
	for _, f := range facts {
		if f.Scope != factScopeDownstream && f.Scope != factScopeBoth {
			continue
		}
		if !substepIsUpstream(f.Substep, currentSubstep) {
			continue
		}
		eligible = append(eligible, f)
	}
	if len(eligible) == 0 {
		return ""
	}

	// Newest first: facts without a parseable timestamp sort last so a
	// missing timestamp doesn't push a newer entry off the front.
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].Timestamp > eligible[j].Timestamp
	})

	elided := 0
	if len(eligible) > priorDiscoveriesCap {
		elided = len(eligible) - priorDiscoveriesCap
		eligible = eligible[:priorDiscoveriesCap]
	}

	var b strings.Builder
	b.WriteString("## Prior discoveries (recorded earlier this session)\n\n")
	b.WriteString("These facts were surfaced by upstream subagents during the current deploy run. ")
	b.WriteString("They do NOT belong in published content — they are framework / tooling observations ")
	b.WriteString("that would otherwise cost you investigation time. Use them as background; do not ")
	b.WriteString("re-investigate the same surface unless you have a specific reason to verify.\n")
	for _, f := range eligible {
		b.WriteString("\n- ")
		b.WriteString(formatPriorDiscoveryBullet(f))
	}
	if elided > 0 {
		fmt.Fprintf(&b, "\n\n_… and %d more earlier discoveries (see %s)_", elided, logPath)
	}
	return b.String()
}

// readPriorDiscoveries reads the facts log at the given path. Missing
// or unreadable files yield an empty slice; malformed lines are dropped
// silently rather than aborting the read — a single corrupt line
// shouldn't blank the prior-discoveries block.
func readPriorDiscoveries(logPath string) []priorDiscoveryRecord {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []priorDiscoveryRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec priorDiscoveryRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// formatPriorDiscoveryBullet renders one downstream-scoped fact as a
// markdown bullet. Title is bold; mechanism (when present) appears in
// italics in parentheses; failureMode + fixApplied form the trailing
// "what happened, what unblocked it" sentence.
func formatPriorDiscoveryBullet(f priorDiscoveryRecord) string {
	var b strings.Builder
	b.WriteString("**")
	b.WriteString(f.Title)
	b.WriteString("**")
	if f.Mechanism != "" {
		fmt.Fprintf(&b, " (_%s_)", f.Mechanism)
	}
	tail := strings.TrimSpace(strings.Join(nonEmptyFactParts(f.FailureMode, f.FixApplied), " — "))
	if tail != "" {
		b.WriteString(" — ")
		b.WriteString(tail)
	}
	return b.String()
}

func nonEmptyFactParts(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// substepOrder gives every recipe substep a stable numeric position in
// the deploy + close pipeline. Substeps not in the list (e.g. generate-
// step substeps, which run before any downstream-scoped fact would be
// recorded) sort to position -1 — facts recorded there are always
// considered upstream of any deploy/close substep.
//
// Ordering follows recipe_substeps.go (deploy showcase shape):
// deploy-dev → start-processes → verify-dev → init-commands → subagent
// → snapshot-dev → feature-sweep-dev → browser-walk → cross-deploy →
// verify-stage → feature-sweep-stage → readmes → close.code-review →
// close.close-browser-walk.
var substepOrder = map[string]int{
	SubStepDeployDev:         0,
	SubStepStartProcs:        1,
	SubStepVerifyDev:         2,
	SubStepInitCommands:      3,
	SubStepSubagent:          4,
	SubStepSnapshotDev:       5,
	SubStepFeatureSweepDev:   6,
	SubStepBrowserWalk:       7,
	SubStepCrossDeploy:       8,
	SubStepVerifyStage:       9,
	SubStepFeatureSweepStage: 10,
	SubStepReadmes:           11,
	SubStepCloseReview:       12,
	SubStepCloseBrowserWalk:  13,
}

// substepIsUpstream reports whether candidate appears strictly earlier
// than current in the deploy/close substep sequence. Generate-step
// substeps (and unknown values) sort upstream of every deploy/close
// position — a scaffold fact recorded at SubStepZeropsYAML is upstream
// of every deploy substep.
//
// Special cases:
//   - Empty candidate (fact carries no substep): treat as upstream so
//     legacy facts without a substep field still surface.
//   - Empty current (caller forgot to thread the substep): treat as no
//     filter — every eligible fact passes through.
func substepIsUpstream(candidate, current string) bool {
	if current == "" {
		return true
	}
	curPos, knownCur := substepOrder[current]
	if !knownCur {
		// Unknown current substep — be permissive so a future substep
		// rename doesn't silently drop every fact.
		return true
	}
	if candidate == "" {
		return true
	}
	candPos, knownCand := substepOrder[candidate]
	if !knownCand {
		// Generate-step substeps and other non-deploy substeps land
		// here. Treat them as strictly upstream of the deploy pipeline.
		return true
	}
	return candPos < curPos
}
