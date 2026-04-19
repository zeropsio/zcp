package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v8.96 §6.4 — BuildPriorDiscoveriesBlock reads the session facts log
// and renders a markdown block of upstream-recorded downstream-scoped
// facts for downstream subagent dispatch. Filters: scope ∈ {downstream,
// both}, substep strictly upstream of currentSubstep, sorted newest-
// first, capped at 8 with elision.

// testFactRecord mirrors the on-disk JSONL shape that ops.AppendFact
// produces. The workflow package cannot import internal/ops (cycle —
// ops imports workflow), so tests synthesize the wire shape here.
// Tags MUST stay byte-identical to ops.FactRecord; a drift would mean
// the prior-discoveries reader silently misses fields.
type testFactRecord struct {
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
}

// writeTestFacts lays down a JSONL facts log in dir under the given
// session ID and returns the resolved path so callers can pass it back
// into the unit under test.
func writeTestFacts(t *testing.T, dir, sessionID string, recs []testFactRecord) string {
	t.Helper()
	path := filepath.Join(dir, "zcp-facts-"+sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open facts log: %v", err)
	}
	defer f.Close()
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal fact: %v", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("write fact: %v", err)
		}
	}
	return path
}

// TestBuildPriorDiscoveriesBlock_EmptyLog returns the empty string when
// the facts log is missing — no header, no error, no separator. The
// downstream subagent runs as if the v8.96 mechanism didn't exist.
func TestBuildPriorDiscoveriesBlock_EmptyLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got := buildPriorDiscoveriesBlockFromPath(filepath.Join(dir, "zcp-facts-empty.jsonl"), SubStepSubagent)
	if got != "" {
		t.Errorf("missing facts log should yield empty block, got %d bytes:\n%s", len(got), got)
	}
}

// TestBuildPriorDiscoveriesBlock_FiltersToDownstreamScope drops content-
// scoped facts and keeps downstream + both. The two downstream facts must
// appear; the content-scoped fact must not.
func TestBuildPriorDiscoveriesBlock_FiltersToDownstreamScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC()
	recs := []testFactRecord{
		{
			Timestamp: now.Add(-3 * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepScaffold,
			Type:      "platform_observation",
			Title:     "Meilisearch v0.57 renamed class from MeiliSearch to Meilisearch",
			Mechanism: "Meilisearch SDK v0.57",
			Scope:     factScopeDownstream,
		},
		{
			Timestamp: now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepScaffold,
			Type:      "gotcha_candidate",
			Title:     "L7 balancer terminates SSL — services bind 0.0.0.0",
			Mechanism: "L7 balancer",
			Scope:     factScopeContent,
		},
		{
			Timestamp: now.Add(-1 * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepZeropsYAML,
			Type:      "fix_applied",
			Title:     "cache-manager v6 returns absolute-epoch TTLs",
			Mechanism: "cache-manager v6",
			Scope:     factScopeBoth,
		},
	}
	path := writeTestFacts(t, dir, "filter", recs)
	got := buildPriorDiscoveriesBlockFromPath(path, SubStepSubagent)
	if got == "" {
		t.Fatal("expected non-empty block when downstream-scoped facts exist")
	}
	if !strings.Contains(got, "Meilisearch v0.57") {
		t.Error("downstream-scoped fact missing from block")
	}
	if !strings.Contains(got, "cache-manager v6") {
		t.Error("both-scoped fact missing from block")
	}
	if strings.Contains(got, "L7 balancer terminates SSL") {
		t.Error("content-scoped fact must NOT appear in downstream block")
	}
	if !strings.Contains(got, "Prior discoveries") {
		t.Error("block missing the 'Prior discoveries' heading")
	}
}

// TestBuildPriorDiscoveriesBlock_ExcludesForwardInTimeFacts — facts
// recorded at substeps DOWNSTREAM of the dispatch substep must not leak
// into a brief that's about to be delivered. The brief for a feature
// subagent dispatched at SubStepSubagent should not see facts that were
// recorded at SubStepReadmes (later in the deploy pipeline).
func TestBuildPriorDiscoveriesBlock_ExcludesForwardInTimeFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC()
	recs := []testFactRecord{
		{
			Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepReadmes, // DOWNSTREAM of SubStepSubagent
			Type:      "platform_observation",
			Title:     "writer noticed a stale README claim",
			Scope:     factScopeDownstream,
		},
		{
			Timestamp: now.Add(-3 * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepScaffold, // UPSTREAM of SubStepSubagent
			Type:      "platform_observation",
			Title:     "scaffold discovered a queue-group quirk",
			Scope:     factScopeDownstream,
		},
	}
	path := writeTestFacts(t, dir, "fwd", recs)
	got := buildPriorDiscoveriesBlockFromPath(path, SubStepSubagent)
	if !strings.Contains(got, "scaffold discovered a queue-group quirk") {
		t.Error("upstream downstream-scoped fact must appear")
	}
	if strings.Contains(got, "writer noticed a stale README claim") {
		t.Error("downstream-recorded fact must NOT leak into a dispatch brief")
	}
}

// TestBuildPriorDiscoveriesBlock_CapsAtEightAndElides — pathological
// over-recording must not bloat the brief. Cap is 8, with an elision
// footer that names the count and the log path so the subagent can
// fetch the rest if it really needs them.
func TestBuildPriorDiscoveriesBlock_CapsAtEightAndElides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC()
	recs := make([]testFactRecord, 0, 12)
	for i := range 12 {
		recs = append(recs, testFactRecord{
			Timestamp: now.Add(time.Duration(-12+i) * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepScaffold,
			Type:      "platform_observation",
			Title:     "scaffold-fact-" + string(rune('A'+i)),
			Scope:     factScopeDownstream,
		})
	}
	path := writeTestFacts(t, dir, "cap", recs)
	got := buildPriorDiscoveriesBlockFromPath(path, SubStepSubagent)
	bullets := strings.Count(got, "\n- ")
	if bullets != 8 {
		t.Errorf("want 8 bullet entries, got %d", bullets)
	}
	if !strings.Contains(got, "and 4 more") {
		t.Errorf("elision footer missing the elided-count for the 4 trimmed entries")
	}
	if !strings.Contains(got, ".jsonl") {
		t.Error("elision footer should point at the log file")
	}
}
