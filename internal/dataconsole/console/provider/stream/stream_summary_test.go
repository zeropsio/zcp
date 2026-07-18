package stream

import (
	"encoding/json"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

// ---- S18: kafka topic summary is enriched + honestly labelled metadata ----
//
// kafkaTopicInfo's live path needs a broker (see stream_test.go's note), so the
// SHAPE it hands the SPA is factored into the pure kafkaSummary builder and
// pinned here without one — exactly how kafkaTopicNotFound's classifier is
// unit-tested while its I/O is e2e. The card (§7.5) needs partitions +
// partitionIds + a consumer-group field that is ALWAYS present as real data or
// an explicit "unavailable" (never omitted — an absent field reads as "zero
// groups", a lie), and message peek stays out of scope (DD-3).

func TestKafkaSummary_CarriesPartitionsAndAvailableConsumers(t *testing.T) {
	t.Parallel()
	got := marshalSummary(t, kafkaSummary("orders", []int{2, 1, 0}, consumersAvailable([]string{"g1", "g2"})))

	if got["topic"] != "orders" {
		t.Errorf("topic = %v, want orders", got["topic"])
	}
	if got["partitions"] != float64(3) {
		t.Errorf("partitions = %v, want 3", got["partitions"])
	}
	if ids, ok := got["partitionIds"].([]any); !ok || len(ids) != 3 {
		t.Errorf("partitionIds = %v, want a 3-element array", got["partitionIds"])
	}
	cg, ok := got["consumerGroups"].(map[string]any)
	if !ok {
		t.Fatalf("consumerGroups = %v, want an object (the card's consumer field)", got["consumerGroups"])
	}
	if cg["available"] != true {
		t.Errorf("consumerGroups.available = %v, want true", cg["available"])
	}
	if cg["count"] != float64(2) {
		t.Errorf("consumerGroups.count = %v, want 2", cg["count"])
	}
	if groups, ok := cg["groups"].([]any); !ok || len(groups) != 2 {
		t.Errorf("consumerGroups.groups = %v, want [g1 g2]", cg["groups"])
	}
}

// The best-effort contract's core: when consumer-group data is unavailable
// (privilege denied / cost / failure) the field is STILL present, carrying an
// explicit available:false + a reason — never dropped, never a fabricated zero.
func TestKafkaSummary_ConsumersUnavailable_IsExplicitNotOmitted(t *testing.T) {
	t.Parallel()
	got := marshalSummary(t, kafkaSummary("orders", []int{0}, consumersUnavailable(reasonConsumersNotPermitted)))

	cg, ok := got["consumerGroups"].(map[string]any)
	if !ok {
		t.Fatalf("consumerGroups = %v, want an object even when unavailable", got["consumerGroups"])
	}
	if cg["available"] != false {
		t.Errorf("consumerGroups.available = %v, want false", cg["available"])
	}
	if cg["reason"] != reasonConsumersNotPermitted {
		t.Errorf("consumerGroups.reason = %v, want %q", cg["reason"], reasonConsumersNotPermitted)
	}
	// An unavailable field must not masquerade as "0 groups": no groups list.
	if _, present := cg["groups"]; present {
		t.Errorf("consumerGroups.groups present on an unavailable result: %v", cg["groups"])
	}
}

func TestConsumersAvailable_And_Unavailable_Constructors(t *testing.T) {
	t.Parallel()
	av := consumersAvailable([]string{"a", "b", "c"})
	if !av.Available || av.Count != 3 || len(av.Groups) != 3 || av.Reason != "" {
		t.Errorf("consumersAvailable = %+v, want available/count=3/3 groups/no reason", av)
	}
	un := consumersUnavailable("nope")
	if un.Available || un.Count != 0 || un.Groups != nil || un.Reason != "nope" {
		t.Errorf("consumersUnavailable = %+v, want unavailable/0/nil/reason=nope", un)
	}
}

// ---- S18: nats summary keeps its fields + gains the consumer count ----
//
// natsSummary is a pure function of a jetstream.StreamInfo value, which a test
// can build by hand — no live broker. Consumers is the nats analog of kafka's
// consumer groups, already carried in StreamState (no extra round trip).

func TestNatsSummary_CarriesStreamStateAndConsumers(t *testing.T) {
	t.Parallel()
	info := &jetstream.StreamInfo{
		Config: jetstream.StreamConfig{Subjects: []string{"events.>"}},
		State: jetstream.StreamState{
			Msgs: 30, Bytes: 1920, FirstSeq: 1, LastSeq: 30, Consumers: 2,
		},
	}
	got := marshalSummary(t, natsSummary("EVENTS", info))

	for k, want := range map[string]float64{
		"messages": 30, "bytes": 1920, "firstSeq": 1, "lastSeq": 30, "consumers": 2,
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if got["stream"] != "EVENTS" {
		t.Errorf("stream = %v, want EVENTS", got["stream"])
	}
	subjects, ok := got["subjects"].([]any)
	if !ok || len(subjects) != 1 || subjects[0] != "events.>" {
		t.Errorf("subjects = %v, want [events.>]", got["subjects"])
	}
}

// ---- S18: the metadata-not-content signal (U-04) ----
//
// streamBlobMeta is the one place ReadBlob stamps the honest discriminator: the
// summary is application/json AND flagged StreamMetadata so a viewer labels it
// "metadata, not messages" instead of rendering it as an editable document.

func TestStreamBlobMeta_SignalsMetadataNotContent(t *testing.T) {
	t.Parallel()
	meta := streamBlobMeta(87)
	if meta.ContentType != contentTypeJSON {
		t.Errorf("ContentType = %q, want %q", meta.ContentType, contentTypeJSON)
	}
	if meta.Size != 87 {
		t.Errorf("Size = %d, want 87", meta.Size)
	}
	if !meta.StreamMetadata {
		t.Error("StreamMetadata = false, want true (the SPA keys on this to render a metadata card, never editable content — U-04/DD-3)")
	}
}

// marshalSummary round-trips a summary map through the provider's own jsonBytes
// encoder into a generic map — asserting on exactly the wire bytes the SPA
// receives, not the in-memory Go value.
func marshalSummary(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	b, err := jsonBytes(v)
	if err != nil {
		t.Fatalf("jsonBytes: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal summary: %v (%q)", err, b)
	}
	return got
}
