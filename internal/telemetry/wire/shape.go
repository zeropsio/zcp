package wire

// UnknownIdentifier is the sentinel S3's MCP middleware extraction site
// (internal/server) substitutes for an identifier-family field
// (tool/action/workflow_route/workflow_step/close_mode) whose peeked value
// fails ValidIdentifierShape (spec §5.3 "Values failing shape checks →
// unknown"). It deliberately satisfies identifierPattern itself, so
// substituting it never turns a shape failure into a second, event-wide
// rejection at ValidateLite/ValidateStrict.
const UnknownIdentifier = "unknown"

// UnknownErrorCode is the sentinel substituted for error_code when it is
// absent (no ErrorWire content, or the JSON has no "code") or fails
// ValidErrorCodeShape (spec §5.3 "absent → UNKNOWN"). It satisfies
// errorCodePattern itself for the same reason UnknownIdentifier satisfies
// identifierPattern.
const UnknownErrorCode = "UNKNOWN"

// ValidIdentifierShape reports whether s matches the shape wire enforces for
// the tool-registry/topology-owned identifier fields (tool, command, action,
// workflow_route, workflow_step, close_mode — spec §4.3): lowercase
// snake_case, max 64 chars. Exported so callers outside this package (the S3
// middleware extraction site) can shape-check a single peeked value against
// the SAME pattern ValidateLite/ValidateStrict use, instead of re-authoring
// the regex at the call site (CLAUDE.md single-owner discipline). Unlike
// ValidateLite's checkOptionalPattern, an empty string is NOT valid here —
// callers that want to treat "" as "field absent, leave empty" check that
// case themselves before calling.
func ValidIdentifierShape(s string) bool {
	return identifierPattern.MatchString(s)
}

// ValidErrorCodeShape reports whether s matches error_code's wire shape
// (`^[A-Z][A-Z0-9_]{0,63}$`, owned by internal/platform/errors.go — spec
// §4.3). Exported for the same single-owner reason as ValidIdentifierShape.
func ValidErrorCodeShape(s string) bool {
	return errorCodePattern.MatchString(s)
}

// ValidSubcodeShape reports whether s matches error_subcode's wire shape
// (spec §4.2/§4.3): owned partly by internal/platform/errors.go's Subcode*
// catalog (uppercase) and partly by the Zerops platform's own error-code
// vocabulary (lowerCamelCase, carried verbatim for API_ERROR). Exported so
// the S3/S4 middleware extraction site can shape-check a peeked subcode
// against the SAME pattern ValidateLite uses, instead of leaving a
// shape-invalid value to reach wire.ValidateLite and reject the whole event
// (spec §5.3 "values failing shape checks → unknown" — error_subcode is
// optional, so callers here leave it empty rather than substituting a
// sentinel).
func ValidSubcodeShape(s string) bool {
	return subcodePattern.MatchString(s)
}
