// Package wire is the single owner of the ZCP telemetry wire schema and its
// validation (docs/spec-telemetry.md §4). It is imported by both the
// telemetry client (emit-side lite validation, internal/telemetry) and the
// standalone ingest service (strict validation, internal/ingest /
// cmd/zcp-ingest), so it stays stdlib-only forever — a third-party or
// internal/ import here would leak into both sides of the wire boundary.
//
// wire owns exactly two kinds of vocabulary (spec §4.3, the anti-drift
// rule): the CLOSED enums (event_type, usage_channel, runtime_env, dims
// keys, and dims value sets where enumerable) and the SHAPE constraints
// (pattern + length) for fields owned elsewhere (tool/action/route/step/
// close_mode by the tool registry and topology, error_code by
// internal/platform/errors.go, os/arch by GOOS/GOARCH, zcp_version by
// semver). wire must never re-author those external vocabularies as enums —
// doing so would let an older ingest reject a newer client's new tool name.
package wire

// ProtocolVersion is the batch envelope's protocol_version (spec §4.1).
const ProtocolVersion = 1

// SchemaVersion is the per-event schema_version (spec §4.2).
const SchemaVersion = 1

// Event types — closed enum (spec §4.2/§4.3).
const (
	EventToolCall       = "tool_call"
	EventCLICommand     = "cli_command"
	EventSessionStart   = "session_start"
	EventSessionEnd     = "session_end"
	EventClientThrottle = "client_throttle"
)

// Usage channels — closed enum (spec §3.2/§4.3).
const (
	ChannelExternal     = "external"
	ChannelInternalDev  = "internal_dev"
	ChannelInternalEval = "internal_eval"
	ChannelCI           = "ci"
)

// Runtime environments — closed enum (spec §4.2/§4.3).
const (
	RuntimeContainer = "container"
	RuntimeLocal     = "local"
)

// UnknownChannel / UnknownRuntimeEnv are the sentinels ValidateStrict
// substitutes for usage_channel/runtime_env when a NEWER client sends a
// value this (older) ingest's enum doesn't yet recognize (spec §4.3
// tolerant mode) — added to each enum's own accepted set below so the
// coerced value always validates on any subsequent pass, the same pattern
// UnknownIdentifier/UnknownErrorCode use (shape.go).
const (
	UnknownChannel    = "unknown"
	UnknownRuntimeEnv = "unknown"
)

// Dim keys — closed enum (spec §4.3). v1 has exactly one key.
const (
	DimShutdownReason = "shutdown_reason"
)

// Shutdown reasons — the closed value set for dims[DimShutdownReason]
// (spec §4.3 "per-key value sets where enumerable"). Mirrors the shutdown
// categories cmd/zcp/main.go's logShutdown already distinguishes.
const (
	ShutdownSignal           = "signal"
	ShutdownClientDisconnect = "client_disconnect"
	ShutdownStdinClosed      = "stdin_closed"
	ShutdownBrokenPipe       = "broken_pipe"
	ShutdownError            = "error"
)

// Limits (spec §4.3).
const (
	// MaxDims is the max number of dims keys per event.
	MaxDims = 12
	// MaxDimKeyLen is the max length of a dims key.
	MaxDimKeyLen = 32
	// MaxDimValueLen is the max length of a dims value.
	MaxDimValueLen = 64
	// MaxEventBytes is the max size of a single event, JSON-encoded.
	MaxEventBytes = 4096
	// MaxBatchEvents is the max number of events in one batch envelope.
	// Enforced by the batcher (client) and the ingest handler, not by the
	// per-Event validators in validate.go.
	MaxBatchEvents = 100
)

// Event is the wire-level telemetry event, schema_version 1 (spec §4.2).
type Event struct {
	SchemaVersion int    `json:"schema_version"`
	EventID       string `json:"event_id"`
	InstallID     string `json:"install_id"`
	SessionID     string `json:"session_id"`
	Seq           uint64 `json:"seq"`
	EventType     string `json:"event_type"`

	ClientTime string `json:"client_time"`

	ZcpVersion string `json:"zcp_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	RuntimeEnv string `json:"runtime_env"`

	UsageChannel string `json:"usage_channel"`

	Tool    string `json:"tool"`
	Command string `json:"command"`
	Action  string `json:"action"`

	DurationMs int64  `json:"duration_ms"`
	Success    bool   `json:"success"`
	ErrorCode  string `json:"error_code"`
	// ErrorSubcode optionally narrows ErrorCode into a stable, more
	// diagnosable class (platform.Subcode* catalog, spec §4.2) — empty for
	// every call site that hasn't been split. Additive field, schema_version
	// stays 1 (spec §8): an older ingest that doesn't know this key yet
	// simply drops it on decode (Go's zero-value default), same as every
	// other forward-compat field.
	ErrorSubcode string `json:"error_subcode,omitempty"`

	WorkflowRoute string `json:"workflow_route"`
	WorkflowStep  string `json:"workflow_step"`
	CloseMode     string `json:"close_mode"`

	Dims map[string]string `json:"dims,omitempty"`

	DroppedCount int64 `json:"dropped_count"`
}

// Batch is the envelope POSTed to the ingest endpoint (spec §4.1).
type Batch struct {
	ProtocolVersion int     `json:"protocol_version"`
	BatchID         string  `json:"batch_id"`
	SentAt          string  `json:"sent_at"`
	Events          []Event `json:"events"`
}

// Response is the ingest's per-request response envelope (spec §4.4):
// accept/reject/duplicate counts, a 429 retry hint, and the schema_version
// this ingest currently supports — MaxSchemaVersion lets a client newer
// than the deployed ingest warn once (spec §8 "backend deploys before
// client releases" — this is the visibility for getting that order wrong).
// wire owns this struct so both sides of the boundary share ONE definition:
// internal/ingest's IngestResponse is a type alias of Response, never a
// hand-duplicated copy of the same JSON tags.
type Response struct {
	Accepted         int   `json:"accepted"`
	Rejected         int   `json:"rejected"`
	Duplicate        int   `json:"duplicate"`
	RetryAfterMs     int64 `json:"retry_after_ms"`
	MaxSchemaVersion int   `json:"max_schema_version"`
}
