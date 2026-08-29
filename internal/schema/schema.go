// Package schema provides access to live Zerops YAML schemas (zerops.yaml + import.yaml).
// Schemas are fetched from the public API and cached with a TTL.
// Extracted enums are used for validation; formatted output is used for LLM knowledge injection.
package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CanonicalAPIHost is the public Zerops API base. It is BOTH the empty-host
// default (so a user with no ZCP_API_HOST is unchanged) AND the pinned host for
// dev tooling: `schema sync`/`check` write SHARED committed artifacts, so they
// must target one canonical host regardless of whoever's ZCP_API_HOST ran them.
const CanonicalAPIHost = "api.app-prg1.zerops.io"

// Public schema paths — no auth required.
const (
	zeropsYmlSchemaPath = "/api/rest/public/settings/zerops-yml-json-schema.json"
	importYmlSchemaPath = "/api/rest/public/settings/import-project-yml-json-schema.json"
)

// URLs derives the two public schema URLs from a resolved API host so the
// schema describing what an instance accepts is fetched from the SAME host the
// user operates against (ZCP_API_HOST), not a hardcoded region. Normalization
// MIRRORS platform.resolveEndpoint (NOT strip-and-force-https — forcing https
// while the platform client honors http:// would recreate the host mismatch
// this removes): empty → CanonicalAPIHost; missing scheme → prepend https://;
// an explicit scheme + port are preserved; a trailing slash is trimmed. The
// schema package must not import platform (layering), so the few lines are
// replicated here, pinned by a shared test matrix.
//
// URLs("") returns the two canonical prg1 URLs byte-for-byte (pinned), so
// default users see identical behavior to the removed const strings.
func URLs(apiHost string) (zeropsURL, importURL string) {
	base := normalizeSchemaHost(apiHost)
	return base + zeropsYmlSchemaPath, base + importYmlSchemaPath
}

func normalizeSchemaHost(apiHost string) string {
	endpoint := apiHost
	if endpoint == "" {
		endpoint = CanonicalAPIHost
	}
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "https://" + endpoint
	}
	return strings.TrimSuffix(endpoint, "/")
}

// Schemas holds parsed and extracted data from both Zerops schemas.
type Schemas struct {
	ZeropsYml *ZeropsYmlSchema
	ImportYml *ImportYmlSchema
}

// ZeropsYmlSchema holds extracted data from the zerops.yaml JSON schema.
type ZeropsYmlSchema struct {
	BuildBases []string       // valid build.base values (e.g., "php@8.4", "nodejs@22")
	RunBases   []string       // valid run.base values (e.g., "php-nginx@8.4", "static")
	Raw        map[string]any // full parsed schema for knowledge injection

	// Precomputed sets — built once in Parse, used for O(1) lookups.
}

// ImportYmlSchema holds extracted data from the import.yaml JSON schema.
type ImportYmlSchema struct {
	ServiceTypes    []string       // valid service types (e.g., "php-nginx@8.4", "postgresql@16")
	Modes           []string       // HA, NON_HA
	CorePackages    []string       // LIGHT, SERIOUS
	Locations       []string       // project.location region codes (eu-central, us-east-1, us-west-1, ...)
	StoragePolicies []string       // object storage policies
	Raw             map[string]any // full parsed schema

	// Precomputed set — built once in Parse, used for O(1) lookups.
}

// ParseZeropsYmlSchema parses raw JSON into a ZeropsYmlSchema with extracted enums.
func ParseZeropsYmlSchema(data []byte) (*ZeropsYmlSchema, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse zerops.yaml schema: %w", err)
	}

	s := &ZeropsYmlSchema{Raw: raw}

	// build.base: properties.zerops.items.properties.build.properties.base
	buildBase := navigatePath(raw, "properties", "zerops", "items", "properties", "build", "properties", "base")
	s.BuildBases = extractEnum(buildBase)
	if len(s.BuildBases) == 0 {
		fmt.Fprintln(os.Stderr, "zcp: schema: zerops.yaml build.base enum is empty — schema structure may have changed")
	}

	// run.base: properties.zerops.items.properties.run.properties.base
	runBase := navigatePath(raw, "properties", "zerops", "items", "properties", "run", "properties", "base")
	s.RunBases = extractEnum(runBase)
	if len(s.RunBases) == 0 {
		fmt.Fprintln(os.Stderr, "zcp: schema: zerops.yaml run.base enum is empty — schema structure may have changed")
	}

	// Precompute sets once.

	return s, nil
}

// ParseImportYmlSchema parses raw JSON into an ImportYmlSchema with extracted enums.
func ParseImportYmlSchema(data []byte) (*ImportYmlSchema, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse import.yaml schema: %w", err)
	}

	s := &ImportYmlSchema{Raw: raw}

	// services[].type: properties.services.items.properties.type
	svcType := navigatePath(raw, "properties", "services", "items", "properties", "type")
	s.ServiceTypes = extractEnum(svcType)
	if len(s.ServiceTypes) == 0 {
		fmt.Fprintln(os.Stderr, "zcp: schema: import.yaml service type enum is empty — schema structure may have changed")
	}

	// services[].mode
	mode := navigatePath(raw, "properties", "services", "items", "properties", "mode")
	s.Modes = extractEnum(mode)

	// project.corePackage
	corePkg := navigatePath(raw, "properties", "project", "properties", "corePackage")
	s.CorePackages = extractEnum(corePkg)

	// project.location — the live region menu. The offer the agent shows
	// MUST derive from this enum (single source of truth), never a
	// hardcoded subset: the platform added us-west-1 while the embedded
	// copy still listed two regions.
	loc := navigatePath(raw, "properties", "project", "properties", "location")
	s.Locations = extractEnum(loc)

	// objectStoragePolicy
	policy := navigatePath(raw, "properties", "services", "items", "properties", "objectStoragePolicy")
	s.StoragePolicies = extractEnum(policy)

	return s, nil
}

// navigatePath walks a nested map[string]any by key path.
func navigatePath(m map[string]any, keys ...string) map[string]any {
	current := m
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

// extractEnum extracts string enum values from a JSON schema node.
// Handles direct enum, oneOf with enum, and items with enum.
func extractEnum(node map[string]any) []string {
	if node == nil {
		return nil
	}

	// Direct enum.
	if enum := toStringSlice(node["enum"]); len(enum) > 0 {
		return enum
	}

	// oneOf: check each alternative for enum.
	if oneOf, ok := node["oneOf"].([]any); ok {
		for _, item := range oneOf {
			if m, ok := item.(map[string]any); ok {
				if enum := toStringSlice(m["enum"]); len(enum) > 0 {
					return enum
				}
				// Check items.enum (array variant).
				if items, ok := m["items"].(map[string]any); ok {
					if enum := toStringSlice(items["enum"]); len(enum) > 0 {
						return enum
					}
				}
			}
		}
	}

	// items.enum (array type).
	if items, ok := node["items"].(map[string]any); ok {
		if enum := toStringSlice(items["enum"]); len(enum) > 0 {
			return enum
		}
	}

	return nil
}

// toStringSlice converts []any to []string, skipping non-strings.
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
