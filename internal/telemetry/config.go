package telemetry

import (
	"fmt"
	"io"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// DefaultEndpoint is the production ingest endpoint (spec §4.1 — production
// domain TBD), overridable via ZCP_TELEMETRY_ENDPOINT for internal/test use.
const DefaultEndpoint = "https://telemetry.zerops.io/v1/events"

// Env var names. ZCP_TELEMETRY and DO_NOT_TRACK are frozen forever (spec B7).
const (
	EnvTelemetry  = "ZCP_TELEMETRY"
	EnvDoNotTrack = "DO_NOT_TRACK"
	EnvChannel    = "ZCP_TELEMETRY_CHANNEL"
	EnvEndpoint   = "ZCP_TELEMETRY_ENDPOINT"
	EnvDebug      = "ZCP_TELEMETRY_DEBUG"
	envCI         = "CI"
)

// disclosureNotice is printed exactly once per install, before the first
// event is ever emitted (spec §3.3, B5). Single paragraph: what's collected,
// what's never collected, where to read more, how to opt out.
const disclosureNotice = "ZCP collects anonymous usage events (tool names, durations, error codes — " +
	"never arguments, file paths, or identifiers) to help the team understand real usage and improve " +
	"the product. Details: https://docs.zerops.io/zcp/telemetry. Opt out any time with ZCP_TELEMETRY=0 " +
	"or \"zcp telemetry disable\"."

// Reason values narrate WHICH §3.1 precedence rule produced Config —
// consumed by `zcp telemetry status` (spec §3.5 "the precedence rule that
// produced it"). Local-only human text, never sent on the wire (the wire's
// enums are EventType/UsageChannel/etc, not this).
const (
	ReasonOptedOutEnvTelemetry = "opted out via ZCP_TELEMETRY"
	ReasonOptedOutDoNotTrack   = "opted out via DO_NOT_TRACK"
	ReasonDisabledInstallFile  = "disabled via install file (zcp telemetry enable to re-enable)"
	ReasonPreDisclosure        = "pre-disclosure: this run recorded consent, telemetry starts next run"
	ReasonEnabled              = "enabled"
	ReasonSessionIDError       = "disabled: could not generate a session id"
	ReasonInstallFileError     = "disabled: could not read the install file"
	ReasonDisclosureWriteError = "disabled: could not write the install file"
)

// Config is the resolved, immutable telemetry configuration for one process
// (spec §3.1 — resolved ONCE, then threaded as a value; call sites never
// re-read env).
type Config struct {
	// Enabled is the single field call sites need to check: false means
	// Emit is a no-op for this process — covers opt-out, disabled,
	// read/write error, AND pre-disclosure (see PreDisclosure to
	// distinguish the last case for introspection/status reporting).
	Enabled bool
	// PreDisclosure is true when THIS process is the one that stamped the
	// disclosure marker (spec §3.3). Enabled is always false alongside it —
	// the marker exists for the NEXT process, which will resolve enabled
	// (barring another opt-out rule).
	PreDisclosure bool

	Channel  string // wire.Channel* (spec §3.2)
	Endpoint string
	Debug    bool // spec §3.4

	// Reason narrates which §3.1 precedence rule produced Enabled (one of
	// the Reason* consts) — introspection only, consumed by `zcp telemetry
	// status` (spec §3.5).
	Reason string

	InstallID  string
	SessionID  string
	ZcpVersion string // coerced to semver shape or "dev" (spec §4.3)
	OS         string
	Arch       string
	RuntimeEnv string // wire.Runtime* — caller-supplied so this package never imports internal/runtime
}

// Resolve implements spec §3.1–§3.4 precedence exactly, once per process.
// Every input is explicit so tests never touch real env/files/stdlib
// runtime:
//   - getenv: env lookup function (os.Getenv in production).
//   - homeDir: base of ~/.zcp/telemetry/.
//   - zcpVersion: raw version string; coerced to semver-or-"dev" here.
//   - runtimeEnv: wire.RuntimeContainer/RuntimeLocal, resolved by the caller
//     (internal/runtime.Detect()) — kept out of this package by design.
//   - disclosureWriter: receives the disclosure notice text exactly when
//     this process stamps pre-disclosure (CLI mode: os.Stdout; MCP serve
//     mode: os.Stderr — the caller picks which, per spec §3.3).
func Resolve(getenv func(string) string, homeDir, zcpVersion, runtimeEnv string, disclosureWriter io.Writer) Config {
	cfg := Config{
		Channel:    resolveChannel(getenv),
		Endpoint:   resolveEndpoint(getenv),
		Debug:      isTruthy(getenv(EnvDebug)),
		ZcpVersion: coerceVersion(zcpVersion),
		OS:         goruntime.GOOS,
		Arch:       goruntime.GOARCH,
		RuntimeEnv: runtimeEnv,
	}

	// session_id is minted per OS process regardless of consent outcome
	// (spec §2) — cheap, and useful even for a disabled process's status
	// introspection.
	sessionID, err := newUUIDv4()
	if err != nil {
		cfg.Reason = ReasonSessionIDError
		return cfg // crypto/rand failure: never crash, stay disabled (spec §3.1 ethos)
	}
	cfg.SessionID = sessionID

	if isOptOutToken(getenv(EnvTelemetry)) {
		cfg.Reason = ReasonOptedOutEnvTelemetry
		return cfg // rule 1
	}
	if isTruthy(getenv(EnvDoNotTrack)) {
		cfg.Reason = ReasonOptedOutDoNotTrack
		return cfg // rule 2
	}

	internalChannel := IsInternalChannel(cfg.Channel)
	path := installFilePath(homeDir, internalChannel)

	f, exists, err := loadInstallFile(path)
	if err != nil {
		cfg.Reason = ReasonInstallFileError
		return cfg // read error → disabled, never crash (spec §3.1)
	}
	if exists {
		cfg.InstallID = f.InstallID
	}
	if exists && f.Disabled {
		cfg.Reason = ReasonDisabledInstallFile
		return cfg // rule 3
	}
	if !exists || f.DisclosedAt == "" {
		stamped, err := stampDisclosure(path, time.Now())
		if err != nil {
			cfg.Reason = ReasonDisclosureWriteError
			return cfg // write error → disabled, never crash (spec §3.1)
		}
		printDisclosure(disclosureWriter)
		cfg.InstallID = stamped.InstallID
		cfg.PreDisclosure = true
		cfg.Reason = ReasonPreDisclosure
		return cfg // rule 4
	}

	cfg.Enabled = true // rule 5
	cfg.Reason = ReasonEnabled
	return cfg
}

// IsInternalChannel reports whether channel uses the separate
// install-internal.json identity namespace (spec §2) — internal_dev/
// internal_eval only. Exported so callers outside this package (the `zcp
// telemetry` CLI subcommand) derive the same install-file selector Resolve
// uses, instead of re-deriving the predicate.
func IsInternalChannel(channel string) bool {
	return channel == wire.ChannelInternalDev || channel == wire.ChannelInternalEval
}

func resolveChannel(getenv func(string) string) string {
	v := strings.ToLower(strings.TrimSpace(getenv(EnvChannel)))
	switch v {
	case wire.ChannelExternal, wire.ChannelInternalDev, wire.ChannelInternalEval, wire.ChannelCI:
		return v
	case "":
		if isTruthy(getenv(envCI)) {
			return wire.ChannelCI
		}
		return wire.ChannelExternal
	default:
		return wire.ChannelExternal // invalid value → external (spec §3.2)
	}
}

func resolveEndpoint(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv(EnvEndpoint)); v != "" {
		return v
	}
	return DefaultEndpoint
}

var optOutTokens = map[string]struct{}{"0": {}, "false": {}, "off": {}, "no": {}}

// isOptOutToken reports whether v is one of ZCP_TELEMETRY's opt-out tokens
// (spec §3.1 rule 1), matched case-insensitively.
func isOptOutToken(v string) bool {
	_, ok := optOutTokens[strings.ToLower(strings.TrimSpace(v))]
	return ok
}

// isTruthy reports whether v is "1" or "true" (case-insensitive) — used for
// DO_NOT_TRACK (spec §3.1 rule 2), CI (spec §3.2), and
// ZCP_TELEMETRY_DEBUG (spec §3.4).
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// versionShapePattern mirrors wire's semver alternative of versionPattern
// (spec §4.3) so the client can decide when to coerce a non-semver
// zcp_version to "dev" BEFORE handing the event to wire.ValidateLite, which
// remains the authoritative shape gate on the wire — this is only the
// client-side fallback decision, not a second validator.
var versionShapePattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

func coerceVersion(v string) string {
	if versionShapePattern.MatchString(v) {
		return v
	}
	return "dev"
}

func printDisclosure(w io.Writer) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, disclosureNotice)
}

// PrintDisclosureNotice writes the disclosure notice text (spec §3.3).
// Exported so `zcp telemetry enable` (spec §3.5, the explicit consent path)
// shows the SAME text Resolve's automatic pre-disclosure stamp prints,
// instead of a second hand-authored copy.
func PrintDisclosureNotice(w io.Writer) {
	printDisclosure(w)
}
