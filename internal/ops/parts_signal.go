package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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
	// DeclaredServers counts the servers in .zcp/dev-servers.json.
	DeclaredServers int
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
	return len(s.YAMLParts) > 1 || s.DeclaredServers > 1 || (s.Workspace && s.Packages > 1)
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
	sig.YAMLParts = yamlParts(yamlDoc, hostname, devSetup)
	sig.DeclaresBase = declaresBase(yamlDoc)
	var decl struct {
		Servers []json.RawMessage `json:"servers"`
	}
	if json.Unmarshal([]byte(strings.Join(sections["declaration"], "\n")), &decl) == nil {
		sig.DeclaredServers = len(decl.Servers)
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
// other than the dev setup and other than a base only `extends` names.
func yamlParts(data []byte, hostname, devSetup string) []string {
	var doc struct {
		Zerops []struct {
			Setup   string    `yaml:"setup"`
			Extends yaml.Node `yaml:"extends"`
		} `yaml:"zerops"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil
	}
	extended := map[string]bool{}
	for _, e := range doc.Zerops {
		if e.Extends.Kind == yaml.ScalarNode {
			extended[e.Extends.Value] = true
		}
		if e.Extends.Kind == yaml.SequenceNode {
			for _, n := range e.Extends.Content {
				extended[n.Value] = true
			}
		}
	}
	isDev := func(setup string) bool {
		if devSetup != "" {
			return setup == devSetup
		}
		return setup == "dev" || setup == hostname
	}
	var parts []string
	for _, e := range doc.Zerops {
		if e.Setup == "" || isDev(e.Setup) || extended[e.Setup] {
			continue
		}
		parts = append(parts, e.Setup)
	}
	return parts
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
