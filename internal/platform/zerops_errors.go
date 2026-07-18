package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/zeropsio/zerops-go/apiError"
)

// jsonUnmarshal is a package-level alias kept for symmetry — decodeAPIMetaJSON
// is the only consumer. Using the alias lets test code override it if a
// future hermetic test ever needs to simulate unmarshal failure without
// feeding malformed bytes (not currently required).
var jsonUnmarshal = json.Unmarshal

// mapSDKError converts SDK/API errors to ZCP platform errors.
func mapSDKError(err error, entityType string) error {
	if err == nil {
		return nil
	}

	var apiErr apiError.Error
	if errors.As(err, &apiErr) {
		return mapAPIError(apiErr, entityType)
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return withCause(NewPlatformError(ErrNetworkError, err.Error(), "Check network connectivity"), err)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return withCause(NewPlatformError(ErrNetworkError, err.Error(), "Check API host DNS"), err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return withCause(NewPlatformError(ErrAPITimeout, "API request timed out", "Retry the operation"), err)
	}
	if errors.Is(err, context.Canceled) {
		return withCause(NewPlatformError(ErrAPIError, "request canceled", ""), err)
	}

	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "no such host") {
		return withCause(NewPlatformError(ErrNetworkError, errStr, "Check API host and network"), err)
	}

	return withCause(NewPlatformError(ErrAPIError, errStr, ""), err)
}

// withCause attaches the underlying error so errors.Is/As keep working
// through the PlatformError wrap (e.g. a user-initiated context.Canceled
// stays distinguishable from a real platform failure by code consumers).
// Set only at the non-API mapping seam — API errors carry their own code.
func withCause(pe *PlatformError, cause error) *PlatformError {
	pe.Cause = cause
	return pe
}

// withAPICode attaches APICode + APIMeta on every apiError-derived branch.
// Centralizing the attachment keeps APIMeta from being silently dropped when
// a new HTTP-status branch is added without copying the meta assignment.
func withAPICode(pe *PlatformError, apiCode string, meta []APIMetaItem) *PlatformError {
	pe.APICode = apiCode
	pe.APIMeta = meta
	return pe
}

func mapAPIError(apiErr apiError.Error, entityType string) error {
	code := apiErr.GetHttpStatusCode()
	errCode := apiErr.GetErrorCode()
	msg := apiErr.GetMessage()
	meta := decodeAPIMeta(apiErr.GetMeta())

	switch code {
	case http.StatusUnauthorized:
		return withAPICode(NewPlatformError(ErrAuthTokenExpired, msg, "Check token validity"), errCode, meta)
	case http.StatusForbidden:
		return withAPICode(NewPlatformError(ErrPermissionDenied, msg, "Check token permissions"), errCode, meta)
	case http.StatusNotFound:
		switch entityType {
		case "process":
			return withAPICode(NewPlatformError(ErrProcessNotFound, msg, "Check process ID"), errCode, meta)
		default:
			return withAPICode(NewPlatformError(ErrServiceNotFound, msg, "Check service hostname"), errCode, meta)
		}
	case http.StatusTooManyRequests:
		return withAPICode(NewPlatformError(ErrAPIRateLimited, msg, "Wait and retry"), errCode, meta)
	}

	if code >= 500 {
		return withAPICode(withSubcode(NewPlatformError(ErrAPIError, msg, "Zerops API server error — retry later"), errCode), errCode, meta)
	}

	// Client error (4xx) — tell LLM to fix input. When the server sent
	// field-level detail in meta, expand it into an actionable suggestion
	// so the agent reads "Field X rejected with reason Y" instead of a
	// pointer at the structured block. apiMeta stays in the response for
	// programmatic consumers; suggestion becomes the human/LLM summary.
	// Pre-2026-05-06 the suggestion was a pointer ("see apiMeta...") and
	// agents needed an out-of-band atom (`develop-api-error-meta`) to
	// learn how to read the structured block. Expanding inline made the
	// atom redundant; the response is now self-evident at the moment of
	// failure.
	suggestion := "Check the request parameters"
	switch {
	case len(meta) > 0:
		suggestion = formatAPIMetaActionable(meta)
	case errCode != "":
		suggestion = fmt.Sprintf("API rejected the request (code: %s) — check the input parameters", errCode)
	}
	return withAPICode(withSubcode(NewPlatformError(ErrAPIError, msg, suggestion), errCode), errCode, meta)
}

// withSubcode carries the platform's own error-code class into
// PlatformError.Subcode for the ErrAPIError branches specifically (spec-telemetry.md
// §4.2 "API_ERROR → carry platform error-code class", telemetry-production-readiness
// plan S4) — errCode is ALREADY the single source of truth for "what kind of
// API error this is" (it also becomes APICode via withAPICode), so this is a
// verbatim carry, never a re-authored classification. A blank errCode (the
// API sent no error code) leaves Subcode empty — optional field, no
// sentinel needed. Deliberately NOT folded into withAPICode: the 401/403/404/429
// branches above already get their own distinct top-level Code and don't need
// a redundant subcode.
func withSubcode(pe *PlatformError, errCode string) *PlatformError {
	if errCode != "" {
		pe.Subcode = errCode
	}
	return pe
}

// formatAPIMetaActionable flattens APIMeta field-level detail into a
// one-line actionable summary. The output names the rejected fields
// and their reasons so the agent has the actionable answer in
// `suggestion` without parsing the apiMeta tree separately.
//
// Falls back to the generic "see apiMeta" pointer in two edge cases:
//   - meta items carry no metadata map (only code+error): no field
//     paths to report, nothing to expand.
//   - more than maxAPIMetaInlineFields rejected fields: summary would
//     be too long to be useful; agent reads apiMeta directly.
//
// Special-case: the `parameter` metadata key (used by
// projectImportMissingParameter) carries the missing field name as
// its value — output flips so the value becomes the field path with
// reason "missing".
//
// Determinism: metadata-key iteration is sorted so identical input
// produces identical output (used in error wire goldens, MCP response
// canonicalization).
func formatAPIMetaActionable(meta []APIMetaItem) string {
	pairs := flattenAPIMetaPairs(meta)
	if len(pairs) == 0 {
		return "The platform flagged specific fields — see apiMeta for each field's failure reason."
	}
	if len(pairs) > maxAPIMetaInlineFields {
		return fmt.Sprintf("The platform flagged %d fields — see apiMeta for each field's failure reason.", len(pairs))
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		reason := strings.Join(p.reasons, "; ")
		if reason == "" {
			parts = append(parts, fmt.Sprintf("'%s'", p.field))
		} else {
			parts = append(parts, fmt.Sprintf("'%s' (%s)", p.field, reason))
		}
	}
	if len(parts) == 1 {
		return fmt.Sprintf("Field %s rejected. Fix in YAML and retry.", parts[0])
	}
	return fmt.Sprintf("Rejected fields: %s. Fix in YAML and retry.", strings.Join(parts, ", "))
}

// maxAPIMetaInlineFields caps how many rejected fields land inline in
// the suggestion text. Above this, the suggestion summarizes the count
// and points at apiMeta — keeps the suggestion readable while preserving
// the full structured detail in the apiMeta field.
const maxAPIMetaInlineFields = 5

// apiMetaPair is one (field-path, reasons) tuple extracted from the
// apiMeta tree. Internal to formatAPIMetaActionable.
type apiMetaPair struct {
	field   string
	reasons []string
}

// flattenAPIMetaPairs walks apiMeta items and emits one pair per
// metadata key with its reasons. Sorted iteration order for
// deterministic output.
func flattenAPIMetaPairs(meta []APIMetaItem) []apiMetaPair {
	var out []apiMetaPair
	for _, item := range meta {
		keys := make([]string, 0, len(item.Metadata))
		for k := range item.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			values := item.Metadata[k]
			if k == "parameter" {
				// projectImportMissingParameter shape: the metadata
				// value IS the missing field name, key "parameter" is
				// the meta-property descriptor. Flip so the field path
				// is reported with reason "missing".
				for _, v := range values {
					if v == "" {
						continue
					}
					out = append(out, apiMetaPair{field: v, reasons: []string{"missing"}})
				}
				continue
			}
			out = append(out, apiMetaPair{field: k, reasons: values})
		}
	}
	return out
}

// decodeAPIMetaJSON is the JSON-bytes entrypoint used by per-service error
// mapping (zerops_search.go). The import endpoint's `ErrorObject.Meta` is
// `JsonRawMessage` rather than `any`; unmarshal first, then share
// the same typed decoder so the output shape is identical whether meta
// arrived as a top-level 4xx body or as a per-service-stack error.
func decodeAPIMetaJSON(raw []byte) []APIMetaItem {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	if err := jsonUnmarshal(raw, &v); err != nil {
		return nil
	}
	return decodeAPIMeta(v)
}

// decodeAPIMeta converts the SDK's untyped meta (`any`) into typed
// APIMetaItem slices. The server sends `meta: [{code, error, metadata}, ...]`
// where metadata is `map<string, []string>`. Unexpected shapes return nil —
// never panics, never drops a recognized item because a sibling is malformed.
func decodeAPIMeta(raw any) []APIMetaItem {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]APIMetaItem, 0, len(arr))
	for _, rawItem := range arr {
		m, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		item := APIMetaItem{
			Code:  asString(m["code"]),
			Error: asString(m["error"]),
		}
		if mdRaw, hasMD := m["metadata"]; hasMD {
			item.Metadata = asStringSliceMap(mdRaw)
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// asStringSliceMap converts `map<string, []any>` (JSON decode of
// `map<string, []string>`) into its typed form. Keys with non-slice values
// are skipped; an empty map returns nil to keep "no detail" consistent.
func asStringSliceMap(raw any) map[string][]string {
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string][]string, len(m))
	for _, k := range keys {
		v := m[k]
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		strs := make([]string, 0, len(arr))
		for _, a := range arr {
			strs = append(strs, asString(a))
		}
		out[k] = strs
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
