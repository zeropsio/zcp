// Tests for: internal/tools/workflow_checks_zerops_yml_depth.go
package tools

import (
	"strings"
	"testing"
)

// v7GoldZeropsYmlSample — the v7-era zerops.yaml comment shape:
// inline reasoning attached to specific fields, explaining WHY each
// value is chosen and WHAT BREAKS if the decision flips.
const v7GoldZeropsYmlSample = `zerops:
  - setup: dev
    # nodejs@22 rather than nodejs@20 because the NestJS TypeORM driver
    # requires the structuredClone polyfill that only ships in Node 22.
    # Downgrading here causes seed.ts to crash on startup.
    build:
      base: nodejs@22
      # prepareCommands pre-installs dependencies at build time so the
      # deploy container starts with warm node_modules — without this
      # the dev container blocks on npm ci every single rebuild.
      prepareCommands:
        - npm ci
    run:
      # 0.0.0.0 because the L7 balancer terminates connections at the edge
      # and forwards to the container on the project-internal network.
      # Binding to 127.0.0.1 makes the balancer return 502.
      start: node dist/main.js

  - setup: prod
    build:
      base: nodejs@22
      # Production builds must run buildCommands separately from dev so that
      # dist/ is emitted at build time, not runtime. Without this the prod
      # container starts without dist/main.js and crashes with ENOENT.
      buildCommands:
        - npm ci
        - npm run build
`

// v22RegressionSample — comments describe WHAT the field does without
// explaining the reasoning. Copy-paste from framework docs. Passes the
// coarse comment-ratio check but teaches the reader nothing.
const v22RegressionSample = `zerops:
  - setup: dev
    # NestJS dev setup with TypeScript compilation
    build:
      base: nodejs@22
      # Install dependencies
      prepareCommands:
        - npm ci
    run:
      # Start the application
      start: node dist/main.js

  - setup: prod
    build:
      # Production build with tree-shaking
      base: nodejs@22
      # Compile TypeScript
      buildCommands:
        - npm ci
        - npm run build
`

func TestZeropsYmlCommentDepth_SufficientReasoning_Passes(t *testing.T) {
	t.Parallel()
	checks := checkZeropsYmlCommentDepth(v7GoldZeropsYmlSample, "apidev")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != statusPass {
		t.Errorf("v7-style zerops.yaml should pass comment_depth; got fail:\n%s", checks[0].Detail)
	}
}

func TestZeropsYmlCommentDepth_FieldNarrationOnly_Fails(t *testing.T) {
	t.Parallel()
	checks := checkZeropsYmlCommentDepth(v22RegressionSample, "apidev")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != statusFail {
		t.Errorf("field-narration sample should fail comment_depth; got pass")
	}
	if !strings.Contains(checks[0].Detail, "WHY") && !strings.Contains(checks[0].Detail, "why") {
		t.Errorf("fail detail should reference the WHY rule, got: %s", checks[0].Detail)
	}
}

func TestZeropsYmlCommentDepth_NoComments_SilentPass(t *testing.T) {
	t.Parallel()
	input := `zerops:
  - setup: dev
    build:
      base: nodejs@22
`
	checks := checkZeropsYmlCommentDepth(input, "apidev")
	if len(checks) != 1 || checks[0].Status != statusPass {
		t.Errorf("sparse-comments yaml → silent pass; got %+v", checks)
	}
}

func TestZeropsYmlCommentDepth_HardFloorOfTwo_EnforcedEvenAboveRatio(t *testing.T) {
	t.Parallel()
	// 3 substantive blocks, 1 with marker (33%). Above 35% ratio boundary
	// but below absolute floor of 2 reasoning comments.
	input := `zerops:
  - setup: dev
    # nodejs@22 because the ORM requires structuredClone.
    build:
      base: nodejs@22
      # Installing dependencies for deploy.
      prepareCommands:
        - npm ci
      # Compile the TypeScript source and bundle.
      start: node dist/main.js
`
	checks := checkZeropsYmlCommentDepth(input, "apidev")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != statusFail {
		t.Errorf("below hard floor of 2 reasoning comments → should fail; got pass")
	}
}

func TestZeropsYmlCommentDepth_CheckNamePrefixed(t *testing.T) {
	t.Parallel()
	checks := checkZeropsYmlCommentDepth(v7GoldZeropsYmlSample, "workerdev")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	want := "workerdev_zerops_yml_comment_depth"
	if checks[0].Name != want {
		t.Errorf("check name = %q, want %q", checks[0].Name, want)
	}
}

func TestZeropsYmlCommentDepth_EmptyContent_NoOp(t *testing.T) {
	t.Parallel()
	checks := checkZeropsYmlCommentDepth("", "apidev")
	if len(checks) != 0 {
		t.Errorf("empty content → no-op; got %+v", checks)
	}
}
