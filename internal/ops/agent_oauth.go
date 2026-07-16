package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// agentOAuthSuffixes enumerates the launcher-registry agent ids valid for
// `zcp agent mark-oauth`, mirroring extension.js's REGISTRY ids and suffix
// field (internal/content/templates/vscode-bootstrap-extension.js) — one
// intentional duplication: the Go and JS runtimes share no module boundary
// (like welcome.js's own CRED_PROBE/ZEMBED_DIR duplication). A drift is
// caught by TestAgentOAuthEnvKey_SuffixTable pinning all five ids/suffixes
// together against the same values the JS REGISTRY hardcodes.
var agentOAuthSuffixes = map[string]string{
	"claude-code": "CLAUDE_CODE",
	"codex":       "CODEX",
	"antigravity": "ANTIGRAVITY",
	"grok":        "GROK",
	"cursor":      "CURSOR",
}

// AgentOAuthEnvKey returns the ZCP_AGENT_OAUTH_<SUFFIX> platform-flag env
// key (docs/spec-welcome-mode.md §3 W-STATE) for a launcher-registry agent
// id, and false when agentID is not one of the known five — the single
// owner of the id-enum + suffix mapping, reused by both `zcp agent
// mark-oauth`'s pre-network validation and MarkAgentOAuth's mutation below
// so the two can never drift apart.
func AgentOAuthEnvKey(agentID string) (string, bool) {
	suffix, ok := agentOAuthSuffixes[agentID]
	if !ok {
		return "", false
	}
	return "ZCP_AGENT_OAUTH_" + suffix, true
}

// MarkAgentOAuthResult reports what MarkAgentOAuth did.
type MarkAgentOAuthResult struct {
	Key     string `json:"key"`
	Changed bool   `json:"changed"`
}

// MarkAgentOAuth upserts ZCP_AGENT_OAUTH_<SUFFIX>=true as a SERVICE-scope
// env on serviceID — the platform-flag half of the §3 W-STATE auth matrix.
// `zcp agent mark-oauth` (cmd/zcp/agent.go) calls this after a Tier-A
// terminal login completes locally (spec §4 W-AUTH), so the platform flag,
// the sidebar launcher (env-only), and the Zerops GUI agree with local
// reality.
//
// Check-before-mutate: reads the CURRENT value first and short-circuits
// when it is already "true" — EnvSetSecretService's delete-then-create
// would otherwise perform a redundant mutation (and momentarily blank the
// flag mid-write) on every repeat call, e.g. a second mark-oauth invocation
// racing the credential watcher. Mutation itself routes through the
// existing EnvSetSecretService owner (single owner of the SERVICE-scope
// upsert path) rather than reimplementing delete-then-create here.
func MarkAgentOAuth(ctx context.Context, client platform.Client, serviceID, agentID string) (*MarkAgentOAuthResult, error) {
	if serviceID == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"MarkAgentOAuth: serviceID required", "")
	}
	key, ok := AgentOAuthEnvKey(agentID)
	if !ok {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("MarkAgentOAuth: unknown agent id %q", agentID), "")
	}

	existing, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("mark agent oauth: read existing env: %w", err)
	}
	for _, e := range existing {
		if e.Key == key && e.Content == "true" {
			return &MarkAgentOAuthResult{Key: key, Changed: false}, nil
		}
	}

	if _, err := EnvSetSecretService(ctx, client, serviceID, key, "true"); err != nil {
		return nil, fmt.Errorf("mark agent oauth: %w", err)
	}
	return &MarkAgentOAuthResult{Key: key, Changed: true}, nil
}
