package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// preprocessorHeader is prepended verbatim as line 1 of import.yaml
// when any value contains a `<@...>` directive. Platform preprocessor
// skips expansion if this header is missing or not on line 1.
const preprocessorHeader = "#zeropsPreprocessor=on\n"

// importModeNonHA is the platform scaling mode for single-runtime
// bundle entries. Platform schema enforces `HA` / `NON_HA` only.
const importModeNonHA = "NON_HA"

// importModeHA is the platform scaling-mode value for HA managed
// services. Used in launch composition where managed deps promote
// from NON_HA (export default) to HA for production.
const importModeHA = "HA"

// autoSecretPreprocessor is the preprocessor directive emitted for
// SecretClassAutoSecret envs — the target project generates a fresh
// random 32-char string on re-import.
const autoSecretPreprocessor = "<@generateRandomString(<32>)>"

// ExternalSecretPlaceholder is the literal value emitted for
// SecretClassExternalSecret envs with a non-empty source value. The
// new project's owner replaces this in the Zerops dashboard (or via
// `zerops_env action=set`) before the runtime depends on the value.
//
// We emit a literal string (not a preprocessor function) because the
// platform's preprocessor `pickRandom(<a>, <b>, ...)` syntax requires
// `<value>` wrapping per docs (zerops-docs/references/import-yaml/
// pre-processor.mdx), and the previous emission
// `<@pickRandom(["REPLACE_ME"])>` was JSON-array form which the
// platform rejects with `yamlPreprocessingError: variable
// [["REPLACE_ME"]] not found`. A literal string surfaces the
// placeholder verbatim to the user — clearer + always-importable.
//
// Karel hit this in a real launch 2026-05-16: GIT_TOKEN+ZCP_API_KEY
// classified external-secret → import refused with 8 field errors.
// Workaround was reclassifying to plain-config; permanent fix is
// this constant.
// ExternalSecretPlaceholder is also consumed by the existing-project
// mutation path (internal/tools/launch_existing.go) where CreateProjectEnv
// bypasses the preprocessor — the value lands in the target as-is, so the
// same literal "REPLACE_ME" surfaces verbatim to the operator.
const ExternalSecretPlaceholder = "REPLACE_ME"

// composeProjectEnvVariables applies the four-category classification
// to the project envVariables snapshot. Returns the rendered map
// keyed by env name, plus per-env warnings for buckets that need
// explicit user review.
func composeProjectEnvVariables(
	envs []ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
) (map[string]string, []string) {
	out := map[string]string{}
	var warnings []string

	for _, env := range envs {
		bucket := classifications[env.Key]
		switch bucket {
		case topology.SecretClassInfrastructure:
			continue
		case topology.SecretClassAutoSecret:
			out[env.Key] = autoSecretPreprocessor
		case topology.SecretClassExternalSecret:
			if env.Value == "" {
				out[env.Key] = ""
				warnings = append(warnings, fmt.Sprintf(
					"env %q: empty external secret — review before publish (plan §3.4 M4)", env.Key))
			} else {
				out[env.Key] = ExternalSecretPlaceholder
				warnings = append(warnings, fmt.Sprintf(
					"env %q: external-secret bucket — value set to placeholder %q in target yaml; replace in Zerops dashboard (or via `zerops_env action=set`) before the runtime depends on it",
					env.Key, ExternalSecretPlaceholder))
				if isLikelySentinel(env.Value) {
					warnings = append(warnings, fmt.Sprintf(
						"env %q: external secret value %q matches a known sentinel/test pattern — verify classification (PlainConfig may be more appropriate; plan §3.4 M4)",
						env.Key, env.Value))
				}
			}
		case topology.SecretClassPlainConfig:
			out[env.Key] = env.Value
		case topology.SecretClassUnset:
			out[env.Key] = env.Value
			warnings = append(warnings, fmt.Sprintf(
				"env %q: not classified — emitted as plain-config; classify before publish (plan §3.4)", env.Key))
		default:
			out[env.Key] = env.Value
			warnings = append(warnings, fmt.Sprintf(
				"env %q: unknown classification %q — emitted as plain-config", env.Key, bucket))
		}
	}

	return out, warnings
}

// runtimeImportMode returns the platform scaling-mode (`HA` / `NON_HA`)
// for the bundle's runtime service entry.
//
// Single-runtime bundles always emit `NON_HA` — the topology-level
// dev / stage / simple / local-only shape lives in the destination
// project's bootstrap meta after re-import, not in import.yaml.
func runtimeImportMode(_ topology.Mode) string {
	return importModeNonHA
}

// addPreprocessorHeader prepends `#zeropsPreprocessor=on\n` to body
// when any project envVariable carries a `<@...>` directive. Header
// MUST be line 1 or the platform preprocessor skips expansion on
// import.
func addPreprocessorHeader(body string, projectEnvs map[string]string) string {
	for _, v := range projectEnvs {
		if strings.Contains(v, "<@") && strings.Contains(v, ")>") {
			return preprocessorHeader + body
		}
	}
	return body
}

// verifyZeropsYAMLSetup confirms the body is a parseable zerops.yaml
// with a `zerops:` list containing an entry whose `setup:` matches
// the requested name.
func verifyZeropsYAMLSetup(body, setupName string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("zerops.yaml body is empty (chain to scaffold-zerops-yaml)")
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return fmt.Errorf("parse zerops.yaml: %w", err)
	}
	setups, ok := doc["zerops"].([]any)
	if !ok {
		return fmt.Errorf("zerops.yaml missing top-level 'zerops:' list")
	}
	for _, item := range setups {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["setup"].(string); ok && name == setupName {
			return nil
		}
	}
	return fmt.Errorf("zerops.yaml does not contain setup %q (chain to scaffold-zerops-yaml or correct the setup name)", setupName)
}

// composeLaunchTags returns the canonical tag set for a launch
// bundle. "env:prod" + "source-project:<sourceID>" + "managed-by:zcp-launch"
// are always emitted; AdditionalTags are appended in order
// (deduplicated).
func composeLaunchTags(sourceProjectID string, additional []string) []string {
	tags := []string{
		"env:prod",
		"source-project:" + sourceProjectID,
		"managed-by:zcp-launch",
	}
	seen := map[string]bool{
		tags[0]: true,
		tags[1]: true,
		tags[2]: true,
	}
	for _, t := range additional {
		if t == "" {
			continue
		}
		if seen[t] {
			continue
		}
		tags = append(tags, t)
		seen[t] = true
	}
	return tags
}

// computeSourceSnapshot produces a deterministic digest of source
// state for the immutability guard (P-LP-3).
func computeSourceSnapshot(inputs LaunchBundleInputs) SourceSnapshot {
	return SourceSnapshot{
		GitCommitSHA:      inputs.GitCommitSHA,
		ZeropsYAMLSHA256:  sha256Hex(inputs.ZeropsYAMLBody),
		ProjectEnvsDigest: hashProjectEnvs(inputs.ProjectEnvs),
		ServiceListDigest: hashServiceList(inputs.ManagedServices, inputs.TargetHostname, inputs.ServiceType),
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// hashProjectEnvs returns sha256 over sorted "key=value\n" lines.
// Stable across runs with identical inputs.
func hashProjectEnvs(envs []ProjectEnvVar) string {
	pairs := make([]string, 0, len(envs))
	for _, e := range envs {
		pairs = append(pairs, e.Key+"="+e.Value)
	}
	sort.Strings(pairs)
	return sha256Hex(strings.Join(pairs, "\n"))
}

// hashServiceList returns sha256 over sorted "hostname:type\n" lines
// including the runtime + all managed deps.
func hashServiceList(managed []ManagedServiceEntry, runtimeHost, runtimeType string) string {
	lines := make([]string, 0, 1+len(managed))
	lines = append(lines, runtimeHost+":"+runtimeType)
	for _, m := range managed {
		lines = append(lines, m.Hostname+":"+m.Type)
	}
	sort.Strings(lines)
	return sha256Hex(strings.Join(lines, "\n"))
}
