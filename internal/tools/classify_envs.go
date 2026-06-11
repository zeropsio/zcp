package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/envclass"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// validateEnvClassifications rejects any agent-supplied classification bucket
// that is not one of topology.SecretClassificationValues(). Without this a
// typo'd bucket ("secret", "autosecret") passes the presence-only
// needsClassifyPrompt gate and the composers' default branch emits the raw
// source value verbatim — routing a credential into a publish-ready bundle /
// prod project as non-sensitive. An empty value is treated as "unclassified"
// (the prompt re-fires) rather than an error.
func validateEnvClassifications(classifications map[string]string) error {
	valid := make(map[string]bool, len(topology.SecretClassificationValues()))
	for _, v := range topology.SecretClassificationValues() {
		valid[v] = true
	}
	var invalid []string
	for key, bucket := range classifications {
		b := strings.TrimSpace(bucket)
		if b == "" {
			continue // unclassified — handled by needsClassifyPrompt re-prompt
		}
		if !valid[b] {
			invalid = append(invalid, fmt.Sprintf("%s=%q", key, bucket))
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	sort.Strings(invalid)
	return platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("invalid env classification bucket(s): %s", strings.Join(invalid, ", ")),
		fmt.Sprintf("Each envClassifications value must be one of: %s. Re-call with corrected buckets.",
			strings.Join(topology.SecretClassificationValues(), ", ")),
	)
}

// suggestBucketForKey computes a server-side classification hint for an
// env entry the agent will review in the classify-prompt response. Bias
// is name-pattern based — the value never enters the computation, which
// preserves the no-leak invariant pinned by
// TestHandleExport_ClassifyPromptDoesNotLeakValues and the launch-side
// equivalent. Branch order matters: the control-plane allowlist trumps
// credentialPattern (GIT_TOKEN ends in _TOKEN but must bucket as
// infrastructure, not auto-secret). The rationale string is the
// agent-facing explanation of which branch fired.
func suggestBucketForKey(env platform.ProjectEnvVar) (topology.SecretClassification, string) {
	if topology.IsClassifyInfrastructure(env.Key) {
		return topology.SecretClassInfrastructure, "ZCP control-plane / platform re-emits on import"
	}
	bias := envclass.ClassifyProjectEnv(env).Bias
	switch bias {
	case topology.SecretClassAutoSecret:
		return bias, "key matches credentialPattern (_KEY|_SECRET|_TOKEN|_PASS|APP_KEY suffix); verify state continuity for migrate-into-existing-project path"
	case topology.SecretClassPlainConfig:
		return bias, "no credential-pattern match; defaulting to plain-config"
	case topology.SecretClassUnset, topology.SecretClassInfrastructure,
		topology.SecretClassExternalSecret, topology.SecretClassExclude:
		// Unreachable: envclass.ClassifyProjectEnv.Bias only ever returns
		// AutoSecret or PlainConfig for PromptUser decisions, and callers
		// pre-filter PromptUser. (Exclude additionally is a user-judgment
		// bucket the server never suggests — staleness needs a source-tree
		// grep the server can't do.) The exhaustive linter still wants the
		// branches; surface the bias verbatim if envclass ever evolves.
		return bias, ""
	}
	return bias, ""
}

// envsForClassifyPrompt returns the subset of source project envs the
// user needs to classify — envclass-Decision = PromptUser. Drop-decision
// entries (project SYSTEM envs, e.g. zeropsSubdomain*, CDN URLs,
// envIsolation) are excluded; the target project regenerates them on
// import. Source slice is not mutated. Shared between export and launch
// classify-prompt builders.
func envsForClassifyPrompt(envs []platform.ProjectEnvVar) []platform.ProjectEnvVar {
	out := make([]platform.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		if envclass.ClassifyProjectEnv(env).Decision == envclass.PromptUser {
			out = append(out, env)
		}
	}
	return out
}

// needsClassifyPrompt reports whether the classify-prompt status must
// fire. True when at least one envclass-PromptUser env lacks a user-
// supplied classification. Drop-decision envs (SYSTEM scope) are already
// satisfied — they never reach the composer. Shared between export and
// launch handlers; both consume the live platform shape so Type-based
// filtering works end-to-end.
func needsClassifyPrompt(classifications map[string]string, envs []platform.ProjectEnvVar) bool {
	if len(envs) == 0 {
		return false
	}
	for _, env := range envs {
		if envclass.ClassifyProjectEnv(env).Decision != envclass.PromptUser {
			continue
		}
		if _, ok := classifications[env.Key]; !ok {
			return true
		}
	}
	return false
}

// bundleProjectEnvsFromSource converts the platform-shaped source envs
// into the lossy bundle composer input (`{Key, Value}` only). Drops envs
// the classifier marks as Drop (project SYSTEM, etc.) — keeps the
// composer + SourceSnapshot digest focused on user-controlled values
// only. Used by both export and launch composers.
func bundleProjectEnvsFromSource(envs []platform.ProjectEnvVar) []ops.ProjectEnvVar {
	out := make([]ops.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		if envclass.ClassifyProjectEnv(env).Decision != envclass.PromptUser {
			continue
		}
		out = append(out, ops.ProjectEnvVar{Key: env.Key, Value: env.Content})
	}
	return out
}
