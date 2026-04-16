package tools

import (
	"strings"
	"testing"
)

// TestPerIGItem_AllStandalone_Pass — every IG item carries its own
// platform-anchor token in the first prose paragraph plus a code block.
// v20 apidev shape (real fixture).
func TestPerIGItem_AllStandalone_Pass(t *testing.T) {
	t.Parallel()
	ig := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n" +
		"### 1. Adding `zerops.yaml`\n\n" +
		"Place this file at the repo root. The Zerops build container reads it for setup directives.\n\n" +
		"```yaml\nzerops: []\n```\n\n" +
		"### 2. Binding to `0.0.0.0`\n\n" +
		"Zerops containers route traffic through an internal L7 balancer. NestJS defaults to 127.0.0.1, unreachable from the balancer.\n\n" +
		"```typescript\nawait app.listen(port, '0.0.0.0');\n```\n\n" +
		"### 3. Configuring CORS with `CORS_ORIGIN`\n\n" +
		"The `CORS_ORIGIN` env var is set per-setup in `zerops.yaml`. Use it at bootstrap so the frontend subdomain is always allowed.\n\n" +
		"```typescript\napp.enableCors({ origin: process.env.CORS_ORIGIN });\n```\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
	checks := checkPerIGItemStandalone(ig, "apidev")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestPerIGItem_LeansOnNeighbor_Fail — IG #2 has only "Setup steps:"
// + code, no platform anchor in prose. Reader has to read IG #1 to
// understand why this matters.
func TestPerIGItem_LeansOnNeighbor_Fail(t *testing.T) {
	t.Parallel()
	ig := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n" +
		"### 1. Adding `zerops.yaml`\n\n" +
		"The Zerops build container reads `zerops.yaml`. Place it at root.\n\n" +
		"```yaml\nzerops: []\n```\n\n" +
		"### 2. Setup steps for the api binding\n\n" +
		"Steps to make this work properly:\n\n" +
		"```typescript\napp.listen(3000);\n```\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
	checks := checkPerIGItemStandalone(ig, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — IG #2 has no platform anchor in prose; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "Setup steps") {
		t.Fatalf("detail must name failing item: %s", checks[0].Detail)
	}
}

// TestPerIGItem_NoCodeBlock_Fail — every IG item must ship a code
// block. Prose-only items violate the contract.
func TestPerIGItem_NoCodeBlock_Fail(t *testing.T) {
	t.Parallel()
	ig := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n" +
		"### 1. Adding `zerops.yaml`\n\n" +
		"Place at root. The Zerops build container reads it.\n\n" +
		"```yaml\nzerops: []\n```\n\n" +
		"### 2. Configuring `CORS_ORIGIN` via the L7 balancer\n\n" +
		"This step requires careful attention to the cross-origin rules enforced by the Zerops L7 balancer at the edge — see the Express docs for details.\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
	checks := checkPerIGItemStandalone(ig, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — IG #2 has prose but no code; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestPerIGItem_EmptyFragment_NoOp — no IG fragment to check.
func TestPerIGItem_EmptyFragment_NoOp(t *testing.T) {
	t.Parallel()
	if checks := checkPerIGItemStandalone("", "apidev"); len(checks) != 0 {
		t.Fatalf("empty fragment → no-op; got %+v", checks)
	}
}

// TestEnvCommentUniqueness_DistinctReasoning_Pass — v20 env-4 shape.
// Each service comment names service-specific mechanism. Token overlap
// on rationale clauses well below threshold.
func TestEnvCommentUniqueness_DistinctReasoning_Pass(t *testing.T) {
	t.Parallel()
	yaml := `services:
  # Svelte SPA — static bundle on Nginx. minContainers 2 because a single
  # Nginx container drops traffic during rolling deploys, and static file
  # serving has near-zero CPU cost per replica so the second container is
  # essentially free.
  - hostname: app
    type: static
  # NestJS API — minContainers 2 because the L7 balancer drains connections
  # from the old container while routing new requests to the fresh one, so
  # at least two replicas must exist for this handoff without a traffic gap.
  - hostname: api
    type: nodejs@22
  # NATS worker — minContainers 2 because a single-container worker drops
  # in-flight jobs during rolling deploys. The queue group subscription
  # (queue: 'workers') ensures exactly-once delivery across replicas.
  - hostname: worker
    type: nodejs@22
`
	checks := checkEnvCommentUniqueness(yaml, "env3")
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestEnvCommentUniqueness_Templated_Fail — agent copy-pastes the same
// rationale to multiple services with only hostname swapped. Must fail.
func TestEnvCommentUniqueness_Templated_Fail(t *testing.T) {
	t.Parallel()
	yaml := `services:
  # The app service runs the application code on a managed runtime container
  # with autoscaling enabled to handle variable load throughout the day.
  - hostname: app
    type: nodejs@22
  # The api service runs the application code on a managed runtime container
  # with autoscaling enabled to handle variable load throughout the day.
  - hostname: api
    type: nodejs@22
  # The worker service runs the application code on a managed runtime container
  # with autoscaling enabled to handle variable load throughout the day.
  - hostname: worker
    type: nodejs@22
`
	checks := checkEnvCommentUniqueness(yaml, "env3")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — three near-identical templated comments; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "app") || !strings.Contains(checks[0].Detail, "api") {
		t.Fatalf("detail must name colliding services: %s", checks[0].Detail)
	}
}

// TestEnvCommentUniqueness_SingleService_NoOp — only one service has
// comments → nothing to compare.
func TestEnvCommentUniqueness_SingleService_NoOp(t *testing.T) {
	t.Parallel()
	yaml := `services:
  # The app service runs the application code on a managed runtime container.
  - hostname: app
    type: nodejs@22
`
	checks := checkEnvCommentUniqueness(yaml, "env3")
	if len(checks) != 0 {
		t.Fatalf("single service → no-op; got %+v", checks)
	}
}
