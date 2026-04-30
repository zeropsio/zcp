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
