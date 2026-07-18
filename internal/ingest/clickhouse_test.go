package ingest

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

func TestInsertColumns_MatchesSchemaColumnCount(t *testing.T) {
	t.Parallel()

	cols := strings.Split(insertColumns, ", ")
	// schema.sql telemetry.events has 29 base columns; received_at (server
	// DEFAULT) and ingest_version (MATERIALIZED) are never explicitly
	// inserted, so the base insert column list carries 27. Migration
	// 0002_add_error_subcode.sql (spec §4.2/§8) adds one more column on top
	// of the base shape, so the current insert column list carries 28.
	const want = 28
	if len(cols) != want {
		t.Errorf("insertColumns has %d columns, want %d: %v", len(cols), want, cols)
	}
	for _, forbidden := range []string{"received_at", "ingest_version"} {
		for _, c := range cols {
			if c == forbidden {
				t.Errorf("insertColumns must not list %q (CH DEFAULT/MATERIALIZED column)", forbidden)
			}
		}
	}
}

func TestBuildInsertQuery_ContainsDatabaseAndColumns(t *testing.T) {
	t.Parallel()

	q := buildInsertQuery("telemetry")
	if !strings.Contains(q, "telemetry.events") {
		t.Errorf("query %q does not reference telemetry.events", q)
	}
	if !strings.Contains(q, insertColumns) {
		t.Errorf("query %q does not contain the insert column list", q)
	}
}

func TestClampUint32_ClampsNegativeAndOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int64
		want uint32
	}{
		{"zero", 0, 0},
		{"typical", 1842, 1842},
		{"negative", -5, 0},
		{"maxUint32", int64(math.MaxUint32), math.MaxUint32},
		{"overflow", int64(math.MaxUint32) + 100, math.MaxUint32},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := clampUint32(tc.in); got != tc.want {
				t.Errorf("clampUint32(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func validRow() Row {
	var r Row
	r.SchemaVersion = 1
	r.EventID = "s:1"
	r.InstallID = "9d3e8f1a-4b2c-4a5d-9e6f-1234567890ab"
	r.SessionID = "9d3e8f1a-4b2c-4a5d-9e6f-1234567890ac"
	r.Seq = 1
	r.EventType = "tool_call"
	r.ClientTime = "2026-07-02T10:00:00.000Z"
	r.Dims = map[string]string{}
	r.BatchID = "9d3e8f1a-4b2c-4a5d-9e6f-1234567890ad"
	r.ProtocolVersion = 1
	r.EventTime = time.Unix(0, 0)
	r.ClockSkewMs = 12
	return r
}

func TestRowValues_ProducesOneValuePerColumn(t *testing.T) {
	t.Parallel()

	values, err := rowValues(validRow())
	if err != nil {
		t.Fatalf("rowValues: %v", err)
	}
	wantCols := len(strings.Split(insertColumns, ", "))
	if len(values) != wantCols {
		t.Errorf("rowValues returned %d values, want %d (one per insert column)", len(values), wantCols)
	}
}

func TestRowValues_InvalidInstallUUID_ReturnsError(t *testing.T) {
	t.Parallel()

	r := validRow()
	r.InstallID = "not-a-uuid"
	if _, err := rowValues(r); err == nil {
		t.Error("expected an error for a malformed install_id UUID")
	}
}

func TestRowValues_InvalidBatchUUID_ReturnsError(t *testing.T) {
	t.Parallel()

	r := validRow()
	r.BatchID = "not-a-uuid"
	if _, err := rowValues(r); err == nil {
		t.Error("expected an error for a malformed batch_id UUID")
	}
}

// fakeCHBatch is a driver.Batch test double recording every Append call.
type fakeCHBatch struct {
	appends [][]any
	sent    bool
	aborted bool
	closed  bool
}

func (b *fakeCHBatch) Abort() error                  { b.aborted = true; return nil }
func (b *fakeCHBatch) Append(v ...any) error         { b.appends = append(b.appends, v); return nil }
func (b *fakeCHBatch) AppendStruct(any) error        { return nil }
func (b *fakeCHBatch) Column(int) driver.BatchColumn { return nil }
func (b *fakeCHBatch) Flush() error                  { return nil }
func (b *fakeCHBatch) Send() error                   { b.sent = true; return nil }
func (b *fakeCHBatch) IsSent() bool                  { return b.sent }
func (b *fakeCHBatch) Rows() int                     { return len(b.appends) }
func (b *fakeCHBatch) Columns() []column.Interface   { return nil }
func (b *fakeCHBatch) Close() error                  { b.closed = true; return nil }

// fakeCHConn is a driver.Conn test double; only PrepareBatch is exercised
// by chInsertConn.Insert — every other method is an unused stub to satisfy
// the interface.
type fakeCHConn struct {
	batch        *fakeCHBatch
	prepareQuery string
	prepareErr   error
}

func (c *fakeCHConn) Contributors() []string { return nil }

// ServerVersion/Query are unused driver.Conn stub methods — chInsertConn.Insert
// only ever calls PrepareBatch/Ping/Close on the connection, so these two
// legitimately return a nil value with a nil error (never inspected by
// production code under test).
func (c *fakeCHConn) ServerVersion() (*driver.ServerVersion, error) {
	return nil, nil //nolint:nilnil // unused stub, see comment above
}
func (c *fakeCHConn) Select(context.Context, any, string, ...any) error { return nil }
func (c *fakeCHConn) Query(context.Context, string, ...any) (driver.Rows, error) {
	return nil, nil //nolint:nilnil // unused stub, see comment above
}
func (c *fakeCHConn) QueryRow(context.Context, string, ...any) driver.Row { return nil }
func (c *fakeCHConn) PrepareBatch(_ context.Context, query string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
	c.prepareQuery = query
	if c.prepareErr != nil {
		return nil, c.prepareErr
	}
	c.batch = &fakeCHBatch{}
	return c.batch, nil
}
func (c *fakeCHConn) Exec(context.Context, string, ...any) error              { return nil }
func (c *fakeCHConn) AsyncInsert(context.Context, string, bool, ...any) error { return nil }
func (c *fakeCHConn) Ping(context.Context) error                              { return nil }
func (c *fakeCHConn) Stats() driver.Stats                                     { return driver.Stats{} }
func (c *fakeCHConn) Close() error                                            { return nil }

func TestCHInsertConn_EmptyRows_NeverPreparesBatch(t *testing.T) {
	t.Parallel()

	fc := &fakeCHConn{}
	ins := &chInsertConn{conn: fc, database: "telemetry"}

	if err := ins.Insert(context.Background(), nil); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if fc.prepareQuery != "" {
		t.Error("Insert must not call PrepareBatch for an empty row slice")
	}
}

func TestCHInsertConn_Insert_AppendsAndSends(t *testing.T) {
	t.Parallel()

	fc := &fakeCHConn{}
	ins := &chInsertConn{conn: fc, database: "telemetry"}

	rows := []Row{validRow(), validRow()}
	if err := ins.Insert(context.Background(), rows); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if !strings.Contains(fc.prepareQuery, "telemetry.events") {
		t.Errorf("PrepareBatch query %q missing telemetry.events", fc.prepareQuery)
	}
	if fc.batch == nil {
		t.Fatal("PrepareBatch was never called")
	}
	if len(fc.batch.appends) != 2 {
		t.Fatalf("Append called %d times, want 2 (one per row)", len(fc.batch.appends))
	}
	wantCols := len(strings.Split(insertColumns, ", "))
	for i, appended := range fc.batch.appends {
		if len(appended) != wantCols {
			t.Errorf("append %d has %d values, want %d", i, len(appended), wantCols)
		}
	}
	if !fc.batch.sent {
		t.Error("Send was never called")
	}
}

func TestCHInsertConn_PrepareBatchError_Propagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection refused")
	fc := &fakeCHConn{prepareErr: wantErr}
	ins := &chInsertConn{conn: fc, database: "telemetry"}

	err := ins.Insert(context.Background(), []Row{validRow()})
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("Insert error = %v, want wrapping %v", err, wantErr)
	}
}

func TestCHInsertConn_MalformedRow_AbortsBatchAndReturnsError(t *testing.T) {
	t.Parallel()

	fc := &fakeCHConn{}
	ins := &chInsertConn{conn: fc, database: "telemetry"}

	bad := validRow()
	bad.InstallID = "not-a-uuid"

	if err := ins.Insert(context.Background(), []Row{bad}); err == nil {
		t.Error("expected an error for a malformed row")
	}
	if fc.batch.sent {
		t.Error("Send must not be called when a row fails to append")
	}
}
