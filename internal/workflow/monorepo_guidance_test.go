package workflow

import (
	"strings"
	"testing"
)

// renderedAtomBody synthesizes env and returns atomID's body with whitespace
// collapsed, failing when the atom did not render.
func renderedAtomBody(t *testing.T, env StateEnvelope, atomID string) string {
	t.Helper()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	out, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	for _, m := range out {
		if m.AtomID == atomID {
			return strings.Join(strings.Fields(m.Body), " ")
		}
	}
	t.Fatalf("%s did not render", atomID)
	return ""
}

// TestMonorepoGuidance_ReachesTheAgentWhenItDecides pins the guidance a repo
// with several parts needs at the step where the agent makes the decision it
// governs: the bootstrap plan, the import and the first zerops.yaml.
func TestMonorepoGuidance_ReachesTheAgentWhenItDecides(t *testing.T) {
	t.Parallel()

	scenario := func(id string) StateEnvelope {
		for _, sc := range canonicalGoldenScenarios() {
			if sc.id == id {
				return sc.envelope
			}
		}
		t.Fatalf("no golden scenario %q", id)
		return StateEnvelope{}
	}
	discover := scenario("bootstrap/classic/discover-standard-dynamic")
	firstDeploy := scenario("develop/first-deploy-dev-dynamic-container")

	t.Run("a classic plan for a repo with several parts reads the monorepo playbook and names a stage per part before it is submitted", func(t *testing.T) {
		t.Parallel()
		body := renderedAtomBody(t, discover, "bootstrap-classic-plan-dynamic")
		requireAll(t, body, "several deployable parts", `zerops_knowledge uri="zerops://playbooks/monorepo"`, "`stages`")
	})

	t.Run("the first zerops.yaml holds a dev setup plus one setup per part, never a setup per hostname", func(t *testing.T) {
		t.Parallel()
		body := renderedAtomBody(t, firstDeploy, "develop-first-deploy-scaffold-yaml")
		requireAll(t, body, "`setup: dev`", "one setup per deployable part")
		for _, stale := range []string{"matches the runtime hostname", "per targeted runtime hostname", "per targeted hostname"} {
			if strings.Contains(body, stale) {
				t.Errorf("scaffold guidance still asks for a setup per hostname (%q):\n%s", stale, body)
			}
		}
	})

	t.Run("every port a dev server will listen on is declared at the first deploy, because a port added later takes a redeploy", func(t *testing.T) {
		t.Parallel()
		body := renderedAtomBody(t, firstDeploy, "develop-first-deploy-existing-repo")
		requireAll(t, body, "`run.ports` with every port a dev server will listen on", "a port added later takes a redeploy")
	})

	t.Run("an import sizes the hobby PostgreSQL that every part's dev and stage share for all their connection pools", func(t *testing.T) {
		t.Parallel()
		body := renderedAtomBody(t, scenario("bootstrap/classic/provision-local"), "bootstrap-provision-rules")
		requireAll(t, body, "connection pool", "every part's dev and stage", "port `6432`")
	})

	t.Run("an import gives a Node.js stage enough RAM to start", func(t *testing.T) {
		t.Parallel()
		body := renderedAtomBody(t, scenario("bootstrap/classic/provision-local"), "bootstrap-provision-rules")
		requireAll(t, body, "| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes | `1.0` for Node.js |")
	})
}
