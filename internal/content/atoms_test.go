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

// TestDevelopReadyToDeployAtom_ManualSubdomainFallback pins B12: when
// the post-recovery code deploy doesn't auto-enable the L7 subdomain
// (eval `develop-first-deploy-branch` saw `http_root: skip "subdomain
// not enabled"` after a `startWithoutCode override=true` re-import +
// code deploy), the atom must surface the one-shot manual recovery —
// `zerops_subdomain action="enable"` — so the agent doesn't have to
// derive it from a verify hint. This is reactive guidance grounded in
// the eval observation; we deliberately do NOT bake in a particular
// hypothesis about WHY auto-enable misses (that's a separate
// investigation against the live platform, not an atom claim).
func TestDevelopReadyToDeployAtom_ManualSubdomainFallback(t *testing.T) {
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

	if !strings.Contains(body, `zerops_subdomain action="enable"`) {
		t.Error("atom must name the `zerops_subdomain action=\"enable\"` manual fallback for cases where post-recovery auto-enable misses (B12 eval observation)")
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

// TestDevelopDeployModesAtom_CrossDeployYamlLocation pins B6: the
// deploy-modes atom must spell out where pre-flight searches for
// zerops.yaml AND that the source mount is excluded. Tier-3 eval
// `bootstrap-recipe-static-simple` hit `PREFLIGHT_FAILED: zerops.yaml
// not found: tried /var/www/appstage, /var/www` because the agent
// scaffolded under `/var/www/appdev/` (source mount) and pre-flight
// only consults the per-target mount + project root. The atom names
// the search order plus the project-root recommendation so the agent
// places the yaml correctly the first time.
func TestDevelopDeployModesAtom_CrossDeployYamlLocation(t *testing.T) {
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

	if !strings.Contains(body, "Where pre-flight finds zerops.yaml") {
		t.Error("atom must contain a `### Where pre-flight finds zerops.yaml` subsection")
	}
	if !strings.Contains(body, "source mount is never searched") {
		t.Error("atom must explicitly state that the source mount is excluded from pre-flight search — that's the cross-deploy gotcha")
	}
	if !strings.Contains(body, "project root") {
		t.Error("atom must recommend project root as the canonical location for shared zerops.yaml in standard pairs")
	}
}
