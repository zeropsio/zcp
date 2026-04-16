package tools

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkCausalAnchor enforces that every gotcha bullet is load-bearing —
// it must name a SPECIFIC Zerops mechanism AND describe a CONCRETE
// failure mode caused by that mechanism. A gotcha that mentions only
// generic platform tokens ("envVariables", "container", "service")
// without naming a specific mechanism, or that names a mechanism but
// no concrete symptom, is decorative — it reads like generic Node/PHP
// advice and trains the reader to skim.
//
// The presence-based shape classifier in recipe_gotcha_shape.go scores
// platform-term presence and gives a +2 bonus for failure-mode terms.
// That score is calibrated to admit borderline gotchas if at least 3
// pass — the new causal-anchor check is per-gotcha (every gotcha must
// pass) and uses a narrower mechanism-token list that excludes generic
// terms. The two checks complement: the classifier provides a soft
// floor; the causal-anchor check enforces a hard per-bullet rule.
//
// Two halves, both required per gotcha:
//
//  1. **Specific Zerops mechanism** — a token from the curated narrow
//     list (L7, execOnce, readinessCheck, subdomain, ${X_hostname},
//     etc.). NOT the generic terms ("envVariables", "container") that
//     the legacy classifier admits.
//
//  2. **Concrete failure mode** — an HTTP status code, a quoted error
//     name in backticks, a named exception, or a strong symptom verb
//     ("rejects", "deadlocks", "drops", "crashes", "times out", etc.).
//
// hostname is used to scope the check name so multi-codebase recipes
// surface failures per-codebase. Returns a single check that lists every
// failing gotcha stem in the failure detail.
func checkCausalAnchor(kbContent, hostname string) []workflow.StepCheck {
	if kbContent == "" {
		return nil
	}
	entries := workflow.ExtractGotchaEntries(kbContent)
	if len(entries) == 0 {
		return nil
	}
	var failing []string
	for _, e := range entries {
		// Combine stem + body for matching — both halves are searched
		// across the whole bullet because authors split the mechanism
		// (often in body) from the symptom (often in stem) freely.
		text := e.Stem + " " + e.Body
		if hasSpecificMechanism(text) && hasConcreteFailureMode(text) {
			continue
		}
		failing = append(failing, e.Stem)
	}
	checkName := hostname + "_gotcha_causal_anchor"
	if len(failing) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	tail := ""
	if len(failing) > 5 {
		tail = fmt.Sprintf("; and %d more", len(failing)-5)
		failing = failing[:5]
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s gotchas are not load-bearing — each gotcha must name a SPECIFIC Zerops mechanism (e.g. L7 balancer, execOnce, readinessCheck, ${X_hostname}, httpSupport, serviceStackIsNotHttp, SSHFS mount, initCommands, deployFiles, minContainers — NOT generic terms like 'container' or 'envVariables') AND describe a CONCRETE failure mode (HTTP status code, quoted error name in backticks, named exception, or strong symptom verb like 'rejects/deadlocks/drops/crashes/times out'). A gotcha that names only generic platform terms or only a vague symptom is generic Node/PHP advice the reader could find in any tutorial. Failing stems: %q%s",
			hostname, failing, tail,
		),
	}}
}

// specificMechanismTokens is the curated narrow list of Zerops-specific
// mechanisms that count as load-bearing. Distinct from the
// platform_terms list in recipe_gotcha_shape.go: that list is broad
// (admits "container", "service", "envVariables") for soft scoring;
// this list is narrow (rejects them) for hard per-bullet enforcement.
//
// Add-only: every entry here must name a mechanism that is unique to
// Zerops or whose specific behavior on Zerops is the point. Generic
// concepts ("HTTP", "TLS", "service") do not belong.
var specificMechanismTokens = []string{
	// L7 balancer + routing
	"L7 balancer", "L7-balancer", "l7 balancer", "L7", " l7 ",
	"subdomain", "enableSubdomainAccess",
	"serviceStackIsNotHttp", "httpSupport",
	// Execution gating
	"execOnce", "zsc execOnce", "zsc noop", "zsc test",
	"--retryUntilSuccessful", "appVersionId",
	// Health / lifecycle
	"readinessCheck", "healthCheck", "firstStart", "forceRestart",
	"initCommands", "preStart", "preStop",
	// File mount
	"SSHFS", "sshfs", "/var/www",
	// zerops.yaml mechanisms
	"deployFiles", "buildCommands", "buildFromGit",
	"setup: dev", "setup: prod", "setup: worker",
	"base: static", "base: nodejs", "base: nginx",
	"verticalAutoscaling", "minContainers", "maxContainers",
	"corePackage", "cpuMode: DEDICATED", "cpuMode: SHARED",
	"mode: HA", "mode: NON_HA", "objectStorageSize", "objectStoragePolicy",
	"priority:", "envSecrets", "<@generateRandomString",
	"#zeropsPreprocessor", "#yamlPreprocessor",
	// Static base specifics
	"static base", "Nginx fallback", "_nginx.json",
	"try_files", "proxy_pass",
	// Service-discovery env-var pattern (matched by regex too — see below)
	"${zeropsSubdomainHost}", "${appVersionId}",
	// Dev/prod container distinction
	"dev container", "prod container", "build container", "runtime container",
	// Common Zerops error / failure names that ARE Zerops-specific
	"AUTHORIZATION_VIOLATION",
	"serviceStackIsNotHttp",
	// Managed service brand names — when a gotcha says "Valkey on
	// Zerops…" or "PostgreSQL on Zerops…" it's anchored to a specific
	// service product the platform offers, with Zerops-specific defaults
	// (no-auth Valkey, NON_HA-by-default Postgres, ephemeral Meilisearch
	// indexes, etc.). These count as specific mechanisms.
	"Valkey", "PostgreSQL", "Postgres", "NATS",
	"Meilisearch", "Object Storage", "MinIO",
	"Redis-compatible", "ioredis",
	"keydb", "shared-storage", "object-storage",
	// Migration / queue framework names that are Zerops-anchored when
	// referenced alongside container lifecycle.
	"TypeORM synchronize", "TypeORM migrationsRun",
	"queue group", "queue: 'workers'", "queue: \"workers\"",
}

// envVarRefRe matches Zerops service-discovery env-var references like
// `${db_hostname}`, `${queue_password}`, `${storage_bucketName}`. These
// are platform-injected references and count as a specific mechanism
// even when the surrounding text doesn't carry an explicit "Zerops" word.
var envVarRefRe = regexp.MustCompile(`\$\{[a-zA-Z_][a-zA-Z0-9_]*_(?:hostname|host|port|user|password|pass|dbName|db_name|apiUrl|api_url|accessKeyId|access_key|secretAccessKey|secret_key|bucketName|bucket_name|masterKey|master_key|connectionString|connection_string|url)\}`)

func hasSpecificMechanism(text string) bool {
	low := strings.ToLower(text)
	for _, tok := range specificMechanismTokens {
		if strings.Contains(low, strings.ToLower(tok)) {
			return true
		}
	}
	return envVarRefRe.MatchString(text)
}

// concreteFailureSymptoms are strong indicators that the gotcha is
// describing a real, observable failure mode rather than abstract
// architecture talk. The list intentionally favors verbs and quoted
// shapes — "deadlocks", "crashes", "rejects" — over passive narration
// like "may cause issues" or "could be problematic".
var concreteFailureSymptoms = []string{
	"rejects", "rejected",
	"crashes", "crashed", "crashing",
	"deadlocks", "deadlock",
	"drops", "dropped", "dropping",
	"loses", "lost", "losing",
	"hangs", "hung", "hanging",
	"times out", "timed out", "timeout",
	"fails with", "fails to", "failed with",
	"returns 5", "returns 4", "returns 3", "returns 2",
	"returns empty", "returns html", "returns text/",
	"throws", "throw error", "throw new",
	"empty response", "blank screen", "blank page",
	"silently", "silent",
	"forever", "never", "permanently",
	"infinite loop", "stuck", "stalled",
	"out of memory", "oom", "killed", "sigterm", "sigkill",
	"connection refused", "connection error", "econnrefused", "eaddrinuse",
	"masquerade", "masquerades",
	"shadow", "shadows", "shadowed",
	"double", "duplicate", "twice",
	"corruption", "corrupt",
	"deprecation",
	"403", "404", "500", "502", "503", "504",
	"200 ok", "401",
	// Subtler symptom verbs. "break"/"breaks"/"broke" describe a
	// real downstream consequence ("rate limiters break"); without
	// these the check rejects authentic gotchas whose symptom isn't
	// a quoted error or HTTP code.
	"break", "breaks", "broke",
	"override", "overrides", "overridden",
	"misbehaves", "misbehave",
	"unresponsive", "no longer",
	"missing", "absent",
}

// httpStatusRe matches an HTTP status code as a standalone integer
// surrounded by non-digit context. Used as a fallback when the status
// is mentioned without one of the symptomatic phrasings above.
var httpStatusRe = regexp.MustCompile(`(?:^|[^0-9])(?:200|201|202|301|302|400|401|403|404|409|410|413|429|500|501|502|503|504)(?:$|[^0-9])`)

// quotedErrorRe matches a backtick-or-double-quote-wrapped error name
// containing at least one underscore or CamelCase pattern — a strong
// hint that it's an actual error identifier (`AUTHORIZATION_VIOLATION`,
// `QueryFailedError`) rather than a generic word in code style.
var quotedErrorRe = regexp.MustCompile("(?:`|\")([A-Z][A-Z0-9_]{4,}|[A-Z][a-z]+[A-Z][A-Za-z]+(?:Error|Exception|Violation))(?:`|\")")

func hasConcreteFailureMode(text string) bool {
	low := strings.ToLower(text)
	for _, sym := range concreteFailureSymptoms {
		if strings.Contains(low, sym) {
			return true
		}
	}
	if httpStatusRe.MatchString(text) {
		return true
	}
	return quotedErrorRe.MatchString(text)
}
