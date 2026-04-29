package ops

import (
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestEnrichSetupNotFound_AppendsAvailableSetupsAPIMeta pins F3 closure
// (audit-prerelease-internal-testing-2026-04-29). When the platform's
// `zeropsYamlSetupNotFound` 4xx surfaces, the helper appends a fresh
// APIMeta entry naming the available-setups list so the agent doesn't
// have to fetch or guess.
func TestEnrichSetupNotFound_AppendsAvailableSetupsAPIMeta(t *testing.T) {
	t.Parallel()

	pe := &platform.PlatformError{
		Code:    platform.ErrInvalidZeropsYml,
		Message: "Setup name not found",
		APICode: "zeropsYamlSetupNotFound",
	}
	err := EnrichSetupNotFound(pe, "remindersdev", []string{"dev", "prod"})

	var got *platform.PlatformError
	if !errors.As(err, &got) {
		t.Fatalf("EnrichSetupNotFound returned non-PlatformError: %T", err)
	}
	if len(got.APIMeta) != 1 {
		t.Fatalf("APIMeta: want 1 entry, got %d", len(got.APIMeta))
	}
	entry := got.APIMeta[0]
	if entry.Code != "availableSetups" {
		t.Errorf("APIMeta.Code = %q, want %q", entry.Code, "availableSetups")
	}
	if entry.Metadata["attemptedSetup"][0] != "remindersdev" {
		t.Errorf("metadata.attemptedSetup = %v, want [remindersdev]", entry.Metadata["attemptedSetup"])
	}
	if got, want := entry.Metadata["availableSetups"], []string{"dev", "prod"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("metadata.availableSetups = %v, want %v", got, want)
	}
}

// TestEnrichSetupNotFound_NonMatchingAPICode_NoChange pins the
// idempotent / no-op behavior: only `zeropsYamlSetupNotFound` enriches.
// Other PlatformErrors pass through unchanged so unrelated 4xx codes
// don't accumulate spurious metadata.
func TestEnrichSetupNotFound_NonMatchingAPICode_NoChange(t *testing.T) {
	t.Parallel()

	pe := &platform.PlatformError{
		Code:    platform.ErrAPIError,
		APICode: "someOtherCode",
	}
	err := EnrichSetupNotFound(pe, "x", []string{"a", "b"})

	var got *platform.PlatformError
	if !errors.As(err, &got) {
		t.Fatalf("non-PlatformError returned: %T", err)
	}
	if len(got.APIMeta) != 0 {
		t.Errorf("APIMeta: want 0, got %d (helper enriched a non-matching APICode)", len(got.APIMeta))
	}
}

// TestEnrichSetupNotFound_NonPlatformError_PassThrough pins the type
// guard: errors that aren't PlatformError pass through untouched.
func TestEnrichSetupNotFound_NonPlatformError_PassThrough(t *testing.T) {
	t.Parallel()

	plain := errors.New("transport failed")
	got := EnrichSetupNotFound(plain, "x", []string{"a"})
	if got != plain {
		t.Errorf("non-PlatformError: want pass-through, got %v", got)
	}
}

// TestEnrichSetupNotFound_NilError_NilOut pins nil-safety: nil in → nil
// out, no panic on the type-assert path.
func TestEnrichSetupNotFound_NilError_NilOut(t *testing.T) {
	t.Parallel()

	if got := EnrichSetupNotFound(nil, "x", []string{"a"}); got != nil {
		t.Errorf("nil in: want nil out, got %v", got)
	}
}
