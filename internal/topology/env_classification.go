package topology

import "regexp"

// classifyInfrastructureKeys is the exact-key allowlist of source-project
// envs that always bucket as infrastructure during export / launch-production
// classification — irrespective of credentialPattern match. These keys are
// platform-injected control-plane values; the destination project will
// re-emit its own equivalents at launch-time (GIT_TOKEN on git-push setup,
// ZCP_* during ZCP container init), so carrying source values forward
// would either be redundant or actively wrong.
//
// Closed-set policy by design (per atoms/launch-classify-platform-envs.md
// §"matches by exact key only"): a stray `ZCP_CUSTOM_USER_THING` falls
// through to the agent's classification as a normal USER env. Prefix
// matching would silently absorb future user-named ZCP_* envs into the
// drop bucket without their consent.
var classifyInfrastructureKeys = map[string]bool{
	"ZCP_API_KEY": true,
	// ZCP_AGENT_TYPE / ZCP_AGENT_TYPES are the legacy launcher env — no
	// longer emitted, but existing services still carry it; classifying it
	// stays until every fleet service has rotated off it.
	"ZCP_AGENT_TYPE":  true,
	"ZCP_AGENT_TYPES": true,
	// ZCP_AGENTS is the current zcp-owned available-agent presentation
	// policy env (replaces ZCP_AGENT_TYPES).
	"ZCP_AGENTS": true,
	"GIT_TOKEN":  true,
	// Staged launch token (single-token launch lifecycle). The literal
	// must match ops.LaunchTokenEnvKey — topology imports stdlib only, so
	// the tie is pinned by TestLaunchTokenEnvKey_ClassifiedInfrastructure
	// in internal/tools (which imports both packages).
	"ZCP_LAUNCH_TOKEN": true,
}

// IsClassifyInfrastructure reports whether the env key is in the
// hard-wired infrastructure bucket. See classifyInfrastructureKeys for
// the policy. The check is exact-key — no prefix sniff.
func IsClassifyInfrastructure(key string) bool {
	return classifyInfrastructureKeys[key]
}

// CredentialPattern flags env key shapes that almost certainly hold a
// credential value the LLM/user wants bucketed as auto-secret. Case-
// insensitive suffix match (so `APP_KEY`, `MY_SECRET`, `JWT_TOKEN`,
// `DB_PASS`, `OPENAI_API_KEY` all hit). PlainConfig is the fallback bias
// when this pattern misses.
//
// Lives in topology so both the envclass classifier (which assigns bias)
// and the classify-prompt response builders (which surface rationale to
// the agent) read the same pattern without an import cycle. Bias is a
// hint, not a verdict — agent overrides when the key happens to also
// resolve to a managed-service reference or to plain config.
var CredentialPattern = regexp.MustCompile(`(?i)(_KEY|_SECRET|_TOKEN|_PASS|APP_KEY)$`)
