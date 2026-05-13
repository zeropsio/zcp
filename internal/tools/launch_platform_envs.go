package tools

import (
	"maps"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
)

// platformEnvAction encodes the auto-classification decision for a
// known platform-injected env. Two terminal actions:
//
//   - bucket: bake the env into the target import yaml under this
//     classification (e.g. zeropsSubdomainHost → infrastructure, so
//     the new prod project re-emits its own subdomain references).
//   - drop:   exclude the env from the target import yaml entirely.
//     Used for ZCP-control-plane envs (which only exist in the
//     dev-side container) and project-level Zerops settings that the
//     new project will receive on its own (envIsolation, sshIsolation).
type platformEnvAction struct {
	// Bucket is the classification to apply when Action == "classify".
	// Empty for Action == "drop".
	Bucket topology.SecretClassification
	// Action is "classify" (use Bucket) or "drop" (exclude entirely).
	Action string
}

const (
	platformEnvActionClassify = "classify"
	platformEnvActionDrop     = "drop"
)

// platformEnvAutoClass is the exact-key table of well-known
// platform-injected envs. Membership is closed — keys not in this map
// fall through to user-driven classification in the classify-prompt.
//
// Codex review 2026-05-13 explicitly rejected a blanket `ZCP_*` prefix
// drop: a user app could legitimately use that prefix. Each ZCP-control
// env is enumerated by exact name. Same rule for the `zerops*` Zerops
// subdomain envs — only the documented platform-injected ones auto-class.
//
// Storage / CDN URL keys are deliberately omitted until exact key
// patterns are verified against a live project — the wrong drop or
// re-classification would point prod at the source project's storage.
// Until that verification, user classification stays authoritative.
//
//nolint:gochecknoglobals // closed look-up table; not state
var platformEnvAutoClass = map[string]platformEnvAction{
	// Platform-injected subdomain envs — every Zerops project gets its
	// own pair on project creation. Importing under `infrastructure`
	// causes the new project to emit a fresh `${zeropsSubdomainHost}`
	// / `${zeropsSubdomainString}` pointing at the prod project's
	// own subdomain, not the source's.
	"zeropsSubdomainHost":   {Bucket: topology.SecretClassInfrastructure, Action: platformEnvActionClassify},
	"zeropsSubdomainString": {Bucket: topology.SecretClassInfrastructure, Action: platformEnvActionClassify},

	// Project-level Zerops settings. The new project is provisioned
	// with its own defaults via the platform create flow; carrying
	// these values forward would either be no-op (project picks its
	// own) or actively wrong (e.g. sshIsolation="vpn service@zcp"
	// references the SOURCE project's zcp container).
	"envIsolation": {Action: platformEnvActionDrop},
	"sshIsolation": {Action: platformEnvActionDrop},

	// ZCP control-plane envs injected by the ZCP container into the
	// source project's env scope. They have no meaning in production —
	// ZCP does not run as part of the user's prod project.
	"ZCP_API_KEY":      {Action: platformEnvActionDrop},
	"ZCP_AGENT_TYPE":   {Action: platformEnvActionDrop},
	"ZCP_BASE_HOST":    {Action: platformEnvActionDrop},
	"ZCP_BUILTINS_DIR": {Action: platformEnvActionDrop},
	"ZCP_PROJECT_DIR":  {Action: platformEnvActionDrop},
}

// classifyPlatformEnv returns the auto-classification action for a
// known platform-injected env key. The second return is `false` for
// any key not in the closed table — those keys fall through to
// user-driven classification.
func classifyPlatformEnv(key string) (platformEnvAction, bool) {
	action, ok := platformEnvAutoClass[key]
	return action, ok
}

// mergePlatformAutoClassifications returns the effective classification
// map for bundle composition. The result is the union of:
//
//   - user-supplied classifications (input.EnvClassifications converted
//     to the topology enum)
//   - auto-classifications for known platform envs whose Action ==
//     "classify" (e.g. zeropsSubdomainHost → infrastructure)
//
// "drop" envs are intentionally NOT in the returned map — the bundle
// composer skips entries without an entry, which is the desired
// exclusion behavior. Caller is the launch handler; export does NOT
// call this (auto-bucketing is launch-specific).
//
// User-supplied classifications take precedence: when a user explicitly
// classifies a platform env (uncommon but allowed for escape hatches),
// the user value wins.
func mergePlatformAutoClassifications(user map[string]topology.SecretClassification) map[string]topology.SecretClassification {
	out := make(map[string]topology.SecretClassification, len(user)+len(platformEnvAutoClass))
	for key, action := range platformEnvAutoClass {
		if action.Action == platformEnvActionClassify {
			out[key] = action.Bucket
		}
	}
	maps.Copy(out, user) // user-supplied entries override auto-classifications
	return out
}

// needsClassifyPromptForLaunch wraps needsClassifyPrompt with the launch
// auto-classification table: known platform envs are considered already
// satisfied even when absent from user-supplied EnvClassifications, so
// the prompt does not loop forever on envs the agent has no business
// classifying.
//
// Reuses needsClassifyPrompt's "partial classification still prompts"
// semantics for user-relevant envs.
func needsClassifyPromptForLaunch(envClassifications map[string]string, envs []ops.ProjectEnvVar) bool {
	if len(envs) == 0 {
		return false
	}
	for _, env := range envs {
		if _, autoHandled := platformEnvAutoClass[env.Key]; autoHandled {
			continue
		}
		if _, ok := envClassifications[env.Key]; !ok {
			return true
		}
	}
	return false
}

// filterUserClassificationEnvs returns the subset of envs the user
// needs to bucket: drops platform-auto-classified keys from the
// classify-prompt rows. Source-side raw envs stay intact for hashing
// (P-LP-3 source-immutability digests are computed over the raw
// snapshot, not this filtered view).
func filterUserClassificationEnvs(envs []ops.ProjectEnvVar) []ops.ProjectEnvVar {
	out := make([]ops.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		if _, autoHandled := platformEnvAutoClass[env.Key]; autoHandled {
			continue
		}
		out = append(out, env)
	}
	return out
}
