package recipe

import (
	"strings"
	"testing"
)

// TestScaffoldAtom_StageCrossDeploy — run-8-readiness §2.B.1 requires
// the scaffold phase to cross-deploy dev→stage and verify, proving the
// prod setup path works (optimized build, `./dist/~` deployFiles), not
// just the dev self-deploy.
func TestScaffoldAtom_StageCrossDeploy(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseScaffold)
	for _, anchor := range []string{
		"targetService=<hostname>stage",
		"Cross-deploy dev → stage",
		"prod setup path",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("scaffold atom missing stage-cross-deploy anchor %q", anchor)
		}
	}
}

// TestScaffoldAtom_InitCommandsVerification — §2.B.6 requires the
// procedural verification half of the init-commands model: read logs
// for success lines, query app state directly, burned-key recovery
// via file-touch + redeploy.
func TestScaffoldAtom_InitCommandsVerification(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseScaffold)
	for _, anchor := range []string{
		"zerops_logs serviceHostname=<hostname>",
		"post-deploy data check",
		"Burned-key recovery",
		"touch any source file",
	} {
		if !strings.Contains(body, anchor) {
			// Some anchors use variant casing; broaden the check.
			low := strings.ToLower(body)
			if !strings.Contains(low, strings.ToLower(anchor)) {
				t.Errorf("scaffold atom missing init-commands-verification anchor %q", anchor)
			}
		}
	}
}

// TestFeatureAtom_SeedStep — §2.B.2 requires a seed step so tier 4/5
// porters see populated dashboards on first click-deploy.
func TestFeatureAtom_SeedStep(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseFeature)
	for _, anchor := range []string{
		"Seed data",
		"click-deploy",
		"static execOnce key",
		"init-commands-model",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("feature atom missing seed-step anchor %q", anchor)
		}
	}
}

// TestFeatureAtom_BrowserVerification — §2.B.3 + Q4 resolution: the
// feature phase records a `browser_verification` fact per feature tab
// with console/screenshot capture in evidence.
func TestFeatureAtom_BrowserVerification(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseFeature)
	for _, anchor := range []string{
		"zerops_browser",
		"browser-verification",
		"console",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("feature atom missing browser-verification anchor %q", anchor)
		}
	}
}

// TestFeatureAtom_StageCrossDeploy — §2.B.4 every feature-touched
// codebase cross-deploys dev→stage at feature close.
func TestFeatureAtom_StageCrossDeploy(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseFeature)
	for _, anchor := range []string{
		"Cross-deploy dev → stage",
		"targetService=<h>stage",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("feature atom missing feature-stage-cross-deploy anchor %q", anchor)
		}
	}
}

// TestFinalizeAtom_SurfaceTestQuestions — §2.B.5 finalize atom carries
// the one-question test for each surface so the main agent filters
// fragments before recording.
func TestFinalizeAtom_SurfaceTestQuestions(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseFinalize)
	for _, anchor := range []string{
		"Can a reader decide in 30 seconds",
		"Does this teach me when to outgrow this",
		"not narrate what the",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("finalize atom missing surface-test-question anchor %q", anchor)
		}
	}
}

// TestAtoms_WrapperDiscipline — §2.B "Wrapper discipline refinement"
// requires scaffold + feature atoms to explicitly separate
// main-decides from sub-agent-discovers.
func TestAtoms_WrapperDiscipline(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"scaffold", "feature"} {
		body := loadPhaseEntry(Phase(phase))
		for _, anchor := range []string{
			"main decides",
			"sub-agent discovers",
			"zerops_knowledge",
		} {
			if !strings.Contains(strings.ToLower(body), strings.ToLower(anchor)) {
				t.Errorf("%s atom missing wrapper-discipline anchor %q", phase, anchor)
			}
		}
	}
}
