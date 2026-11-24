// Command release-on-green names zcp's next release for a commit whose main CI is green, unless
// the commits since the last release change what zcp shows agents: those wait for a live check of
// a Mate and of a standalone zcp, and their release stays a person's `make release`. It only
// decides and never tags: the maintainers' release tooling runs it on the green commit, in a clone
// holding the history and the tags, and cuts the release with `make release` once Zerops starts
// containers (no zcp release while it cannot: each Mate's `zcp self restart` would not come back):
//
//	go run ./cmd/release-on-green <sha>
//
// stdout is the tag `make release` cuts (a minor bump) or nothing; the reason goes to stderr.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// agentFacing is what zcp shows agents, by path: AGENTS.md, its blocks, the atoms and the guided
// skill (internal/content); knowledge, recipes and playbooks (internal/knowledge); tool
// descriptions and results (internal/tools); workflow guidance, envelopes and the route menu
// (internal/workflow); the MCP server's instructions; the skills zcp installs
// (internal/skillpacks); and the agent context, permissions and config `zcp init` writes
// (internal/init). A change under any of them is released by a person after the live check.
var agentFacing = []string{
	"internal/content/",
	"internal/knowledge/",
	"internal/tools/",
	"internal/workflow/",
	"internal/server/instructions.go",
	"internal/skillpacks/",
	"internal/init/",
}

// showsAgents returns the changed paths that lie under what zcp shows agents; tests and their
// testdata ship to no agent.
func showsAgents(changed []string) []string {
	var held []string
	for _, path := range changed {
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/testdata/") {
			continue
		}
		for _, prefix := range agentFacing {
			if path == prefix || (strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, prefix)) {
				held = append(held, path)
				break
			}
		}
	}
	return held
}

// nextTag is the minor bump of a vMAJOR.MINOR.PATCH tag.
func nextTag(latest string) (string, error) {
	parts := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("tag %s is not vMAJOR.MINOR.PATCH", latest)
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return "", fmt.Errorf("tag %s is not vMAJOR.MINOR.PATCH", latest)
	}
	return fmt.Sprintf("v%d.%d.0", major, minor+1), nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: release-on-green <sha>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "release-on-green:", err)
		os.Exit(1)
	}
}

func run(sha string, stdout, stderr io.Writer) error {
	tags, err := git("tag", "-l", "v*", "--sort=-v:refname")
	if err != nil {
		return err
	}
	latest, _, _ := strings.Cut(tags, "\n")
	if latest == "" {
		return fmt.Errorf("no release tag found")
	}
	if _, err := git("merge-base", "--is-ancestor", sha, latest); err == nil {
		fmt.Fprintf(stderr, "%s is already released in %s\n", sha, latest)
		return nil
	}
	if _, err := git("merge-base", "--is-ancestor", latest, sha); err != nil {
		return fmt.Errorf("%s does not descend from the latest release %s; release by hand", sha, latest)
	}
	diff, err := git("diff", "--name-only", latest, sha)
	if err != nil {
		return err
	}
	if held := showsAgents(strings.Fields(diff)); len(held) > 0 {
		fmt.Fprintf(stderr, "%s..%s changes what zcp shows agents; after the live check of a Mate and a standalone zcp, `make release` cuts it:\n  %s\n",
			latest, sha, strings.Join(held, "\n  "))
		return nil
	}
	next, err := nextTag(latest)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "%s..%s is green and shows agents nothing new: releasing %s\n", latest, sha, next)
	fmt.Fprintln(stdout, next)
	return nil
}

func git(args ...string) (string, error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
