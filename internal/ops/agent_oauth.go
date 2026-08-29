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
// oauthAuthorizedValue is the value ZCP_AGENT_OAUTH_<SUFFIX> carries when an
// agent is authorized — the platform stores the flag as the string "true".
const oauthAuthorizedValue = "true"

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

// MarkAgentOAuthResult reports what MarkAgentOAuth did. Migrated is true
// only when a pre-existing SENSITIVE row was rewritten non-sensitive (see
// MarkAgentOAuth's doc comment) — omitted from JSON on every other path so
// the common create/no-op cases stay a two-field line.
type MarkAgentOAuthResult struct {
	Key      string `json:"key"`
	Changed  bool   `json:"changed"`
	Migrated bool   `json:"migrated,omitempty"`
}

// MarkAgentOAuth upserts ZCP_AGENT_OAUTH_<SUFFIX>=true as a SERVICE-scope,
// NON-SENSITIVE env on serviceID — the platform-flag half of the §3
// W-STATE auth matrix. `zcp agent mark-oauth` (cmd/zcp/agent.go) calls this
// after a Tier-A terminal login completes locally (spec §4 W-AUTH), so the
// platform flag, the sidebar launcher (env-only), and the Zerops GUI agree
// with local reality.
//
// Written sensitive:false deliberately (spec-welcome-mode.md §4.2): the
// flag is boolean metadata, not a secret, and the Zerops GUI's own
// POST /user-data/search read path redacts sensitive content — even for
// the org owner — so a sensitive row renders as NOT authorized in the GUI
// though the platform holds it "true". The GUI's own writer already writes
// the flag non-sensitive; this matches it.
//
// Check-before-mutate reads the CURRENT row first, keyed on Sensitive, not
// Content — a sensitive row's Content can read back "REDACTED" for a
// read-only token, which can never be compared against oauthAuthorizedValue:
//   - absent                              -> create, Changed:true
//   - present, non-sensitive, content true -> no-op, Changed:false
//   - present, non-sensitive, other value  -> set,   Changed:true
//   - present, SENSITIVE (any content)     -> migrate (delete + recreate
//     non-sensitive), Changed:true, Migrated:true — repairs a row written
//     before this fix, or by a stale binary
//
// Every mutating branch routes through the existing EnvSetService owner
// (single owner of the SERVICE-scope upsert path) rather than
// reimplementing delete-then-create here.
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
		if e.Key != key {
			continue
		}
		if !e.Sensitive && e.Content == oauthAuthorizedValue {
			return &MarkAgentOAuthResult{Key: key, Changed: false}, nil
		}
		migrated := e.Sensitive
		if _, err := EnvSetService(ctx, client, serviceID, key, oauthAuthorizedValue, false); err != nil {
			return nil, fmt.Errorf("mark agent oauth: %w", err)
		}
		return &MarkAgentOAuthResult{Key: key, Changed: true, Migrated: migrated}, nil
	}

	if _, err := EnvSetService(ctx, client, serviceID, key, oauthAuthorizedValue, false); err != nil {
		return nil, fmt.Errorf("mark agent oauth: %w", err)
	}
	return &MarkAgentOAuthResult{Key: key, Changed: true}, nil
}
