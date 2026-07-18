package ingest

import (
	"context"
	"fmt"
	"math"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// insertColumns is the exact column list inserted into telemetry.events
// (schema.sql base shape PLUS every applied migration — currently
// migrations/0002_add_error_subcode.sql, docs/spec-telemetry.md §4.2/§8),
// single-sourced here for both the PrepareBatch query and rowValues'
// positional Append order — the two MUST stay in lockstep. received_at (CH
// DEFAULT now64(3)) and ingest_version (MATERIALIZED) are deliberately
// excluded: ClickHouse computes both server-side.
const insertColumns = "schema_version, protocol_version, event_id, batch_id, " +
	"install_id, session_id, seq, event_type, event_time, client_time, clock_skew_ms, " +
	"zcp_version, os, arch, runtime_env, usage_channel, " +
	"tool, command, action, duration_ms, success, error_code, error_subcode, " +
	"workflow_route, workflow_step, close_mode, dims, dropped_count"

// buildInsertQuery composes the INSERT statement for database.events.
func buildInsertQuery(database string) string {
	return fmt.Sprintf("INSERT INTO %s.events (%s)", database, insertColumns)
}

// clampUint32 saturates an int64 into ClickHouse's UInt32 range instead of
// wrapping on overflow — duration_ms/dropped_count are UInt32 columns
// (schema.sql) but wire.Event carries them as int64.
func clampUint32(v int64) uint32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}

// rowValues converts a Row into the positional Append(...) argument list,
// in exactly insertColumns' order.
func rowValues(r Row) ([]any, error) {
	installID, err := uuid.Parse(r.InstallID)
	if err != nil {
		return nil, fmt.Errorf("parse install_id: %w", err)
	}
	sessionID, err := uuid.Parse(r.SessionID)
	if err != nil {
		return nil, fmt.Errorf("parse session_id: %w", err)
	}
	batchID, err := uuid.Parse(r.BatchID)
	if err != nil {
		return nil, fmt.Errorf("parse batch_id: %w", err)
	}

	var success uint8
	if r.Success {
		success = 1
	}

	return []any{
		// G115 (int→uint16 narrowing) is excluded repo-wide in
		// .golangci.yaml; schema_version/protocol_version are tiny fixed
		// wire constants (currently 1), never user-controlled.
		uint16(r.SchemaVersion),
		uint16(r.ProtocolVersion),
		r.EventID, batchID,
		installID, sessionID, r.Seq, r.EventType, r.EventTime, r.ClientTimeParsed, r.ClockSkewMs,
		r.ZcpVersion, r.OS, r.Arch, r.RuntimeEnv, r.UsageChannel,
		r.Tool, r.Command, r.Action, clampUint32(r.DurationMs), success, r.ErrorCode, r.ErrorSubcode,
		r.WorkflowRoute, r.WorkflowStep, r.CloseMode, r.Dims, clampUint32(r.DroppedCount),
	}, nil
}

// chInsertConn is the real chInserter, backed by clickhouse-go v2's native
// protocol (spec §6 item 5). conn is the driver.Conn seam so tests can
// substitute a fake without a live ClickHouse server.
type chInsertConn struct {
	conn     driver.Conn
	database string
}

// newCHConn opens a (lazy, non-dialing) ClickHouse native-protocol
// connection per cfg. clickhouse.Open never dials eagerly — the first real
// network round-trip happens on the first PrepareBatch/Ping.
func newCHConn(cfg Config) (*chInsertConn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.CHHost, cfg.CHPort)},
		Auth: clickhouse.Auth{
			Database: cfg.CHDatabase,
			Username: cfg.CHUser,
			Password: cfg.CHPassword,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse connection: %w", err)
	}
	return &chInsertConn{conn: conn, database: cfg.CHDatabase}, nil
}

// Insert appends rows to a single PrepareBatch and sends it. A no-op for
// an empty slice (never touches the connection). Aborts the batch and
// returns an error on the first malformed row rather than sending a
// partial/corrupt batch.
func (c *chInsertConn) Insert(ctx context.Context, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, buildInsertQuery(c.database))
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	for _, r := range rows {
		values, err := rowValues(r)
		if err != nil {
			if abortErr := batch.Abort(); abortErr != nil {
				return fmt.Errorf("append row: %w (abort also failed: %w)", err, abortErr)
			}
			return fmt.Errorf("append row: %w", err)
		}
		if err := batch.Append(values...); err != nil {
			if abortErr := batch.Abort(); abortErr != nil {
				return fmt.Errorf("append row: %w (abort also failed: %w)", err, abortErr)
			}
			return fmt.Errorf("append row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send batch: %w", err)
	}
	return nil
}

// Ping best-effort-checks connectivity (used by /healthz, spec §6 item 2).
func (c *chInsertConn) Ping(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

// Close releases the underlying connection pool.
func (c *chInsertConn) Close() error {
	return c.conn.Close()
}
