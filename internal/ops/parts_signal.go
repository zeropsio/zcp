package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// partsSignalScript prints, from a dev service's repo root, what the
// multi-part signal reads: the root config file's name and content (zerops.yaml,
// else zerops.yml), the dev-server declaration,
// the workspace markers present, and the packages with a start or dev script
// one or two folders down (outside node_modules) — one SSH round trip. Each
// file is followed by a newline so a file without one cannot swallow the
// next section's marker.
const partsSignalScript = `cd ` + defaultWorkingDir + ` 2>/dev/null || exit 0
echo @@file
for f in zerops.yaml zerops.yml; do if [ -f "$f" ]; then echo "$f"; break; fi; done
echo @@yaml
for f in zerops.yaml zerops.yml; do if [ -f "$f" ]; then cat "$f"; printf '\n'; break; fi; done
echo @@declaration
cat .zcp/dev-servers.json 2>/dev/null; printf '\n'
echo @@markers
for f in pnpm-workspace.yaml turbo.json nx.json lerna.json go.work; do [ -f "$f" ] && echo "$f"; done
grep -qs '"workspaces"' package.json && echo package.json
echo @@packages
for p in */package.json */*/package.json; do case "$p" in node_modules/*|*/node_modules/*) continue;; esac; grep -qsE '"(start|dev)"[[:space:]]*:' "$p" && echo "$p"; done
for p in */go.mod */*/go.mod; do [ -f "$p" ] && echo "$p"; done
true`

// PartsSignal is what a dev service's repo says about its deployable parts.
type PartsSignal struct {
	// ConfigFile is the name of the repo's root config file — "zerops.yaml",
	// else "zerops.yml" — or "" when it has none.
	ConfigFile string
	// YAMLParts names the root zerops.yaml's setups other than the dev
	// setup and other than a base only `extends` names, in file order.
	YAMLParts []string
	// YAMLSetups names every setup of the root zerops.yaml, in file order.
	YAMLSetups []string
	// RunParts names the YAMLParts that run a process: their setup, or a
	// setup it extends, names a run.start. A part without one (a static
	// output) runs no dev server.
	RunParts []string
	// DeclaredServers names the servers in .zcp/dev-servers.json, in file
	// order.
	DeclaredServers []string
	// Workspace is true when a workspace marker (pnpm-workspace.yaml,
	// turbo.json, nx.json, lerna.json, go.work, package.json workspaces) is
	// present; Packages counts the packages with a start/dev script or a
	// go.mod one or two folders down.
	Workspace bool
	Packages  int
	// DeclaresBase is true when a setup of the root zerops.yaml names a
	// build.base or a run.base.
	DeclaresBase bool
}

// MultiPart is the mechanical multi-part signal: the repo's zerops.yaml
// declares more than one part, its declaration lists more than one dev
// server, or workspace markers name several deployable packages.
func (s PartsSignal) MultiPart() bool {
	return len(s.YAMLParts) > 1 || len(s.DeclaredServers) > 1 || (s.Workspace && s.Packages > 1)
}

// PartsWithoutDevServer names, in stage order, the parts a pair's stages
// build that run a process and have no dev server declared on the dev service
// devHostname — nil for a pair with one stage. A part's dev server is declared
// under the part's name; the pair's first part may also be served by the one
// named after the dev hostname, which a start without a name declares
// (docs/spec-mate.md §2.8a.1). A server named after no part may serve any
// part, so while those are as many as the unnamed parts, none is named.
func (s PartsSignal) PartsWithoutDevServer(stages []topology.StageRef, devHostname string) []string {
	if len(stages) < 2 {
		return nil
	}
	declared := map[string]bool{}
	for _, name := range s.DeclaredServers {
		declared[name] = true
	}
	parts := map[string]bool{}
	for _, st := range stages {
		parts[st.SetupName] = true
	}
	var missing []string
	for i, st := range stages {
		if !slices.Contains(s.RunParts, st.SetupName) || declared[st.SetupName] || slices.Contains(missing, st.SetupName) {
			continue
		}
		if i == 0 && declared[devHostname] {
			continue
		}
		missing = append(missing, st.SetupName)
	}
	unattributed := 0
	for name := range declared {
		if !parts[name] && name != devHostname {
			unattributed++
		}
	}
	if len(missing) <= unattributed {
		return nil
	}
	return missing
}

// ReadPartsSignal reads hostname's repo for the multi-part signal; devSetup
// is the dev service's own setup ("" when unknown: then a setup named "dev"
// or after the hostname is taken as it).
func ReadPartsSignal(ctx context.Context, ssh SSHDeployer, hostname, devSetup string) (PartsSignal, error) {
	out, err := ssh.ExecSSH(ctx, hostname, partsSignalScript)
	if err != nil {
		return PartsSignal{}, fmt.Errorf("read parts of %s: %w", hostname, err)
	}
	return parsePartsSignal(string(out), hostname, devSetup), nil
}

func parsePartsSignal(out, hostname, devSetup string) PartsSignal {
	sections := map[string][]string{}
	current := ""
	for line := range strings.SplitSeq(out, "\n") {
		if name, ok := strings.CutPrefix(line, "@@"); ok && !strings.Contains(name, " ") {
			current = name
			continue
		}
		sections[current] = append(sections[current], line)
	}
	var sig PartsSignal
	for _, l := range sections["file"] {
		if name := strings.TrimSpace(l); name != "" {
			sig.ConfigFile = name
		}
	}
	yamlDoc := []byte(strings.Join(sections["yaml"], "\n"))
	sig.YAMLParts, sig.YAMLSetups = yamlParts(yamlDoc, hostname, devSetup)
	sig.RunParts = runParts(yamlDoc, sig.YAMLParts)
	sig.DeclaresBase = declaresBase(yamlDoc)
	var decl struct {
		Servers []struct {
			Name string `json:"name"`
		} `json:"servers"`
	}
	if json.Unmarshal([]byte(strings.Join(sections["declaration"], "\n")), &decl) == nil {
		for _, s := range decl.Servers {
			sig.DeclaredServers = append(sig.DeclaredServers, s.Name)
		}
	}
	for _, l := range sections["markers"] {
		if strings.TrimSpace(l) != "" {
			sig.Workspace = true
		}
	}
	for _, l := range sections["packages"] {
		if strings.TrimSpace(l) != "" {
			sig.Packages++
		}
	}
	return sig
}

// yamlParts names zerops.yaml's parts (docs/spec-mate.md §2.8a.4): setups
// other than the dev setup and other than a base only `extends` names; setups
// names every setup.
func yamlParts(data []byte, hostname, devSetup string) (parts, setups []string) {
	var doc struct {
		Zerops []struct {
			Setup   string    `yaml:"setup"`
			Extends yaml.Node `yaml:"extends"`
		} `yaml:"zerops"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, nil
	}
	extended := map[string]bool{}
	for _, e := range doc.Zerops {
		for _, base := range extendedSetups(e.Extends) {
			extended[base] = true
		}
	}
	isDev := func(setup string) bool {
		if devSetup != "" {
			return setup == devSetup
		}
		return setup == devSetupName || setup == hostname
	}
	for _, e := range doc.Zerops {
		if e.Setup == "" {
			continue
		}
		setups = append(setups, e.Setup)
		if isDev(e.Setup) || extended[e.Setup] {
			continue
		}
		parts = append(parts, e.Setup)
	}
	return parts, setups
}

// extendedSetups names the setups an `extends` value names: a scalar or a
// list.
func extendedSetups(n yaml.Node) []string {
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	var out []string
	if n.Kind == yaml.SequenceNode {
		for _, c := range n.Content {
			out = append(out, c.Value)
		}
	}
	return out
}

// runParts names the parts whose setup, or a setup it extends (transitively),
// names a run.start.
func runParts(data []byte, parts []string) []string {
	var doc struct {
		Zerops []struct {
			Setup   string    `yaml:"setup"`
			Extends yaml.Node `yaml:"extends"`
			Run     struct {
				Start string `yaml:"start"`
			} `yaml:"run"`
		} `yaml:"zerops"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil
	}
	starts := map[string]bool{}
	extends := map[string][]string{}
	for _, e := range doc.Zerops {
		starts[e.Setup] = strings.TrimSpace(e.Run.Start) != ""
		extends[e.Setup] = extendedSetups(e.Extends)
	}
	var runs func(setup string, seen map[string]bool) bool
	runs = func(setup string, seen map[string]bool) bool {
		if starts[setup] {
			return true
		}
		seen[setup] = true
		for _, base := range extends[setup] {
			if !seen[base] && runs(base, seen) {
				return true
			}
		}
		return false
	}
	var out []string
	for _, p := range parts {
		if runs(p, map[string]bool{}) {
			out = append(out, p)
		}
	}
	return out
}

// declaresBase reports whether a setup of zerops.yaml names a build.base or
// a run.base (a scalar or a list).
func declaresBase(data []byte) bool {
	var doc struct {
		Zerops []struct {
			Build struct {
				Base yaml.Node `yaml:"base"`
			} `yaml:"build"`
			Run struct {
				Base yaml.Node `yaml:"base"`
			} `yaml:"run"`
		} `yaml:"zerops"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return false
	}
	for _, e := range doc.Zerops {
		if !e.Build.Base.IsZero() || !e.Run.Base.IsZero() {
			return true
		}
	}
	return false
}
