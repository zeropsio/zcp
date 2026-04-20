// Tests for: internal/tools/workflow_checks_comment_depth.go
package tools

import (
	"strings"
	"testing"
)

// v7GoldCommentSample is a representative v7 env import.yaml comment
// block. Each comment explains WHY, WHAT-BREAKS, or HOW-THIS-AFFECTS
// operations — the reasoning rubric the check enforces.
const v7GoldCommentSample = `#zeropsPreprocessor=on

# JWT_SECRET is project-scoped — at production scale this is critical because
# a session-cookie validation fails if any container in the L7 pool disagrees on
# the signing key. Project-level placement guarantees both api containers see
# the same value. Service-level envSecrets would force every container to be
# redeployed when the key rotates.
project:
  name: nestjs-showcase

services:
  # Small production NestJS API. minContainers:2 spreads requests across two
  # containers and lets rolling deploys complete without an outage. zsc execOnce
  # gates migrations so only one container per deploy version runs them while
  # the others wait.
  - hostname: api
    type: nodejs@22

  # Production worker. minContainers:2 means the demo.jobs subject is
  # consumed by two queue-group members — NATS load-balances messages between
  # them, so a single container restart doesn't pause processing. SIGTERM drains
  # in-flight jobs cleanly.
  - hostname: worker
    type: nodejs@22

  # PostgreSQL — single-node production database with verticalAutoscaling so
  # RAM grows with table size. Both api containers and both worker containers
  # read/write the same tasks table here.
  - hostname: db
    type: postgresql@18
`

// v16NarrationSample matches the v16 regression style: comments
// describe what fields do without explaining the reasoning.
const v16NarrationSample = `# Small production environment for moderate throughput.

project:
  name: nestjs-showcase

services:
  # Static Svelte SPA — Nginx serves the compiled bundle.
  - hostname: app
    type: static

  # NestJS API on port 3000 with TypeORM and Meilisearch integration enabled.
  - hostname: api
    type: nodejs@22

  # NestJS NATS worker on port 3001 handles background jobs from the API.
  - hostname: worker
    type: nodejs@22

  # PostgreSQL 18 database for application data storage in single-node mode.
  - hostname: db
    type: postgresql@18

  # Valkey 7.2 cache store for session data and cached responses.
  - hostname: redis
    type: valkey@7.2
`

func TestCheckCommentDepth_V7StylePasses(t *testing.T) {
	t.Parallel()
	checks := checkCommentDepth(t.Context(), v7GoldCommentSample, "env")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != statusPass {
		t.Errorf("v7 gold sample should pass comment_depth; got fail:\n%s", checks[0].Detail)
	}
}

func TestCheckCommentDepth_V16NarrationFails(t *testing.T) {
	t.Parallel()
	checks := checkCommentDepth(t.Context(), v16NarrationSample, "env")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != statusFail {
		t.Errorf("v16 narration sample should fail comment_depth; got pass")
	}
	if !strings.Contains(checks[0].Detail, "WHY") {
		t.Errorf("fail detail should reference the WHY rule, got: %s", checks[0].Detail)
	}
}

func TestCheckCommentDepth_FewCommentsSilentlyPass(t *testing.T) {
	t.Parallel()
	// Under 3 substantive comments — the check passes silently so
	// the comment_ratio check handles the "too few comments" case.
	input := `# Short label here does not matter.
project:
  name: tiny
`
	checks := checkCommentDepth(t.Context(), input, "env")
	if len(checks) != 1 || checks[0].Status != statusPass {
		t.Errorf("expected silent pass for sparse comments, got %+v", checks)
	}
}

func TestCheckCommentDepth_MixedRealistic(t *testing.T) {
	t.Parallel()
	// 4 substantive comments, 2 with markers (50%). Should pass.
	input := `# Development environment for local iteration on the recipe.
# JWT_SECRET is rotated here because the dev subdomain hash changes per-branch.
# Frontend: Vite dev server with hot-reload; run.base is nodejs@22 so the container
# has a Node runtime — static base would have no shell for npm run dev.
# API: shares env vars with prod so behavior mirrors prod at runtime.
`
	checks := checkCommentDepth(t.Context(), input, "env")
	if checks[0].Status != statusPass {
		t.Errorf("mixed realistic sample should pass, got fail: %s", checks[0].Detail)
	}
}

// TestContainsAny moved to internal/ops/checks alongside the
// containsAny helper (post-C-7d). The tool-layer is a thin wrapper
// that no longer owns the predicate body.
