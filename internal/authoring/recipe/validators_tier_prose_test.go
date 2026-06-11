package recipe

import (
	"context"
	"strings"
	"testing"
)

// TestValidateTierProseVsEmit_FlagsReplicaCountMismatch — run-13 §V.
// A service-block comment claiming a replica count that disagrees
// with the field's emitted minContainers is a structural mismatch.
// Tier 5 emits minContainers=2; comment claims 3 → flag.
func TestValidateTierProseVsEmit_FlagsReplicaCountMismatch(t *testing.T) {
	t.Parallel()

	body := []byte(`services:
  # Three replicas because production scale demands it.
  - hostname: api
    type: nodejs@22
    minContainers: 2
`)
	plan := syntheticShowcasePlan()
	vs := validateTierProseVsEmit("5 — Highly-available Production/import.yaml", body, SurfaceInputs{Plan: plan})
	if !containsCode(vs, "tier-prose-replica-count-mismatch") {
		t.Errorf("expected tier-prose-replica-count-mismatch, got %+v", vs)
	}
}

// TestValidateTierProseVsEmit_FlagsHAClaimVsNonHA — run-13 §V. The
// Meilisearch downgrade at tier 5 (managed family doesn't support HA
// → emitter writes mode: NON_HA) means any prose claiming "HA" /
// "high-availability" / "backed-up" / "replicated" for that service
// block is divergent from the field 6 lines below.
func TestValidateTierProseVsEmit_FlagsHAClaimVsNonHA(t *testing.T) {
	t.Parallel()

	body := []byte(`services:
  # Meilisearch in HA mode keeps a backup of the index.
  - hostname: search
    type: meilisearch@1.20
    mode: NON_HA
`)
	plan := syntheticShowcasePlan()
	vs := validateTierProseVsEmit("5 — Highly-available Production/import.yaml", body, SurfaceInputs{Plan: plan})
	if !containsCode(vs, "tier-prose-ha-claim-vs-non-ha") {
		t.Errorf("expected tier-prose-ha-claim-vs-non-ha, got %+v", vs)
	}
}

// TestValidateTierProseVsEmit_FlagsStorageSizeMismatch — run-13 §V.
// Object-storage emits objectStorageSize: 1 uniformly; prose claiming
// 10 GB / 50 GB / etc. is divergent.
func TestValidateTierProseVsEmit_FlagsStorageSizeMismatch(t *testing.T) {
	t.Parallel()

	body := []byte(`services:
  # Bucket sized at 50 GB for production fan-out.
  - hostname: storage
    type: object-storage
    objectStorageSize: 1
    objectStoragePolicy: private
`)
	plan := syntheticShowcasePlan()
	vs := validateTierProseVsEmit("5 — Highly-available Production/import.yaml", body, SurfaceInputs{Plan: plan})
	if !containsCode(vs, "tier-prose-storage-size-mismatch") {
		t.Errorf("expected tier-prose-storage-size-mismatch, got %+v", vs)
	}
}

// TestValidateTierProseVsEmit_AcceptsConsistentClaim — run-13 §V. A
// "two replicas" prose paired with minContainers: 2 is fine.
func TestValidateTierProseVsEmit_AcceptsConsistentClaim(t *testing.T) {
	t.Parallel()

	body := []byte(`services:
  # Two replicas because rolling deploys need a sibling.
  - hostname: api
    type: nodejs@22
    minContainers: 2
`)
	plan := syntheticShowcasePlan()
	vs := validateTierProseVsEmit("4 — Small Production/import.yaml", body, SurfaceInputs{Plan: plan})
	if containsCode(vs, "tier-prose-replica-count-mismatch") {
		t.Errorf("did not expect mismatch, got %+v", vs)
	}
}

// TestValidateTierProseVsEmit_FuzzyClaimDoesNotFire — run-13 §V risk
// mitigation. Edge-case prose ("production replicas", "scales
// horizontally", "multi-replica") doesn't carry numbers and shouldn't
// trip the detector.
func TestValidateTierProseVsEmit_FuzzyClaimDoesNotFire(t *testing.T) {
	t.Parallel()

	body := []byte(`services:
  # Scales horizontally with multi-replica fan-out for production.
  - hostname: api
    type: nodejs@22
    minContainers: 2
`)
	plan := syntheticShowcasePlan()
	vs := validateTierProseVsEmit("5 — Highly-available Production/import.yaml", body, SurfaceInputs{Plan: plan})
	if containsCode(vs, "tier-prose-replica-count-mismatch") {
		t.Errorf("fuzzy claim should not flag, got %+v", vs)
	}
}

// TestValidateEnvImportComments_WiresTierProseValidator — run-13 §V.
// validateEnvImportComments now delegates to validateTierProseVsEmit
// so the structural-relation check fires inside the existing surface
// validator slot.
func TestValidateEnvImportComments_WiresTierProseValidator(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	body := []byte(`services:
  # Three replicas because production scale.
  - hostname: api
    type: nodejs@22
    minContainers: 2
`)
	vs, err := validateEnvImportComments(context.Background(),
		"5 — Highly-available Production/import.yaml", body, SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validateEnvImportComments: %v", err)
	}
	if !containsCode(vs, "tier-prose-replica-count-mismatch") {
		t.Errorf("expected wired tier-prose-replica-count-mismatch from import-comments validator, got %+v", vs)
	}
}

// TestValidateTierProseVsEmit_AllNoticesNotBlocking — run-13 §V wired
// as Notice severity. §T (the brief teaching) is the load-bearing
// fix; §V is backstop. Promotion to Blocking needs a dogfood run.
func TestValidateTierProseVsEmit_AllNoticesNotBlocking(t *testing.T) {
	t.Parallel()

	body := []byte(`services:
  # Three replicas because production scale.
  - hostname: api
    type: nodejs@22
    minContainers: 2
  # Bucket sized at 50 GB.
  - hostname: storage
    type: object-storage
    objectStorageSize: 1
  # Meilisearch HA mode keeps a backup.
  - hostname: search
    type: meilisearch@1.20
    mode: NON_HA
`)
	plan := syntheticShowcasePlan()
	vs := validateTierProseVsEmit("5 — Highly-available Production/import.yaml", body, SurfaceInputs{Plan: plan})
	if len(vs) == 0 {
		t.Fatalf("expected at least one violation, got none")
	}
	for _, v := range vs {
		if !strings.HasPrefix(v.Code, "tier-prose-") {
			continue
		}
		if v.Severity != SeverityNotice {
			t.Errorf("violation %q expected SeverityNotice, got %q", v.Code, v.Severity)
		}
	}
}
