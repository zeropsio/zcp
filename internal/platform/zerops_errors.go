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
		return NewPlatformError(ErrNetworkError, err.Error(), "Check network connectivity")
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return NewPlatformError(ErrNetworkError, err.Error(), "Check API host DNS")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewPlatformError(ErrAPITimeout, "API request timed out", "Retry the operation")
	}
	if errors.Is(err, context.Canceled) {
		return NewPlatformError(ErrAPIError, "request canceled", "")
	}

	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "no such host") {
		return NewPlatformError(ErrNetworkError, errStr, "Check API host and network")
	}

	return NewPlatformError(ErrAPIError, errStr, "")
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
		return withAPICode(NewPlatformError(ErrAPIError, msg, "Zerops API server error — retry later"), errCode, meta)
	}

	// Client error (4xx) — tell LLM to fix input. When the server sent
	// field-level detail in meta, the suggestion points at apiMeta so the
	// LLM doesn't skip the structured block in favor of the generic line.
	suggestion := "Check the request parameters"
	switch {
	case len(meta) > 0:
		suggestion = "The platform flagged specific fields — see apiMeta for each field's failure reason."
	case errCode != "":
		suggestion = fmt.Sprintf("API rejected the request (code: %s) — check the input parameters", errCode)
	}
	return withAPICode(NewPlatformError(ErrAPIError, msg, suggestion), errCode, meta)
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
