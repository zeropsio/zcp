package workflow

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// existingRepoAtom is the guidance that runs an existing repository's own dev
// flow in the dev service.
const existingRepoAtom = "develop-first-deploy-existing-repo"

// existingRepoBody renders the first-deploy develop guidance of a never-deployed
// container dev service of typeVersion under intent and returns the matched
// atom IDs and the existing-repo atom's body ("" when it did not render).
func existingRepoBody(t *testing.T, typeVersion string, mode topology.Mode, intent string) (ids []string, body string) {
	t.Helper()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	session := fixSession("appdev")
	session.Intent = intent
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvContainer,
		Services:    []ServiceSnapshot{fixSnapBootstrappedNeverDeployedUnset("appdev", typeVersion, topology.RuntimeDynamic, mode)},
		WorkSession: session,
	}
	out, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	for _, m := range out {
		ids = append(ids, m.AtomID)
		if m.AtomID == existingRepoAtom {
			body = strings.Join(strings.Fields(m.Body), " ")
		}
	}
	return ids, body
}

func requireAll(t *testing.T, body string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(body, n) {
			t.Errorf("guidance lacks %q:\n%s", n, body)
		}
	}
}

// TestDevelopExistingRepo_RunsTheReposOwnDevFlow pins how an agent asked to
// develop or run an existing repository here gets the repository's own dev
// flow in the dev service instead of a new environment built as a deploy.
func TestDevelopExistingRepo_RunsTheReposOwnDevFlow(t *testing.T) {
	t.Parallel()

	t.Run("before writing any setup the agent finds how the repo itself is developed and runs exactly that", func(t *testing.T) {
		t.Parallel()
		_, body := existingRepoBody(t, "nodejs@24", topology.ModeDev, "run this repo here")
		requireAll(t, body,
			"before you write any setup",
			"`AGENTS.md`", "`CONTRIBUTING.md`", "README", "package scripts", "`Makefile`", "`.devcontainer/`", "`Procfile`", "compose file",
			"Install as its docs say",
			`zerops_dev_server action=start hostname="appdev" name="<name>" command="<the repo's dev command>"`,
			"the command exactly as the repo runs it",
		)
	})

	t.Run("the dev setup stays minimal and dependencies install in the running container, not through build cycles", func(t *testing.T) {
		t.Parallel()
		_, body := existingRepoBody(t, "nodejs@24", topology.ModeDev, "develop it here")
		requireAll(t, body,
			"The dev setup stays minimal",
			"`run.prepareCommands`",
			"no dependency install in `buildCommands`",
			"the first deploy puts its `run.start` under the dev-server supervisor",
			`ssh appdev "cd /var/www && <the repo's install command>"`,
			"A restart keeps the installed dependencies",
			"a redeploy replaces the container with what git carries",
			"`notCarried`",
		)
		intro, err := os.ReadFile("../content/atoms/develop-first-deploy-intro.md")
		if err != nil {
			t.Fatalf("read intro atom: %v", err)
		}
		if strings.Contains(strings.Join(strings.Fields(string(intro)), " "), "dependencies arrive with this first deploy's build") {
			t.Error("the first-deploy intro still says an existing repo's dependencies arrive with the first deploy's build")
		}
	})

	t.Run("an install over SSH runs without waiting for input and uses the package manager the image already ships", func(t *testing.T) {
		t.Parallel()
		_, body := existingRepoBody(t, "nodejs@24", topology.ModeDev, "run this repo here")
		requireAll(t, body,
			"Nothing answers a prompt over SSH: prefix the command with `CI=true` and end it with `</dev/null`",
			"A package manager the run image already ships is used as it stands",
			"`EEXIST`",
		)
	})

	t.Run("a dev server is reached through the repo's own host knob and never through a proxy the agent adds", func(t *testing.T) {
		t.Parallel()
		_, body := existingRepoBody(t, "nodejs@24", topology.ModeDev, "get it running")
		requireAll(t, body,
			"the repo's own host or allowed-hosts setting",
			"a dev mode the repo documents for remote use",
			"Never put your own proxy (socat, a Node pipe) in front of a dev server",
			"tell the person plainly that it listens only on loopback",
			"do not improvise",
		)
	})

	t.Run("the dev service is sized before the first heavy start through the existing scaling path", func(t *testing.T) {
		t.Parallel()
		_, body := existingRepoBody(t, "nodejs@24", topology.ModeDev, "run it")
		requireAll(t, body,
			"before the first install",
			"the repo's own hints",
			`zerops_scale serviceHostname="appdev" minRam=8 minCpu=2`,
		)
	})

	t.Run("the guidance renders whenever develop starts on a never-deployed dev service in a container, without a playbook call", func(t *testing.T) {
		t.Parallel()
		for _, mode := range []topology.Mode{topology.ModeDev, topology.ModeStandard} {
			ids, body := existingRepoBody(t, "nodejs@24", mode, "x")
			if body == "" {
				t.Errorf("mode %s: %s did not render; got %v", mode, existingRepoAtom, ids)
			}
		}
		atom := readAtomFile(t, existingRepoAtom)
		if strings.Contains(atom, "zerops_knowledge") {
			t.Errorf("%s sends the agent to pull-only knowledge instead of carrying the rules", existingRepoAtom)
		}
	})

	t.Run("nothing keys on the prompt wording, a repo name or one stack", func(t *testing.T) {
		t.Parallel()
		intents := []string{"rozbehni mi tady https://github.com/zeropsio/mate", "develop my Django app here", ""}
		renders := make([][]string, 0, len(intents))
		for _, intent := range intents {
			ids, _ := existingRepoBody(t, "nodejs@24", topology.ModeDev, intent)
			renders = append(renders, ids)
		}
		for _, ids := range renders[1:] {
			if !slices.Equal(ids, renders[0]) {
				t.Errorf("the rendered atoms depend on the intent: %v vs %v", ids, renders[0])
			}
		}
		for _, tv := range []string{"python@3.12", "go@1", "ruby@3.4", "ubuntu/nodejs@24"} {
			if _, body := existingRepoBody(t, tv, topology.ModeDev, "x"); body == "" {
				t.Errorf("%s did not render for %s", existingRepoAtom, tv)
			}
		}
		atom := strings.ToLower(readAtomFile(t, existingRepoAtom))
		for _, specific := range []string{"zeropsio", "mate", "vite", "pnpm", "runtimebases:"} {
			if strings.Contains(atom, specific) {
				t.Errorf("%s names %q: it must hold for any repo and stack", existingRepoAtom, specific)
			}
		}
	})
}

// TestDevelopExistingRepo_OutsideTheFirstDeployContainerPathNothingChanges pins
// that the existing-repo guidance stays out of every golden scenario that is
// not a never-deployed dynamic dev service developed in a container.
func TestDevelopExistingRepo_OutsideTheFirstDeployContainerPathNothingChanges(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	for _, sc := range canonicalGoldenScenarios() {
		got, err := Synthesize(sc.envelope, corpus)
		if err != nil {
			continue // a scenario Synthesize refuses is the golden test's to report
		}
		fires := slices.ContainsFunc(got, func(m MatchedRender) bool { return m.AtomID == existingRepoAtom })
		want := sc.envelope.Phase == PhaseDevelopActive && sc.envelope.Environment == EnvContainer &&
			slices.ContainsFunc(sc.envelope.Services, func(s ServiceSnapshot) bool {
				return !s.Deployed && s.Bootstrapped && s.RuntimeClass == topology.RuntimeDynamic &&
					(s.Mode == topology.ModeDev || s.Mode == topology.ModeStandard)
			})
		if fires && !want {
			t.Errorf("%s fired in %s, outside the first-deploy container path", existingRepoAtom, sc.id)
		}
	}
}

func readAtomFile(t *testing.T, id string) string {
	t.Helper()
	b, err := os.ReadFile("../content/atoms/" + id + ".md")
	if err != nil {
		t.Fatalf("read atom %s: %v", id, err)
	}
	return string(b)
}
