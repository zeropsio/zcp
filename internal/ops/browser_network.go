package ops

import (
	"bytes"
	"encoding/json"
	"strings"
)

const browserRedactedHeader = "<redacted>"

// browserNetworkRequests reads the command the way agent-browser does, with
// global flags removed even when they precede the command.
func browserNetworkRequests(cmd []string) bool {
	own, _ := browserCommandArgs(cmd)
	return len(own) >= 2 && cmd[own[0]] == browserCmdNetwork && cmd[own[1]] == browserSubcmdRequests
}

// redactBrowserNetworkHeaders keeps a network result's requests and metadata
// while taking credential values out of its request and response headers.
func redactBrowserNetworkHeaders(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var data any
	if err := decoder.Decode(&data); err != nil {
		return nil
	}
	switch data.(type) {
	case map[string]any, []any:
		redactBrowserHeaderFields(data)
	default:
		return nil
	}
	redacted, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return redacted
}

func redactBrowserHeaderFields(data any) {
	switch value := data.(type) {
	case map[string]any:
		for name, child := range value {
			switch strings.ToLower(name) {
			case "headers", "requestheaders", "responseheaders":
				value[name] = redactBrowserHeaders(child)
			default:
				redactBrowserHeaderFields(child)
			}
		}
	case []any:
		for _, child := range value {
			redactBrowserHeaderFields(child)
		}
	}
}

// redactBrowserHeaders handles both a name-to-value map and a list of header
// entries. An opaque header block cannot be inspected, so it is withheld.
func redactBrowserHeaders(headers any) any {
	switch value := headers.(type) {
	case nil:
		return nil
	case map[string]any:
		for name := range value {
			if browserCredentialHeader(name) {
				value[name] = browserRedactedHeader
			}
		}
		return value
	case []any:
		for i, entry := range value {
			if fields, ok := entry.(map[string]any); ok && redactBrowserHeaderEntry(fields) {
				continue
			}
			if pair, ok := entry.([]any); ok && len(pair) == 2 {
				if name, ok := pair[0].(string); ok {
					if browserCredentialHeader(name) {
						pair[1] = browserRedactedHeader
					}
					continue
				}
			}
			value[i] = redactBrowserHeaders(entry)
		}
		return value
	default:
		return browserRedactedHeader
	}
}

// redactBrowserHeaderEntry recognizes a named entry in a header list.
func redactBrowserHeaderEntry(fields map[string]any) bool {
	nameKey, name, hasValue := "", "", false
	for key, value := range fields {
		switch strings.ToLower(key) {
		case "name":
			if s, ok := value.(string); ok {
				nameKey, name = key, s
			}
		case "key":
			if s, ok := value.(string); ok && !strings.EqualFold(nameKey, "name") {
				nameKey, name = key, s
			}
		case "value", "values":
			hasValue = true
		}
	}
	if nameKey == "" || (strings.EqualFold(nameKey, "key") && !hasValue) {
		return false
	}
	for key := range fields {
		if key != nameKey && (browserCredentialHeader(name) || browserCredentialHeader(key)) {
			fields[key] = browserRedactedHeader
		}
	}
	return true
}

func browserCredentialHeader(name string) bool {
	name = strings.ToLower(name)
	switch name {
	case "authorization", "cookie", "set-cookie", "proxy-authorization":
		return true
	}
	return strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "key")
}
