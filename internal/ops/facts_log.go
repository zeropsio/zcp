package ops

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Fact record types. v8.86 §3.1: writers operating late in a deploy consume
// these records to consolidate 90 minutes of discovered facts without doing
// session-log archaeology. Every type corresponds to a README/CLAUDE.md
// surface the writer sub-agent authors at the readmes sub-step.
const (
	FactTypeGotchaCandidate       = "gotcha_candidate"
	FactTypeIGItemCandidate       = "ig_item_candidate"
	FactTypeVerifiedBehavior      = "verified_behavior"
	FactTypePlatformObservation   = "platform_observation"
	FactTypeFixApplied            = "fix_applied"
	FactTypeCrossCodebaseContract = "cross_codebase_contract"
)

var knownFactTypes = map[string]bool{
	FactTypeGotchaCandidate:       true,
	FactTypeIGItemCandidate:       true,
	FactTypeVerifiedBehavior:      true,
	FactTypePlatformObservation:   true,
	FactTypeFixApplied:            true,
	FactTypeCrossCodebaseContract: true,
}

// FactRecord is one append-only entry in the deploy facts log. The agent
// writes these at the moment of freshest knowledge (when a fix is applied,
// a platform behavior is observed, a contract binding is established); the
// readmes-sub-step writer subagent reads them back as structured input.
type FactRecord struct {
	Timestamp   string `json:"ts"`
	Substep     string `json:"substep,omitempty"`
	Codebase    string `json:"codebase,omitempty"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Mechanism   string `json:"mechanism,omitempty"`
	FailureMode string `json:"failureMode,omitempty"`
	FixApplied  string `json:"fixApplied,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

// FactLogPath returns the canonical facts-log path for a session. Lives in
// /tmp rather than the .zcp state directory because it's transient — the
// writer reads it once at deploy.readmes, then the file's job is done.
func FactLogPath(sessionID string) string {
	name := "zcp-facts-" + sessionID + ".jsonl"
	return filepath.Join(os.TempDir(), name)
}

// AppendFact validates and appends a record to the given facts-log path.
// Sets Timestamp if unset. Returns an error when Type or Title is missing,
// or when Type isn't one of the known fact types — an unknown type means
// the caller typo'd a fact kind, and silently accepting would land bad
// structure into the writer's input.
func AppendFact(path string, rec FactRecord) error {
	if strings.TrimSpace(rec.Type) == "" {
		return fmt.Errorf("fact record: type is required")
	}
	if !knownFactTypes[rec.Type] {
		return fmt.Errorf("fact record: unknown fact type %q (valid: gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract)", rec.Type)
	}
	if strings.TrimSpace(rec.Title) == "" {
		return fmt.Errorf("fact record: title is required")
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("fact record marshal: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("fact record open: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("fact record write: %w", err)
	}
	return nil
}

// ReadFacts returns every record from the facts-log path in append order.
// Missing file yields an empty slice (nil error) — the writer should handle
// a silent deploy that happened to record nothing without treating it as an
// authoring blocker.
func ReadFacts(path string) ([]FactRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read facts: %w", err)
	}
	defer f.Close()

	var out []FactRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec FactRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("read facts: line %q: %w", line, err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read facts scan: %w", err)
	}
	return out, nil
}
