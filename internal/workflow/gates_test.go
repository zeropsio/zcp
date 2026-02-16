// Tests for: workflow gates — evidence-based phase transition checks.
package workflow

import (
	"testing"
	"time"
)

func TestCheckGate_AllGatesPass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-gates"

	// Save all required evidence.
	evidenceTypes := []string{"recipe_review", "discovery", "dev_verify", "deploy_evidence", "stage_verify"}
	for _, typ := range evidenceTypes {
		ev := &Evidence{
			SessionID: sessionID, Type: typ, VerificationType: "attestation",
			Timestamp: time.Now().Format(time.RFC3339),
		}
		if err := SaveEvidence(dir, sessionID, ev); err != nil {
			t.Fatalf("SaveEvidence(%s): %v", typ, err)
		}
	}

	tests := []struct {
		name string
		from Phase
		to   Phase
		mode Mode
	}{
		{"G0_init_to_discover", PhaseInit, PhaseDiscover, ModeFull},
		{"G1_discover_to_develop", PhaseDiscover, PhaseDevelop, ModeFull},
		{"G2_develop_to_deploy", PhaseDevelop, PhaseDeploy, ModeFull},
		{"G3_deploy_to_verify", PhaseDeploy, PhaseVerify, ModeFull},
		{"G4_verify_to_done", PhaseVerify, PhaseDone, ModeFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := CheckGate(tt.from, tt.to, tt.mode, dir, sessionID)
			if err != nil {
				t.Fatalf("CheckGate: %v", err)
			}
			if !result.Passed {
				t.Errorf("expected gate to pass, missing: %v", result.Missing)
			}
		})
	}
}

func TestCheckGate_MissingEvidence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-missing"

	tests := []struct {
		name        string
		from        Phase
		to          Phase
		mode        Mode
		wantGate    string
		wantMissing []string
	}{
		{
			"G0_missing_recipe_review",
			PhaseInit, PhaseDiscover, ModeFull,
			"G0", []string{"recipe_review"},
		},
		{
			"G1_missing_discovery",
			PhaseDiscover, PhaseDevelop, ModeFull,
			"G1", []string{"discovery"},
		},
		{
			"G2_missing_dev_verify",
			PhaseDevelop, PhaseDeploy, ModeFull,
			"G2", []string{"dev_verify"},
		},
		{
			"G3_missing_deploy_evidence",
			PhaseDeploy, PhaseVerify, ModeFull,
			"G3", []string{"deploy_evidence"},
		},
		{
			"G4_missing_stage_verify",
			PhaseVerify, PhaseDone, ModeFull,
			"G4", []string{"stage_verify"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := CheckGate(tt.from, tt.to, tt.mode, dir, sessionID)
			if err != nil {
				t.Fatalf("CheckGate: %v", err)
			}
			if result.Passed {
				t.Error("expected gate to fail")
			}
			if result.Gate != tt.wantGate {
				t.Errorf("gate: want %s, got %s", tt.wantGate, result.Gate)
			}
			if len(result.Missing) != len(tt.wantMissing) {
				t.Fatalf("missing count: want %d, got %d (%v)", len(tt.wantMissing), len(result.Missing), result.Missing)
			}
			for i, m := range tt.wantMissing {
				if result.Missing[i] != m {
					t.Errorf("missing[%d]: want %s, got %s", i, m, result.Missing[i])
				}
			}
		})
	}
}

func TestCheckGate_G0ConditionalSkip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-conditional"

	// Save fresh discovery evidence (less than 24h old).
	ev := &Evidence{
		SessionID: sessionID, Type: "discovery", VerificationType: "attestation",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	// G0 should pass without recipe_review when discovery is fresh.
	result, err := CheckGate(PhaseInit, PhaseDiscover, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected G0 to pass with fresh discovery, missing: %v", result.Missing)
	}
}

func TestCheckGate_G0StaleDiscovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-stale"

	// Save stale discovery evidence (more than 24h old).
	staleTime := time.Now().Add(-25 * time.Hour).Format(time.RFC3339)
	ev := &Evidence{
		SessionID: sessionID, Type: "discovery", VerificationType: "attestation",
		Timestamp: staleTime,
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	// G0 should fail — discovery is stale and no recipe_review.
	result, err := CheckGate(PhaseInit, PhaseDiscover, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if result.Passed {
		t.Error("expected G0 to fail with stale discovery and no recipe_review")
	}
}

func TestCheckGate_ModeAware_DevOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-devonly"

	// DevOnly has no DEPLOY or VERIFY phases, so G2/G3/G4 should not apply.
	// The only gates in dev_only: G0 (INIT→DISCOVER), G1 (DISCOVER→DEVELOP).
	// DEVELOP→DONE has no gate (it's a mode shortcut).
	tests := []struct {
		name     string
		from     Phase
		to       Phase
		wantGate string
	}{
		{"G0_applies", PhaseInit, PhaseDiscover, "G0"},
		{"G1_applies", PhaseDiscover, PhaseDevelop, "G1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := CheckGate(tt.from, tt.to, ModeDevOnly, dir, sessionID)
			if err != nil {
				t.Fatalf("CheckGate: %v", err)
			}
			if result.Gate != tt.wantGate {
				t.Errorf("gate: want %s, got %s", tt.wantGate, result.Gate)
			}
		})
	}
}

func TestCheckGate_ModeAware_Hotfix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-hotfix"

	// Hotfix: INIT→DEVELOP (no gate/G0 — no DISCOVER phase), DEVELOP→DEPLOY (G2), DEPLOY→VERIFY (G3), VERIFY→DONE (G4).
	// INIT→DEVELOP should have no gate.
	result, err := CheckGate(PhaseInit, PhaseDevelop, ModeHotfix, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected no gate for hotfix INIT→DEVELOP, missing: %v", result.Missing)
	}
	if result.Gate != "" {
		t.Errorf("expected empty gate for hotfix INIT→DEVELOP, got %s", result.Gate)
	}
}

func TestCheckGate_QuickMode_NoGates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-quick"

	// Quick mode has no phases and no gates.
	result, err := CheckGate(PhaseInit, PhaseDiscover, ModeQuick, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if !result.Passed {
		t.Error("expected quick mode to always pass (no gates)")
	}
}

func TestCheckGate_InvalidTransition(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Skip a phase — invalid transition.
	_, err := CheckGate(PhaseInit, PhaseDeploy, ModeFull, dir, "sess-x")
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
}

func TestGateName_Coverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from Phase
		to   Phase
		want string
	}{
		{PhaseInit, PhaseDiscover, "G0"},
		{PhaseDiscover, PhaseDevelop, "G1"},
		{PhaseDevelop, PhaseDeploy, "G2"},
		{PhaseDeploy, PhaseVerify, "G3"},
		{PhaseVerify, PhaseDone, "G4"},
		{PhaseInit, PhaseDevelop, ""},
		{PhaseDevelop, PhaseDone, ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			t.Parallel()
			got := GateName(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("GateName(%s, %s): want %q, got %q", tt.from, tt.to, tt.want, got)
			}
		})
	}
}
