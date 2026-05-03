package content

import (
	"regexp"
	"strings"
	"testing"
)

// TestReadAllAtoms_ReturnsSortedMarkdown confirms the embedded atoms are
// discoverable, non-empty, and returned in a deterministic order.
func TestReadAllAtoms_ReturnsSortedMarkdown(t *testing.T) {
	t.Parallel()

	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	if len(atoms) == 0 {
		t.Fatal("expected at least one embedded atom")
	}
	for i := 1; i < len(atoms); i++ {
		if atoms[i-1].Name >= atoms[i].Name {
			t.Errorf("atoms not sorted: %s >= %s", atoms[i-1].Name, atoms[i].Name)
		}
	}
	for _, a := range atoms {
		if !strings.HasPrefix(a.Content, "---\n") {
			t.Errorf("atom %s missing frontmatter opening", a.Name)
		}
		if !strings.HasSuffix(a.Name, ".md") {
			t.Errorf("atom %s not .md", a.Name)
		}
	}
}

// TestDevelopReadyToDeployAtom_NoSelfDeployContradiction pins the B9 fix:
// the original atom listed self-deploy via `zerops_deploy targetService=...`
// as a recovery path for a never-deployed runtime, contradicting its own
// "no zerops_deploy" prohibition above. DeploySSH SSHes to the source
// service; for a READY_TO_DEPLOY runtime the source container isn't up,
// so the call fails with `Could not resolve hostname`. The canonical
// recovery is `zerops_import startWithoutCode=true override=true`. This
// test asserts: (1) the canonical recovery path is named, (2) the atom
// shows no `zerops_deploy targetService=` example at all (any example
// would re-introduce the contradiction).
func TestDevelopReadyToDeployAtom_NoSelfDeployContradiction(t *testing.T) {
	t.Parallel()

	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	var body string
	for _, a := range atoms {
		if a.Name == "develop-ready-to-deploy.md" {
			body = a.Content
			break
		}
	}
	if body == "" {
		t.Fatal("develop-ready-to-deploy.md not found in corpus")
	}

	if !strings.Contains(body, "startWithoutCode: true") || !strings.Contains(body, "override=true") {
		t.Error("atom must name `startWithoutCode: true` + `override=true` re-import as canonical recovery")
	}

	deployLine := regexp.MustCompile(`zerops_deploy[^\n]*targetService=`)
	if deployLine.MatchString(body) {
		t.Error("atom must not show a `zerops_deploy targetService=...` example — self-deploy on READY_TO_DEPLOY is fictional (DeploySSH source unreachable), and the recovery is re-import, not deploy")
	}
}

// TestDevelopPlatformRulesLocalAtom_BackgroundTaskCallout pins B15: the
// local-mode platform rules must promote the "dev server runs through
// the harness background-task primitive" guardrail OUT of the table
// into a dedicated section with the failure mode named explicitly.
// Phase 1.5 eval `develop-dev-server-local` hit max-turns because the
// agent ran `php artisan serve` foreground and the bash channel blocked
// — a class of failure that recurs on every local-mode dev-server
// scenario when the guardrail is buried in a table cell. The section
// must show both the canonical pattern AND an anti-pattern callout so
// recognition fires before invocation.
func TestDevelopPlatformRulesLocalAtom_BackgroundTaskCallout(t *testing.T) {
	t.Parallel()

	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	var body string
	for _, a := range atoms {
		if a.Name == "develop-platform-rules-local.md" {
			body = a.Content
			break
		}
	}
	if body == "" {
		t.Fatal("develop-platform-rules-local.md not found in corpus")
	}

	if !strings.Contains(body, "### Dev server") {
		t.Error("dev-server guidance must live in its own H3 subsection — burying it in the table cell was the B15 failure mode")
	}
	if !strings.Contains(body, "run_in_background=true") {
		t.Error("atom must show the canonical `run_in_background=true` example")
	}
	if !strings.Contains(body, "Anti-pattern") {
		t.Error("atom must include an Anti-pattern callout — recognition of the foreground-bash trap before invocation depends on the example being adjacent to the rule")
	}
}

// TestDevelopDeployModesAtom_NoProjectRootYamlGuidance pins the inverse
// of the original B6 fix: the deploy-modes atom must NOT reintroduce the
// "place yaml at project root" / "source mount is never searched"
// guidance that commit e769c9f7 added in response to a preflight bug
// (yaml was looked up by target hostname instead of source). The bug was
// fixed at the root in `deployPreFlight` — yaml lookup now resolves from
// the source service's SSHFS mount, mirroring `ops.deploySSH`. The atom's
// previous documentation taught a workaround for a layer that no longer
// exists; recipe layouts (yaml at the source repo root, lands on source
// mount via buildFromGit) now Just Work for both self-deploy and
// cross-deploy without manual copy/symlink.
//
// If this test fails, the atom is teaching agents to do work that ZCP
// already handles — drift back into the e769c9f7-class symptom-doctrine.
func TestDevelopDeployModesAtom_NoProjectRootYamlGuidance(t *testing.T) {
	t.Parallel()

	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	var body string
	for _, a := range atoms {
		if a.Name == "develop-deploy-modes.md" {
			body = a.Content
			break
		}
	}
	if body == "" {
		t.Fatal("develop-deploy-modes.md not found in corpus")
	}

	for _, forbidden := range []string{
		"Where pre-flight finds zerops.yaml",
		"source mount is never searched",
		"copy (or symlink) it to the project root",
		"Place a single shared `zerops.yaml` at the project root",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("atom must not reintroduce %q — preflight reads yaml from the source mount; project-root copies are not part of the architecture", forbidden)
		}
	}
}

// TestDevelopFirstDeployIntroAtom_CloseModePrerequisite pins B7: the
// first-deploy intro atom must surface the `closeDeployMode` gate so
// an agent who finishes deploy + verify and waits for auto-close has
// the explicit "set close-mode" tool call without having to derive it
// from `workSessionState.reason`. Tier-3 eval
// `bootstrap-classic-node-standard` had to figure this out reactively.
//
// The example syntax must NOT use the `{services-list:...}` directive
// — this atom's frontmatter has no `multiService: aggregate` field, so
// the synthesizer leaves the directive unexpanded and agents see the
// raw template literal. Plain-text with a `<host>` placeholder renders
// correctly in any atom context.
func TestDevelopFirstDeployIntroAtom_CloseModePrerequisite(t *testing.T) {
	t.Parallel()

	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	var body string
	for _, a := range atoms {
		if a.Name == "develop-first-deploy-intro.md" {
			body = a.Content
			break
		}
	}
	if body == "" {
		t.Fatal("develop-first-deploy-intro.md not found in corpus")
	}

	if !strings.Contains(body, "closeDeployMode") {
		t.Error("atom must name `closeDeployMode` as the auto-close gate so agents know what blocks the close")
	}
	if !strings.Contains(body, `action="close-mode"`) {
		t.Error("atom must show the `zerops_workflow action=\"close-mode\"` invocation — knowing the field name without the call shape costs a turn")
	}
	if strings.Contains(body, "{services-list:") {
		t.Error("atom must not use the `{services-list:...}` directive — this atom is non-aggregate (multiService unset), so the directive ships as raw text. Use plain syntax with a `<host>` placeholder instead.")
	}
}
