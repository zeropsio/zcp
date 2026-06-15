package recipe

import (
	"testing"
)

// Run-17 §11 — deployFiles narrowness validator. Closes R-17-C8.

func TestValidateDeployFilesNarrowness_Referenced_NoViolation(t *testing.T) {
	t.Parallel()
	body := []byte(`zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm run build
      deployFiles:
        - dist
        - node_modules
        - package.json
    run:
      base: nodejs@22
      start: node dist/main.js
      initCommands:
        - node dist/scripts/migrate.js
`)
	vs := validateDeployFilesNarrowness(t.Context(), "/x/zerops.yaml", body, SurfaceInputs{})
	if len(vs) != 0 {
		t.Errorf("expected no violations when every entry is referenced; got %v", vs)
	}
}

func TestValidateDeployFilesNarrowness_Unreferenced_Violation(t *testing.T) {
	t.Parallel()
	// src/scripts ships but run.start / run.initCommands invoke from
	// dist/scripts/. No field_rationale fact justifies the entry —
	// classic R-17-C8 path-mismatch.
	body := []byte(`zerops:
  - setup: prod
    build:
      base: nodejs@22
      deployFiles:
        - dist
        - src/scripts
    run:
      base: nodejs@22
      start: node dist/main.js
      initCommands:
        - node dist/scripts/migrate.js
`)
	vs := validateDeployFilesNarrowness(t.Context(), "/x/zerops.yaml", body, SurfaceInputs{})
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation for unreferenced src/scripts; got %d (%v)", len(vs), vs)
	}
	if vs[0].Code != "deploy-files-unreferenced" {
		t.Errorf("violation code = %q, want deploy-files-unreferenced", vs[0].Code)
	}
	if vs[0].Severity != SeverityNotice {
		t.Errorf("violation severity = %v, want SeverityNotice", vs[0].Severity)
	}
}

func TestValidateDeployFilesNarrowness_FieldRationale_NoViolation(t *testing.T) {
	t.Parallel()
	body := []byte(`zerops:
  - setup: prod
    build:
      base: nodejs@22
      deployFiles:
        - dist
        - src/scripts
    run:
      base: nodejs@22
      start: node dist/main.js
`)
	facts := []FactRecord{
		{
			Topic:     "api-src-scripts-shipped",
			Kind:      FactKindFieldRationale,
			FieldPath: "build.deployFiles[src/scripts]",
			Why:       "src/scripts ships TypeScript sources for ad-hoc ts-node debugging at runtime.",
		},
	}
	vs := validateDeployFilesNarrowness(t.Context(), "/x/zerops.yaml", body, SurfaceInputs{Facts: facts})
	if len(vs) != 0 {
		t.Errorf("expected no violations when entry is backed by field_rationale; got %v", vs)
	}
}

func TestValidateDeployFilesNarrowness_DevSetup_Skipped(t *testing.T) {
	t.Parallel()
	// Dev setups ship `.` by construction; narrowness only applies to
	// prod-class setups.
	body := []byte(`zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles:
        - .
    run:
      base: nodejs@22
      start: npm run start:dev
`)
	vs := validateDeployFilesNarrowness(t.Context(), "/x/zerops.yaml", body, SurfaceInputs{})
	if len(vs) != 0 {
		t.Errorf("dev setup should be skipped; got %v", vs)
	}
}
