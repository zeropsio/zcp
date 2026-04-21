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

// Fact scope values. v8.96 §6.1: Scope is orthogonal to Type and routes
// the fact between the readmes-writer subagent (content lane) and downstream
// dispatch briefs (delegation lane). Default ("") preserves pre-v8.96
// behavior — writer reads, downstream subagents do not.
const (
	FactScopeContent    = "content"
	FactScopeDownstream = "downstream"
	FactScopeBoth       = "both"
)

// knownScopes guards against typos like "downsteam" silently defaulting
// to content-scope (writer would read, downstream subagents would skip).
// Empty string is accepted — that's the legacy content-only path.
var knownScopes = map[string]bool{
	"":                  true,
	FactScopeContent:    true,
	FactScopeDownstream: true,
	FactScopeBoth:       true,
}

// Fact routing destinations. P5: fact routing is a two-way graph — the
// writer subagent classifies every recorded fact with a routed_to value
// that pairs with the published surface. The ZCP_CONTENT_MANIFEST.json
// schema references the same enum; routing claimed on the manifest must
// align with the published surface (v34 DB_PASS / DB_PASSWORD
// cross-scaffold coordination class closed by C-8's expansion).
//
// RouteTo is stored on FactRecord so a fact's route can be declared at
// record time (when classification is freshest) and consumed by the
// writer subagent + downstream honesty check without re-inferring.
const (
	FactRouteToContentGotcha     = "content_gotcha"
	FactRouteToContentIntro      = "content_intro"
	FactRouteToContentIG         = "content_ig"
	FactRouteToContentEnvComment = "content_env_comment"
	FactRouteToClaudeMD          = "claude_md"
	FactRouteToZeropsYAMLComment = "zerops_yaml_comment"
	FactRouteToScaffoldPreamble  = "scaffold_preamble"
	FactRouteToFeaturePreamble   = "feature_preamble"
	FactRouteToDiscarded         = "discarded"
)

// knownRouteTos validates against typos like "content_gocha" or
// "clude_md". Empty string is accepted as the legacy default so existing
// facts logs round-trip without mass backfill — the writer treats empty
// as "not yet routed" rather than a routing assertion.
var knownRouteTos = map[string]bool{
	"":                           true,
	FactRouteToContentGotcha:     true,
	FactRouteToContentIntro:      true,
	FactRouteToContentIG:         true,
	FactRouteToContentEnvComment: true,
	FactRouteToClaudeMD:          true,
	FactRouteToZeropsYAMLComment: true,
	FactRouteToScaffoldPreamble:  true,
	FactRouteToFeaturePreamble:   true,
	FactRouteToDiscarded:         true,
}

// IsKnownFactRouteTo reports whether s is the empty string (legacy default)
// or one of the enumerated routed-to destinations. Exported so the content
// manifest parser and downstream honesty check can share the same taxonomy.
func IsKnownFactRouteTo(s string) bool {
	return knownRouteTos[s]
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
	// Scope (v8.96 §6.1) routes the fact between the writer subagent
	// (content lane) and downstream dispatch briefs (delegation lane).
	// Empty defaults to FactScopeContent (pre-v8.96 behavior).
	Scope string `json:"scope,omitempty"`
	// RouteTo (P5 two-way-graph) declares the published surface this
	// fact belongs on so the writer subagent + manifest-honesty checker
	// can enforce consistency. Empty = "not yet routed" (legacy default);
	// non-empty must be one of FactRouteTo* constants. See IsKnownFactRouteTo.
	RouteTo string `json:"routeTo,omitempty"`
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
	if !knownScopes[rec.Scope] {
		return fmt.Errorf("fact record: unknown scope %q (valid: content, downstream, both)", rec.Scope)
	}
	if !knownRouteTos[rec.RouteTo] {
		return fmt.Errorf("fact record: unknown routeTo %q (valid: content_gotcha, content_intro, content_ig, content_env_comment, claude_md, zerops_yaml_comment, scaffold_preamble, feature_preamble, discarded)", rec.RouteTo)
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
