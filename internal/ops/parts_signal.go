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
	// setup it extends, names a run.start or run.startCommands. A part
	// without one (a static output) runs no dev server.
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
	sig.YAMLParts, sig.YAMLSetups, sig.RunParts, sig.DeclaresBase = yamlSetups(yamlDoc, hostname, devSetup)
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

// yamlSetups reads zerops.yaml once, with extends resolved as the platform
// resolves it (decodeResolved): its parts (docs/spec-mate.md §2.8a.4) — setups
// other than the dev setup and other than a base only `extends` names —, every
// setup, the parts that run a process (a run.start or run.startCommands, their
// own or inherited) and whether a setup names a build.base or a run.base.
func yamlSetups(data []byte, hostname, devSetup string) (parts, setups, run []string, declaresBase bool) {
	var doc ZeropsYmlDoc
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, nil, nil, false
	}
	extended := map[string]bool{}
	for _, e := range doc.Zerops {
		for _, base := range extendsNames(e.Extends) {
			extended[base] = true
		}
	}
	if doc.extends() && decodeResolved(data, &doc) != nil {
		return nil, nil, nil, false
	}
	isDev := func(setup string) bool {
		if devSetup != "" {
			return setup == devSetup
		}
		return setup == devSetupName || setup == hostname
	}
	for _, e := range doc.Zerops {
		if e.Build.Base != nil || e.Run.Base != "" {
			declaresBase = true
		}
		if e.Setup == "" {
			continue
		}
		setups = append(setups, e.Setup)
		if isDev(e.Setup) || extended[e.Setup] {
			continue
		}
		parts = append(parts, e.Setup)
		if strings.TrimSpace(e.Run.Start) != "" || e.Run.StartCommands != nil {
			run = append(run, e.Setup)
		}
	}
	return parts, setups, run, declaresBase
}
