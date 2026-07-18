package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Sentinel error classes. Every validation failure wraps exactly one of
// these so callers (and tests) can distinguish rejection classes with
// errors.Is without parsing message text.
var (
	// ErrShape marks a field that fails its pattern/length constraint —
	// fields owned elsewhere (tool registry, topology, platform errors,
	// GOOS/GOARCH, semver): wire enforces shape only, never the vocabulary
	// (spec §4.3 anti-drift rule).
	ErrShape = errors.New("wire: value fails shape validation")
	// ErrLimit marks a field/collection that exceeds a wire-owned size or
	// count limit (spec §4.3 Limits).
	ErrLimit = errors.New("wire: value exceeds limit")
	// ErrEnum marks a value outside a wire-owned closed enum (spec §4.3).
	ErrEnum = errors.New("wire: value not in closed enum")
	// ErrRequired marks a required field that is empty/zero.
	ErrRequired = errors.New("wire: required field is empty")
	// ErrConsistency marks a cross-field consistency failure
	// (event_id != session_id:seq).
	ErrConsistency = errors.New("wire: field consistency check failed")
)

// Shape patterns. Precompiled package-level (immutable, allowed under
// CLAUDE.md's no-global-mutable-state rule).
var (
	// identifierPattern bounds the tool-registry/topology-owned fields
	// (tool, command, action, workflow_route, workflow_step, close_mode):
	// lowercase snake_case, max 64 chars. wire checks shape only — the
	// actual vocabulary is owned by the tool registry / topology, so a
	// newer client's new tool/route name must never be enum-rejected here.
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	// errorCodePattern mirrors internal/platform/errors.go's error code
	// shape exactly (spec §4.3): wire does not own the code vocabulary.
	errorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	// subcodePattern bounds error_subcode (spec §4.2/§4.3): owned by
	// internal/platform/errors.go's Subcode* catalog (uppercase snake, e.g.
	// AMBIGUOUS_SCOPE) for the ZCP-authored conflation splits, AND by the
	// Zerops platform's own error-code vocabulary (lowerCamelCase, e.g.
	// badGateway) for the API_ERROR "carry platform error-code class" case
	// (spec §4.2) — wire owns shape only, not either vocabulary, so the
	// pattern accepts both cases rather than picking one.
	subcodePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	// osPattern/archPattern bound GOOS/GOARCH shapes. archPattern allows a
	// leading digit (GOARCH "386").
	osPattern   = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)
	archPattern = regexp.MustCompile(`^[a-z0-9]{2,16}$`)
	// versionPattern accepts a semver (optional leading "v") or the
	// literal fallback token "dev". The client (S2) decides WHEN to
	// coerce a non-semver version string to "dev" — wire only defines
	// which final shapes are acceptable on the wire.
	versionPattern = regexp.MustCompile(`^(v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?|dev)$`)
	// uuidPattern is a generic 8-4-4-4-12 hex UUID shape (not v4-specific).
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Closed enums (spec §4.3).
var (
	eventTypes = map[string]struct{}{
		EventToolCall:       {},
		EventCLICommand:     {},
		EventSessionStart:   {},
		EventSessionEnd:     {},
		EventClientThrottle: {},
	}
	// usageChannels/runtimeEnvs include their Unknown* sentinel so a value
	// ValidateStrict already coerced (spec §4.3 tolerant mode) validates
	// cleanly on any later re-check (e.g. a retried row).
	usageChannels = map[string]struct{}{
		ChannelExternal:     {},
		ChannelInternalDev:  {},
		ChannelInternalEval: {},
		ChannelCI:           {},
		UnknownChannel:      {},
	}
	runtimeEnvs = map[string]struct{}{
		RuntimeContainer:  {},
		RuntimeLocal:      {},
		UnknownRuntimeEnv: {},
	}
	// dimValueSets is the closed value set per dims key, where enumerable
	// (spec §4.3). The map's keys are also the complete allowed dims-key
	// set — an unrecognized key is rejected regardless of its value.
	dimValueSets = map[string]map[string]struct{}{
		DimShutdownReason: {
			ShutdownSignal:           {},
			ShutdownClientDisconnect: {},
			ShutdownStdinClosed:      {},
			ShutdownBrokenPipe:       {},
			ShutdownError:            {},
		},
	}
)

// ValidateLite is the emit-side check (internal/telemetry, hot path): shape
// + length patterns for the fields wire does not own the vocabulary of,
// plus dims/event size limits. It deliberately does NOT check closed
// enums, required-field presence, UUID shape, or event_id consistency —
// those are ValidateStrict's job at ingest. Lite exists to catch a
// malformed event before spending network/spool bytes on it; it is not the
// authority (spec §4.3).
func ValidateLite(e *Event) error {
	if e == nil {
		return fmt.Errorf("event is nil: %w", ErrRequired)
	}
	for _, f := range []struct {
		name  string
		value string
	}{
		{"tool", e.Tool},
		{"command", e.Command},
		{"action", e.Action},
		{"workflow_route", e.WorkflowRoute},
		{"workflow_step", e.WorkflowStep},
		{"close_mode", e.CloseMode},
	} {
		if err := checkOptionalPattern(f.name, f.value, identifierPattern); err != nil {
			return err
		}
	}
	if err := checkOptionalPattern("error_code", e.ErrorCode, errorCodePattern); err != nil {
		return err
	}
	if err := checkOptionalPattern("error_subcode", e.ErrorSubcode, subcodePattern); err != nil {
		return err
	}
	// os/arch/zcp_version are always-present context fields (spec §4.2
	// table) — checked required-shape, not optional-shape.
	if err := checkPattern("os", e.OS, osPattern); err != nil {
		return err
	}
	if err := checkPattern("arch", e.Arch, archPattern); err != nil {
		return err
	}
	if err := checkPattern("zcp_version", e.ZcpVersion, versionPattern); err != nil {
		return err
	}
	if err := checkDimsCaps(e.Dims); err != nil {
		return err
	}
	if err := checkEventSize(e); err != nil {
		return err
	}
	return nil
}

// StrictOutcome reports what ValidateStrict tolerated in order to accept an
// event (spec §4.3 tolerant mode) — every field is zero on a plain strict
// pass. ValidateStrict MUTATES the event in place for the two coercion
// cases (dims dropped, enum coerced to "unknown"): the caller (the ingest
// handler) inserts the SANITIZED event, never the raw wire values, so
// storage/dashboards never see a value outside the enum they expect.
// Callers fold these into running counters (dims_dropped_total,
// forward_compat_accepted_total) surfaced via GET /statsz.
type StrictOutcome struct {
	// DimsDropped counts dims entries removed because ingest didn't
	// recognize the key OR the known key's value (spec §4.3).
	DimsDropped int
	// ChannelCoerced/RuntimeCoerced report whether usage_channel/
	// runtime_env held a value outside ingest's current enum and was
	// coerced to UnknownChannel/UnknownRuntimeEnv (spec §4.3).
	ChannelCoerced bool
	RuntimeCoerced bool
	// ForwardCompat is true when e.SchemaVersion is NEWER than SchemaVersion
	// and was accepted on the version-1-core-only path (spec §4.3/§8).
	ForwardCompat bool
}

// ValidateStrict is the ingest-side check (internal/ingest): the tolerant
// mode contract (spec §4.3, R2 "dozens of client versions"):
//
//   - Hard resource caps (event size, dims count/length) are enforced
//     UNCONDITIONALLY — they protect the pipeline from abuse/bugs, not from
//     a vocabulary mismatch, so they apply regardless of schema_version.
//   - The version-1 "core" — event_id/install_id/session_id (required, UUID
//     shape), client_time (required, RFC3339), seq (nonzero), event_type
//     (closed enum — the one structural enum that STAYS strict always,
//     because storage/dashboards branch on it), and event_id ==
//     session_id:seq consistency — is always required and never tolerated.
//   - Dims: an unrecognized key OR a known key's unrecognized value is
//     dropped from the event (not rejected), counted in
//     StrictOutcome.DimsDropped.
//   - usage_channel/runtime_env: an unrecognized value is coerced to
//     Unknown{Channel,RuntimeEnv} (not rejected), flagged in
//     StrictOutcome.{Channel,Runtime}Coerced.
//   - schema_version NEWER than SchemaVersion: accepted once the core above
//     validates — every other field (tool/action/os/arch/zcp_version/etc)
//     is accepted as-is, unvalidated, because ingest cannot know what a
//     schema it has never seen means for those fields. A JSON field this
//     Event struct doesn't know about is already dropped for free by the
//     struct decode before ValidateStrict ever runs — nothing to
//     additionally "stash". StrictOutcome.ForwardCompat is set.
//   - schema_version OLDER than SchemaVersion (no such version has ever
//     shipped) is structural garbage: rejected.
//   - schema_version == SchemaVersion: full ValidateLite shape checks apply
//     on top of the core + tolerant sanitization above (unchanged
//     strictness for the common case).
func ValidateStrict(e *Event) (StrictOutcome, error) {
	var out StrictOutcome
	if e == nil {
		return out, fmt.Errorf("event is nil: %w", ErrRequired)
	}

	// Hard resource caps: always enforced, on the RAW incoming event,
	// before any tolerant sanitization below.
	if err := checkEventSize(e); err != nil {
		return out, err
	}
	if err := checkDimsCaps(e.Dims); err != nil {
		return out, err
	}

	if err := checkCore(e); err != nil {
		return out, err
	}

	// Tolerant sanitization: unconditional, regardless of schema_version —
	// never rejects the event, only mutates it.
	out.DimsDropped = sanitizeDims(e)
	out.ChannelCoerced = sanitizeEnum(&e.UsageChannel, usageChannels, UnknownChannel)
	out.RuntimeCoerced = sanitizeEnum(&e.RuntimeEnv, runtimeEnvs, UnknownRuntimeEnv)

	switch {
	case e.SchemaVersion > SchemaVersion:
		out.ForwardCompat = true
		return out, nil
	case e.SchemaVersion == SchemaVersion:
		if err := ValidateLite(e); err != nil {
			return out, err
		}
		return out, nil
	default:
		return out, fmt.Errorf("schema_version %d: %w", e.SchemaVersion, ErrEnum)
	}
}

// checkCore validates the version-1 core fields ValidateStrict requires for
// EVERY schema_version, including a newer one ingest otherwise doesn't
// understand (spec §4.3 forward-compat: "accept when the version-1 core
// parses").
func checkCore(e *Event) error {
	for _, f := range []struct {
		name  string
		value string
	}{
		{"event_id", e.EventID},
		{"install_id", e.InstallID},
		{"session_id", e.SessionID},
		{"client_time", e.ClientTime},
	} {
		if err := checkRequired(f.name, f.value); err != nil {
			return err
		}
	}
	if e.Seq == 0 {
		return fmt.Errorf("seq is zero: %w", ErrRequired)
	}
	if err := checkPattern("install_id", e.InstallID, uuidPattern); err != nil {
		return err
	}
	if err := checkPattern("session_id", e.SessionID, uuidPattern); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, e.ClientTime); err != nil {
		return fmt.Errorf("client_time %q: %w", e.ClientTime, ErrShape)
	}
	if err := checkEnum("event_type", e.EventType, eventTypes); err != nil {
		return err
	}
	wantEventID := e.SessionID + ":" + strconv.FormatUint(e.Seq, 10)
	if e.EventID != wantEventID {
		return fmt.Errorf("event_id %q != %q: %w", e.EventID, wantEventID, ErrConsistency)
	}
	return nil
}

// sanitizeDims drops every dims entry ingest doesn't recognize — an unknown
// key, or a known key's unrecognized value (spec §4.3 tolerant mode) — in
// place, and returns how many were dropped. The event itself is always
// kept; dims is the schema's designed-for-growth surface (spec §8 "new
// dims = wire allowlist edit"), so an entry this ingest hasn't caught up to
// carries no information worth rejecting the whole event over.
func sanitizeDims(e *Event) int {
	if len(e.Dims) == 0 {
		return 0
	}
	dropped := 0
	for k, v := range e.Dims {
		values, ok := dimValueSets[k]
		if !ok {
			delete(e.Dims, k)
			dropped++
			continue
		}
		if _, ok := values[v]; !ok {
			delete(e.Dims, k)
			dropped++
		}
	}
	return dropped
}

// sanitizeEnum coerces *field to unknownValue in place when it falls
// outside set, and reports whether it did (spec §4.3: usage_channel/
// runtime_env tolerate an unrecognized value from a newer client instead of
// rejecting the event — unlike event_type, which stays strict).
func sanitizeEnum(field *string, set map[string]struct{}, unknownValue string) bool {
	if _, ok := set[*field]; ok {
		return false
	}
	*field = unknownValue
	return true
}

func checkOptionalPattern(field, value string, pattern *regexp.Regexp) error {
	if value == "" {
		return nil
	}
	return checkPattern(field, value, pattern)
}

func checkPattern(field, value string, pattern *regexp.Regexp) error {
	if !pattern.MatchString(value) {
		return fmt.Errorf("%s %q: %w", field, value, ErrShape)
	}
	return nil
}

func checkRequired(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s: %w", field, ErrRequired)
	}
	return nil
}

func checkEnum(field, value string, set map[string]struct{}) error {
	if _, ok := set[value]; !ok {
		return fmt.Errorf("%s %q: %w", field, value, ErrEnum)
	}
	return nil
}

func checkDimsCaps(dims map[string]string) error {
	if len(dims) > MaxDims {
		return fmt.Errorf("dims has %d keys: %w", len(dims), ErrLimit)
	}
	for k, v := range dims {
		if len(k) > MaxDimKeyLen {
			return fmt.Errorf("dims key %q length %d: %w", k, len(k), ErrLimit)
		}
		if len(v) > MaxDimValueLen {
			return fmt.Errorf("dims[%s] value length %d: %w", k, len(v), ErrLimit)
		}
	}
	return nil
}

func checkEventSize(e *Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if len(b) > MaxEventBytes {
		return fmt.Errorf("event encoded size %d bytes: %w", len(b), ErrLimit)
	}
	return nil
}
