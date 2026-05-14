package recipe

// Run-47 Item D — gate refusal telemetry ledger.
//
// Engine-side gates that fire at sub-agent boundaries (Item 3 snapshot/
// restore wrapper, Item 4 friendly-auth gate, Item 7 self-referential
// linter, Item B citation validator revert path) leave no main-session
// evidence: the sub-agent boundary swallows the refusal + auto-fixes
// and the main agent has no observability into how many times each
// gate fired. Item D is the observability primitive that lets the
// substrate-design feedback loop close — every gate refusal writes a
// structured JSONL entry to `<outputRoot>/.gate-refusals.jsonl`. One
// line per refusal.
//
// Architecture constraints (CLAUDE.md):
//   - **No Session.mu held during ledger I/O.** Callers MUST snapshot
//     outputRoot + entry payload under any held lock, release the lock,
//     then call AppendGateRefusal. The helper itself never reaches
//     into Session state.
//   - **JSON-only stdout.** All telemetry writes go to the ledger file;
//     append-failures log to stderr (debug channel), never stdout
//     (pinned by TestNoStdoutOutsideJSONPath).
//   - **Append-only.** O_APPEND|O_CREATE|O_WRONLY only; no read-modify-
//     write cycles.
//   - **Telemetry failures never fail the request.** If the ledger
//     append fails (readonly fs, disk-full, perms), log to stderr and
//     continue — observability is not authoritative.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// GateRefusalLedgerName is the per-session telemetry filename inside
// outputRoot. One JSON object per line; readers parse line-by-line.
const GateRefusalLedgerName = ".gate-refusals.jsonl"

// Stable gate / violation-code identifiers carried in the ledger.
// Keep these in sync with the substrate gates they front (Run-46 Item 4
// friendly-auth, Item 7 self-referential, Run-17/Run-46 Item 3 wrapper
// revert path that also surfaces Run-47 Item B citation reverts).
const (
	gateFriendlyAuthFloor       = "friendly-auth-floor"
	gateSelfReferentialNaming   = "self-referential-naming"
	gateRefinementReplaceRevert = "refinement-replace-revert"

	violationFriendlyAuthFloorBelowTunable = "friendly-auth-floor-below-tunable-count"
)

// GateRefusalEntry is one line in the gate refusal ledger. JSON-encoded
// one per line under <outputRoot>/.gate-refusals.jsonl.
//
// Fields:
//   - Timestamp: RFC3339 UTC timestamp of the refusal.
//   - Gate: stable identifier of the gate that fired (e.g.
//     "friendly-auth-floor", "self-referential-naming",
//     "refinement-replace-revert").
//   - Phase: lifecycle phase at the time of refusal (codebase-content,
//     refinement, env-content, ...).
//   - Codebase: optional codebase hostname; omit for env-tier refusals.
//   - Fragment: optional fragment-id when the refusal is scoped to one
//     fragment (e.g. "codebase/apidev/zerops-yaml").
//   - Reason: human-readable summary of why the gate refused. For
//     refinement-replace-revert entries, this carries the underlying
//     validator code + a short context fragment.
type GateRefusalEntry struct {
	Timestamp string `json:"ts"`
	Gate      string `json:"gate"`
	Phase     string `json:"phase"`
	Codebase  string `json:"codebase,omitempty"`
	Fragment  string `json:"fragment,omitempty"`
	Reason    string `json:"reason"`
}

// gateRefusalWriteMu serializes concurrent appends within one process
// so the per-line ordering on disk matches write-time arrival. We
// can't rely on OS-level O_APPEND atomicity for the JSON+newline pair
// across all platforms (macOS / Linux differ on write boundaries when
// writes exceed PIPE_BUF), and the ledger is per-session so contention
// is bounded.
//
// This mutex is intentionally PACKAGE-LEVEL (not session-keyed): the
// ledger path is keyed by outputRoot, callers pass the path, and the
// helper has no Session reference. CLAUDE.md forbids cross-call
// handler state in `internal/tools/`; `internal/recipe/` is the
// engine-internal layer where this kind of serialization is the
// canonical pattern (cf. consumes_services.go's writer serialization).
var gateRefusalWriteMu sync.Mutex

// AppendGateRefusal writes one entry to <outputRoot>/.gate-refusals.jsonl.
// Failures (path missing, readonly fs, encode error) log to stderr and
// return without propagating — the caller's request is not failed.
//
// Caller contract:
//   - outputRoot MUST already be snapshot OUTSIDE any held Session lock
//     (CLAUDE.md "Hold mutexes during I/O — copy under lock, release,
//     then I/O").
//   - When outputRoot is empty (in-memory session with no on-disk
//     state), the call is a no-op.
//   - When entry.Timestamp is empty, the helper stamps time.Now().UTC()
//     in RFC3339 format. Callers can pre-stamp for deterministic tests.
func AppendGateRefusal(outputRoot string, entry GateRefusalEntry) {
	if outputRoot == "" {
		return
	}
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gate-refusal ledger: marshal entry: %v\n", err)
		return
	}
	gateRefusalWriteMu.Lock()
	defer gateRefusalWriteMu.Unlock()
	path := filepath.Join(outputRoot, GateRefusalLedgerName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gate-refusal ledger: open %s: %v\n", path, err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(encoded, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "gate-refusal ledger: write %s: %v\n", path, err)
		return
	}
}

// ReadGateRefusalLedger returns every entry currently in
// <outputRoot>/.gate-refusals.jsonl, in write order. Returns an empty
// slice (not an error) when the file does not exist — a fresh session
// with no refusals yet legitimately has no ledger.
//
// Used by the status action to summarize counts per gate per phase.
// Read pattern matches the JSONL contract (one valid JSON object per
// line; blank lines silently skipped; malformed lines surface as
// errors so a corrupt ledger fails loudly rather than silently
// under-counting).
func ReadGateRefusalLedger(outputRoot string) ([]GateRefusalEntry, error) {
	if outputRoot == "" {
		return nil, nil
	}
	path := filepath.Join(outputRoot, GateRefusalLedgerName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read gate-refusal ledger: %w", err)
	}
	var entries []GateRefusalEntry
	for i, line := range splitJSONLines(data) {
		if len(line) == 0 {
			continue
		}
		var entry GateRefusalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("gate-refusal ledger: malformed line %d: %w", i+1, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// splitJSONLines returns the byte ranges of each newline-terminated
// line in data. Trailing data without a newline still counts as a
// line. Blank lines emit empty byte slices the caller can skip.
func splitJSONLines(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// recordGateViolations is the engine-side telemetry hook that emits
// one ledger entry per violation produced by a gate at phase close.
// Currently covers:
//   - friendly-auth-floor-below-tunable-count (Run-46 Item 4) → gate=friendly-auth-floor
//
// Caller passes the live session (for OutputRoot + Plan) and the phase
// the close-gate ran against. We snapshot outputRoot under the session
// lock and release before any disk I/O.
//
// New violation codes added to gates SHOULD extend gateNameForViolation
// so the ledger gate-name is stable across the codebase.
func recordGateViolations(sess *Session, phase string, violations []Violation) {
	if sess == nil || len(violations) == 0 {
		return
	}
	sess.mu.Lock()
	outputRoot := sess.OutputRoot
	sess.mu.Unlock()
	if outputRoot == "" {
		return
	}
	for _, v := range violations {
		gate := gateNameForViolation(v.Code)
		if gate == "" {
			continue
		}
		AppendGateRefusal(outputRoot, GateRefusalEntry{
			Gate:     gate,
			Phase:    phase,
			Codebase: codebaseHostFromFragmentID(v.Path),
			Fragment: v.Path,
			Reason:   fmt.Sprintf("%s: %s", v.Code, v.Message),
		})
	}
}

// recordRevertViolations emits one ledger entry per
// refinement-replace-reverted notice produced by the snapshot/restore
// wrapper at PhaseRefinement. Each entry's `reason` carries the
// underlying validator code + a short context fragment so a reader
// can distinguish Item B (citation-validator) reverts from other
// gate-driven reverts.
//
// fragmentID is the codebase or env fragment the wrapper reverted.
// The notice list is the slice returned by runCodebaseRefinementWrapper
// / runEnvRefinementWrapper; only entries with Code ==
// codeRefinementReplaceReverted emit telemetry.
func recordRevertViolations(sess *Session, fragmentID string, notices []Violation) {
	if sess == nil || len(notices) == 0 {
		return
	}
	sess.mu.Lock()
	outputRoot := sess.OutputRoot
	sess.mu.Unlock()
	if outputRoot == "" {
		return
	}
	for _, n := range notices {
		if n.Code != codeRefinementReplaceReverted {
			continue
		}
		AppendGateRefusal(outputRoot, GateRefusalEntry{
			Gate:     gateRefinementReplaceRevert,
			Phase:    string(PhaseRefinement),
			Codebase: codebaseHostFromFragmentID(fragmentID),
			Fragment: fragmentID,
			Reason:   n.Message,
		})
	}
}

// recordSelfReferentialRefusal emits one ledger entry when the
// self-referential KB linter refuses a record-fragment call. Phase
// is sourced from sess.Current so a refusal at codebase-content
// (the canonical entry point per Run-46 Item 7) emits
// phase="codebase-content"; refusals during refinement would emit
// phase="refinement".
func recordSelfReferentialRefusal(sess *Session, fragmentID, reason string) {
	if sess == nil || reason == "" {
		return
	}
	sess.mu.Lock()
	outputRoot := sess.OutputRoot
	phase := string(sess.Current)
	sess.mu.Unlock()
	if outputRoot == "" {
		return
	}
	AppendGateRefusal(outputRoot, GateRefusalEntry{
		Gate:     gateSelfReferentialNaming,
		Phase:    phase,
		Codebase: codebaseHostFromFragmentID(fragmentID),
		Fragment: fragmentID,
		Reason:   reason,
	})
}

// gateNameForViolation maps a gate-produced violation code to the
// stable gate identifier the ledger surfaces. Returns "" for codes
// that are not engine-side substrate gates (caller skips the emit).
//
// Add new mappings here when introducing a new substrate gate that
// produces a refusal worth surfacing in the run validation report.
func gateNameForViolation(code string) string {
	switch code {
	case violationFriendlyAuthFloorBelowTunable:
		return gateFriendlyAuthFloor
	default:
		return ""
	}
}

// SummarizeGateRefusals walks the ledger and returns counts per gate
// per phase: result[gate][phase] = count. Used by the status action to
// surface refusal telemetry without forcing every reader to parse the
// JSONL stream themselves.
func SummarizeGateRefusals(entries []GateRefusalEntry) map[string]map[string]int {
	if len(entries) == 0 {
		return nil
	}
	out := map[string]map[string]int{}
	for _, e := range entries {
		if e.Gate == "" {
			continue
		}
		phaseBucket, ok := out[e.Gate]
		if !ok {
			phaseBucket = map[string]int{}
			out[e.Gate] = phaseBucket
		}
		phaseBucket[e.Phase]++
	}
	return out
}
