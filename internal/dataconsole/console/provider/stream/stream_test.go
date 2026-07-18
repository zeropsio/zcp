package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// ---- New: config validation ----
//
// stream.Config carries connection facts directly (Engine/Addr/URL/User/
// Password); the descriptor->Config mapping (provider.StreamConn ->
// stream.Config, including the net.JoinHostPort(Host,Port) composition) lives
// in cmd/zcp/studio_console.go's streamFactory, outside this package and out
// of scope here. What IS in-package is New's own engine validation.

func TestNew_RejectsUnknownEngine(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{"", "rabbitmq", "Kafka", "KAFKA", "redis"} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			if _, err := New(Config{Engine: engine, Addr: "127.0.0.1:1"}); !errors.Is(err, provider.ErrInvalid) {
				t.Fatalf("New(engine=%q) = %v, want ErrInvalid", engine, err)
			}
		})
	}
}

func TestNew_AcceptsKnownEngines(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{engineKafka, engineNATS} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			p, err := New(Config{Engine: engine, Addr: "127.0.0.1:1"})
			if err != nil {
				t.Fatalf("New(engine=%q): %v", engine, err)
			}
			if p.Kind() != engine {
				t.Errorf("Kind() = %q, want %q", p.Kind(), engine)
			}
		})
	}
}

// ---- Caps: messaging is unconditionally view-only + read-only ----
//
// Unlike object/tabular/kv, stream.Config carries no ReadOnly field at all —
// there is no launch posture that makes this family writable, by design (a
// message log has no addressable mutable row). This pins that invariant.

func TestNew_CapsAreAlwaysReadOnly(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{engineKafka, engineNATS} {
		p, err := New(Config{Engine: engine, Addr: "127.0.0.1:1"})
		if err != nil {
			t.Fatalf("New(%q): %v", engine, err)
		}
		caps := p.Caps()
		if caps.Family != provider.FamilyStream {
			t.Errorf("%s: Family = %q, want %q", engine, caps.Family, provider.FamilyStream)
		}
		if caps.Support != provider.SupportViewOnly {
			t.Errorf("%s: Support = %q, want SupportViewOnly", engine, caps.Support)
		}
		if !caps.ReadOnly {
			t.Errorf("%s: ReadOnly = false, want true (messaging is always read-only)", engine)
		}
		if caps.MaxInlineBytes != 1<<20 {
			t.Errorf("%s: MaxInlineBytes = %d, want %d", engine, caps.MaxInlineBytes, 1<<20)
		}
	}
}

// ---- read-only gate: unconditional, regardless of path ----

func TestWriteBlob_Delete_AlwaysReadOnly(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: engineKafka, Addr: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	path := provider.Path{Segments: []string{"topic"}}
	if err := p.WriteBlob(ctx, path, []byte("x"), "text/plain"); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("WriteBlob = %v, want ErrReadOnly", err)
	}
	if err := p.Delete(ctx, path); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("Delete = %v, want ErrReadOnly", err)
	}
}

// TestMutationRefusals_AllNormalizeToReadOnly is resolution 7 (object-stream.md
// STR-AUD-02 companion finding): every stream mutation attempt returns the
// single ErrReadOnly "view-only" signal, never the read_only/unsupported
// split that used to exist only because the type never implemented
// Rename/SetTTL/SetEntry/DeleteEntry/InsertRow/DeleteRow/EditCell at all (so
// the server's own type-assertion dispatch fell through to the generic
// ErrUnsupported 422 for those, while the hardwired WriteBlob/Delete already
// returned the correct ErrReadOnly 403). The split was invisible to the SPA
// (stream advertises zero mutating actions either way — actions.go) but
// mattered to a raw API caller; this pins one honest "view-only" code for
// every mutation this family can be asked to perform, for both engines.
func TestMutationRefusals_AllNormalizeToReadOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := provider.Path{Segments: []string{"topic"}}
	for _, engine := range []string{engineKafka, engineNATS} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			p, err := New(Config{Engine: engine, Addr: "127.0.0.1:1"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := p.Rename(ctx, path, path); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("Rename = %v, want ErrReadOnly", err)
			}
			if err := p.SetTTL(ctx, path, nil); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("SetTTL = %v, want ErrReadOnly", err)
			}
			if _, err := p.SetEntry(ctx, provider.KVEntryEdit{Path: path, Field: "f"}); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("SetEntry = %v, want ErrReadOnly", err)
			}
			if _, err := p.DeleteEntry(ctx, path, "f"); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("DeleteEntry = %v, want ErrReadOnly", err)
			}
			if _, err := p.InsertRow(ctx, path, map[string]any{"a": 1}); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("InsertRow = %v, want ErrReadOnly", err)
			}
			if _, err := p.DeleteRow(ctx, path, map[string]any{"a": 1}); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("DeleteRow = %v, want ErrReadOnly", err)
			}
			if _, err := p.EditCell(ctx, provider.CellEdit{Path: path, Column: "a"}); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("EditCell = %v, want ErrReadOnly", err)
			}
		})
	}
}

// ---- List/Stat/ReadBlob: arity guards short-circuit before any network call ----

// TestList_NonRootPath_ShortCircuitsNoNetwork proves List never dials out for
// a non-root path: the Addr points at a closed port that would error/hang if
// touched, so a passing test proves the arity check runs first.
func TestList_NonRootPath_ShortCircuitsNoNetwork(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: engineKafka, Addr: closedPortAddr(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := [][]string{{"topic"}, {"topic", "extra"}}
	for _, segs := range cases {
		nodes, next, err := p.List(context.Background(), provider.Path{Segments: segs}, provider.Page{})
		if err != nil || nodes != nil || next != "" {
			t.Errorf("List(segs=%v) = (%v,%q,%v), want (nil,\"\",nil)", segs, nodes, next, err)
		}
	}
}

func TestStat_Arity(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: engineKafka, Addr: closedPortAddr(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Stat(context.Background(), provider.Path{}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("Stat(0 segs) = %v, want ErrInvalid", err)
	}
	if _, err := p.Stat(context.Background(), provider.Path{Segments: []string{"a", "b"}}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("Stat(2 segs) = %v, want ErrInvalid", err)
	}
	node, err := p.Stat(context.Background(), provider.Path{Service: "svc", Segments: []string{"topic-1"}})
	if err != nil {
		t.Fatalf("Stat(1 seg): %v", err)
	}
	if node.Name != "topic-1" || node.Kind != provider.KindBlob {
		t.Errorf("Stat(1 seg) = %+v, want Name=topic-1 Kind=blob", node)
	}
}

func TestReadBlob_RejectsWrongArity(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: engineKafka, Addr: closedPortAddr(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := p.ReadBlob(context.Background(), provider.Path{}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("ReadBlob(0 segs) = %v, want ErrInvalid", err)
	}
	if _, _, err := p.ReadBlob(context.Background(), provider.Path{Segments: []string{"a", "b"}}); !errors.Is(err, provider.ErrInvalid) {
		t.Errorf("ReadBlob(2 segs) = %v, want ErrInvalid", err)
	}
}

// ---- jsonBytes: pure formatting helper ----

func TestJSONBytes(t *testing.T) {
	t.Parallel()
	b, err := jsonBytes(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("jsonBytes: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("jsonBytes produced invalid JSON: %v (%q)", err, b)
	}
	if got["a"] != float64(1) {
		t.Errorf("jsonBytes roundtrip = %v, want a=1", got)
	}
	// MarshalIndent with "  " -> multi-line output.
	if !slices.Contains(b, '\n') {
		t.Errorf("jsonBytes(%q) not indented", b)
	}
}

// ---- connection-establishment error wrapping (no live broker; a refused
// loopback port stands in for "the endpoint is unreachable") ----

// closedPortAddr allocates an ephemeral loopback port, closes it immediately,
// and returns its address — a deterministic, near-instant "connection
// refused" target with no live server involved. Confirmed refusal latency in
// this environment is sub-millisecond (no filtered/dropped-packet delay).
func closedPortAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

func TestKafkaDial_ConnectionRefused_WrapsUpstream(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: engineKafka, Addr: closedPortAddr(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.kafkaDial(ctx); !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("kafkaDial(refused port) = %v, want ErrUpstream", err)
	}
}

func TestNatsConnect_ConnectionRefused_WrapsUpstream(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Engine: engineNATS, Addr: closedPortAddr(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.natsConnect(); !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("natsConnect(refused port) = %v, want ErrUpstream", err)
	}
}

// Health's failure path (List -> names -> {kafkaTopics,natsStreams} ->
// {kafkaDial,natsConnect}) is fully exercised by a refused port, with no live
// broker required; only its SUCCESS path needs one — see
// internal/dataconsole/console/provider/conformance (TestStream_Conversions).
func TestHealth_ConnectionRefused_ReturnsUpstreamError(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{engineKafka, engineNATS} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			p, err := New(Config{Engine: engine, Addr: closedPortAddr(t)})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := p.Health(ctx); !errors.Is(err, provider.ErrUpstream) {
				t.Fatalf("Health(refused port) = %v, want ErrUpstream", err)
			}
		})
	}
}

// The success paths of List/Health (kafkaTopics/natsStreams reading real
// partition/JetStream metadata) and ReadBlob (kafkaTopicInfo/natsStreamInfo)
// need a live Kafka/NATS broker — no in-memory fake is vendored for either.
// They live in internal/dataconsole/console/provider/conformance
// (TestStream_Conversions, e2e-tagged), not as t.Skip stubs here; the
// failure path above is covered without one.

// ---- STR-AUD-02: kafka's "topic not found" classification, offline ----
//
// kafkaTopicInfo itself needs a live broker (see above), but the
// classification it must apply is a pure function of the error kafka-go's
// Conn.ReadPartitions returns — a nonexistent topic comes back as the
// protocol-level kafka.UnknownTopicOrPartition (Kafka error code 3), never a
// transport/dial failure. Testing the classifier directly, offline, is what
// makes this fix verifiable without a broker; kafkaTopicInfo just wires it in.

func TestKafkaTopicNotFound(t *testing.T) {
	t.Parallel()
	if !kafkaTopicNotFound(kafka.UnknownTopicOrPartition) {
		t.Error("want true for kafka.UnknownTopicOrPartition")
	}
	if !kafkaTopicNotFound(fmt.Errorf("wrapped: %w", kafka.UnknownTopicOrPartition)) {
		t.Error("want true through a wrapped error")
	}
	if kafkaTopicNotFound(kafka.LeaderNotAvailable) {
		t.Error("want false for a different kafka protocol error code")
	}
	if kafkaTopicNotFound(errors.New("connection refused")) {
		t.Error("want false for a plain transport error")
	}
	if kafkaTopicNotFound(nil) {
		t.Error("want false for nil")
	}
}
