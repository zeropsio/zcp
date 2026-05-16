//go:build live

// Package tools — live test harness. Files in this package with build
// tag `live` exercise ZCP handlers against the real Zerops platform
// (eval-zcp project) and other live infrastructure (GitHub, container
// SSH). They are NOT part of the default test suite; run with:
//
//   go test -tags live ./internal/tools/ -run TestLive -v -timeout 30m
//
// Required environment:
//   ZCP_API_KEY            project-scoped token on eval-zcp source project
//                          (read by /Users/macbook/Documents/Zerops-MCP/zcp/.mcp.json
//                          but tests read directly from env to keep harness
//                          self-contained).
//   ZCP_E2E_LAUNCH_KEY     account-wide one-shot LaunchKey with
//                          canCreateProjects=true. Required for Test 4
//                          (launch-production) only.
//   ZCP_E2E_GITHUB_PAT     fine-grained PAT for a writable test repo.
//                          Required for Tests 2/3 (git-push-setup,
//                          build-integration) which read the PAT shape.
//
// Why a build tag: live tests cost real platform API calls + may create/
// delete projects. They're operator-driven, not CI-driven. Build tag
// keeps them out of `go test ./...`. Karel's directive (2026-05-16): no
// release of v9.92 until each of the four flows (export, git-push-setup,
// build-integration, launch-production) passes a live test against
// eval-zcp.
package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// liveAPIHost mirrors `defaultAPIHost` used elsewhere in the e2e tests.
// Tests can override via ZCP_API_HOST if pointing at a non-prg1 region.
func liveAPIHost() string {
	if h := os.Getenv("ZCP_API_HOST"); h != "" {
		return h
	}
	return "api.app-prg1.zerops.io"
}

// requireLiveEnv reads an env var or skips the test. Mirrors the
// e2e/launch_baseline_test.go pattern.
func requireLiveEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("%s not set — skipping live test", key)
	}
	return v
}

// liveSourceClient builds a platform.Client from ZCP_API_KEY and resolves
// the source project ID (expected to be the only project the token can
// access — eval-zcp). Returns (client, sourceProjectID, clientID).
func liveSourceClient(t *testing.T) (platform.Client, string, string) {
	t.Helper()
	apiKey := requireLiveEnv(t, "ZCP_API_KEY")
	c, err := platform.NewZeropsClient(apiKey, liveAPIHost())
	if err != nil {
		t.Fatalf("source client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, err := c.GetUserInfo(ctx)
	if err != nil {
		t.Fatalf("source GetUserInfo: %v", err)
	}
	if info == nil || info.ID == "" {
		t.Fatalf("source token did not resolve to a client")
	}
	projects, err := c.ListProjects(ctx, info.ID)
	if err != nil {
		t.Fatalf("source ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("ZCP_API_KEY expected to resolve exactly 1 project (eval-zcp); got %d", len(projects))
	}
	return c, projects[0].ID, info.ID
}

// liveTestCtx returns a deadline-bound context for the test. d=0 →
// default 5 min. Test 4 (launch-production) passes 15 min.
func liveTestCtx(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	if d <= 0 {
		d = 5 * time.Minute
	}
	return context.WithTimeout(context.Background(), d)
}
