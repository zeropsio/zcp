package ingest

import (
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// eventTimeClampWindow is the ±30 d window around server now within which
// client_time is trusted verbatim (spec §6 "Timestamps").
const eventTimeClampWindow = 30 * 24 * time.Hour

// Row is one telemetry.events row (schema.sql): the wire event plus the
// enclosing batch's envelope fields and the server-computed fields
// (event_time clamp, clock_skew_ms) that never travel on the wire itself.
type Row struct {
	wire.Event
	BatchID         string
	ProtocolVersion int
	EventTime       time.Time
	// ClientTimeParsed is the embedded Event.ClientTime (an RFC3339 string
	// on the wire) parsed to time.Time — clickhouse-go needs a time.Time
	// for DateTime64 column appends (a raw RFC3339 string fails; live-hit
	// 2026-07-02).
	ClientTimeParsed time.Time
	ClockSkewMs      int64
}

// clampEventTime derives the canonical event_time + clock_skew_ms for a row
// (spec §6/§7): client_time is trusted when it falls within ±30 d of
// server now (receivedAt); otherwise event_time falls back to receivedAt.
// clock_skew_ms is always receivedAt - clientTime, regardless of the clamp.
func clampEventTime(clientTime, receivedAt time.Time) (eventTime time.Time, clockSkewMs int64) {
	skew := receivedAt.Sub(clientTime).Milliseconds()
	delta := receivedAt.Sub(clientTime)
	if delta < 0 {
		delta = -delta
	}
	if delta > eventTimeClampWindow {
		return receivedAt, skew
	}
	return clientTime, skew
}

// buildRow assembles a Row from a validated wire.Event + its enclosing
// batch envelope fields. e.ClientTime has already been proven RFC3339
// parseable by wire.ValidateStrict before this is called.
func buildRow(e wire.Event, batchID string, protocolVersion int, receivedAt time.Time) (Row, error) {
	clientTime, err := time.Parse(time.RFC3339, e.ClientTime)
	if err != nil {
		return Row{}, err
	}
	eventTime, skewMs := clampEventTime(clientTime, receivedAt)
	return Row{
		Event:            e,
		BatchID:          batchID,
		ProtocolVersion:  protocolVersion,
		EventTime:        eventTime,
		ClientTimeParsed: clientTime,
		ClockSkewMs:      skewMs,
	}, nil
}
