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

// composeServiceEnvSecrets renders a runtime's per-service USER-set env
// layer (Type=SECRET slim service /env) into the `envSecrets` map for the
// runtime's import.yaml entry, applying the same four-category
// classification as project envs — with one critical difference: the
// UNSET / unknown default is SECRET-SAFE. These entries are SECRET-typed
// user data (e.g. an API key set via `zerops_env set serviceHostname=X`);
// emitting an unclassified one verbatim would leak the secret into the
// committed bundle, so an unclassified entry collapses to the REPLACE_ME
// placeholder (never the source value). The classify-prompt surfaces these
// keys so the agent can reclassify (e.g. plain-config for non-secret
// config) before publish (GAP0-1/GAP0-2).
func composeServiceEnvSecrets(
	envs []ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
) (map[string]string, []string) {
	out := map[string]string{}
	var warnings []string

	for _, env := range envs {
		switch classifications[env.Key] {
		case topology.SecretClassInfrastructure:
			// Resolves at re-import via the managed service ref — drop.
			continue
		case topology.SecretClassAutoSecret:
			out[env.Key] = autoSecretPreprocessor
		case topology.SecretClassPlainConfig:
			out[env.Key] = env.Value
		case topology.SecretClassExternalSecret:
			out[env.Key] = ExternalSecretPlaceholder
			warnings = append(warnings, fmt.Sprintf(
				"service env %q: external-secret — emitted as placeholder %q in envSecrets; set the real value in the target (Zerops dashboard or `zerops_env action=set serviceHostname=…`) before the runtime depends on it",
				env.Key, ExternalSecretPlaceholder))
		case topology.SecretClassUnset:
			// SECRET-safe: an unclassified SECRET-typed service env NEVER
			// leaks its source value — it collapses to the placeholder.
			out[env.Key] = ExternalSecretPlaceholder
			warnings = append(warnings, fmt.Sprintf(
				"service env %q: SECRET-typed but not classified — emitted as placeholder %q (secret-safe default); classify it (plain-config if it is non-secret config) before publish",
				env.Key, ExternalSecretPlaceholder))
		default:
			// Unknown future bucket: stay secret-safe.
			out[env.Key] = ExternalSecretPlaceholder
			warnings = append(warnings, fmt.Sprintf(
				"service env %q: unknown classification %q — emitted as placeholder %q (secret-safe)",
				env.Key, classifications[env.Key], ExternalSecretPlaceholder))
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

// addPreprocessorHeader prepends `#zeropsPreprocessor=on\n` to body when
// any rendered env value (across the given maps — project envVariables
// and/or service envSecrets) carries a `<@...>` directive. Header MUST be
// line 1 or the platform preprocessor skips expansion on import.
func addPreprocessorHeader(body string, envMaps ...map[string]string) string {
	for _, m := range envMaps {
		for _, v := range m {
			if strings.Contains(v, "<@") && strings.Contains(v, ")>") {
				return preprocessorHeader + body
			}
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

// computeSourceSnapshotMulti produces a deterministic digest of source
// state for the immutability guard (P-LP-3) across N promoted runtimes.
// Per-runtime SHA + zerops.yaml body fold into stable composite
// digests; multi-runtime drift on any single runtime trips the guard.
func computeSourceSnapshotMulti(inputs LaunchBundleInputs) SourceSnapshot {
	gitLines := make([]string, 0, len(inputs.Runtimes))
	yamlLines := make([]string, 0, len(inputs.Runtimes))
	svcLines := make([]string, 0, len(inputs.Runtimes)+len(inputs.ManagedServices))
	for _, r := range inputs.Runtimes {
		gitLines = append(gitLines, r.ProdHostname+":"+r.GitCommitSHA)
		yamlLines = append(yamlLines, r.ProdHostname+":"+sha256Hex(r.ZeropsYAMLBody))
		svcLines = append(svcLines, r.ProdHostname+":"+r.ServiceType)
	}
	for _, m := range inputs.ManagedServices {
		svcLines = append(svcLines, m.Hostname+":"+m.Type)
	}
	sort.Strings(gitLines)
	sort.Strings(yamlLines)
	sort.Strings(svcLines)
	return SourceSnapshot{
		GitCommitSHA:      sha256Hex(strings.Join(gitLines, "\n")),
		ZeropsYAMLSHA256:  sha256Hex(strings.Join(yamlLines, "\n")),
		ProjectEnvsDigest: hashProjectEnvs(inputs.ProjectEnvs),
		ServiceListDigest: sha256Hex(strings.Join(svcLines, "\n")),
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
