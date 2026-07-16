package stream

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	// contentTypeJSON is the MIME every stream summary carries. The summary is
	// valid JSON so a raw API caller can parse it, but the StreamMetadata flag
	// (streamBlobMeta) is the real render signal — the content-type alone routes
	// it through the editable-document branch, which is exactly the U-04 bug.
	contentTypeJSON = "application/json"

	// reasonConsumersNotPermitted / reasonConsumersUnavailable are the honest,
	// sanitized explanations attached to an unavailable consumer-group result.
	// They are fixed strings, never the raw driver error text (which can embed
	// broker/host detail) — the same no-leak discipline as object.mapErr.
	reasonConsumersNotPermitted = "consumer-group listing not permitted for these credentials"
	reasonConsumersUnavailable  = "consumer-group listing unavailable from the broker"
)

// consumerGroups is the best-effort consumer-group view attached to a kafka
// topic summary (§7.5, DD-3). Listing consumer groups needs a cluster-level
// DESCRIBE/LIST privilege the console's managed credentials may not hold, and
// is a separate round trip; when it cannot be obtained the summary says so
// explicitly (Available=false + Reason) rather than omitting the field or
// failing the whole read — an absent field would read as "zero groups", a lie
// (the same masquerade U-04 flags for the summary as a whole). The groups are
// cluster-scoped (one ListGroups call, the cheap honest signal); per-topic
// attribution would need an N-way DescribeGroups walk, out of scope for a card.
type consumerGroups struct {
	Available bool     `json:"available"`
	Count     int      `json:"count"`
	Groups    []string `json:"groups,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

func consumersAvailable(groups []string) consumerGroups {
	return consumerGroups{Available: true, Count: len(groups), Groups: groups}
}

func consumersUnavailable(reason string) consumerGroups {
	return consumerGroups{Available: false, Reason: reason}
}

// kafkaSummary is the pure builder for a kafka topic's metadata card: partition
// count + ids, plus the always-present consumer-group field. Pure so it is
// unit-testable without a broker (stream_summary_test.go); kafkaTopicInfo does
// the I/O and calls it, exactly as kafkaTopicNotFound's classifier is unit-
// tested while its I/O is e2e.
func kafkaSummary(topic string, partitionIDs []int, consumers consumerGroups) map[string]any {
	return map[string]any{
		"topic":          topic,
		"partitions":     len(partitionIDs),
		"partitionIds":   partitionIDs,
		"consumerGroups": consumers,
	}
}

// natsSummary is the pure builder for a JetStream stream's metadata card:
// subjects + message/byte counts + sequence range, plus the consumer count
// (StreamState.Consumers — the nats analog of kafka's consumer groups, already
// carried in the info, no extra round trip). Pure, unit-testable without a
// broker.
func natsSummary(name string, info *jetstream.StreamInfo) map[string]any {
	return map[string]any{
		"stream":    name,
		"subjects":  info.Config.Subjects,
		"messages":  info.State.Msgs,
		"bytes":     info.State.Bytes,
		"firstSeq":  info.State.FirstSeq,
		"lastSeq":   info.State.LastSeq,
		"consumers": info.State.Consumers,
	}
}

// streamBlobMeta is the metadata every stream summary read carries: valid JSON,
// flagged StreamMetadata so a viewer renders a labelled "metadata, not messages"
// card instead of the editable-document branch (U-04, DD-3). Pure of the bytes
// it describes, so ReadBlob stamps it identically for both engines.
func streamBlobMeta(size int64) provider.BlobMeta {
	return provider.BlobMeta{ContentType: contentTypeJSON, Size: size, StreamMetadata: true}
}

// kafkaConsumerGroups fetches the cluster's consumer groups best-effort. Any
// failure — a transport error, or a top-level protocol error (typically an
// authorization denial: managed credentials often lack the cluster LIST/DESCRIBE
// ACL) — folds into an explicit "unavailable" result with a sanitized reason,
// never a hard error: the topic summary must still render. Group ids are
// app-level consumer names, not secrets, so they are returned as-is. The live
// path needs a broker, so it is proven by the e2e conformance suite; the
// pure builder + constructors it feeds are unit-tested offline.
func (p *Provider) kafkaConsumerGroups(ctx context.Context) consumerGroups {
	client := &kafka.Client{Addr: kafka.TCP(p.cfg.Addr), Timeout: dialTimeout}
	if p.cfg.User != "" {
		client.Transport = &kafka.Transport{
			SASL: plain.Mechanism{Username: p.cfg.User, Password: p.cfg.Password},
		}
	}
	resp, err := client.ListGroups(ctx, &kafka.ListGroupsRequest{Addr: kafka.TCP(p.cfg.Addr)})
	if err != nil {
		return consumersUnavailable(reasonConsumersUnavailable)
	}
	if resp.Error != nil {
		return consumersUnavailable(reasonConsumersNotPermitted)
	}
	groups := make([]string, 0, len(resp.Groups))
	for _, g := range resp.Groups {
		groups = append(groups, g.GroupID)
	}
	return consumersAvailable(groups)
}
