// Tests for: Cluster D (run-14 §2.D) — operational preempts that
// recurred in run-13 (git-identity, zcli-scope, Vite host-allowlist,
// browser-walk staleness, watcher PID volatility, scaffold close
// ordering). Content-only — phase-entry / atom / brief extensions.

package recipe

import (
	"strings"
	"testing"
)

// TestPhaseEntry_ScaffoldCarriesGitIdentitySection — D.1 (R-13-13).
// Phase-entry scaffold prescribes git identity on the dev container so
// SSH_DEPLOY_FAILED on the default-identity error never fires.
func TestPhaseEntry_ScaffoldCarriesGitIdentitySection(t *testing.T) {
	t.Parallel()
	body := loadPhaseEntry(PhaseScaffold)
	if !strings.Contains(body, "## Git identity on the dev container") {
		t.Error("scaffold phase-entry missing 'Git identity on the dev container' section")
	}
	if !strings.Contains(body, "git config") {
		t.Error("scaffold phase-entry git-identity section missing the git config command")
	}
}

// TestPrinciples_MountVsContainerCarriesZcliScope — D.2 (R-13-14).
// Mount-vs-container atom names zcli as a host-side tool that doesn't
// work inside the dev container.
func TestPrinciples_MountVsContainerCarriesZcliScope(t *testing.T) {
	t.Parallel()
	body, err := readAtom("principles/mount-vs-container.md")
	if err != nil {
		t.Fatalf("readAtom: %v", err)
	}
	if !strings.Contains(body, "## zcli scope") {
		t.Error("mount-vs-container atom missing 'zcli scope' section")
	}
}

// TestScaffoldBrief_FrontendCarriesBuildToolHostAllowlist — D.3
// (R-13-15, third recurrence). When the codebase is a frontend on a
// nodejs runtime base, the scaffold brief carries a positive shape
// for the build-tool host-allowlist knob (Vite / Webpack / Rollup).
func TestScaffoldBrief_FrontendCarriesBuildToolHostAllowlist(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	var frontend Codebase
	found := false
	for _, cb := range plan.Codebases {
		if cb.Role == RoleFrontend && strings.HasPrefix(cb.BaseRuntime, "nodejs") {
			frontend = cb
			found = true
			break
		}
	}
	if !found {
		t.Fatal("synthetic plan has no frontend nodejs codebase fixture")
	}
	brief, err := BuildScaffoldBrief(plan, frontend, nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "## Build-tool host-allowlist") {
		t.Error("frontend scaffold brief missing 'Build-tool host-allowlist' section")
	}
	if !strings.Contains(brief.Body, "allowedHosts") {
		t.Error("build-tool host-allowlist section missing the canonical Vite knob")
	}
}

// TestScaffoldBrief_NonFrontendOmitsBuildToolHostAllowlist — D.3 scope
// guard. API/worker scaffolds don't author bundler config; the section
// stays out of those briefs to keep them slim.
func TestScaffoldBrief_NonFrontendOmitsBuildToolHostAllowlist(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	var api Codebase
	for _, cb := range plan.Codebases {
		if cb.Role == RoleAPI {
			api = cb
			break
		}
	}
	brief, err := BuildScaffoldBrief(plan, api, nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if strings.Contains(brief.Body, "## Build-tool host-allowlist") {
		t.Error("API scaffold brief should not carry the bundler host-allowlist section")
	}
}

// TestShowcaseScenarioAtom_CarriesStableSelectors — D.4 (R-13-16).
// The showcase-scenario atom teaches stable data-* selectors for
// browser-walk verification so a stale per-snapshot ref never
// silently no-ops a click.
func TestShowcaseScenarioAtom_CarriesStableSelectors(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("readAtom: %v", err)
	}
	if !strings.Contains(body, "### Stable selectors") {
		t.Error("showcase scenario missing 'Stable selectors' subsection")
	}
	if !strings.Contains(body, "data-feature") {
		t.Error("stable-selectors subsection missing data-feature attribute")
	}
}

// TestPrinciples_DevLoopCarriesNestWatcherPIDNote — D.5 (R-13-17).
// Dev-loop atom warns about nest-watcher PID rotation so an agent
// doesn't conclude the worker is down based on a stale pidfile.
func TestPrinciples_DevLoopCarriesNestWatcherPIDNote(t *testing.T) {
	t.Parallel()
	body, err := readAtom("principles/dev-loop.md")
	if err != nil {
		t.Fatalf("readAtom: %v", err)
	}
	if !strings.Contains(body, "watcher PID") && !strings.Contains(body, "watcher pid") {
		t.Error("dev-loop atom missing the watcher-PID volatility note")
	}
}

// TestPhaseEntry_ScaffoldCarriesCloseSequence — D.6 (R-13-20).
// Phase-entry scaffold names the deploy → verify → complete-phase
// ordering explicitly so main doesn't waste a turn calling
// complete-phase before deploy on the run-14 dogfood.
func TestPhaseEntry_ScaffoldCarriesCloseSequence(t *testing.T) {
	t.Parallel()
	body := loadPhaseEntry(PhaseScaffold)
	if !strings.Contains(body, "Scaffold close") || !strings.Contains(body, "deploy") || !strings.Contains(body, "verify") {
		t.Error("scaffold phase-entry missing explicit close sequence (deploy → verify → complete-phase)")
	}
}
