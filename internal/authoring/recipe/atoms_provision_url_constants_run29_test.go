package recipe

import (
	"strings"
	"testing"
)

// Run-29 prep — provision.md gains a discrete step 4 for cross-service
// URL constants (between APP_SECRET at step 3 and Mount at step 5).
// Pre-fix the agent inferred URL-constant set-up at scaffold time, which
// was inconsistent (run-28 set them; run-29 mid-flight missed them).
//
// The new step uses the renamed convention: API_URL / FRONTEND_URL
// (project-scope; tier-conditional meaning) plus DEV_API_URL /
// DEV_FRONTEND_URL (dev-pair tiers only).

func TestProvisionAtom_HasURLConstantsStep4(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/provision.md")
	if err != nil {
		t.Fatalf("read provision.md: %v", err)
	}
	if !strings.Contains(body, "4. **Set cross-service URL constants**") {
		t.Errorf("provision.md missing new step 4 heading for URL constants")
	}
}

func TestProvisionAtom_StepRenumbered(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/provision.md")
	if err != nil {
		t.Fatalf("read provision.md: %v", err)
	}
	// Step renumbering: Mount → 5, Catalog → 6, Complete → 7, Advance → 8.
	for _, want := range []string{
		"5. **Mount dev codebases**",
		"6. **Catalog cross-service env var keys**",
		"7. **Complete the phase**",
		"8. **Advance to scaffold**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("provision.md missing renumbered step %q", want)
		}
	}
}

func TestProvisionAtom_URLConstantsTeachConstructionTemplate(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/provision.md")
	if err != nil {
		t.Fatalf("read provision.md: %v", err)
	}
	// New convention: API_URL / FRONTEND_URL (no STAGE_ prefix), with
	// DEV_API_URL / DEV_FRONTEND_URL still naming the dev-pair side.
	for _, want := range []string{
		"API_URL=https://",
		"FRONTEND_URL=https://",
		"DEV_API_URL=https://",
		"DEV_FRONTEND_URL=https://",
		"${zeropsSubdomainHost}",
		"projectEnvVars",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("provision.md step 4 missing construction-template anchor %q", want)
		}
	}
}

func TestProvisionAtom_URLConstantsNoStagePrefix(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/provision.md")
	if err != nil {
		t.Fatalf("read provision.md: %v", err)
	}
	// The legacy STAGE_* names must not survive in current teaching.
	for _, forbid := range []string{"STAGE_API_URL", "STAGE_FRONTEND_URL"} {
		if strings.Contains(body, forbid) {
			t.Errorf("provision.md still references legacy %q (rename incomplete)", forbid)
		}
	}
}

func TestProvisionAtom_URLConstantsTwoChannels(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/provision.md")
	if err != nil {
		t.Fatalf("read provision.md: %v", err)
	}
	// Both channels — live workspace (zerops_env) AND plan record
	// (update-plan projectEnvVars) — must be taught together.
	if !strings.Contains(body, "zerops_env project=true action=set") {
		t.Errorf("provision.md step 4 missing zerops_env channel teaching")
	}
	if !strings.Contains(body, "update-plan") {
		t.Errorf("provision.md step 4 missing update-plan channel teaching")
	}
}

// TestCrossServiceURLsAtom_NoStagePrefix pins the rename across the
// principles atom that scaffold sub-agents fetch when teaching the
// build-time bake trap. After the run-29 prep the literal STAGE_ token
// disappears from current teaching surfaces; only DEV_* keeps a prefix
// (it names a real distinct dev-pair value).
func TestCrossServiceURLsAtom_NoStagePrefix(t *testing.T) {
	t.Parallel()
	body, err := readAtom("principles/cross-service-urls.md")
	if err != nil {
		t.Fatalf("read cross-service-urls.md: %v", err)
	}
	for _, forbid := range []string{"STAGE_API_URL", "STAGE_FRONTEND_URL"} {
		if strings.Contains(body, forbid) {
			t.Errorf("cross-service-urls.md still references legacy %q (rename incomplete)", forbid)
		}
	}
	// Positive shape: the renamed keys are present.
	for _, want := range []string{"API_URL", "FRONTEND_URL", "DEV_API_URL", "DEV_FRONTEND_URL"} {
		if !strings.Contains(body, want) {
			t.Errorf("cross-service-urls.md missing renamed key %q", want)
		}
	}
}
