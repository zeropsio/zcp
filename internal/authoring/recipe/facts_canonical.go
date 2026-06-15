package recipe

import "time"

// Run-40 ENG-4 — facts.jsonl topic normalization + latest-by-canonical.
//
// Facts.jsonl is append-only by design (every fact is a frozen
// observation at recording time), but downstream phases (env-content,
// refinement) reading the full stream see ambiguous duplication when
// the same concept gets recorded under different topic strings —
// run-39 carried three aliases for the NATS queue group:
//
//   - `worker-nats-queue-group`           — scaffold-time decision
//   - `worker-raw-nats-subscribe-queue-group` — feature-phase
//     reimplementation
//   - `worker-queue-group-renamed-workers` — feature-phase rename to
//     `'workers'`
//
// Bare latest-by-topic is insufficient because the topic strings
// don't collide. Codex flagged this during run-40 validation:
// downstream phases need to read the latest *canonical* observation
// for each concept, not the latest under any one topic string.
//
// canonicalTopicAliases is the alias table. Entries are added as
// drift is observed in `docs/zcprecipator3/runs/N/facts.jsonl` and
// pinned by a test. Unrecognized topics pass through CanonicalTopic
// verbatim — drift only collapses under entries explicitly listed
// here. Spec: docs/spec-knowledge-distribution.md.
//
// Diagnosed in plans/run-40-evidence-grounded-plan.md §"S1-1" + §"ENG-4".

// canonicalTopicAliases maps recorded-topic strings to canonical
// concept keys. Add an entry when two or more topic strings record
// the same observable across phases. Naming convention for the
// canonical key: lowercase-hyphenated, no codebase prefix (the
// codebase is carried in fact.Scope, not the topic key).
var canonicalTopicAliases = map[string]string{
	// NATS queue-group concept — run-39 surfaced 3 aliases for the
	// same observable. Canonical key: "nats-queue-group".
	"worker-nats-queue-group":               "nats-queue-group",
	"worker-raw-nats-subscribe-queue-group": "nats-queue-group",
	"worker-queue-group-renamed-workers":    "nats-queue-group",

	// NATS connection-pattern concept — same observable recorded per
	// codebase (`-api`, `-worker`). Canonical key drops the codebase
	// suffix because scope already carries that distinction.
	"nats-pattern-a-separate-credentials-api":    "nats-connection-pattern",
	"nats-pattern-a-separate-credentials-worker": "nats-connection-pattern",
}

// CanonicalTopic returns the canonical concept key for a recorded
// topic. Returns the input verbatim when no alias applies.
func CanonicalTopic(topic string) string {
	if canonical, ok := canonicalTopicAliases[topic]; ok {
		return canonical
	}
	return topic
}

// LatestByCanonicalTopic groups records by their canonical topic key
// and returns the most recent record per key (RecordedAt-ordered).
// Records without a parseable RecordedAt fall back to insertion-order
// last-wins so the function is total.
//
// Used by env-content brief composition + refinement reads to resolve
// cross-phase fact-stream drift. Pre-fix, env-content reading the
// raw stream picked up the earliest fact under each unique topic
// string — for the NATS queue group that meant the scaffold-time
// `'showcase-workers'` value leaked into tier yaml comments even
// after feature-phase renamed it to `'workers'`.
func LatestByCanonicalTopic(records []FactRecord) map[string]FactRecord {
	if len(records) == 0 {
		return map[string]FactRecord{}
	}
	latest := make(map[string]FactRecord, len(records))
	for _, r := range records {
		key := CanonicalTopic(r.Topic)
		existing, ok := latest[key]
		if !ok {
			latest[key] = r
			continue
		}
		if factRecordedBefore(existing, r) {
			latest[key] = r
		}
	}
	return latest
}

// factRecordedBefore reports whether a's RecordedAt is strictly
// earlier than b's. Both empty / unparseable: returns false (existing
// record wins). Only one parseable: parseable wins as the "more
// recent" signal so legacy records without timestamps don't shadow
// timestamped ones.
func factRecordedBefore(a, b FactRecord) bool {
	at, aOK := parseRecordedAt(a.RecordedAt)
	bt, bOK := parseRecordedAt(b.RecordedAt)
	switch {
	case aOK && bOK:
		return at.Before(bt)
	case !aOK && bOK:
		return true // b has a timestamp, a doesn't — b is "more recent"
	case aOK && !bOK:
		return false
	default:
		return false // neither parseable — existing wins
	}
}

// parseRecordedAt parses the ISO-8601 RecordedAt slot. RFC3339 covers
// the canonical "2026-05-11T08:45:36Z" form facts.jsonl carries.
func parseRecordedAt(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// AliasedCanonicalTopics returns the subset of canonical keys whose
// records came from more than one distinct source topic in records.
// Used by env-content + refinement brief composers to surface only
// the keys where aliasing actually happened — the unaliased majority
// stays in the raw facts stream.
//
// Returns map[canonicalKey]latestFact for keys where aliasing was
// observed. Empty map = no aliasing in the input.
func AliasedCanonicalTopics(records []FactRecord) map[string]FactRecord {
	if len(records) == 0 {
		return map[string]FactRecord{}
	}
	sources := map[string]map[string]struct{}{}
	for _, r := range records {
		key := CanonicalTopic(r.Topic)
		if key == r.Topic {
			continue
		}
		if sources[key] == nil {
			sources[key] = map[string]struct{}{}
		}
		sources[key][r.Topic] = struct{}{}
	}
	latest := LatestByCanonicalTopic(records)
	out := make(map[string]FactRecord, len(sources))
	for key, srcSet := range sources {
		if len(srcSet) == 0 {
			continue
		}
		if rec, ok := latest[key]; ok {
			out[key] = rec
		}
	}
	return out
}
