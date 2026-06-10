package ops

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

var durationRegex = regexp.MustCompile(`^(\d+)(s|m|h|d)$`)

// FindService resolves a service hostname to its ServiceStack against a
// pre-fetched service list. Returns the canonical ErrServiceNotFound +
// "Available: ..." suggestion when the hostname is absent — every caller
// (tools, eval, ops itself) gets identical error wording.
func FindService(services []platform.ServiceStack, hostname string) (*platform.ServiceStack, error) {
	svc := findServiceByHostname(services, hostname)
	if svc == nil {
		// The "Available services" suggestion hides system-category services
		// (core, build*, L7 balancer) — they are not legitimate targets and
		// naming them taught the agent system hostnames it could then aim
		// mutating tools at (live-confirmed leak via zerops_manage).
		return nil, platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service '%s' not found", hostname),
			"Available services: "+ListHostnames(filterUserVisible(services)),
		)
	}
	return svc, nil
}

// FindUserVisibleService resolves a hostname like FindService but ALSO refuses
// a hostname that resolves to a system-category service (core, build*, L7
// balancer). Use from mutating callers (delete / manage / scale / mount) so a
// typo'd or copied system hostname is rejected at ZCP rather than reaching the
// platform. Read/classify-downstream callers (subdomain, import, unmount) keep
// using FindService — they need to resolve system services to classify them.
func FindUserVisibleService(services []platform.ServiceStack, hostname string) (*platform.ServiceStack, error) {
	svc, err := FindService(services, hostname)
	if err != nil {
		return nil, err
	}
	if svc.IsSystem() {
		return nil, platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service '%s' not found", hostname),
			"Available services: "+ListHostnames(filterUserVisible(services)),
		)
	}
	return svc, nil
}

// findServiceByHostname scans a slice for matching hostname.
func findServiceByHostname(services []platform.ServiceStack, hostname string) *platform.ServiceStack {
	for i := range services {
		if services[i].Name == hostname {
			return &services[i]
		}
	}
	return nil
}

// ListHostnames returns comma-separated hostnames for error messages.
// Useful for callers building their own "service not found" suggestion
// text without going through FindService.
func ListHostnames(services []platform.ServiceStack) string {
	if len(services) == 0 {
		return "(none)"
	}
	names := make([]string, len(services))
	for i, s := range services {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// filterUserVisible returns the slice with system-category services
// (CORE/BUILD/INTERNAL/PREPARE_RUNTIME/HTTP_L7_BALANCER) removed.
// Used by ErrServiceNotFound "Available services:" suggestions so the
// suggested list matches the user-visible inventory (`zerops_discover`
// without hostname filter also hides these). Without this filter the
// not-found message would name system hostnames the agent can't
// legitimately target anyway.
func filterUserVisible(services []platform.ServiceStack) []platform.ServiceStack {
	out := make([]platform.ServiceStack, 0, len(services))
	for i := range services {
		if services[i].IsSystem() {
			continue
		}
		out = append(out, services[i])
	}
	return out
}

// parseSince converts user-friendly time strings to time.Time.
// Supports: "30s", "30m", "1h", "24h", "7d", ISO 8601 (RFC3339).
// Empty string defaults to 1 hour ago.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Now().Add(-1 * time.Hour), nil
	}

	matches := durationRegex.FindStringSubmatch(s)
	if len(matches) == 3 {
		n, err := strconv.Atoi(matches[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid duration number: %s", s)
		}
		switch matches[2] {
		case "s":
			if n < 1 || n > 86400 {
				return time.Time{}, fmt.Errorf("seconds must be 1-86400")
			}
			return time.Now().Add(-time.Duration(n) * time.Second), nil
		case "m":
			if n < 1 || n > 1440 {
				return time.Time{}, fmt.Errorf("minutes must be 1-1440")
			}
			return time.Now().Add(-time.Duration(n) * time.Minute), nil
		case "h":
			if n < 1 || n > 168 {
				return time.Time{}, fmt.Errorf("hours must be 1-168")
			}
			return time.Now().Add(-time.Duration(n) * time.Hour), nil
		case "d":
			if n < 1 || n > 30 {
				return time.Time{}, fmt.Errorf("days must be 1-30")
			}
			return time.Now().Add(-time.Duration(n) * 24 * time.Hour), nil
		}
	}

	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid format: %s", s)
}

// envPair holds a parsed KEY=value pair.
type envPair struct {
	Key   string
	Value string
}

// parseEnvPairs splits "KEY=value" strings into key/value pairs.
// Splits on first '=' only (value may contain '=').
func parseEnvPairs(vars []string) ([]envPair, error) {
	pairs := make([]envPair, 0, len(vars))
	for _, v := range vars {
		key, value, ok := strings.Cut(v, "=")
		if !ok {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidEnvFormat,
				fmt.Sprintf("Invalid format '%s', expected KEY=value", v),
				"Format: KEY=value (split on first '=')",
			)
		}
		if key == "" {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidEnvFormat,
				"Empty key in env var",
				"Format: KEY=value",
			)
		}
		pairs = append(pairs, envPair{Key: key, Value: value})
	}
	return pairs, nil
}

// crossRefPattern matches Zerops cross-service env var references like ${db_hostname}.
var crossRefPattern = regexp.MustCompile(`\$\{[a-zA-Z_][a-zA-Z0-9_]*\}`)

// envKeyZeropsSubdomain is the platform-injected env var containing the full subdomain URL.
const envKeyZeropsSubdomain = "zeropsSubdomain"

// platformInjectedKeys are env vars injected by the Zerops platform (not user-defined).
// These are annotated with isPlatformInjected: true in discover output.
var platformInjectedKeys = map[string]bool{
	envKeyZeropsSubdomain: true,
}

// envVarsToMaps converts platform env vars to a slice of maps for JSON
// output. Generic over any EnvAccessor implementer — same code path
// for both ProjectEnvVar and ServiceEnvVar.
//
// When includeValues is false, only keys and annotations are returned
// (no secret values in LLM context). Values containing ${...}
// cross-service references are annotated with isReference: true.
// Platform-injected keys are annotated with isPlatformInjected: true.
func envVarsToMaps[T platform.EnvAccessor](envs []T, includeValues bool) []map[string]any {
	result := make([]map[string]any, 0, len(envs))
	for _, e := range envs {
		key := e.GetKey()
		content := e.GetContent()
		m := map[string]any{
			"key": key,
		}
		if includeValues {
			if masked, isCredential := RedactCredentialValue(key, content); isCredential {
				m["value"] = masked
				m["isCredentialRedacted"] = true
			} else {
				m["value"] = content
			}
		}
		if crossRefPattern.MatchString(content) {
			m["isReference"] = true
		}
		if platformInjectedKeys[key] {
			m["isPlatformInjected"] = true
		}
		result = append(result, m)
	}
	return result
}

// annotateConnectionStringShape walks the env map slice (post-render
// output of envVarsToMaps) and attaches `completenessFlags` +
// `warning` to the `connectionString` entry when the service type is
// Postgres or MariaDB. Both expose a connectionString resolving to
// `protocol://${user}:${password}@${hostname}:${port}` — without
// `/${dbName}` appended — and clients that need a fully-qualified URL
// (Prisma, Drizzle, sqlx, SQLAlchemy, Sequelize) silently connect to
// the driver's default admin DB unless the warning is honoured.
//
// Empirical basis: live `zerops_discover service=db includeEnvs=true
// includeEnvValues=true` against postgresql@18 in eval-zcp
// (plans/audit-env-vars-20260515/VERIFY-reserved-names.md §D);
// observed agent friction across runs 1 + 2 (5+ wasted deploys +
// manual schema grants in each session before discovering the gap).
//
// MariaDB inherits the same connectionString shape; ClickHouse and
// Kafka are intentionally NOT covered — ClickHouse has multiple
// per-protocol ports and Kafka exposes no connectionString at all.
//
// The warning text uses `hostname` as the reference prefix so the
// worked example resolves against the user's actual service name (e.g.
// `${appdb_user}` when the service is hostnamed `appdb`). A canonical
// `db` hostname produces the `${db_*}` shape documented in
// develop-env-var-model.md; any other hostname propagates to the
// placeholders. The previous implementation hardcoded `db` regardless
// of hostname — services named `appdb` / `customers_pg` / `analytics_pg`
// got a misleading template that wouldn't resolve at deploy time.
//
// Pinning: TestAnnotateConnectionStringShape_* in helpers_test.go.
func annotateConnectionStringShape(envs []map[string]any, serviceType, hostname string) {
	if !dbServiceTypeWithBareConnectionString(serviceType) {
		return
	}
	for _, m := range envs {
		key, _ := m["key"].(string)
		if key != "connectionString" {
			continue
		}
		m["completenessFlags"] = map[string]any{"includesDbName": false}
		m["warning"] = "connectionString omits /${dbName}; for Prisma / Drizzle / sqlx / SQLAlchemy / Sequelize compose explicitly: protocol://${" + hostname + "_user}:${" + hostname + "_password}@${" + hostname + "_hostname}:${" + hostname + "_port}/${" + hostname + "_dbName}"
	}
}

// dbServiceTypeWithBareConnectionString reports whether a service type
// exposes the bare-shape connectionString documented in
// annotateConnectionStringShape.
func dbServiceTypeWithBareConnectionString(serviceType string) bool {
	base, _, _ := strings.Cut(serviceType, "@")
	switch base {
	case "postgresql", "mariadb":
		return true
	}
	return false
}

// findEnvIDByKey finds an env var ID by key name. Generic over
// EnvAccessor implementers.
func findEnvIDByKey[T platform.EnvAccessor](envs []T, key string) string {
	for _, e := range envs {
		if e.GetKey() == key {
			return e.GetID()
		}
	}
	return ""
}
