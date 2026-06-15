package recipe

import (
	"strings"
	"testing"
)

// Run-40 ENG-4 — facts.jsonl topic normalization + latest-by-canonical.

// TestCanonicalTopic_KnownAliases pins each of the three run-39
// queue-group aliases to the canonical key, plus the two NATS
// connection-pattern codebase variants.
func TestCanonicalTopic_KnownAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		topic, want string
	}{
		{"worker-nats-queue-group", "nats-queue-group"},
		{"worker-raw-nats-subscribe-queue-group", "nats-queue-group"},
		{"worker-queue-group-renamed-workers", "nats-queue-group"},
		{"nats-pattern-a-separate-credentials-api", "nats-connection-pattern"},
		{"nats-pattern-a-separate-credentials-worker", "nats-connection-pattern"},
	}
	for _, tc := range tests {
		t.Run(tc.topic, func(t *testing.T) {
			t.Parallel()
			got := CanonicalTopic(tc.topic)
			if got != tc.want {
				t.Errorf("CanonicalTopic(%q) = %q, want %q", tc.topic, got, tc.want)
			}
		})
	}
}

// TestCanonicalTopic_UnknownPassesThrough — topic strings not in the
// alias table return as-is. Pinned so a future regression that
// over-normalizes (e.g. via a heuristic strip) surfaces immediately.
func TestCanonicalTopic_UnknownPassesThrough(t *testing.T) {
	t.Parallel()
	for _, topic := range []string{
		"api-health-path-no-prefix",
		"app-storage-card-dual-affordance",
		"meilisearch-master-key-server-side",
	} {
		if got := CanonicalTopic(topic); got != topic {
			t.Errorf("CanonicalTopic(%q) returned %q, expected verbatim pass-through", topic, got)
		}
	}
}

// TestLatestByCanonicalTopic_QueueGroup_ReturnsWorkers pins the
// closure of S1-1: the three run-39 NATS queue-group facts collapse
// under canonical `nats-queue-group`, and the latest by RecordedAt
// is the rename-to-`'workers'` fact, NOT the scaffold-time
// `'showcase-workers'` fact. Without this fix, env-content reading
// the raw stream picked up the earliest record under each unique
// topic string and emitted `showcase-workers` in tier yaml comments
// while source code used `workers`.
func TestLatestByCanonicalTopic_QueueGroup_ReturnsWorkers(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{
			Topic:      "worker-nats-queue-group",
			RecordedAt: "2026-05-11T08:45:36Z",
			Kind:       FactKindFieldRationale,
			FieldPath:  "src/main.ts:queue",
			FieldValue: "showcase-workers",
			Why:        "scaffold-time queue group name",
		},
		{
			Topic:      "worker-raw-nats-subscribe-queue-group",
			RecordedAt: "2026-05-11T09:09:49Z",
			Kind:       FactKindPorterChange,
			Phase:      "feature",
			ChangeKind: "code-addition",
			Diff:       "this.sub = nc.subscribe(JOBS_DEMO_SUBJECT, { queue: 'workers' });",
			Why:        "raw client subscribe with explicit queue option",
		},
		{
			Topic:      "worker-queue-group-renamed-workers",
			RecordedAt: "2026-05-11T09:10:04Z",
			Kind:       FactKindFieldRationale,
			Phase:      "feature",
			FieldPath:  "run.envVariables.NATS_QUEUE_GROUP",
			FieldValue: "workers",
			Why:        "rename from showcase-workers to workers",
		},
	}

	latest := LatestByCanonicalTopic(facts)
	canonical, ok := latest["nats-queue-group"]
	if !ok {
		t.Fatalf("canonical key `nats-queue-group` missing from result; got keys %v", keysOf(latest))
	}
	if canonical.Topic != "worker-queue-group-renamed-workers" {
		t.Errorf("expected latest canonical fact to be the rename record (topic %q); got %q",
			"worker-queue-group-renamed-workers", canonical.Topic)
	}
	if canonical.FieldValue != "workers" {
		t.Errorf("expected canonical FieldValue=`workers`; got %q", canonical.FieldValue)
	}
	// Only one entry per canonical key.
	if len(latest) != 1 {
		t.Errorf("expected single canonical key; got %d (keys=%v)", len(latest), keysOf(latest))
	}
}

// TestLatestByCanonicalTopic_PreservesUnaliased — facts whose topics
// aren't in the alias table still pass through latest-by-canonical
// (each unique topic becomes its own key). Pinned so the
// normalization pass doesn't accidentally drop unaliased records.
func TestLatestByCanonicalTopic_PreservesUnaliased(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{Topic: "api-health-path-no-prefix", RecordedAt: "2026-05-11T08:00:00Z"},
		{Topic: "app-storage-card-dual-affordance", RecordedAt: "2026-05-11T08:30:00Z"},
	}
	latest := LatestByCanonicalTopic(facts)
	if len(latest) != 2 {
		t.Errorf("expected 2 entries (no aliasing); got %d", len(latest))
	}
	if _, ok := latest["api-health-path-no-prefix"]; !ok {
		t.Errorf("missing api-health-path-no-prefix in result")
	}
	if _, ok := latest["app-storage-card-dual-affordance"]; !ok {
		t.Errorf("missing app-storage-card-dual-affordance in result")
	}
}

// TestLatestByCanonicalTopic_MissingTimestamp — records without
// RecordedAt fall back to insertion-order last-wins, and timestamped
// records always beat untimestamped ones (signals that the
// timestamped record is the more deliberate observation).
func TestLatestByCanonicalTopic_MissingTimestamp(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{Topic: "worker-nats-queue-group", FieldValue: "no-timestamp"},                                           // no RecordedAt
		{Topic: "worker-queue-group-renamed-workers", RecordedAt: "2026-05-11T09:00:00Z", FieldValue: "workers"}, // timestamped
	}
	latest := LatestByCanonicalTopic(facts)
	canonical, ok := latest["nats-queue-group"]
	if !ok {
		t.Fatalf("canonical key missing")
	}
	if canonical.FieldValue != "workers" {
		t.Errorf("timestamped record should win over untimestamped one; got FieldValue=%q", canonical.FieldValue)
	}
}

// TestLatestByCanonicalTopic_Empty — empty input returns a non-nil
// empty map. Callers can range over the result without nil-check.
func TestLatestByCanonicalTopic_Empty(t *testing.T) {
	t.Parallel()
	got := LatestByCanonicalTopic(nil)
	if got == nil {
		t.Fatalf("expected non-nil empty map; got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map; got %d entries", len(got))
	}
}

// keysOf extracts map keys for diagnostic output in test failures.
func keysOf(m map[string]FactRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestAliasedCanonicalTopics_NatsQueueGroup_AllThreeContribute pins
// the "aliasing happened" detector. The three run-39 NATS topics
// share one canonical key; the helper returns one entry whose latest
// record is the rename fact.
func TestAliasedCanonicalTopics_NatsQueueGroup_AllThreeContribute(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{Topic: "worker-nats-queue-group", RecordedAt: "2026-05-11T08:00:00Z", Kind: FactKindFieldRationale, FieldPath: "x", FieldValue: "showcase-workers"},
		{Topic: "worker-raw-nats-subscribe-queue-group", RecordedAt: "2026-05-11T08:30:00Z", Kind: FactKindPorterChange, Diff: "queue: 'workers'"},
		{Topic: "worker-queue-group-renamed-workers", RecordedAt: "2026-05-11T09:00:00Z", Kind: FactKindFieldRationale, FieldPath: "y", FieldValue: "workers"},
	}
	out := AliasedCanonicalTopics(facts)
	if len(out) != 1 {
		t.Fatalf("expected 1 aliased canonical key; got %d (keys=%v)", len(out), keysOf(out))
	}
	rec, ok := out["nats-queue-group"]
	if !ok {
		t.Fatalf("missing canonical key `nats-queue-group`")
	}
	if rec.Topic != "worker-queue-group-renamed-workers" {
		t.Errorf("expected latest aliased record to be the rename; got %q", rec.Topic)
	}
}

// TestAliasedCanonicalTopics_NoAliasing_EmptyMap — when every topic
// maps to its own canonical key (i.e. no entries in the alias
// table apply), the helper returns an empty map so the brief
// composer skips the canonical-latest section entirely.
func TestAliasedCanonicalTopics_NoAliasing_EmptyMap(t *testing.T) {
	t.Parallel()
	facts := []FactRecord{
		{Topic: "api-health-path-no-prefix", RecordedAt: "2026-05-11T08:00:00Z"},
		{Topic: "app-storage-card-dual-affordance", RecordedAt: "2026-05-11T08:30:00Z"},
	}
	out := AliasedCanonicalTopics(facts)
	if len(out) != 0 {
		t.Errorf("expected empty map (no aliasing); got %d entries: %v", len(out), keysOf(out))
	}
}

// TestBuildEnvContentBrief_CarriesCanonicalLatestSection_WhenAliased
// is the integration-level pin: when the facts stream carries the
// run-39 NATS aliases, the rendered brief includes a `## Latest by
// canonical topic` section naming the canonical key + the latest
// (rename) record.
func TestBuildEnvContentBrief_CarriesCanonicalLatestSection_WhenAliased(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	facts := []FactRecord{
		{Topic: "worker-nats-queue-group", RecordedAt: "2026-05-11T08:00:00Z", Kind: FactKindFieldRationale, FieldPath: "src/main.ts:queue", FieldValue: "showcase-workers", Why: "scaffold-time value"},
		{Topic: "worker-queue-group-renamed-workers", RecordedAt: "2026-05-11T09:00:00Z", Kind: FactKindFieldRationale, FieldPath: "run.envVariables.NATS_QUEUE_GROUP", FieldValue: "workers", Why: "feature-phase rename"},
	}
	brief, err := BuildEnvContentBrief(plan, nil, facts)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Latest by canonical topic") {
		t.Errorf("env-content brief missing canonical-latest section")
	}
	if !strings.Contains(brief.Body, "canonical=nats-queue-group") {
		t.Errorf("env-content brief missing canonical key entry; got body suffix:\n%s", briefTail(brief.Body, 600))
	}
	if !strings.Contains(brief.Body, "workers") {
		t.Errorf("env-content brief should cite the latest value `workers`; got body suffix:\n%s", briefTail(brief.Body, 600))
	}
}

// TestBuildEnvContentBrief_OmitsCanonicalLatestSection_NoAliasing —
// when the facts stream has no aliased topics, the section is
// suppressed entirely. Pins the noise-suppression contract.
func TestBuildEnvContentBrief_OmitsCanonicalLatestSection_NoAliasing(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	facts := []FactRecord{
		{Topic: "api-health-path-no-prefix", RecordedAt: "2026-05-11T08:00:00Z", Kind: FactKindFieldRationale, FieldPath: "main.ts", FieldValue: "health", Why: "no-prefix"},
	}
	brief, err := BuildEnvContentBrief(plan, nil, facts)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Latest by canonical topic") {
		t.Errorf("env-content brief should NOT include canonical-latest section without aliasing")
	}
}

func briefTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
