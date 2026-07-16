package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// errAgentUsage marks a validation failure that must exit 2 without ever
// touching the network (docs/spec-welcome-mode.md §4 W-AUTH: "never accepts
// arbitrary key/value/service arguments"). Every other failure (not in a
// container, platform error) exits 1.
var errAgentUsage = errors.New("usage error")

// agentMarkOAuthOutput is the single-line JSON stdout contract for
// `zcp agent mark-oauth` — field order matches the brief's illustration;
// never carries an env VALUE or token.
type agentMarkOAuthOutput struct {
	OK      bool   `json:"ok"`
	Agent   string `json:"agent"`
	Key     string `json:"key"`
	Changed bool   `json:"changed"`
}

// agentMarkOAuthDeps are runAgentMarkOAuth's collaborators, injected so the
// validation AND execution logic can be tested without a real container or
// real credentials — mirrors the welcome.js DI pattern this command's Go
// side pairs with.
type agentMarkOAuthDeps struct {
	runtimeInfo func() runtime.Info
	client      func() (platform.Client, error)
	stdout      io.Writer
}

func defaultAgentMarkOAuthDeps() agentMarkOAuthDeps {
	return agentMarkOAuthDeps{
		runtimeInfo: runtime.Detect,
		client:      buildAgentPlatformClient,
		stdout:      os.Stdout,
	}
}

// buildAgentPlatformClient mirrors the existing cmd credential-resolution
// path (env -> .mcp.json -> zcli fallback, whatever auth.ResolveCredentials
// currently does — see cmd/zcp/schema.go's schemaActiveVersionsProvider for
// the same pattern) rather than inventing a parallel one. No project
// discovery (auth.Resolve) is needed: mark-oauth already has its serviceID
// from runtime detection.
func buildAgentPlatformClient() (platform.Client, error) {
	creds, err := auth.ResolveCredentials()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		return nil, fmt.Errorf("create platform client: %w", err)
	}
	return client, nil
}

// runAgent handles `zcp agent <subcommand>`.
func runAgent(args []string) {
	if len(args) == 0 || args[0] != "mark-oauth" {
		fmt.Fprintln(os.Stderr, "Usage: zcp agent mark-oauth <agent-id>")
		os.Exit(2)
	}
	if err := runAgentMarkOAuth(args[1:], defaultAgentMarkOAuthDeps()); err != nil {
		fmt.Fprintf(os.Stderr, "zcp agent mark-oauth: %v\n", err)
		if errors.Is(err, errAgentUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// runAgentMarkOAuth implements `zcp agent mark-oauth <agent-id>`
// (docs/spec-welcome-mode.md §4 W-AUTH): validates the agent id against the
// launcher-registry enum with NO network access, then resolves the
// container's service identity and upserts the platform flag through
// ops.MarkAgentOAuth. Kept as a pure, deps-injected function (rather than
// calling os.Exit itself) so validation and execution are both testable —
// see cmd/zcp/agent_test.go.
func runAgentMarkOAuth(args []string, deps agentMarkOAuthDeps) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: expected exactly one argument <agent-id>", errAgentUsage)
	}
	agentID := args[0]
	// Validated here (no network) only to fail fast on a bad id; the key
	// itself is resolved again inside ops.MarkAgentOAuth.
	if _, ok := ops.AgentOAuthEnvKey(agentID); !ok {
		return fmt.Errorf("%w: unknown agent id %q (expected one of: claude-code, codex, antigravity, grok, cursor)", errAgentUsage, agentID)
	}

	rt := deps.runtimeInfo()
	if !rt.InContainer {
		return errors.New("not inside a Zerops container")
	}

	client, err := deps.client()
	if err != nil {
		return fmt.Errorf("agent mark-oauth: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := ops.MarkAgentOAuth(ctx, client, rt.ServiceID, agentID)
	if err != nil {
		return fmt.Errorf("agent mark-oauth: %w", err)
	}

	if err := json.NewEncoder(deps.stdout).Encode(agentMarkOAuthOutput{
		OK:      true,
		Agent:   agentID,
		Key:     result.Key,
		Changed: result.Changed,
	}); err != nil {
		return fmt.Errorf("agent mark-oauth: encode output: %w", err)
	}
	return nil
}
