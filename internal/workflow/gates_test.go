// Tests for: workflow gates — evidence-based phase transition checks.
package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestCheckGate_AllGatesPass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-gates"

	// Save all required evidence with valid content (Passed > 0, non-empty attestation).
	evidenceTypes := []string{"recipe_review", "discovery", "dev_verify", "deploy_evidence", "stage_verify"}
	for _, typ := range evidenceTypes {
		ev := &Evidence{
			SessionID: sessionID, Type: typ, VerificationType: "attestation",
			Timestamp:   time.Now().Format(time.RFC3339),
			Attestation: "verified successfully",
			Passed:      1,
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

// --- ValidateEvidence tests ---

func TestValidateEvidence_ZeroPassed_ZeroFailed_Passes(t *testing.T) {
	t.Parallel()
	// Vacuous evidence (Passed==0, Failed==0) is acceptable — auto-complete of skipped steps.
	ev := &Evidence{Type: "dev_verify", Attestation: "looks good", Passed: 0, Failed: 0}
	if err := ValidateEvidence(ev); err != nil {
		t.Errorf("expected no error for vacuous evidence, got: %v", err)
	}
}

func TestValidateEvidence_NonZeroFailed_Fails(t *testing.T) {
	t.Parallel()
	ev := &Evidence{Type: "dev_verify", Attestation: "some failed", Passed: 2, Failed: 1}
	err := ValidateEvidence(ev)
	if err == nil {
		t.Fatal("expected error for non-zero failed")
	}
	if !strings.Contains(err.Error(), "1 failure(s)") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateEvidence_EmptyAttestation_Fails(t *testing.T) {
	t.Parallel()
	ev := &Evidence{Type: "dev_verify", Attestation: "", Passed: 1, Failed: 0}
	err := ValidateEvidence(ev)
	if err == nil {
		t.Fatal("expected error for empty attestation")
	}
	if !strings.Contains(err.Error(), "empty attestation") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateEvidence_Valid_Passes(t *testing.T) {
	t.Parallel()
	ev := &Evidence{Type: "dev_verify", Attestation: "all checks passed", Passed: 3, Failed: 0}
	if err := ValidateEvidence(ev); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestCheckGate_EvidenceWithFailures_Blocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-failures"

	ev := &Evidence{
		SessionID: sessionID, Type: "discovery", VerificationType: "attestation",
		Timestamp: time.Now().Format(time.RFC3339), Attestation: "has failures",
		Passed: 2, Failed: 1,
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	result, err := CheckGate(PhaseDiscover, PhaseDevelop, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if result.Passed {
		t.Error("expected gate to fail when evidence has failures")
	}
	if len(result.Failures) == 0 {
		t.Error("expected non-empty Failures list")
	}
}

func TestCheckGate_SessionMismatch_Blocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-current"

	// Save evidence with a different session ID.
	ev := &Evidence{
		SessionID: "sess-other", Type: "discovery", VerificationType: "attestation",
		Timestamp: time.Now().Format(time.RFC3339), Attestation: "verified",
		Passed: 1,
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	result, err := CheckGate(PhaseDiscover, PhaseDevelop, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if result.Passed {
		t.Error("expected gate to fail on session mismatch")
	}
	if len(result.Failures) == 0 {
		t.Error("expected non-empty Failures for session mismatch")
	}
}

// --- Multi-service verification tests ---

func TestCheckGate_MultiService_FailedService_Blocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-multi-fail"

	ev := &Evidence{
		SessionID: sessionID, Type: "dev_verify", VerificationType: "attestation",
		Timestamp: time.Now().Format(time.RFC3339), Attestation: "multi-service check",
		Passed: 2, Failed: 0,
		ServiceResults: []ServiceResult{
			{Hostname: "appdev", Status: "pass", Detail: "ok"},
			{Hostname: "apidev", Status: "fail", Detail: "connection refused"},
		},
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	result, err := CheckGate(PhaseDevelop, PhaseDeploy, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if result.Passed {
		t.Error("expected gate to fail when a service result has status=fail")
	}
	if len(result.Failures) == 0 {
		t.Error("expected non-empty Failures for failed service result")
	}
}

func TestCheckGate_MultiService_AllPassed_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-multi-pass"

	ev := &Evidence{
		SessionID: sessionID, Type: "stage_verify", VerificationType: "attestation",
		Timestamp: time.Now().Format(time.RFC3339), Attestation: "all services ok",
		Passed: 2, Failed: 0,
		ServiceResults: []ServiceResult{
			{Hostname: "appstage", Status: "pass"},
			{Hostname: "apistage", Status: "pass"},
		},
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	result, err := CheckGate(PhaseVerify, PhaseDone, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected gate to pass, missing=%v failures=%v", result.Missing, result.Failures)
	}
}

func TestCheckGate_MultiService_EmptyResults_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-multi-empty"

	// Backward compat: no ServiceResults is fine.
	ev := &Evidence{
		SessionID: sessionID, Type: "dev_verify", VerificationType: "attestation",
		Timestamp: time.Now().Format(time.RFC3339), Attestation: "verified",
		Passed: 1, Failed: 0,
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	result, err := CheckGate(PhaseDevelop, PhaseDeploy, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected gate to pass with empty ServiceResults, missing=%v failures=%v", result.Missing, result.Failures)
	}
}

// --- Evidence freshness tests ---

func TestCheckGate_StaleEvidence_Blocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-stale-ev"

	// Save discovery evidence that is 25h old.
	staleTime := time.Now().Add(-25 * time.Hour).Format(time.RFC3339)
	ev := &Evidence{
		SessionID: sessionID, Type: "discovery", VerificationType: "attestation",
		Timestamp: staleTime, Attestation: "discovered", Passed: 1,
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	// G1 (DISCOVER→DEVELOP) should fail because discovery evidence is stale.
	result, err := CheckGate(PhaseDiscover, PhaseDevelop, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if result.Passed {
		t.Error("expected gate to fail with stale evidence")
	}
	if len(result.Failures) == 0 {
		t.Error("expected non-empty Failures for stale evidence")
	}
}

func TestCheckGate_FreshEvidence_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionID := "sess-fresh-ev"

	// Save fresh discovery evidence.
	ev := &Evidence{
		SessionID: sessionID, Type: "discovery", VerificationType: "attestation",
		Timestamp: time.Now().Format(time.RFC3339), Attestation: "discovered", Passed: 1,
	}
	if err := SaveEvidence(dir, sessionID, ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	result, err := CheckGate(PhaseDiscover, PhaseDevelop, ModeFull, dir, sessionID)
	if err != nil {
		t.Fatalf("CheckGate: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected gate to pass with fresh evidence, missing=%v failures=%v", result.Missing, result.Failures)
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
