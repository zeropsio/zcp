package wire_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// baseToolCallEvent returns a fully valid tool_call event matching the
// spec §4.2 example. Every rejection-class test case starts from a clone of
// this and mutates exactly one field, so a failure can only be attributed
// to that field.
func baseToolCallEvent() *wire.Event {
	return &wire.Event{
		SchemaVersion: wire.SchemaVersion,
		EventID:       "11111111-1111-4111-8111-111111111111:42",
		InstallID:     "11111111-1111-4111-8111-111111111111",
		SessionID:     "11111111-1111-4111-8111-111111111111",
		Seq:           42,
		EventType:     wire.EventToolCall,
		ClientTime:    "2026-07-02T10:00:00.123Z",
		ZcpVersion:    "v6.31.0",
		OS:            "darwin",
		Arch:          "arm64",
		RuntimeEnv:    wire.RuntimeLocal,
		UsageChannel:  wire.ChannelExternal,
		Tool:          "zerops_deploy",
		Action:        "deploy",
		DurationMs:    1842,
		Success:       false,
		ErrorCode:     "DIAGNOSIS_REQUIRED",
		WorkflowRoute: "adopt",
		WorkflowStep:  "discover",
		CloseMode:     "auto",
		DroppedCount:  0,
	}
}

// baseSessionStartEvent returns a valid session_start event: context fields
// filled, every type-specific field left at its zero value — proves the
// optional-shape fields (tool/command/action/route/step/close_mode/
// error_code) legitimately accept "".
func baseSessionStartEvent() *wire.Event {
	return &wire.Event{
		SchemaVersion: wire.SchemaVersion,
		EventID:       "22222222-2222-4222-8222-222222222222:1",
		InstallID:     "22222222-2222-4222-8222-222222222222",
		SessionID:     "22222222-2222-4222-8222-222222222222",
		Seq:           1,
		EventType:     wire.EventSessionStart,
		ClientTime:    "2026-07-02T10:00:00Z",
		ZcpVersion:    "dev",
		OS:            "linux",
		Arch:          "amd64",
		RuntimeEnv:    wire.RuntimeContainer,
		UsageChannel:  wire.ChannelCI,
	}
}

// baseSessionEndEvent is a valid session_end event carrying the one closed
// dims key/value pair v1 defines.
func baseSessionEndEvent() *wire.Event {
	e := baseSessionStartEvent()
	e.EventType = wire.EventSessionEnd
	e.Seq = 2
	e.EventID = e.SessionID + ":2"
	e.Dims = map[string]string{wire.DimShutdownReason: wire.ShutdownSignal}
	e.DurationMs = 4200
	return e
}

func TestValidate_ValidEvents_Pass(t *testing.T) {
	t.Parallel()

	cases := map[string]*wire.Event{
		"tool_call":     baseToolCallEvent(),
		"session_start": baseSessionStartEvent(),
		"session_end":   baseSessionEndEvent(),
	}

	for name, ev := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := wire.ValidateLite(ev); err != nil {
				t.Fatalf("ValidateLite: unexpected error: %v", err)
			}
			if _, err := wire.ValidateStrict(ev); err != nil {
				t.Fatalf("ValidateStrict: unexpected error: %v", err)
			}
		})
	}
}

// TestValidate_RejectionClasses walks every rejection class from spec §4.3
// and, per the plan's explicit ask, encodes the lite-vs-strict asymmetry:
// ValidateLite only enforces shape patterns + dims/event size limits (it
// does NOT know about closed enums, required-field presence, UUID shape, or
// event_id consistency) — ValidateStrict adds all of those on top.
func TestValidate_RejectionClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// mutate corrupts exactly one field of an otherwise-valid event.
		mutate func(e *wire.Event)
		// wantLiteErr / wantStrictErr: nil means the corresponding
		// validator must accept the mutated event.
		wantLiteErr   error
		wantStrictErr error
	}{
		// --- caught only by ValidateStrict (closed structural enum / identity) ---
		{
			name:          "unknown_event_type",
			mutate:        func(e *wire.Event) { e.EventType = "bogus_type" },
			wantStrictErr: wire.ErrEnum,
		},
		{
			// schema_version OLDER than supported: no such version has ever
			// shipped (SchemaVersion has always been 1), so this is
			// structural garbage, not forward-compat (spec §4.3).
			name:          "schema_version_older_than_supported",
			mutate:        func(e *wire.Event) { e.SchemaVersion = 0 },
			wantStrictErr: wire.ErrEnum,
		},

		// --- tolerated by ValidateStrict (spec §4.3 tolerant mode, S2 G2):
		// accepted, not rejected — the dedicated Test* functions below
		// assert the StrictOutcome each one produces. ---
		{
			name:          "unknown_usage_channel_tolerated",
			mutate:        func(e *wire.Event) { e.UsageChannel = "bogus" },
			wantStrictErr: nil,
		},
		{
			name:          "unknown_runtime_env_tolerated",
			mutate:        func(e *wire.Event) { e.RuntimeEnv = "bogus" },
			wantStrictErr: nil,
		},
		{
			name:          "newer_schema_version_tolerated",
			mutate:        func(e *wire.Event) { e.SchemaVersion = 2 },
			wantStrictErr: nil,
		},
		{
			name:          "unknown_dim_key_tolerated",
			mutate:        func(e *wire.Event) { e.Dims = map[string]string{"unknown_key": "x"} },
			wantStrictErr: nil,
		},
		{
			name:          "unknown_dim_value_tolerated",
			mutate:        func(e *wire.Event) { e.Dims = map[string]string{wire.DimShutdownReason: "bogus"} },
			wantStrictErr: nil,
		},
		{
			name:          "bad_install_id_uuid",
			mutate:        func(e *wire.Event) { e.InstallID = "not-a-uuid" },
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "bad_session_id_uuid",
			mutate:        func(e *wire.Event) { e.SessionID = "not-a-uuid" },
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "bad_client_time_shape",
			mutate:        func(e *wire.Event) { e.ClientTime = "not-a-timestamp" },
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "seq_event_id_mismatch",
			mutate:        func(e *wire.Event) { e.Seq = 43 },
			wantStrictErr: wire.ErrConsistency,
		},
		{
			name:          "missing_event_id",
			mutate:        func(e *wire.Event) { e.EventID = "" },
			wantStrictErr: wire.ErrRequired,
		},
		{
			name:          "missing_install_id",
			mutate:        func(e *wire.Event) { e.InstallID = "" },
			wantStrictErr: wire.ErrRequired,
		},
		{
			name:          "missing_session_id",
			mutate:        func(e *wire.Event) { e.SessionID = "" },
			wantStrictErr: wire.ErrRequired,
		},
		{
			name:          "missing_client_time",
			mutate:        func(e *wire.Event) { e.ClientTime = "" },
			wantStrictErr: wire.ErrRequired,
		},
		{
			name:          "zero_seq",
			mutate:        func(e *wire.Event) { e.Seq = 0 },
			wantStrictErr: wire.ErrRequired,
		},

		// --- caught by both (shape/limits owned by wire) ---
		{
			name: "over_length_dim_value",
			mutate: func(e *wire.Event) {
				e.Dims = map[string]string{wire.DimShutdownReason: strings.Repeat("a", wire.MaxDimValueLen+1)}
			},
			wantLiteErr:   wire.ErrLimit,
			wantStrictErr: wire.ErrLimit,
		},
		{
			name: "over_length_dim_key",
			mutate: func(e *wire.Event) {
				e.Dims = map[string]string{strings.Repeat("a", wire.MaxDimKeyLen+1): "x"}
			},
			wantLiteErr:   wire.ErrLimit,
			wantStrictErr: wire.ErrLimit,
		},
		{
			name: "too_many_dims",
			mutate: func(e *wire.Event) {
				dims := make(map[string]string, wire.MaxDims+1)
				for i := range wire.MaxDims + 1 {
					dims[strconv.Itoa(i)] = "x"
				}
				e.Dims = dims
			},
			wantLiteErr:   wire.ErrLimit,
			wantStrictErr: wire.ErrLimit,
		},
		{
			name:          "bad_error_code_shape",
			mutate:        func(e *wire.Event) { e.ErrorCode = "lowercase_bad" },
			wantLiteErr:   wire.ErrShape,
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "bad_error_subcode_shape",
			mutate:        func(e *wire.Event) { e.ErrorSubcode = "bad subcode!" },
			wantLiteErr:   wire.ErrShape,
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "error_subcode_uppercase_catalog_value_accepted",
			mutate:        func(e *wire.Event) { e.ErrorSubcode = "AMBIGUOUS_SCOPE" },
			wantLiteErr:   nil,
			wantStrictErr: nil,
		},
		{
			name:          "error_subcode_platform_camelcase_value_accepted",
			mutate:        func(e *wire.Event) { e.ErrorSubcode = "badGateway" },
			wantLiteErr:   nil,
			wantStrictErr: nil,
		},
		{
			name:          "bad_os_shape",
			mutate:        func(e *wire.Event) { e.OS = "" },
			wantLiteErr:   wire.ErrShape,
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "bad_arch_shape",
			mutate:        func(e *wire.Event) { e.Arch = "?!" },
			wantLiteErr:   wire.ErrShape,
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "bad_zcp_version_shape",
			mutate:        func(e *wire.Event) { e.ZcpVersion = "not-a-version!" },
			wantLiteErr:   wire.ErrShape,
			wantStrictErr: wire.ErrShape,
		},
		{
			name:          "zcp_version_dev_literal_accepted",
			mutate:        func(e *wire.Event) { e.ZcpVersion = "dev" },
			wantLiteErr:   nil,
			wantStrictErr: nil,
		},
		{
			name:          "bad_tool_shape",
			mutate:        func(e *wire.Event) { e.Tool = "Bad Tool Name!" },
			wantLiteErr:   wire.ErrShape,
			wantStrictErr: wire.ErrShape,
		},
		{
			name: "oversized_event",
			mutate: func(e *wire.Event) {
				// Passes the (deliberately length-unbounded) semver
				// build-metadata shape but blows the 4 KiB encoded cap —
				// the one field wire does not itself length-cap, so it's
				// the only way to reach the size limit without first
				// tripping a per-field shape/length check.
				e.ZcpVersion = "v1.0.0-" + strings.Repeat("x", wire.MaxEventBytes)
			},
			wantLiteErr:   wire.ErrLimit,
			wantStrictErr: wire.ErrLimit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Two independent copies: ValidateStrict MUTATES its argument
			// (tolerant sanitization, spec §4.3), so sharing one event
			// between the two validator calls would let ValidateStrict's
			// mutation leak into what ValidateLite sees.
			eLite := baseToolCallEvent()
			tc.mutate(eLite)
			assertErrClass(t, "ValidateLite", wire.ValidateLite(eLite), tc.wantLiteErr)

			eStrict := baseToolCallEvent()
			tc.mutate(eStrict)
			_, strictErr := wire.ValidateStrict(eStrict)
			assertErrClass(t, "ValidateStrict", strictErr, tc.wantStrictErr)
		})
	}
}

func assertErrClass(t *testing.T, label string, got, want error) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("%s: unexpected error: %v", label, got)
		}
		return
	}
	if got == nil {
		t.Fatalf("%s: expected error wrapping %v, got nil", label, want)
	}
	if !errors.Is(got, want) {
		t.Fatalf("%s: error %v does not wrap %v", label, got, want)
	}
}

func TestValidateLite_NilEvent_ReturnsRequiredError(t *testing.T) {
	t.Parallel()
	if err := wire.ValidateLite(nil); !errors.Is(err, wire.ErrRequired) {
		t.Fatalf("ValidateLite(nil): got %v, want ErrRequired", err)
	}
}

func TestValidateStrict_NilEvent_ReturnsRequiredError(t *testing.T) {
	t.Parallel()
	if _, err := wire.ValidateStrict(nil); !errors.Is(err, wire.ErrRequired) {
		t.Fatalf("ValidateStrict(nil): got %v, want ErrRequired", err)
	}
}

// --- S2 G2: tolerant mode outcome details ----------------------------------

func TestValidateStrict_UnknownDimKey_DroppedAndCounted(t *testing.T) {
	t.Parallel()
	e := baseSessionEndEvent() // carries the one legitimate dims entry (shutdown_reason)
	e.Dims["unknown_key"] = "x"

	outcome, err := wire.ValidateStrict(e)
	if err != nil {
		t.Fatalf("ValidateStrict: unexpected error: %v", err)
	}
	if outcome.DimsDropped != 1 {
		t.Errorf("DimsDropped = %d, want 1", outcome.DimsDropped)
	}
	if _, ok := e.Dims["unknown_key"]; ok {
		t.Errorf("e.Dims still has the unrecognized key after ValidateStrict: %+v", e.Dims)
	}
	if got := e.Dims[wire.DimShutdownReason]; got != wire.ShutdownSignal {
		t.Errorf("legitimate dim was disturbed: shutdown_reason = %q, want %q", got, wire.ShutdownSignal)
	}
}

func TestValidateStrict_UnknownDimValueForKnownKey_DroppedAndCounted(t *testing.T) {
	t.Parallel()
	e := baseSessionEndEvent()
	e.Dims[wire.DimShutdownReason] = "bogus"

	outcome, err := wire.ValidateStrict(e)
	if err != nil {
		t.Fatalf("ValidateStrict: unexpected error: %v", err)
	}
	if outcome.DimsDropped != 1 {
		t.Errorf("DimsDropped = %d, want 1", outcome.DimsDropped)
	}
	if _, ok := e.Dims[wire.DimShutdownReason]; ok {
		t.Errorf("e.Dims still has the entry with the unrecognized value: %+v", e.Dims)
	}
}

func TestValidateStrict_CoercesUnknownUsageChannel(t *testing.T) {
	t.Parallel()
	e := baseToolCallEvent()
	e.UsageChannel = "internal_qa" // hypothetical newer-client channel this ingest doesn't know yet

	outcome, err := wire.ValidateStrict(e)
	if err != nil {
		t.Fatalf("ValidateStrict: unexpected error: %v", err)
	}
	if !outcome.ChannelCoerced {
		t.Error("ChannelCoerced = false, want true")
	}
	if e.UsageChannel != wire.UnknownChannel {
		t.Errorf("e.UsageChannel = %q, want %q", e.UsageChannel, wire.UnknownChannel)
	}
}

func TestValidateStrict_CoercesUnknownRuntimeEnv(t *testing.T) {
	t.Parallel()
	e := baseToolCallEvent()
	e.RuntimeEnv = "serverless" // hypothetical newer-client runtime_env

	outcome, err := wire.ValidateStrict(e)
	if err != nil {
		t.Fatalf("ValidateStrict: unexpected error: %v", err)
	}
	if !outcome.RuntimeCoerced {
		t.Error("RuntimeCoerced = false, want true")
	}
	if e.RuntimeEnv != wire.UnknownRuntimeEnv {
		t.Errorf("e.RuntimeEnv = %q, want %q", e.RuntimeEnv, wire.UnknownRuntimeEnv)
	}
}

func TestValidateStrict_KnownValues_NeverCoerced(t *testing.T) {
	t.Parallel()
	e := baseToolCallEvent()

	outcome, err := wire.ValidateStrict(e)
	if err != nil {
		t.Fatalf("ValidateStrict: unexpected error: %v", err)
	}
	if outcome.ChannelCoerced || outcome.RuntimeCoerced || outcome.DimsDropped != 0 || outcome.ForwardCompat {
		t.Errorf("outcome = %+v, want an entirely plain strict pass", outcome)
	}
}

func TestValidateStrict_NewerSchemaVersion_AcceptedWhenCoreValid(t *testing.T) {
	t.Parallel()
	e := baseToolCallEvent()
	e.SchemaVersion = 2

	outcome, err := wire.ValidateStrict(e)
	if err != nil {
		t.Fatalf("ValidateStrict: unexpected error: %v", err)
	}
	if !outcome.ForwardCompat {
		t.Error("ForwardCompat = false, want true")
	}
}

func TestValidateStrict_NewerSchemaVersion_RejectedWhenCoreInvalid(t *testing.T) {
	t.Parallel()
	e := baseToolCallEvent()
	e.SchemaVersion = 2
	e.EventType = "bogus_type" // event_type is structural — stays strict even under forward-compat

	if _, err := wire.ValidateStrict(e); !errors.Is(err, wire.ErrEnum) {
		t.Fatalf("ValidateStrict: got %v, want ErrEnum (event_type must still validate under forward-compat)", err)
	}
}

func TestValidateStrict_NewerSchemaVersion_ToleratesNonCoreShapeFailures(t *testing.T) {
	t.Parallel()
	e := baseToolCallEvent()
	e.SchemaVersion = 2
	e.Tool = "Not A Valid Tool Shape!" // would be ErrShape at schema_version 1

	outcome, err := wire.ValidateStrict(e)
	if err != nil {
		t.Fatalf("ValidateStrict: unexpected error under forward-compat: %v", err)
	}
	if !outcome.ForwardCompat {
		t.Error("ForwardCompat = false, want true")
	}
}

func TestValidateStrict_NewerSchemaVersion_StillEnforcesResourceCaps(t *testing.T) {
	t.Parallel()
	e := baseToolCallEvent()
	e.SchemaVersion = 2
	dims := make(map[string]string, wire.MaxDims+1)
	for i := range wire.MaxDims + 1 {
		dims[strconv.Itoa(i)] = "x"
	}
	e.Dims = dims

	if _, err := wire.ValidateStrict(e); !errors.Is(err, wire.ErrLimit) {
		t.Fatalf("ValidateStrict: got %v, want ErrLimit (dims cap enforced regardless of schema_version)", err)
	}
}
