package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// giteaRuntimeSetup is the step that puts a service's language on the group's
// runner before the project's tests. The runner is a bare Ubuntu running jobs
// in host mode: git, zcli and the Node that JavaScript actions need, and
// nothing a project's test command calls — not even npm (run 5: a Mate filled
// the Test step in with `npm test`, and both deploys failed with exit 127).
type giteaRuntimeSetup struct {
	label  string      // the language, in the step's name
	action string      // fetched from github.com, where the runner finds actions/checkout too
	with   [][2]string // the action's inputs, in order
	test   string      // what a project of this language usually runs, for the Test step's comment
}

// giteaRuntimeSetups lists the runtimes whose setup action works on the runner
// as it stands: each downloads a .tar.gz build that runs on any x64 Linux, and
// the runner has tar. A runtime that is not here gets no setup step — see
// giteaRuntimeAdvice for what its Test step says instead:
//   - setup-bun and setup-deno download a .zip and extract it with `unzip`,
//     which the runner image does not install (it ships git, ca-certificates,
//     curl and nodejs, with no recommends); setup-bun also resolves a version
//     range through api.github.com with its token input. Adding unzip to the
//     runner image and one live run would bring them back.
//   - setup-python's builds are compiled for the hosted runners'
//     /opt/hostedtoolcache, which this runner does not have.
//   - setup-php installs through apt, and only on the Ubuntu releases it lists.
var giteaRuntimeSetups = map[string]func(version string) giteaRuntimeSetup{
	"nodejs": func(v string) giteaRuntimeSetup {
		return giteaRuntimeSetup{"Node.js", "actions/setup-node@v4", [][2]string{{"node-version", quoteYAML(v)}}, "npm ci && npm test"}
	},
	// setup-go's module cache is on by default and fails the step outright
	// in a module with no go.sum — a project with no dependencies.
	"go": func(v string) giteaRuntimeSetup {
		return giteaRuntimeSetup{"Go", "actions/setup-go@v5", [][2]string{{"go-version", quoteYAML(versionRange(v))}, {"cache", "false"}}, "go test ./..."}
	},
	"java": func(v string) giteaRuntimeSetup {
		return giteaRuntimeSetup{"Java", "actions/setup-java@v4", [][2]string{{"distribution", "temurin"}, {"java-version", quoteYAML(v)}}, "./mvnw -B test"}
	},
}

// giteaRuntimeAdvice is what the Test step says for a runtime with no setup
// action that works on the runner: plainly that none does, and what does work
// there. Jobs run on the runner's own Ubuntu with sudo, so the distribution's
// packages install in a step above Test — at the distribution's version, which
// is not the service's. Bun and Deno have no Ubuntu package; their own
// installers need unzip, which the distribution has.
var giteaRuntimeAdvice = map[string]giteaNoSetupAdvice{
	"python": {"Python", giteaFromTheDistribution, []string{
		"sudo apt-get update && sudo apt-get install -y python3 python3-pip python3-venv",
	}},
	"php-nginx":  giteaPHPAdvice,
	"php-apache": giteaPHPAdvice,
	"bun": {"Bun", "install unzip from the distribution, then Bun's own installer, in steps above the Test step — the newest Bun, not necessarily the service's version", []string{
		"sudo apt-get update && sudo apt-get install -y unzip",
		`curl -fsSL https://bun.sh/install | bash && echo "$HOME/.bun/bin" >> "$GITHUB_PATH"`,
	}},
	"deno": {"Deno", "install unzip from the distribution, then Deno's own installer, in steps above the Test step — the newest Deno, not necessarily the service's version", []string{
		"sudo apt-get update && sudo apt-get install -y unzip",
		`curl -fsSL https://deno.land/install.sh | sh -s -- -y && echo "$HOME/.deno/bin" >> "$GITHUB_PATH"`,
	}},
}

const giteaFromTheDistribution = "install the distribution's packages in a step above the Test step — the distribution's version, not the service's"

var giteaPHPAdvice = giteaNoSetupAdvice{"PHP", giteaFromTheDistribution, []string{
	"sudo apt-get update && sudo apt-get install -y php-cli composer",
}}

// giteaNoSetupAdvice names a language, how it gets onto the runner, and the
// commands that put it there, each a `run:` step of its own above Test.
type giteaNoSetupAdvice struct {
	label    string
	how      string
	commands []string
}

// sentence is the advice in prose: the confirm's note and the Test step's
// comment say the same thing.
func (a giteaNoSetupAdvice) sentence() string {
	return fmt.Sprintf("The runner has no language runtime installed, and no %s setup action works on it. Jobs run on the runner's own Ubuntu with sudo, so %s", a.label, a.how)
}

// zeropsTypeVersion is the version a runtime's type names: a major, or a major
// with a minor and a patch.
var zeropsTypeVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

// giteaRuntimeSetupFor is the setup step for a service of serviceType
// (`nodejs@22`, `ubuntu/nodejs@24`), pinned to its version; false for a type
// zcp has none for, or one that names no plain version.
func giteaRuntimeSetupFor(serviceType string) (giteaRuntimeSetup, bool) {
	build, ok := giteaRuntimeSetups[topology.TypeFamily(serviceType)]
	if !ok {
		return giteaRuntimeSetup{}, false
	}
	_, version, _ := strings.Cut(serviceType, "@")
	if !zeropsTypeVersion.MatchString(version) {
		return giteaRuntimeSetup{}, false
	}
	return build(version), true
}

// versionRange widens a version naming fewer than three parts to the semver
// range it means on Zerops — `go@1` is the newest Go 1, `bun@1.2` the newest
// 1.2 — so the setup action resolves the newest release inside it rather than
// reading the bare number as one exact release.
func versionRange(version string) string {
	if strings.Count(version, ".") < 2 {
		return version + ".x"
	}
	return version
}

func quoteYAML(s string) string { return `"` + s + `"` }

// giteaWorkflowSetupAndTestSteps is the part of the workflow between checkout
// and the deploy: the runtime's setup where zcp knows it, and the Test step the
// project fills in. The Test step's default runs nothing, so a project that has
// not filled it in still deploys.
func giteaWorkflowSetupAndTestSteps(serviceType string) string {
	const noTest = `        run: echo "no test command configured"` + "\n"
	setup, ok := giteaRuntimeSetupFor(serviceType)
	if !ok {
		head := `      - name: Test
        # Replace with this project's own test command; the deploy step
        # below runs only if this one passes.
`
		if advice, known := giteaRuntimeAdvice[topology.TypeFamily(serviceType)]; known {
			var b strings.Builder
			b.WriteString(head)
			for _, line := range wrapComment(advice.sentence()+":", 70) {
				fmt.Fprintf(&b, "        # %s\n", line)
			}
			for _, command := range advice.commands {
				fmt.Fprintf(&b, "        #   - run: %s\n", command)
			}
			b.WriteString(noTest)
			return b.String()
		}
		return head + `        # The runner has no language runtime installed: add the setup step
        # for the project's language above this one first, for example
        #   - uses: actions/setup-node@v4
        #     with:
        #       node-version: "22"
` + noTest
	}
	var b strings.Builder
	fmt.Fprintf(&b, "      - name: Set up %s\n", setup.label)
	b.WriteString("        # The runner has no language runtime installed; this puts the\n")
	fmt.Fprintf(&b, "        # service's own (%s) on it for the steps below.\n", serviceType)
	fmt.Fprintf(&b, "        uses: %s\n        with:\n", setup.action)
	for _, input := range setup.with {
		fmt.Fprintf(&b, "          %s: %s\n", input[0], input[1])
	}
	b.WriteString("      - name: Test\n")
	b.WriteString("        # Replace with this project's own test command, for example\n")
	fmt.Fprintf(&b, "        # `%s`; the deploy step below runs only if this one passes.\n", setup.test)
	b.WriteString(noTest)
	return b.String()
}

// giteaServiceType is the Zerops type of a pair's dev half (`nodejs@22`), which
// says what the workflow sets up; "" when it cannot be read, and the workflow
// then carries the plain comment. It reads the direct service list, not the
// search index that trails an import by seconds — a stand-up wires its pair
// moments after importing it — and only when a workflow is about to be written,
// never on an idle pass or for a file that is current.
func giteaServiceType(ctx context.Context, client platform.Client, projectID, hostname string) string {
	if client == nil || projectID == "" {
		return ""
	}
	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		return ""
	}
	svc, err := ops.FindService(services, hostname)
	if err != nil || svc == nil {
		return ""
	}
	return svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
}

// giteaTestsNote is the build-integration confirm's word on the Test step; it
// says what the file's Test step comment says.
func giteaTestsNote(serviceType string) string {
	const fill = "Put the project's test command in the marked step before the deploy step. ZCP does not know it — read the project's own manifest rather than guessing one."
	if setup, ok := giteaRuntimeSetupFor(serviceType); ok {
		return fmt.Sprintf("%s The runner has no language runtime of its own; the file sets this service's (%s) up before the Test step with %s, so the step can call its tools.", fill, serviceType, setup.action)
	}
	if advice, known := giteaRuntimeAdvice[topology.TypeFamily(serviceType)]; known {
		return fmt.Sprintf("%s %s: `%s`.", fill, advice.sentence(), strings.Join(advice.commands, "`, then `"))
	}
	return fill + " The runner has no language runtime installed: add the setup step for the project's language above the Test step, as its comment shows, before calling that language's tools."
}

// wrapComment breaks prose into lines of at most width characters, at spaces.
func wrapComment(text string, width int) []string {
	var lines []string
	line := ""
	for word := range strings.FieldsSeq(text) {
		if line != "" && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
