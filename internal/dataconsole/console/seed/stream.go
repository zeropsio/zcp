package seed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	kafkaDialTimeout = 12 * time.Second
	kafkaOpTimeout   = 25 * time.Second
	natsDialTimeout  = 12 * time.Second
	natsOpTimeout    = 15 * time.Second
)

// seedKafka explicitly creates a namespace-derived topic (the broker has
// auto-create disabled — a write to a missing topic returns UnknownTopic)
// and writes a few sample messages to it.
func seedKafka(ctx context.Context, conn provider.StreamConn, opts Options) error {
	addr := net.JoinHostPort(conn.Host, conn.Port)
	var mech plain.Mechanism
	d := &kafka.Dialer{Timeout: kafkaDialTimeout}
	if conn.User != "" {
		mech = plain.Mechanism{Username: conn.User, Password: conn.Password}
		d.SASLMechanism = mech
	}
	cx, cancel := context.WithTimeout(ctx, kafkaOpTimeout)
	defer cancel()

	topic := StreamName(opts.Namespace, "orders")

	kconn, err := d.DialContext(cx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("seed kafka: dial: %w", err)
	}
	defer kconn.Close()
	ctrl, err := kconn.Controller()
	if err != nil {
		return fmt.Errorf("seed kafka: controller: %w", err)
	}
	cc, err := d.DialContext(cx, "tcp", net.JoinHostPort(ctrl.Host, fmt.Sprint(ctrl.Port)))
	if err != nil {
		return fmt.Errorf("seed kafka: dial controller: %w", err)
	}
	defer cc.Close()
	if err := cc.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		return fmt.Errorf("seed kafka: create topic %s: %w", topic, err)
	}

	transport := &kafka.Transport{}
	if conn.User != "" {
		transport.SASL = mech
	}
	w := &kafka.Writer{Addr: kafka.TCP(addr), Topic: topic, Balancer: &kafka.LeastBytes{}, Transport: transport}
	defer w.Close()
	msgs := []kafka.Message{
		{Key: []byte("1"), Value: []byte(`{"order":1,"item":"Widget"}`)},
		{Key: []byte("2"), Value: []byte(`{"order":2,"item":"Gadget"}`)},
		{Key: []byte("3"), Value: []byte(`{"order":3,"item":"Gizmo"}`)},
	}
	if err := w.WriteMessages(cx, msgs...); err != nil {
		return fmt.Errorf("seed kafka: write messages to %s: %w", topic, err)
	}
	return nil
}

// cleanupKafka reads every partition's topic name via ReadPartitions,
// filters to the ones StreamOwns(namespace, *) claims, and deletes them via
// the controller connection. Safe to run when nothing matches.
func cleanupKafka(ctx context.Context, conn provider.StreamConn, namespace string) error {
	addr := net.JoinHostPort(conn.Host, conn.Port)
	d := &kafka.Dialer{Timeout: kafkaDialTimeout}
	if conn.User != "" {
		d.SASLMechanism = plain.Mechanism{Username: conn.User, Password: conn.Password}
	}
	cx, cancel := context.WithTimeout(ctx, kafkaOpTimeout)
	defer cancel()

	kconn, err := d.DialContext(cx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("seed cleanup kafka: dial: %w", err)
	}
	defer kconn.Close()
	partitions, err := kconn.ReadPartitions()
	if err != nil {
		return fmt.Errorf("seed cleanup kafka: read partitions: %w", err)
	}
	seen := map[string]bool{}
	var topics []string
	for _, p := range partitions {
		if seen[p.Topic] || !StreamOwns(namespace, p.Topic) {
			continue
		}
		seen[p.Topic] = true
		topics = append(topics, p.Topic)
	}
	if len(topics) == 0 {
		return nil
	}
	ctrl, err := kconn.Controller()
	if err != nil {
		return fmt.Errorf("seed cleanup kafka: controller: %w", err)
	}
	cc, err := d.DialContext(cx, "tcp", net.JoinHostPort(ctrl.Host, fmt.Sprint(ctrl.Port)))
	if err != nil {
		return fmt.Errorf("seed cleanup kafka: dial controller: %w", err)
	}
	defer cc.Close()
	if err := cc.DeleteTopics(topics...); err != nil {
		return fmt.Errorf("seed cleanup kafka: delete topics %v: %w", topics, err)
	}
	return nil
}

// seedNats creates a namespace-derived JetStream stream and publishes a
// few sample messages. The stream NAME is namespace-derived via
// StreamName (joins with "_" — NATS stream names must not contain ".").
// The subject filter/publish subject is a SEPARATE, dot-hierarchy token
// (NATS subjects use "." natively) derived the same way but joined with
// "." instead — it must be namespace-unique too, since JetStream refuses
// two streams whose subject filters overlap.
func seedNats(ctx context.Context, conn provider.StreamConn, opts Options) error {
	nc, err := natsConnect(conn)
	if err != nil {
		return fmt.Errorf("seed nats: %w", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("seed nats: jetstream: %w", err)
	}
	cx, cancel := context.WithTimeout(ctx, natsOpTimeout)
	defer cancel()

	stream := StreamName(opts.Namespace, "EVENTS")
	subject := natsSubjectBase(opts.Namespace)

	_, err = js.CreateStream(cx, jetstream.StreamConfig{Name: stream, Subjects: []string{subject + ".>"}})
	if err != nil && !errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		return fmt.Errorf("seed nats: create stream %s: %w", stream, err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := js.Publish(cx, subject+".seed", fmt.Appendf(nil, `{"seq":%d,"msg":"hello"}`, i)); err != nil {
			return fmt.Errorf("seed nats: publish %d to %s: %w", i, stream, err)
		}
	}
	return nil
}

// cleanupNats deletes every JetStream stream StreamOwns(namespace, *)
// claims. Deleting the stream removes its subject binding too, so cleanup
// only needs to reason about stream names, not the subject namespacing
// seedNats derives separately.
func cleanupNats(ctx context.Context, conn provider.StreamConn, namespace string) error {
	nc, err := natsConnect(conn)
	if err != nil {
		return fmt.Errorf("seed cleanup nats: %w", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("seed cleanup nats: jetstream: %w", err)
	}
	cx, cancel := context.WithTimeout(ctx, natsOpTimeout)
	defer cancel()

	var names []string
	lister := js.StreamNames(cx)
	for name := range lister.Name() {
		if StreamOwns(namespace, name) {
			names = append(names, name)
		}
	}
	if err := lister.Err(); err != nil {
		return fmt.Errorf("seed cleanup nats: list streams: %w", err)
	}
	for _, name := range names {
		if err := js.DeleteStream(cx, name); err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
			return fmt.Errorf("seed cleanup nats: delete stream %s: %w", name, err)
		}
	}
	return nil
}

func natsConnect(conn provider.StreamConn) (*nats.Conn, error) {
	opts := []nats.Option{nats.Timeout(natsDialTimeout)}
	if conn.User != "" {
		opts = append(opts, nats.UserInfo(conn.User, conn.Password))
	}
	nc, err := nats.Connect("nats://"+net.JoinHostPort(conn.Host, conn.Port), opts...)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return nc, nil
}

// natsSubjectBase derives the dot-hierarchy subject prefix a seeded
// stream's messages publish under. Static mode (namespace == "") reproduces
// dcseed's original "events" subject base exactly; a namespaced run
// prefixes with "<namespace>." so two namespaces' streams never claim
// overlapping subject filters.
func natsSubjectBase(namespace string) string {
	if namespace == "" {
		return "events"
	}
	return namespace + ".events"
}
