package recipe

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

// FactRecord is a structured observation from a recipe run. Records are
// written at discovery time — classification is a record-time decision
// by the sub-agent, not a consume-time decision by the writer (plan
// §5 P4).
//
// FailureMode/FixApplied/Evidence/Scope (run-11 gap U-2) capture the
// natural shape of a deploy-time discovery the v2 schema already had —
// adding them to v3 stops agents from flattening hard-won discoveries
// into a thin Symptom and discarding the fix. V-1's classifier reads
// FailureMode + FixApplied to auto-detect self-inflicted shape.
//
// Run-16 §5.5 added a Kind discriminator: legacy Kind="" preserves the
// platform-trap shape (Symptom/Mechanism/SurfaceHint/Citation required);
// new Kind values (porter_change / field_rationale / tier_decision /
// contract) carry their own per-Kind required-field set and use the
// downstream slot-typed fields below. Validate dispatches on Kind.
type FactRecord struct {
	// ─── Existing fields (preserved; required for Kind="" platform-trap) ───
	Topic       string            `json:"topic"`
	Symptom     string            `json:"symptom,omitempty"`
	Mechanism   string            `json:"mechanism,omitempty"`
	SurfaceHint string            `json:"surfaceHint,omitempty"`
	Citation    string            `json:"citation,omitempty"`
	FailureMode string            `json:"failureMode,omitempty"`
	FixApplied  string            `json:"fixApplied,omitempty"`
	Evidence    string            `json:"evidence,omitempty"`
	Scope       string            `json:"scope,omitempty"`
	RecordedAt  string            `json:"recordedAt,omitempty"`
	Author      string            `json:"author,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`

	// Kind is the polymorphic discriminator. Empty = legacy platform-trap
	// shape (back-compat). Non-empty = run-16 fact subtype with per-Kind
	// required fields (validated below).
	Kind string `json:"kind,omitempty"`

	// PorterChange (Kind=porter_change):
	Phase            string `json:"phase,omitempty"`
	ChangeKind       string `json:"changeKind,omitempty"`
	Library          string `json:"library,omitempty"`
	Diff             string `json:"diff,omitempty"`
	Why              string `json:"why,omitempty"`
	CandidateClass   string `json:"candidateClass,omitempty"`
	CandidateHeading string `json:"candidateHeading,omitempty"`
	CandidateSurface string `json:"candidateSurface,omitempty"`
	CitationGuide    string `json:"citationGuide,omitempty"`
	EngineEmitted    bool   `json:"engineEmitted,omitempty"`

	// FieldRationale (Kind=field_rationale):
	FieldPath         string `json:"fieldPath,omitempty"`
	FieldValue        string `json:"fieldValue,omitempty"`
	Alternatives      string `json:"alternatives,omitempty"`
	CompoundReasoning string `json:"compoundReasoning,omitempty"`

	// TierDecision (Kind=tier_decision):
	Tier        int    `json:"tier,omitempty"`
	Service     string `json:"service,omitempty"`
	ChosenValue string `json:"chosenValue,omitempty"`
	TierContext string `json:"tierContext,omitempty"`

	// Contract (Kind=contract):
	Publishers    []string `json:"publishers,omitempty"`
	Subscribers   []string `json:"subscribers,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	QueueGroups   []string `json:"queueGroups,omitempty"`
	PayloadSchema string   `json:"payloadSchema,omitempty"`
	Purpose       string   `json:"purpose,omitempty"`
}

// Fact Kind discriminator values. Empty Kind preserves the legacy
// platform-trap shape (Kind="" path in Validate).
const (
	FactKindPorterChange   = "porter_change"
	FactKindFieldRationale = "field_rationale"
	FactKindTierDecision   = "tier_decision"
	FactKindContract       = "contract"
)

// Validate returns an error if any required field is empty for the
// record's Kind. Empty Kind keeps the legacy Symptom/Mechanism/
// SurfaceHint/Citation requirements so existing platform-trap callers
// keep working byte-for-byte.
func (f FactRecord) Validate() error {
	if f.Topic == "" {
		return errors.New("fact record missing required field \"topic\"")
	}
	switch f.Kind {
	case "":
		return f.validatePlatformTrap()
	case FactKindPorterChange:
		return f.validatePorterChange()
	case FactKindFieldRationale:
		return f.validateFieldRationale()
	case FactKindTierDecision:
		return f.validateTierDecision()
	case FactKindContract:
		return f.validateContract()
	default:
		return fmt.Errorf("fact record has unknown kind %q", f.Kind)
	}
}

func (f FactRecord) validatePlatformTrap() error {
	switch "" {
	case f.Symptom:
		return errors.New("fact record missing required field \"symptom\"")
	case f.Mechanism:
		return errors.New("fact record missing required field \"mechanism\"")
	case f.SurfaceHint:
		return errors.New("fact record missing required field \"surfaceHint\"")
	case f.Citation:
		return errors.New("fact record missing required field \"citation\"")
	}
	return nil
}

// validatePorterChange enforces the porter_change shape. Why is the
// load-bearing slot (the agent must explain WHY the porter would have
// to make this change). Engine-emitted fact shells (§7.2) are exempt
// from Why+CandidateHeading checks until fill-fact-slot lands them; the
// EngineEmitted=true flag signals "shell awaiting agent fill".
func (f FactRecord) validatePorterChange() error {
	if f.EngineEmitted {
		return nil // shell — fill-fact-slot validates the merged record later
	}
	switch "" {
	case f.Why:
		return errors.New("porter_change fact missing required field \"why\"")
	case f.CandidateClass:
		return errors.New("porter_change fact missing required field \"candidateClass\"")
	case f.CandidateSurface:
		return errors.New("porter_change fact missing required field \"candidateSurface\"")
	}
	return nil
}

func (f FactRecord) validateFieldRationale() error {
	switch "" {
	case f.FieldPath:
		return errors.New("field_rationale fact missing required field \"fieldPath\"")
	case f.Why:
		return errors.New("field_rationale fact missing required field \"why\"")
	}
	return nil
}

func (f FactRecord) validateTierDecision() error {
	// Tier 0 (AI Agent) is a real, valid tier; rejecting f.Tier == 0
	// would block legitimate records (reviewer D-1). Validate the tier
	// range against the actual tier set instead — out-of-range values
	// (negative, > 5) signal an unset / wrong int.
	if f.Tier < 0 || f.Tier > 5 {
		return fmt.Errorf("tier_decision fact has out-of-range tier %d (valid: 0..5)", f.Tier)
	}
	switch "" {
	case f.FieldPath:
		return errors.New("tier_decision fact missing required field \"fieldPath\"")
	case f.ChosenValue:
		return errors.New("tier_decision fact missing required field \"chosenValue\"")
	}
	return nil
}

func (f FactRecord) validateContract() error {
	if len(f.Publishers) == 0 {
		return errors.New("contract fact missing required field \"publishers\"")
	}
	if len(f.Subscribers) == 0 {
		return errors.New("contract fact missing required field \"subscribers\"")
	}
	switch "" {
	case f.Subject:
		return errors.New("contract fact missing required field \"subject\"")
	case f.Purpose:
		return errors.New("contract fact missing required field \"purpose\"")
	}
	return nil
}

// FactsLog is a JSONL file of fact records scoped to one run. Safe for
// concurrent Append/Read; serializes writes through an instance mutex.
type FactsLog struct {
	path string
	mu   sync.Mutex
}

// OpenFactsLog returns a FactsLog bound to the given path. The file does
// not need to exist yet — Append creates it on first write.
func OpenFactsLog(path string) *FactsLog {
	return &FactsLog{path: path}
}

// Path returns the underlying file path.
func (l *FactsLog) Path() string { return l.path }

// Append validates the record, stamps RecordedAt if empty, then writes one
// JSON line to the log file. Invalid records are rejected before any I/O
// so a partial fact never lands on disk.
func (l *FactsLog) Append(f FactRecord) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if f.RecordedAt == "" {
		f.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal fact: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open facts log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append fact: %w", err)
	}
	return nil
}

// Read returns all records in the log in write order. A missing file
// returns (nil, nil).
func (l *FactsLog) Read() ([]FactRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open facts log: %w", err)
	}
	defer file.Close()

	var out []FactRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec FactRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("decode fact: %w", err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan facts log: %w", err)
	}
	return out, nil
}

// FilterByHint returns the subset of records whose SurfaceHint matches.
// Used by writer brief composition to deliver only facts the owning
// surface needs.
//
// Run-16: SurfaceHint is the legacy platform-trap routing field — new
// Kind=porter_change/field_rationale/tier_decision records have empty
// SurfaceHint by design and will not appear in this output. Use
// FilterByKind for the run-16 path.
func FilterByHint(records []FactRecord, hint string) []FactRecord {
	var out []FactRecord
	for _, r := range records {
		if r.SurfaceHint == hint {
			out = append(out, r)
		}
	}
	return out
}

// ReplaceByTopic atomically rewrites the facts log, swapping the first
// record whose Topic matches merged.Topic with merged. Returns an error
// when no record carries that topic (no silent insert — the caller should
// use Append for new records).
//
// Run-16 §6.4 — backs fill-fact-slot. Engine-emitted shells (§7.1, §7.2)
// are recorded with EngineEmitted=true + empty Why; the agent fills slots
// via fill-fact-slot; this method applies the merge in place so the
// content-phase brief composer (tranche 3) sees the agent-filled shape
// with no last-write-wins dedup at read time.
func (l *FactsLog) ReplaceByTopic(merged FactRecord) error {
	if err := merged.Validate(); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Read existing records first.
	existing, err := readUnlocked(l.path)
	if err != nil {
		return err
	}
	idx := -1
	for i := range existing {
		if existing[i].Topic == merged.Topic {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("fill-fact-slot: no fact with topic %q", merged.Topic)
	}

	// Preserve RecordedAt from the original shell so the timeline of when
	// the engine seeded the shell stays intact; the agent's fill only
	// updates content slots.
	if merged.RecordedAt == "" {
		merged.RecordedAt = existing[idx].RecordedAt
	}
	existing[idx] = merged

	// Atomic rewrite via temp + rename.
	tmp := l.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open temp facts log: %w", err)
	}
	for _, rec := range existing {
		line, mErr := json.Marshal(rec)
		if mErr != nil {
			file.Close()
			os.Remove(tmp)
			return fmt.Errorf("marshal fact: %w", mErr)
		}
		if _, wErr := file.Write(append(line, '\n')); wErr != nil {
			file.Close()
			os.Remove(tmp)
			return fmt.Errorf("write fact: %w", wErr)
		}
	}
	if cErr := file.Close(); cErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("close temp facts log: %w", cErr)
	}
	if rErr := os.Rename(tmp, l.path); rErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp facts log: %w", rErr)
	}
	return nil
}

// readUnlocked is the read path without taking l.mu — for callers that
// already hold the mutex (e.g. ReplaceByTopic).
func readUnlocked(path string) ([]FactRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open facts log: %w", err)
	}
	defer file.Close()

	var out []FactRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec FactRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("decode fact: %w", err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan facts log: %w", err)
	}
	return out, nil
}

// FilterByKind returns the subset of records whose Kind matches. Run-16
// content-phase brief composition uses this to deliver only facts the
// owning surface needs (porter_change for IG/KB, field_rationale for
// codebase-yaml-comments, tier_decision for env-import-comments).
func FilterByKind(records []FactRecord, kind string) []FactRecord {
	var out []FactRecord
	for _, r := range records {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// FilterByCodebase returns the subset of records scoped to a codebase
// hostname. Matches against either the Scope prefix ("apidev/...") or
// an exact "<hostname>/code" / "<hostname>/zerops.yaml/..." form. Used
// by phase-5 codebase-content brief composition to deliver one
// codebase's facts to its dispatched sub-agent.
func FilterByCodebase(records []FactRecord, hostname string) []FactRecord {
	if hostname == "" {
		return nil
	}
	prefix := hostname + "/"
	var out []FactRecord
	for _, r := range records {
		if r.Scope == hostname || strings.HasPrefix(r.Scope, prefix) {
			out = append(out, r)
		}
	}
	return out
}
