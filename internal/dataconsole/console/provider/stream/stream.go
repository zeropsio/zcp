// Package stream is a READ-ONLY inspector for messaging services (Kafka, NATS).
// A message log is not a browse-and-edit dataset (no addressable mutable row), so
// this family is deliberately view-only: it lists topics / JetStream streams and
// shows their metadata. It implements provider.ObjectProvider (so the generic
// server tree + blob-read endpoints drive it) but every mutation returns
// ErrReadOnly. NATS replaced the removed RabbitMQ.
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	dialTimeout = 12 * time.Second
	engineKafka = "kafka"
	engineNATS  = "nats"
)

// Config is the resolved messaging connection.
type Config struct {
	Engine   string // "kafka" | "nats"
	Addr     string // host:port
	URL      string // nats://host:port (nats)
	User     string
	Password string
}

// Provider lists topics/streams read-only.
type Provider struct {
	cfg  Config
	caps provider.Capabilities
}

// New validates the config (connections are made per-call — an inspector is
// infrequent, so there is no long-lived client to manage).
func New(cfg Config) (*Provider, error) {
	if cfg.Engine != engineKafka && cfg.Engine != engineNATS {
		return nil, fmt.Errorf("stream: %w: unknown engine %q", provider.ErrInvalid, cfg.Engine)
	}
	return &Provider{
		cfg: cfg,
		caps: provider.Capabilities{
			Family: provider.FamilyStream, Support: provider.SupportViewOnly,
			MaxInlineBytes: 1 << 20, ReadOnly: true,
		},
	}, nil
}

func (p *Provider) Kind() string                { return p.cfg.Engine }
func (p *Provider) Caps() provider.Capabilities { return p.caps }
func (p *Provider) Close() error                { return nil }

// Health proves reachability + auth by listing.
func (p *Provider) Health(ctx context.Context) error {
	_, _, err := p.List(ctx, provider.Path{}, provider.Page{})
	return err
}

// List: [] → topics/streams as leaves. There is no deeper browse (a message has
// no stable, editable address).
func (p *Provider) List(ctx context.Context, path provider.Path, _ provider.Page) ([]provider.Node, string, error) {
	if len(path.Segments) != 0 {
		return nil, "", nil
	}
	names, err := p.names(ctx)
	if err != nil {
		return nil, "", err
	}
	nodes := make([]provider.Node, 0, len(names))
	for _, n := range names {
		nodes = append(nodes, provider.Node{
			Name: n, Kind: provider.KindBlob,
			Path: provider.Path{Service: path.Service, Segments: []string{n}},
		})
	}
	return nodes, "", nil
}

// Stat reports a topic/stream as a blob.
func (p *Provider) Stat(_ context.Context, path provider.Path) (provider.Node, error) {
	if len(path.Segments) != 1 {
		return provider.Node{}, provider.ErrInvalid
	}
	return provider.Node{Name: path.Segments[0], Kind: provider.KindBlob, Path: path}, nil
}

// ReadBlob returns a JSON metadata summary for one topic/stream.
func (p *Provider) ReadBlob(ctx context.Context, path provider.Path) ([]byte, provider.BlobMeta, error) {
	if len(path.Segments) != 1 {
		return nil, provider.BlobMeta{}, provider.ErrInvalid
	}
	meta := provider.BlobMeta{ContentType: "application/json"}
	data, err := p.summary(ctx, path.Segments[0])
	if err != nil {
		return nil, meta, err
	}
	meta.Size = int64(len(data))
	return data, meta, nil
}

// WriteBlob / Delete / Rename / SetTTL / SetEntry / DeleteEntry / InsertRow /
// DeleteRow / EditCell: messaging is view-only, unconditionally, for every
// mutation shape any family provider can be asked to perform. Before these
// existed, the server's generic per-method type-assertion dispatch (server.go
// handlers) fell through to the shared ErrUnsupported (422) for whichever of
// these methods the type didn't implement, while WriteBlob/Delete already
// answered ErrReadOnly (403) — a caller saw two different refusal codes for
// the same "this family cannot be written to" fact, invisible to the SPA
// (stream never advertises any mutating action, actions.go) but confusing to
// a raw API caller. Implementing every mutation shape here, all hardcoded to
// ErrReadOnly, normalizes to the one honest "view-only" signal (STR-AUD-02
// resolution 7).
func (p *Provider) WriteBlob(context.Context, provider.Path, []byte, string) error {
	return provider.ErrReadOnly
}
func (p *Provider) Delete(context.Context, provider.Path) error { return provider.ErrReadOnly }
func (p *Provider) Rename(context.Context, provider.Path, provider.Path) error {
	return provider.ErrReadOnly
}
func (p *Provider) SetTTL(context.Context, provider.Path, *int64) error {
	return provider.ErrReadOnly
}
func (p *Provider) SetEntry(context.Context, provider.KVEntryEdit) (provider.Applied, error) {
	return provider.Applied{}, provider.ErrReadOnly
}
func (p *Provider) DeleteEntry(context.Context, provider.Path, string) (provider.Applied, error) {
	return provider.Applied{}, provider.ErrReadOnly
}
func (p *Provider) InsertRow(context.Context, provider.Path, map[string]any) (provider.Applied, error) {
	return provider.Applied{}, provider.ErrReadOnly
}
func (p *Provider) DeleteRow(context.Context, provider.Path, map[string]any) (provider.Applied, error) {
	return provider.Applied{}, provider.ErrReadOnly
}
func (p *Provider) EditCell(context.Context, provider.CellEdit) (provider.Applied, error) {
	return provider.Applied{}, provider.ErrReadOnly
}

// ---- engine dispatch ----

func (p *Provider) names(ctx context.Context) ([]string, error) {
	if p.cfg.Engine == engineKafka {
		return p.kafkaTopics(ctx)
	}
	return p.natsStreams(ctx)
}

func (p *Provider) summary(ctx context.Context, name string) ([]byte, error) {
	if p.cfg.Engine == engineKafka {
		return p.kafkaTopicInfo(ctx, name)
	}
	return p.natsStreamInfo(ctx, name)
}

// ---- Kafka ----

func (p *Provider) kafkaDial(ctx context.Context) (*kafka.Conn, error) {
	d := &kafka.Dialer{Timeout: dialTimeout}
	if p.cfg.User != "" {
		d.SASLMechanism = plain.Mechanism{Username: p.cfg.User, Password: p.cfg.Password}
	}
	conn, err := d.DialContext(ctx, "tcp", p.cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("stream: kafka dial: %w", provider.ErrUpstream)
	}
	return conn, nil
}

func (p *Provider) kafkaTopics(ctx context.Context) ([]string, error) {
	conn, err := p.kafkaDial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	parts, err := conn.ReadPartitions()
	if err != nil {
		return nil, fmt.Errorf("stream: kafka partitions: %w", provider.ErrUpstream)
	}
	seen := map[string]bool{}
	var out []string
	for _, pt := range parts {
		if pt.Topic == "" || seen[pt.Topic] || (len(pt.Topic) > 0 && pt.Topic[0] == '_') {
			continue
		}
		seen[pt.Topic] = true
		out = append(out, pt.Topic)
	}
	return out, nil
}

func (p *Provider) kafkaTopicInfo(ctx context.Context, topic string) ([]byte, error) {
	conn, err := p.kafkaDial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	parts, err := conn.ReadPartitions(topic)
	if err != nil {
		if kafkaTopicNotFound(err) {
			return nil, fmt.Errorf("stream: kafka topic: %w", provider.ErrNotFound)
		}
		return nil, fmt.Errorf("stream: kafka topic: %w", provider.ErrUpstream)
	}
	ids := make([]int, 0, len(parts))
	for _, pt := range parts {
		ids = append(ids, pt.ID)
	}
	return jsonBytes(map[string]any{"topic": topic, "partitions": len(parts), "partitionIds": ids})
}

// kafkaTopicNotFound reports whether err is Kafka's protocol-level "unknown
// topic or partition" response (ReadPartitions(topic) surfaces the broker's
// per-topic metadata error code directly, unwrapped) — the honest signal that
// a caller named a topic that genuinely does not exist (managed Kafka has
// topic auto-create disabled), as distinct from a transport/dial failure.
// Before this, EVERY ReadPartitions failure flattened to ErrUpstream (502),
// so a permanent "this topic will never exist" condition looked identical to
// a transient outage — and disagreed with NATS, which already classified the
// same condition correctly (STR-AUD-02: nats 404 vs kafka 502 for an
// identical nonexistent-resource read). This makes both engines agree.
func kafkaTopicNotFound(err error) bool {
	return errors.Is(err, kafka.UnknownTopicOrPartition)
}

// ---- NATS (JetStream) ----

func (p *Provider) natsConnect() (*nats.Conn, error) {
	opts := []nats.Option{nats.Timeout(dialTimeout)}
	if p.cfg.User != "" {
		opts = append(opts, nats.UserInfo(p.cfg.User, p.cfg.Password))
	}
	url := p.cfg.URL
	if url == "" {
		url = "nats://" + p.cfg.Addr
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("stream: nats connect: %w", provider.ErrUpstream)
	}
	return nc, nil
}

func (p *Provider) natsStreams(ctx context.Context) ([]string, error) {
	nc, err := p.natsConnect()
	if err != nil {
		return nil, err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("stream: jetstream: %w", provider.ErrUpstream)
	}
	var out []string
	lister := js.StreamNames(ctx)
	for name := range lister.Name() {
		out = append(out, name)
	}
	if lerr := lister.Err(); lerr != nil {
		return nil, fmt.Errorf("stream: nats streams: %w", provider.ErrUpstream)
	}
	return out, nil
}

func (p *Provider) natsStreamInfo(ctx context.Context, name string) ([]byte, error) {
	nc, err := p.natsConnect()
	if err != nil {
		return nil, err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("stream: jetstream: %w", provider.ErrUpstream)
	}
	s, err := js.Stream(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("stream: nats stream: %w", provider.ErrNotFound)
	}
	info, err := s.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream: nats info: %w", provider.ErrUpstream)
	}
	return jsonBytes(map[string]any{
		"stream":   name,
		"subjects": info.Config.Subjects,
		"messages": info.State.Msgs,
		"bytes":    info.State.Bytes,
		"firstSeq": info.State.FirstSeq,
		"lastSeq":  info.State.LastSeq,
	})
}

func jsonBytes(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("stream: encode: %w", provider.ErrUpstream)
	}
	return b, nil
}
