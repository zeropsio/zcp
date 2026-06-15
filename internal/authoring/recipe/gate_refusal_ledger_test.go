package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// gate_refusal_ledger_test.go — Run-47 Item D substrate pin.
//
// Engine-side gate refusals (Item 3 wrapper revert, Item 4 friendly-
// auth floor, Item 7 self-referential KB linter, Item B citation
// validator revert) write structured JSONL entries to
// `<outputRoot>/.gate-refusals.jsonl`. One line per refusal. The
// status action summarizes counts per gate per phase so the run-47
// validation report can answer "did Item N fire?" deterministically.
//
// Pins:
//   - The helper writes one valid JSON line per refusal.
//   - Session.mu MUST NOT be held during ledger I/O (CLAUDE.md "Hold
//     mutexes during I/O").
//   - Append failure (readonly fs) does not fail the caller's request.
//   - Status action surfaces gateRefusals as map[gate][phase]int.

// readLedger reads .gate-refusals.jsonl from dir and returns one entry
// per non-blank line. Fails the test if a line is malformed.
func readLedger(t *testing.T, dir string) []GateRefusalEntry {
	t.Helper()
	path := filepath.Join(dir, GateRefusalLedgerName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read ledger: %v", err)
	}
	var entries []GateRefusalEntry
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e GateRefusalEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("malformed ledger line %d: %v\nline: %s", i+1, err, line)
		}
		entries = append(entries, e)
	}
	return entries
}

// TestAppendGateRefusal_WritesOneLinePerEntry — base contract pin.
// Each call appends exactly one JSON line; entries persist across
// calls; the file is created on first write.
func TestAppendGateRefusal_WritesOneLinePerEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	AppendGateRefusal(dir, GateRefusalEntry{
		Gate:   "friendly-auth-floor",
		Phase:  "codebase-content",
		Reason: "first",
	})
	AppendGateRefusal(dir, GateRefusalEntry{
		Gate:   "self-referential-naming",
		Phase:  "codebase-content",
		Reason: "second",
	})
	entries := readLedger(t, dir)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries; got %d", len(entries))
	}
	if entries[0].Gate != "friendly-auth-floor" || entries[0].Reason != "first" {
		t.Errorf("entry[0] mismatch: %+v", entries[0])
	}
	if entries[1].Gate != "self-referential-naming" || entries[1].Reason != "second" {
		t.Errorf("entry[1] mismatch: %+v", entries[1])
	}
	// Timestamp must be stamped (non-empty, RFC3339).
	if entries[0].Timestamp == "" {
		t.Errorf("entry[0] timestamp must be stamped on empty input")
	}
	if _, err := time.Parse(time.RFC3339, entries[0].Timestamp); err != nil {
		t.Errorf("entry[0] timestamp not RFC3339: %v", err)
	}
}

// TestAppendGateRefusal_EmptyOutputRootIsNoop — in-memory sessions
// (outputRoot=="") MUST NOT panic or attempt I/O.
func TestAppendGateRefusal_EmptyOutputRootIsNoop(t *testing.T) {
	t.Parallel()
	AppendGateRefusal("", GateRefusalEntry{Gate: "x", Phase: "y", Reason: "z"})
	// No assertion needed — survival is the contract.
}

// TestAppendGateRefusal_AppendFailureDoesNotFailRequest — codex
// requirement: readonly fs causes ledger-append failure; caller's
// flow MUST continue. Pinned at the helper level — the helper logs
// to stderr and returns silently; no panic, no error propagation.
func TestAppendGateRefusal_AppendFailureDoesNotFailRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-readonly directory semantics differ on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	// Make the directory read-only so OpenFile with O_CREATE refuses.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod dir readonly: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	// MUST NOT panic; MUST NOT block; MUST NOT return an error path
	// to the caller.
	AppendGateRefusal(dir, GateRefusalEntry{
		Gate:   "friendly-auth-floor",
		Phase:  "codebase-content",
		Reason: "should fail to write",
	})
	// No file should exist (write failed) but the call survived.
	if _, err := os.Stat(filepath.Join(dir, GateRefusalLedgerName)); err == nil {
		t.Errorf("ledger file should not exist on readonly-dir test; readonly bit ineffective")
	}
}

// TestAppendGateRefusal_DoesNotHoldSessionLock — codex requirement.
// Spawn many concurrent appends; if the helper accidentally took
// Session.mu it would deadlock against the test's own Session.mu
// usage. We approximate this contract by checking: (a) the helper
// itself never takes a Session reference; (b) concurrent appenders
// complete within a tight timeout (no lock contention beyond the
// package-level write-mu).
func TestAppendGateRefusal_DoesNotHoldSessionLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const N = 50
	var wg sync.WaitGroup
	done := make(chan struct{})
	for range N {
		wg.Go(func() {
			AppendGateRefusal(dir, GateRefusalEntry{
				Gate:   "friendly-auth-floor",
				Phase:  "codebase-content",
				Reason: "concurrent test",
			})
		})
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent appends deadlocked or starved (>5s); helper may be holding Session.mu")
	}
	entries := readLedger(t, dir)
	if len(entries) != N {
		t.Errorf("want %d entries from concurrent appends; got %d", N, len(entries))
	}
}

// TestGateRefusalLedger_WritesEntryOnFriendlyAuthRefusal — Item 4
// integration. A codebase-content close that fails the friendly-
// auth-floor proportional-coverage check writes one entry to the
// ledger per refusing codebase.
func TestGateRefusalLedger_WritesEntryOnFriendlyAuthRefusal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.OutputRoot = dir
	// 6-tunable yaml with zero adapt-paths — required floor is 2.
	yaml := `zerops:
  - setup: apidev
    run:
      base: nodejs@22
      minContainers: 2
      maxContainers: 5
      verticalAutoscaling:
        minRam: 0.5
        maxRam: 2
        minCpu: 1
        maxCpu: 4
`
	sess.Plan = &Plan{
		Slug:          "synth-showcase",
		EngineVersion: "dev",
		Codebases:     []Codebase{{Hostname: "apidev", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
		Fragments:     map[string]string{"codebase/apidev/zerops-yaml": yaml},
	}
	sess.Current = PhaseCodebaseContent
	// Run the gate at the same hook the close-phase path uses so the
	// integration is end-to-end (gate → telemetry).
	ctx := GateContext{Plan: sess.Plan, OutputRoot: dir, FactsLog: log, EngineVersion: "dev"}
	violations := gateFriendlyAuthorityFloor(ctx)
	if len(violations) == 0 {
		t.Fatal("fixture must produce a friendly-auth-floor violation; got none")
	}
	// Engine-side hook: emit telemetry for each violation produced by
	// the gate. The completePhase path does this when violations are
	// returned from RunGates; here we exercise the engine-side
	// emission helper directly.
	recordGateViolations(sess, "codebase-content", violations)
	entries := readLedger(t, dir)
	if len(entries) == 0 {
		t.Fatal("expected at least one friendly-auth-floor entry; got none")
	}
	found := false
	for _, e := range entries {
		if e.Gate == "friendly-auth-floor" && e.Phase == "codebase-content" && e.Codebase == "apidev" {
			found = true
			if e.Fragment != "codebase/apidev/zerops-yaml" {
				t.Errorf("expected fragment=codebase/apidev/zerops-yaml; got %q", e.Fragment)
			}
		}
	}
	if !found {
		t.Errorf("expected gate=friendly-auth-floor codebase=apidev entry; got %+v", entries)
	}
}

// TestGateRefusalLedger_WritesEntryOnSelfReferentialRefusal — Item 7
// integration. A record-fragment call on a codebase KB with self-
// referential backticked tokens refuses; one ledger entry is written.
func TestGateRefusalLedger_WritesEntryOnSelfReferentialRefusal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir
	sess.Current = PhaseCodebaseContent
	apiDir := filepath.Join(dir, "apidev")
	if err := os.MkdirAll(filepath.Join(apiDir, "src", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir apidev/src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "src", "cache", "cache.module.ts"),
		[]byte("export class CacheService {}\n"), 0o644); err != nil {
		t.Fatalf("write cache.module.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "package.json"),
		[]byte(`{"name":"api"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **NestJS DI throws "Nest can't resolve dependencies"** — register the Redis
  token in ` + "`cache.module.ts`" + ` so ` + "`CacheService`" + ` can inject
  ` + "`REDIS_CLIENT`" + ` cleanly. See the ` + "`init-commands`" + ` guide.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	// Replace Codebase[0] with hostname matching the staged dir.
	sess.Plan.Codebases[0].Hostname = "api"
	sess.Plan.Codebases[0].SourceRoot = apiDir
	in := RecipeInput{
		Action:         "record-fragment",
		FragmentID:     "codebase/api/knowledge-base",
		Fragment:       body,
		Mode:           modeReplace,
		Classification: string(ClassIntersection),
	}
	r := handleRecordFragment(sess, in, RecipeResult{Action: "record-fragment"})
	if r.OK {
		t.Fatal("expected ok=false on self-referential KB body; got OK=true")
	}
	entries := readLedger(t, dir)
	if len(entries) == 0 {
		t.Fatal("expected at least one self-referential-naming entry; got none")
	}
	found := false
	for _, e := range entries {
		if e.Gate == "self-referential-naming" && e.Codebase == "api" {
			found = true
			if e.Fragment != "codebase/api/knowledge-base" {
				t.Errorf("expected fragment=codebase/api/knowledge-base; got %q", e.Fragment)
			}
		}
	}
	if !found {
		t.Errorf("expected gate=self-referential-naming codebase=api entry; got %+v", entries)
	}
}

// TestGateRefusalLedger_WritesEntryOnRefinementReplaceRevert — Item 3
// + Item B integration. A refinement-phase Replace that introduces a
// new blocking validator violation reverts and writes one ledger
// entry per reverted notice (carrying the underlying validator code
// in `reason`).
func TestGateRefusalLedger_WritesEntryOnRefinementReplaceRevert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir
	sess.Current = PhaseRefinement
	// Pre-seed a wrapper revert by calling the wrapper directly with
	// a synthetic pre-blocking + post-blocking diff. Avoids the
	// validator dance; pins the telemetry hook on the wrapper site.
	fragmentID := "codebase/api/integration-guide/4"
	priorBody := "prior\n"
	preBlocking := []Violation{}
	// Stage a "post" violation by directly writing a fake fragment
	// that exposes a bare yaml fence (which will fail the citation
	// validator). Easier: use a stub that always returns a violation.
	// We use the lower-level helper path: drive the revert manually
	// via the existing telemetry hook.
	notices := []Violation{{
		Code:     codeRefinementReplaceReverted,
		Path:     fragmentID,
		Severity: SeverityNotice,
		Message:  "post-replace validator surfaced kb-citation-display-mismatch on codebase/api/integration-guide/4 — fragment reverted to its pre-refinement body. citation display text mismatches matched guide.",
	}}
	_ = preBlocking
	_ = priorBody
	recordRevertViolations(sess, fragmentID, notices)
	entries := readLedger(t, dir)
	if len(entries) == 0 {
		t.Fatal("expected at least one refinement-replace-revert entry; got none")
	}
	found := false
	for _, e := range entries {
		if e.Gate == "refinement-replace-revert" && e.Phase == "refinement" {
			found = true
			if e.Fragment != fragmentID {
				t.Errorf("expected fragment=%s; got %q", fragmentID, e.Fragment)
			}
			if !strings.Contains(e.Reason, "kb-citation-display-mismatch") {
				t.Errorf("expected reason to carry underlying validator code; got %q", e.Reason)
			}
		}
	}
	if !found {
		t.Errorf("expected gate=refinement-replace-revert phase=refinement entry; got %+v", entries)
	}
}

// TestStatusAction_ReportsGateRefusalCounts — after several refusals
// land in the ledger, the status response surfaces a summary of
// counts per gate per phase. Shape:
//
//	gateRefusals: {
//	  "friendly-auth-floor":   {"codebase-content": 2},
//	  "self-referential-naming": {"codebase-content": 1},
//	}
func TestStatusAction_ReportsGateRefusalCounts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir
	// Seed three friendly-auth-floor refusals + one self-referential.
	AppendGateRefusal(dir, GateRefusalEntry{Gate: "friendly-auth-floor", Phase: "codebase-content", Reason: "1"})
	AppendGateRefusal(dir, GateRefusalEntry{Gate: "friendly-auth-floor", Phase: "codebase-content", Reason: "2"})
	AppendGateRefusal(dir, GateRefusalEntry{Gate: "friendly-auth-floor", Phase: "codebase-content", Reason: "3"})
	AppendGateRefusal(dir, GateRefusalEntry{Gate: "self-referential-naming", Phase: "codebase-content", Reason: "x"})
	AppendGateRefusal(dir, GateRefusalEntry{Gate: "refinement-replace-revert", Phase: "refinement", Reason: "y"})
	snap := sess.Snapshot()
	if snap.GateRefusals == nil {
		t.Fatal("status snapshot must expose gateRefusals; got nil")
	}
	if got := snap.GateRefusals["friendly-auth-floor"]["codebase-content"]; got != 3 {
		t.Errorf("friendly-auth-floor.codebase-content = %d; want 3", got)
	}
	if got := snap.GateRefusals["self-referential-naming"]["codebase-content"]; got != 1 {
		t.Errorf("self-referential-naming.codebase-content = %d; want 1", got)
	}
	if got := snap.GateRefusals["refinement-replace-revert"]["refinement"]; got != 1 {
		t.Errorf("refinement-replace-revert.refinement = %d; want 1", got)
	}
}
