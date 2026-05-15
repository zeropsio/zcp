// Package envclass is the SDK-driven env classifier (Layer 3) per
// plans/workflow-family-architecture-2026-05-14.md §5.4 + §9.5.
//
// Three rules, total:
//
//  1. Service envs (any UserDataTypeEnum value) → Drop. Target's own
//     managed services regenerate equivalent keys at re-import.
//  2. Project envs, Type=SYSTEM → Drop. Platform-injected
//     (zeropsSubdomainHost, staticCdnUrl, envIsolation, etc.); the
//     target project regenerates own. Covers F19 backlog: CDN URLs +
//     object-storage credentials never leak into target yaml.
//  3. Project envs, Type=USER → PromptUser, with name-pattern bias
//     (AutoSecret when Key matches (?i)(_KEY|_SECRET|_TOKEN|_PASS|
//     APP_KEY)$, PlainConfig otherwise).
//
// `Sensitive` is supplementary signal, never authoritative — server
// marks `ZCP_API_KEY` (a literal bearer token) as `Sensitive=false`
// on eval-zcp (verified live, plans/research/env-types-investigation-
// 2026-05-14.md). LLM downstream may upgrade PromptUser results via
// classify-prompt judgment; envclass returns the bias only.
//
// Empty `Type` (test fixtures using ad-hoc `inventory.ProjectEnvVar{}`
// without setting Type explicitly) is treated as `USER` — the most
// permissive default. Real SDK responses always carry Type.
package envclass

import (
	"regexp"

	"github.com/zeropsio/zcp/internal/ops/inventory"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// Decision is the per-env outcome from a classifier rule.
type Decision int

const (
	// Drop excludes the env from the target import yaml entirely. Rule
	// 1 (service envs) + Rule 2 (project SYSTEM envs).
	Drop Decision = iota
	// PromptUser surfaces the env in the classify-prompt with a Bias
	// suggestion the LLM/user can accept or override. Rule 3 (project
	// USER envs).
	PromptUser
)

// Result carries the classifier's per-env outcome. `Bias` is the
// suggested `topology.SecretClassification` when `Decision == PromptUser`;
// zero (`SecretClassUnset`) when `Decision == Drop`.
type Result struct {
	Decision Decision
	Bias     topology.SecretClassification
}

// credentialPattern flags Key shapes the LLM/user almost certainly
// wants to mark as auto-secret. Case-insensitive suffix match (so
// `APP_KEY`, `MY_SECRET`, `JWT_TOKEN`, `DB_PASS`, `OPENAI_API_KEY`
// all hit). PlainConfig is the fallback bias.
//
// Final say belongs to the LLM/user — bias is a hint, not a verdict.
var credentialPattern = regexp.MustCompile(`(?i)(_KEY|_SECRET|_TOKEN|_PASS|APP_KEY)$`)

// ClassifyServiceEnv applies Rule 1: every service env drops. Source
// service envs are never carried over — the target's own managed
// services regenerate equivalents (accessKeyId, apiUrl, secretAccessKey,
// etc.).
func ClassifyServiceEnv(_ inventory.ServiceEnvVar) Result {
	return Result{Decision: Drop}
}

// ClassifyProjectEnv applies Rule 2 + Rule 3 on project-level envs.
// Type=SYSTEM → Drop. Type=USER (or empty/unset) → PromptUser with
// pattern-based bias.
func ClassifyProjectEnv(env inventory.ProjectEnvVar) Result {
	if env.Type == platform.ProjectEnvSystem {
		return Result{Decision: Drop}
	}
	// Type=USER (or empty default): surface to prompt with bias.
	if credentialPattern.MatchString(env.Key) {
		return Result{Decision: PromptUser, Bias: topology.SecretClassAutoSecret}
	}
	return Result{Decision: PromptUser, Bias: topology.SecretClassPlainConfig}
}
